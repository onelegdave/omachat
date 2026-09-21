// Package signal adapts signal-cli's JSON-RPC stream to OmaChat's common wire model.
package signal

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

const (
	directPrefix  = "signal:user:"
	groupPrefix   = "signal:group:"
	mediaPrefix   = "signal-media:"
	maxMediaBytes = int64(100 << 20)
)

type Backend struct {
	log     zerolog.Logger
	paths   *appStore.Paths
	publish func(wire.Event)

	mu             sync.RWMutex
	status         wire.Status
	account        string
	convs          map[string]wire.Conversation
	order          []string
	messages       map[string][]wire.Message
	reactionActors map[string]map[string]string
	expirations    map[string]int64
	expiryTimers   map[string]*time.Timer
	client         Caller
	cancel         context.CancelFunc
}

func New(log zerolog.Logger, paths *appStore.Paths, publish func(wire.Event)) *Backend {
	data := emptyStoredData()
	if paths != nil {
		data = loadStoredData(paths.SignalStoreFile())
	}
	b := &Backend{
		log: log.With().Str("network", wire.NetworkSignal).Logger(), paths: paths, publish: publish,
		status:  wire.Status{Network: wire.NetworkSignal, State: wire.StateUnpaired, PhoneOK: true},
		account: data.Account, convs: data.Conversations, order: data.Order, messages: data.Messages,
		reactionActors: data.ReactionActors, expirations: data.Expirations, expiryTimers: make(map[string]*time.Timer),
		client: NewRPCClient(),
	}
	b.mu.Lock()
	b.pruneExpiredLocked(time.Now().UnixMilli())
	b.recountUnreadLocked()
	b.scheduleExpirationsLocked()
	_ = b.saveLocked()
	b.mu.Unlock()
	return b
}

func (b *Backend) SetClient(c Caller)                           { b.mu.Lock(); b.client = c; b.mu.Unlock() }
func (b *Backend) Status() wire.Status                          { b.mu.RLock(); defer b.mu.RUnlock(); return b.status }
func (b *Backend) SetState(state wire.ConnState, detail string) { b.setState(state, detail) }

func (b *Backend) setState(state wire.ConnState, detail string) {
	b.mu.Lock()
	b.status.State, b.status.Error = state, detail
	if state != wire.StatePairing {
		b.status.QRURL = ""
	}
	status := b.status
	b.mu.Unlock()
	if b.publish != nil {
		b.publish(wire.Event{Event: wire.EventStatus, Network: wire.NetworkSignal, Data: status})
	}
}

func (b *Backend) Start(parent context.Context) error {
	b.mu.Lock()
	ctx, cancel := context.WithCancel(parent)
	b.cancel = cancel
	client := b.client
	b.mu.Unlock()
	if b.paths == nil {
		return errors.New("Signal storage paths are unavailable")
	}
	if err := client.Start(ctx, b.paths.SignalDataDir(), b.handleNotification, func(err error) {
		if ctx.Err() == nil {
			b.setState(wire.StateDisconnected, "signal-cli stopped: "+err.Error())
		}
	}); err != nil {
		b.setState(wire.StateUnpaired, err.Error())
		return nil
	}
	var accounts []struct {
		Number string `json:"number"`
	}
	callCtx, callCancel := context.WithTimeout(ctx, 15*time.Second)
	err := client.Call(callCtx, "listAccounts", nil, &accounts)
	callCancel()
	if err != nil {
		b.setState(wire.StateDisconnected, err.Error())
		return nil
	}
	if len(accounts) == 0 {
		b.setState(wire.StateUnpaired, "")
		return nil
	}
	b.mu.Lock()
	b.account = accounts[0].Number
	b.status.SelfPhone = b.account
	b.mu.Unlock()
	b.setState(wire.StateConnected, "")
	return b.Refresh(ctx)
}

func (b *Backend) Stop() {
	b.mu.Lock()
	if b.cancel != nil {
		b.cancel()
	}
	for id, timer := range b.expiryTimers {
		timer.Stop()
		delete(b.expiryTimers, id)
	}
	client := b.client
	b.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
}

func (b *Backend) StartPairing(ctx context.Context) (string, error) {
	b.mu.RLock()
	client := b.client
	b.mu.RUnlock()
	var started struct {
		DeviceLinkURI string `json:"deviceLinkUri"`
	}
	if err := client.Call(ctx, "startLink", nil, &started); err != nil {
		return "", err
	}
	if started.DeviceLinkURI == "" {
		return "", errors.New("signal-cli returned an empty device link")
	}
	b.mu.Lock()
	b.status.State, b.status.Error, b.status.QRURL = wire.StatePairing, "", started.DeviceLinkURI
	status := b.status
	b.mu.Unlock()
	if b.publish != nil {
		b.publish(wire.Event{Event: wire.EventStatus, Network: wire.NetworkSignal, Data: status})
		b.publish(wire.Event{Event: wire.EventQR, Network: wire.NetworkSignal, Data: map[string]string{"url": started.DeviceLinkURI}})
	}
	go b.finishPairing(started.DeviceLinkURI)
	return started.DeviceLinkURI, nil
}

func (b *Backend) finishPairing(uri string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	b.mu.RLock()
	client := b.client
	b.mu.RUnlock()
	var result struct {
		Number string `json:"number"`
	}
	err := client.Call(ctx, "finishLink", map[string]any{"deviceLinkUri": uri, "deviceName": "OmaChat"}, &result)
	if err != nil {
		b.setState(wire.StateUnpaired, err.Error())
		return
	}
	b.mu.Lock()
	b.account, b.status.SelfPhone = result.Number, result.Number
	_ = b.saveLocked()
	b.mu.Unlock()
	b.setState(wire.StateConnected, "")
	_ = client.Call(ctx, "sendSyncRequest", map[string]any{"account": result.Number}, nil)
	if err := b.Refresh(ctx); err != nil {
		b.log.Warn().Err(err).Msg("Could not refresh Signal contacts after pairing")
	}
	if b.publish != nil {
		b.publish(wire.Event{Event: wire.EventPaired, Network: wire.NetworkSignal})
	}
}

func (b *Backend) Refresh(ctx context.Context) error {
	b.mu.RLock()
	account, client := b.account, b.client
	b.mu.RUnlock()
	if account == "" {
		return errors.New("Signal is not linked")
	}
	var contacts []struct {
		Number, UUID, Username, Name, GivenName, FamilyName string
		Unregistered                                        bool `json:"unregistered"`
	}
	if err := client.Call(ctx, "listContacts", map[string]any{"account": account, "allRecipients": false}, &contacts); err != nil {
		return err
	}
	var groups []struct {
		ID, Name string
		IsMember bool `json:"isMember"`
	}
	if err := client.Call(ctx, "listGroups", map[string]any{"account": account}, &groups); err != nil {
		return err
	}
	b.mu.Lock()
	for _, c := range contacts {
		if c.Unregistered {
			continue
		}
		peer := first(c.UUID, c.Number, c.Username)
		if peer == "" {
			continue
		}
		id := directPrefix + peer
		if _, exists := b.convs[id]; exists {
			continue
		}
		name := first(c.Name, strings.TrimSpace(c.GivenName+" "+c.FamilyName), c.Username, c.Number, c.UUID)
		b.convs[id] = newConversation(id, name, false)
		b.order = append(b.order, id)
	}
	for _, g := range groups {
		if !g.IsMember || g.ID == "" {
			continue
		}
		id := groupPrefix + g.ID
		if _, exists := b.convs[id]; exists {
			continue
		}
		b.convs[id] = newConversation(id, first(g.Name, "Signal group"), true)
		b.order = append(b.order, id)
	}
	_ = b.saveLocked()
	b.recountUnreadLocked()
	b.mu.Unlock()
	if b.publish != nil {
		for _, conversation := range b.Conversations(0) {
			b.publish(wire.Event{Event: wire.EventConversation, Network: wire.NetworkSignal, Data: conversation})
		}
		b.publish(wire.Event{Event: wire.EventStatus, Network: wire.NetworkSignal, Data: b.Status()})
	}
	return nil
}

func (b *Backend) Conversations(count int) []wire.Conversation {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]wire.Conversation, 0, len(b.order))
	for _, id := range b.order {
		if c, ok := b.convs[id]; ok {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	if count > 0 && len(out) > count {
		out = out[:count]
	}
	return out
}

func (b *Backend) Messages(_ context.Context, p wire.MessagesParams) (wire.MessagesResult, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	items := append([]wire.Message(nil), b.messages[p.ConversationID]...)
	if p.Count > 0 && int64(len(items)) > p.Count {
		items = items[len(items)-int(p.Count):]
	}
	return wire.MessagesResult{ConversationID: p.ConversationID, Messages: items, HasMore: false, HistoryNotice: "Signal history is stored locally from the time this device is linked."}, nil
}

func (b *Backend) Send(ctx context.Context, p wire.SendParams) (*wire.Message, error) {
	if strings.TrimSpace(p.Text) == "" {
		return nil, errors.New("empty message")
	}
	b.mu.RLock()
	account, client := b.account, b.client
	b.mu.RUnlock()
	params := map[string]any{"account": account, "message": p.Text}
	switch {
	case strings.HasPrefix(p.ConversationID, directPrefix):
		params["recipient"] = []string{strings.TrimPrefix(p.ConversationID, directPrefix)}
	case strings.HasPrefix(p.ConversationID, groupPrefix):
		params["groupId"] = strings.TrimPrefix(p.ConversationID, groupPrefix)
	default:
		return nil, errors.New("invalid Signal conversation")
	}
	var result struct {
		Timestamp int64 `json:"timestamp"`
	}
	if err := client.Call(ctx, "send", params, &result); err != nil {
		return nil, err
	}
	if result.Timestamp == 0 {
		result.Timestamp = time.Now().UnixMilli()
	}
	b.ensureConversation(p.ConversationID, fallbackConversationName(p.ConversationID, account), strings.HasPrefix(p.ConversationID, groupPrefix))
	msg := wire.Message{ID: messageID(result.Timestamp, account), TmpID: p.TmpID, ConversationID: p.ConversationID, Text: p.Text, Timestamp: result.Timestamp * 1000, FromMe: true, SenderID: account, Delivery: wire.DeliverySent}
	b.ingest(msg, account)
	return &msg, nil
}

func (b *Backend) SendMedia(ctx context.Context, p wire.SendMediaParams) (*wire.SendMediaResult, error) {
	path := filepath.Clean(p.Path)
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat Signal attachment: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("Signal attachment is not a regular file")
	}
	if info.Size() > maxMediaBytes {
		return nil, fmt.Errorf("Signal attachment exceeds %d MiB limit", maxMediaBytes>>20)
	}
	b.mu.RLock()
	account, client := b.account, b.client
	b.mu.RUnlock()
	params := map[string]any{"account": account, "attachments": []string{path}}
	if strings.TrimSpace(p.Caption) != "" {
		params["message"] = p.Caption
	}
	if err := addConversationTarget(params, p.ConversationID); err != nil {
		return nil, err
	}
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if strings.HasPrefix(mimeType, "audio/") {
		params["voiceNote"] = true
	}
	var result struct {
		Timestamp int64 `json:"timestamp"`
	}
	if err := client.Call(ctx, "send", params, &result); err != nil {
		return nil, err
	}
	if result.Timestamp == 0 {
		result.Timestamp = time.Now().UnixMilli()
	}
	b.ensureConversation(p.ConversationID, fallbackConversationName(p.ConversationID, account), strings.HasPrefix(p.ConversationID, groupPrefix))
	attachment := attachmentFromMetadata(signalAttachment{ID: fmt.Sprintf("out-%d", result.Timestamp), Filename: filepath.Base(path), ContentType: mimeType, Size: info.Size()})
	attachment.Path = path
	msg := wire.Message{ID: messageID(result.Timestamp, account), TmpID: p.TmpID, ConversationID: p.ConversationID, Text: p.Caption, Timestamp: result.Timestamp * 1000, FromMe: true, SenderID: account, Delivery: wire.DeliverySent, Attachments: []wire.Attachment{attachment}}
	b.ingest(msg, account)
	return &wire.SendMediaResult{Message: &msg}, nil
}

func (b *Backend) Media(ctx context.Context, p wire.MediaParams) (*wire.MediaResult, error) {
	key := first(p.Key, p.MediaID)
	if !strings.HasPrefix(key, mediaPrefix) {
		return nil, errors.New("invalid Signal attachment key")
	}
	b.mu.RLock()
	account, client := b.account, b.client
	attachment, conversationID, ok := b.findAttachmentLocked(key)
	b.mu.RUnlock()
	if !ok {
		return nil, errors.New("Signal attachment was not found")
	}
	if attachment.Path != "" {
		if _, err := os.Stat(attachment.Path); err == nil {
			return &wire.MediaResult{Key: key, Path: attachment.Path}, nil
		}
	}
	params := map[string]any{"account": account, "id": strings.TrimPrefix(key, mediaPrefix)}
	if err := addConversationTarget(params, conversationID); err != nil {
		return nil, err
	}
	var result struct {
		Data string `json:"data"`
	}
	if err := client.Call(ctx, "getAttachment", params, &result); err != nil {
		return nil, err
	}
	if result.Data == "" {
		return nil, errors.New("signal-cli returned an empty attachment")
	}
	path, err := b.writeMedia(key, attachment, result.Data)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	for conversation, messages := range b.messages {
		for i := range messages {
			for j := range messages[i].Attachments {
				if messages[i].Attachments[j].Key == key {
					messages[i].Attachments[j].Path = path
				}
			}
		}
		b.messages[conversation] = messages
	}
	_ = b.saveLocked()
	b.mu.Unlock()
	if err := appStore.PruneMedia(b.paths.SignalMediaDir(), path); err != nil {
		b.log.Warn().Err(err).Msg("Could not trim Signal media")
	}
	return &wire.MediaResult{Key: key, Path: path}, nil
}

func (b *Backend) React(ctx context.Context, p wire.ReactParams) error {
	b.mu.RLock()
	account, client := b.account, b.client
	msg, ok := b.findMessageLocked(p.ConversationID, p.MessageID)
	actors := b.reactionActors[p.MessageID]
	current := ""
	if actors != nil {
		current = actors[account]
	}
	b.mu.RUnlock()
	if !ok {
		return errors.New("Signal message was not found")
	}
	targetAuthor := msg.SenderID
	if targetAuthor == "" && msg.FromMe {
		targetAuthor = account
	}
	if targetAuthor == "" {
		return errors.New("Signal message author is unavailable")
	}
	emoji := strings.TrimSpace(p.Emoji)
	remove := emoji == "" || emoji == current
	if remove && current == "" {
		return nil
	}
	params := map[string]any{"account": account, "emoji": first(emoji, current), "targetAuthor": targetAuthor, "targetTimestamp": msg.Timestamp / 1000, "remove": remove}
	if err := addConversationTarget(params, p.ConversationID); err != nil {
		return err
	}
	if err := client.Call(ctx, "sendReaction", params, nil); err != nil {
		return err
	}
	b.applyReaction(p.ConversationID, msg.Timestamp/1000, account, emoji, remove)
	return nil
}

func (b *Backend) MarkRead(_ context.Context, p wire.MarkReadParams) error {
	b.mu.Lock()
	var updated *wire.Conversation
	if conv, ok := b.convs[p.ConversationID]; ok {
		conv.Unread = false
		b.convs[p.ConversationID] = conv
		copy := conv
		updated = &copy
	}
	b.recountUnreadLocked()
	err := b.saveLocked()
	status := b.status
	b.mu.Unlock()
	if b.publish != nil && updated != nil {
		b.publish(wire.Event{Event: wire.EventConversation, Network: wire.NetworkSignal, Data: *updated})
		b.publish(wire.Event{Event: wire.EventStatus, Network: wire.NetworkSignal, Data: status})
	}
	return err
}

func (b *Backend) Unpair(context.Context) error {
	return errors.New("remove OmaChat from Signal's Linked devices screen before deleting local Signal data")
}

type notification struct {
	Account  string `json:"account"`
	Envelope struct {
		Source, SourceNumber, SourceUUID, SourceName string
		Timestamp                                    int64        `json:"timestamp"`
		DataMessage                                  *dataMessage `json:"dataMessage"`
		SyncMessage                                  *struct {
			SentMessage *syncDataMessage `json:"sentMessage"`
		} `json:"syncMessage"`
	} `json:"envelope"`
}
type groupInfo struct {
	GroupID   string `json:"groupId"`
	GroupName string `json:"groupName"`
}
type signalAttachment struct {
	ID          string `json:"id"`
	ContentType string `json:"contentType"`
	Filename    string `json:"filename"`
	Caption     string `json:"caption"`
	Size        int64  `json:"size"`
	Width       int64  `json:"width"`
	Height      int64  `json:"height"`
	IsVoiceNote bool   `json:"isVoiceNote"`
}
type signalReaction struct {
	Emoji               string `json:"emoji"`
	TargetAuthor        string `json:"targetAuthor"`
	TargetAuthorNumber  string `json:"targetAuthorNumber"`
	TargetAuthorUUID    string `json:"targetAuthorUuid"`
	TargetSentTimestamp int64  `json:"targetSentTimestamp"`
	IsRemove            bool   `json:"isRemove"`
}
type remoteDelete struct {
	Timestamp int64 `json:"timestamp"`
}
type dataMessage struct {
	Timestamp        int64              `json:"timestamp"`
	Message          string             `json:"message"`
	GroupInfo        *groupInfo         `json:"groupInfo"`
	Attachments      []signalAttachment `json:"attachments"`
	Reaction         *signalReaction    `json:"reaction"`
	RemoteDelete     *remoteDelete      `json:"remoteDelete"`
	ExpiresInSeconds int64              `json:"expiresInSeconds"`
	ViewOnce         bool               `json:"viewOnce"`
}
type syncDataMessage struct {
	Destination       string             `json:"destination"`
	DestinationNumber string             `json:"destinationNumber"`
	DestinationUUID   string             `json:"destinationUuid"`
	Timestamp         int64              `json:"timestamp"`
	Message           string             `json:"message"`
	GroupInfo         *groupInfo         `json:"groupInfo"`
	Attachments       []signalAttachment `json:"attachments"`
	Reaction          *signalReaction    `json:"reaction"`
	RemoteDelete      *remoteDelete      `json:"remoteDelete"`
	ExpiresInSeconds  int64              `json:"expiresInSeconds"`
	ViewOnce          bool               `json:"viewOnce"`
	DataMessage       *dataMessage       `json:"dataMessage"`
}

func (b *Backend) handleNotification(raw json.RawMessage) {
	var n notification
	if json.Unmarshal(raw, &n) != nil {
		return
	}
	if n.Envelope.DataMessage != nil {
		d := n.Envelope.DataMessage
		peer, name, group := first(n.Envelope.SourceUUID, n.Envelope.SourceNumber, n.Envelope.Source), first(n.Envelope.SourceName, n.Envelope.SourceNumber, n.Envelope.SourceUUID), false
		convID := directPrefix + peer
		if d.GroupInfo != nil && d.GroupInfo.GroupID != "" {
			convID, name, group = groupPrefix+d.GroupInfo.GroupID, first(d.GroupInfo.GroupName, "Signal group"), true
		}
		if peer == "" && !group {
			return
		}
		b.handleDataMessage(convID, peer, name, false, d)
	}
	if n.Envelope.SyncMessage != nil && n.Envelope.SyncMessage.SentMessage != nil {
		s := n.Envelope.SyncMessage.SentMessage
		d := &dataMessage{Timestamp: s.Timestamp, Message: s.Message, GroupInfo: s.GroupInfo, Attachments: s.Attachments, Reaction: s.Reaction, RemoteDelete: s.RemoteDelete, ExpiresInSeconds: s.ExpiresInSeconds, ViewOnce: s.ViewOnce}
		if s.DataMessage != nil {
			d = s.DataMessage
		}
		peer, name, group := first(s.DestinationUUID, s.DestinationNumber, s.Destination), first(s.DestinationNumber, s.DestinationUUID), false
		convID := directPrefix + peer
		if d.GroupInfo != nil && d.GroupInfo.GroupID != "" {
			convID, name, group = groupPrefix+d.GroupInfo.GroupID, first(d.GroupInfo.GroupName, "Signal group"), true
		}
		if peer == "" && !group {
			return
		}
		b.handleDataMessage(convID, b.account, name, true, d)
	}
}

func (b *Backend) handleDataMessage(conversationID, actor, actorName string, fromMe bool, data *dataMessage) {
	if data == nil {
		return
	}
	if data.Reaction != nil {
		b.applyReaction(conversationID, data.Reaction.TargetSentTimestamp, actor, data.Reaction.Emoji, data.Reaction.IsRemove)
		return
	}
	if data.RemoteDelete != nil {
		b.redactMessage(conversationID, data.RemoteDelete.Timestamp)
		return
	}
	// View-once media must remain ephemeral. signal-cli has already delivered it
	// to the linked device; OmaChat intentionally neither lists nor caches it.
	attachments := []wire.Attachment(nil)
	text := data.Message
	if !data.ViewOnce {
		for _, attachment := range data.Attachments {
			if attachment.ID != "" {
				attachments = append(attachments, attachmentFromMetadata(attachment))
				if strings.TrimSpace(text) == "" {
					text = attachment.Caption
				}
			}
		}
	}
	if strings.TrimSpace(text) == "" && len(attachments) == 0 {
		return
	}
	b.ensureConversation(conversationID, actorName, strings.HasPrefix(conversationID, groupPrefix))
	msg := wire.Message{ID: messageID(data.Timestamp, actor), ConversationID: conversationID, Text: text, Timestamp: data.Timestamp * 1000, FromMe: fromMe, SenderID: actor, SenderName: actorName, Attachments: attachments}
	if fromMe {
		msg.Delivery = wire.DeliverySent
	}
	b.ingest(msg, actor)
	if data.ExpiresInSeconds > 0 {
		b.setExpiration(msg.ID, data.Timestamp+data.ExpiresInSeconds*1000)
	}
}

func (b *Backend) ensureConversation(id, name string, group bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	conversation, ok := b.convs[id]
	if !ok {
		conversation = newConversation(id, first(name, fallbackConversationName(id, b.account)), group)
	} else if conversation.ID == "" {
		conversation.ID = id
		conversation.Name = first(conversation.Name, name, fallbackConversationName(id, b.account))
		conversation.IsGroup = group
		if conversation.AvatarColor == "" {
			conversation.AvatarColor = "#3a76f0"
		}
		if conversation.Initials == "" {
			conversation.Initials = conversationInitials(conversation.Name)
		}
	}
	b.convs[id] = conversation
	if !containsString(b.order, id) {
		b.order = append([]string{id}, b.order...)
	}
}

func (b *Backend) ingest(msg wire.Message, _ string) {
	b.mu.Lock()
	items := b.messages[msg.ConversationID]
	for _, existing := range items {
		if existing.ID == msg.ID {
			b.mu.Unlock()
			return
		}
	}
	items = append(items, msg)
	if len(items) > 500 {
		items = items[len(items)-500:]
	}
	b.messages[msg.ConversationID] = items
	conv := b.convs[msg.ConversationID]
	conv.Preview, conv.PreviewMine, conv.Timestamp = messagePreview(msg), msg.FromMe, msg.Timestamp
	if !msg.FromMe {
		conv.Unread = true
	}
	b.convs[msg.ConversationID] = conv
	b.recountUnreadLocked()
	_ = b.saveLocked()
	status := b.status
	b.mu.Unlock()
	if b.publish != nil {
		b.publish(wire.Event{Event: wire.EventMessage, Network: wire.NetworkSignal, Data: msg})
		b.publish(wire.Event{Event: wire.EventConversation, Network: wire.NetworkSignal, Data: conv})
		b.publish(wire.Event{Event: wire.EventStatus, Network: wire.NetworkSignal, Data: status})
	}
}

func addConversationTarget(params map[string]any, conversationID string) error {
	switch {
	case strings.HasPrefix(conversationID, directPrefix):
		params["recipient"] = []string{strings.TrimPrefix(conversationID, directPrefix)}
	case strings.HasPrefix(conversationID, groupPrefix):
		params["groupId"] = strings.TrimPrefix(conversationID, groupPrefix)
	default:
		return errors.New("invalid Signal conversation")
	}
	return nil
}

func attachmentFromMetadata(in signalAttachment) wire.Attachment {
	mimeType := strings.TrimSpace(in.ContentType)
	return wire.Attachment{
		Key: mediaPrefix + in.ID, MediaID: in.ID, Name: in.Filename, MimeType: mimeType,
		Size: in.Size, Width: in.Width, Height: in.Height,
		IsImage: strings.HasPrefix(mimeType, "image/"), IsGif: mimeType == "image/gif",
		IsAudio: in.IsVoiceNote || strings.HasPrefix(mimeType, "audio/"), IsVideo: strings.HasPrefix(mimeType, "video/"),
	}
}

func messagePreview(message wire.Message) string {
	if message.Deleted {
		return "Message deleted"
	}
	if strings.TrimSpace(message.Text) != "" {
		return message.Text
	}
	if len(message.Attachments) != 0 {
		if message.Attachments[0].IsAudio {
			return "Voice message"
		}
		if message.Attachments[0].IsImage {
			return "Photo"
		}
		if message.Attachments[0].IsVideo {
			return "Video"
		}
		return "Attachment"
	}
	return ""
}

func (b *Backend) findAttachmentLocked(key string) (wire.Attachment, string, bool) {
	for conversationID, messages := range b.messages {
		for _, message := range messages {
			for _, attachment := range message.Attachments {
				if attachment.Key == key {
					return attachment, conversationID, true
				}
			}
		}
	}
	return wire.Attachment{}, "", false
}

func (b *Backend) findMessageLocked(conversationID, id string) (wire.Message, bool) {
	for _, message := range b.messages[conversationID] {
		if message.ID == id {
			return message, true
		}
	}
	return wire.Message{}, false
}

func (b *Backend) writeMedia(key string, attachment wire.Attachment, encoded string) (string, error) {
	if b.paths == nil {
		return "", errors.New("Signal media storage is unavailable")
	}
	ext := ""
	if extensions, _ := mime.ExtensionsByType(attachment.MimeType); len(extensions) > 0 {
		ext = extensions[0]
	}
	hash := sha256.Sum256([]byte(key))
	path := filepath.Join(b.paths.SignalMediaDir(), fmt.Sprintf("%x%s", hash[:16], ext))
	tmp, err := os.CreateTemp(b.paths.SignalMediaDir(), ".signal-media-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded))
	written, copyErr := io.Copy(tmp, io.LimitReader(decoder, maxMediaBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		return "", fmt.Errorf("decode Signal attachment: %w", copyErr)
	}
	if closeErr != nil {
		return "", closeErr
	}
	if written > maxMediaBytes {
		return "", fmt.Errorf("Signal attachment exceeds %d MiB limit", maxMediaBytes>>20)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", err
	}
	return path, nil
}

func (b *Backend) applyReaction(conversationID string, targetTimestamp int64, actor, emoji string, remove bool) {
	b.mu.Lock()
	items := b.messages[conversationID]
	var updated *wire.Message
	for i := range items {
		if items[i].Timestamp/1000 != targetTimestamp {
			continue
		}
		actors := b.reactionActors[items[i].ID]
		if actors == nil {
			actors = make(map[string]string)
		}
		if remove || strings.TrimSpace(emoji) == "" {
			delete(actors, actor)
		} else {
			actors[actor] = emoji
		}
		if len(actors) == 0 {
			delete(b.reactionActors, items[i].ID)
		} else {
			b.reactionActors[items[i].ID] = actors
		}
		items[i].Reactions = reactionsFromActors(actors, b.account)
		copy := items[i]
		updated = &copy
		break
	}
	b.messages[conversationID] = items
	_ = b.saveLocked()
	b.mu.Unlock()
	if updated != nil && b.publish != nil {
		b.publish(wire.Event{Event: wire.EventMessage, Network: wire.NetworkSignal, Data: *updated})
	}
}

func reactionsFromActors(actors map[string]string, account string) []wire.Reaction {
	counts := make(map[string]int)
	mine := make(map[string]bool)
	for actor, emoji := range actors {
		if emoji == "" {
			continue
		}
		counts[emoji]++
		if actor == account {
			mine[emoji] = true
		}
	}
	emojis := make([]string, 0, len(counts))
	for emoji := range counts {
		emojis = append(emojis, emoji)
	}
	sort.Strings(emojis)
	out := make([]wire.Reaction, 0, len(emojis))
	for _, emoji := range emojis {
		out = append(out, wire.Reaction{Emoji: emoji, Count: counts[emoji], Mine: mine[emoji]})
	}
	return out
}

func (b *Backend) redactMessage(conversationID string, targetTimestamp int64) {
	b.mu.Lock()
	items := b.messages[conversationID]
	var updated *wire.Message
	for i := range items {
		if items[i].Timestamp/1000 != targetTimestamp {
			continue
		}
		b.removeMessageMediaLocked(items[i])
		items[i].Text, items[i].Attachments, items[i].Reactions, items[i].Deleted = "", nil, nil, true
		delete(b.reactionActors, items[i].ID)
		delete(b.expirations, items[i].ID)
		if timer := b.expiryTimers[items[i].ID]; timer != nil {
			timer.Stop()
			delete(b.expiryTimers, items[i].ID)
		}
		copy := items[i]
		updated = &copy
		break
	}
	b.messages[conversationID] = items
	b.repairConversationLocked(conversationID)
	_ = b.saveLocked()
	conversation := b.convs[conversationID]
	b.mu.Unlock()
	if updated != nil && b.publish != nil {
		b.publish(wire.Event{Event: wire.EventMessage, Network: wire.NetworkSignal, Data: *updated})
		b.publish(wire.Event{Event: wire.EventConversation, Network: wire.NetworkSignal, Data: conversation})
	}
}

func (b *Backend) setExpiration(messageID string, atMillis int64) {
	b.mu.Lock()
	b.expirations[messageID] = atMillis
	b.scheduleExpirationLocked(messageID, atMillis)
	_ = b.saveLocked()
	b.mu.Unlock()
}

func (b *Backend) scheduleExpirationsLocked() {
	for id, at := range b.expirations {
		b.scheduleExpirationLocked(id, at)
	}
}

func (b *Backend) scheduleExpirationLocked(messageID string, atMillis int64) {
	if timer := b.expiryTimers[messageID]; timer != nil {
		timer.Stop()
	}
	delay := time.Until(time.UnixMilli(atMillis))
	if delay < 0 {
		delay = 0
	}
	b.expiryTimers[messageID] = time.AfterFunc(delay, func() { b.expireMessage(messageID) })
}

func (b *Backend) expireMessage(messageID string) {
	b.mu.RLock()
	conversationID := ""
	for id, messages := range b.messages {
		for _, message := range messages {
			if message.ID == messageID {
				conversationID = id
				break
			}
		}
		if conversationID != "" {
			break
		}
	}
	b.mu.RUnlock()
	if conversationID != "" {
		var timestamp int64
		b.mu.RLock()
		if message, ok := b.findMessageLocked(conversationID, messageID); ok {
			timestamp = message.Timestamp / 1000
		}
		b.mu.RUnlock()
		b.redactMessage(conversationID, timestamp)
	}
}

func (b *Backend) pruneExpiredLocked(nowMillis int64) {
	for conversationID, messages := range b.messages {
		changed := false
		for i := range messages {
			at, ok := b.expirations[messages[i].ID]
			if !ok || at > nowMillis {
				continue
			}
			b.removeMessageMediaLocked(messages[i])
			messages[i].Text, messages[i].Attachments, messages[i].Reactions, messages[i].Deleted = "", nil, nil, true
			delete(b.expirations, messages[i].ID)
			delete(b.reactionActors, messages[i].ID)
			changed = true
		}
		b.messages[conversationID] = messages
		if changed {
			b.repairConversationLocked(conversationID)
		}
	}
}

func (b *Backend) removeMessageMediaLocked(message wire.Message) {
	if b.paths == nil {
		return
	}
	for _, attachment := range message.Attachments {
		if attachment.Path != "" && strings.HasPrefix(filepath.Clean(attachment.Path), filepath.Clean(b.paths.SignalMediaDir())+string(os.PathSeparator)) {
			_ = os.Remove(attachment.Path)
		}
	}
}

func (b *Backend) repairConversationLocked(conversationID string) {
	conversation, ok := b.convs[conversationID]
	if !ok {
		return
	}
	items := b.messages[conversationID]
	if len(items) == 0 {
		conversation.Preview, conversation.Timestamp = "", 0
	} else {
		last := items[len(items)-1]
		conversation.Preview, conversation.PreviewMine, conversation.Timestamp = messagePreview(last), last.FromMe, last.Timestamp
	}
	b.convs[conversationID] = conversation
}

func (b *Backend) recountUnreadLocked() {
	count := 0
	for _, conversation := range b.convs {
		if conversation.Unread {
			count++
		}
	}
	b.status.Unread = count
}

func (b *Backend) saveLocked() error {
	if b.paths == nil {
		return nil
	}
	return saveStoredData(b.paths.SignalStoreFile(), storedData{Account: b.account, Conversations: b.convs, Order: b.order, Messages: b.messages, ReactionActors: b.reactionActors, Expirations: b.expirations})
}
func messageID(timestamp int64, author string) string {
	return fmt.Sprintf("signal:%d:%s", timestamp, author)
}
func first(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func newConversation(id, name string, group bool) wire.Conversation {
	return wire.Conversation{ID: id, Name: name, IsGroup: group, AvatarColor: "#3a76f0", Initials: conversationInitials(name)}
}

func fallbackConversationName(id, account string) string {
	if id == directPrefix+account && account != "" {
		return "Note to Self"
	}
	if strings.HasPrefix(id, groupPrefix) {
		return "Signal group"
	}
	return first(strings.TrimPrefix(id, directPrefix), "Signal contact")
}

func conversationInitials(name string) string {
	if r := []rune(strings.TrimSpace(name)); len(r) > 0 {
		return strings.ToUpper(string(r[0]))
	}
	return "?"
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
