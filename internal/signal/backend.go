// Package signal adapts signal-cli's JSON-RPC stream to OmaChat's common wire model.
package signal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

const (
	directPrefix = "signal:user:"
	groupPrefix  = "signal:group:"
)

type Backend struct {
	log     zerolog.Logger
	paths   *appStore.Paths
	publish func(wire.Event)

	mu       sync.RWMutex
	status   wire.Status
	account  string
	convs    map[string]wire.Conversation
	order    []string
	messages map[string][]wire.Message
	client   Caller
	cancel   context.CancelFunc
}

func New(log zerolog.Logger, paths *appStore.Paths, publish func(wire.Event)) *Backend {
	data := emptyStoredData()
	if paths != nil {
		data = loadStoredData(paths.SignalStoreFile())
	}
	return &Backend{
		log: log.With().Str("network", wire.NetworkSignal).Logger(), paths: paths, publish: publish,
		status:  wire.Status{Network: wire.NetworkSignal, State: wire.StateUnpaired, PhoneOK: true},
		account: data.Account, convs: data.Conversations, order: data.Order, messages: data.Messages,
		client: NewRPCClient(),
	}
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
	if err := client.Start(ctx, b.paths.SignalDataDir(), b.handleNotification); err != nil {
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
	b.mu.Unlock()
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
		params["groupId"] = []string{strings.TrimPrefix(p.ConversationID, groupPrefix)}
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
	msg := wire.Message{ID: messageID(result.Timestamp, account), TmpID: p.TmpID, ConversationID: p.ConversationID, Text: p.Text, Timestamp: result.Timestamp * 1000, FromMe: true, Delivery: wire.DeliverySent}
	b.ingest(msg, account)
	return &msg, nil
}

func (b *Backend) MarkRead(_ context.Context, p wire.MarkReadParams) error {
	b.mu.Lock()
	if conv, ok := b.convs[p.ConversationID]; ok {
		conv.Unread = false
		b.convs[p.ConversationID] = conv
	}
	_ = b.saveLocked()
	b.mu.Unlock()
	return nil
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
type dataMessage struct {
	Timestamp int64      `json:"timestamp"`
	Message   string     `json:"message"`
	GroupInfo *groupInfo `json:"groupInfo"`
}
type syncDataMessage struct {
	Destination       string     `json:"destination"`
	DestinationNumber string     `json:"destinationNumber"`
	DestinationUUID   string     `json:"destinationUuid"`
	Timestamp         int64      `json:"timestamp"`
	Message           string     `json:"message"`
	GroupInfo         *groupInfo `json:"groupInfo"`
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
		b.ensureConversation(convID, name, group)
		b.ingest(wire.Message{ID: messageID(d.Timestamp, peer), ConversationID: convID, Text: d.Message, Timestamp: d.Timestamp * 1000, SenderID: peer, SenderName: name}, peer)
	}
	if n.Envelope.SyncMessage != nil && n.Envelope.SyncMessage.SentMessage != nil {
		d := n.Envelope.SyncMessage.SentMessage
		peer, name, group := first(d.DestinationUUID, d.DestinationNumber, d.Destination), first(d.DestinationNumber, d.DestinationUUID), false
		convID := directPrefix + peer
		if d.GroupInfo != nil && d.GroupInfo.GroupID != "" {
			convID, name, group = groupPrefix+d.GroupInfo.GroupID, first(d.GroupInfo.GroupName, "Signal group"), true
		}
		if peer == "" && !group {
			return
		}
		b.ensureConversation(convID, name, group)
		b.ingest(wire.Message{ID: messageID(d.Timestamp, b.account), ConversationID: convID, Text: d.Message, Timestamp: d.Timestamp * 1000, FromMe: true, Delivery: wire.DeliverySent}, b.account)
	}
}

func (b *Backend) ensureConversation(id, name string, group bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.convs[id]; !ok {
		b.convs[id] = newConversation(id, name, group)
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
	conv.Preview, conv.PreviewMine, conv.Timestamp = msg.Text, msg.FromMe, msg.Timestamp
	if !msg.FromMe {
		conv.Unread = true
	}
	b.convs[msg.ConversationID] = conv
	_ = b.saveLocked()
	b.mu.Unlock()
	if b.publish != nil {
		b.publish(wire.Event{Event: wire.EventMessage, Network: wire.NetworkSignal, Data: msg})
		b.publish(wire.Event{Event: wire.EventConversation, Network: wire.NetworkSignal, Data: conv})
	}
}

func (b *Backend) saveLocked() error {
	if b.paths == nil {
		return nil
	}
	return saveStoredData(b.paths.SignalStoreFile(), storedData{Account: b.account, Conversations: b.convs, Order: b.order, Messages: b.messages})
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
	initials := "?"
	if r := []rune(strings.TrimSpace(name)); len(r) > 0 {
		initials = strings.ToUpper(string(r[0]))
	}
	return wire.Conversation{ID: id, Name: name, IsGroup: group, AvatarColor: "#3a76f0", Initials: initials}
}
