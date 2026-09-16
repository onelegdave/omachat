// Package messenger implements native personal Facebook Messenger access.
package messenger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/onelegdave/omachat/internal/browser"
	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/cookies"
	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	metaTypes "go.mau.fi/mautrix-meta/pkg/messagix/types"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waConsumerApplication"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

var ErrNotConfigured = errors.New("Messenger client not configured")

type sessionData struct {
	Cookies map[string]string `json:"cookies"`
}

type Backend struct {
	log         zerolog.Logger
	paths       *appStore.Paths
	publish     func(wire.Event)
	config      *appStore.ConfigStore
	mu          sync.RWMutex
	status      wire.Status
	client      *messagix.Client
	e2eeClient  *whatsmeow.Client
	waStore     *sqlstore.Container
	selfID      int64
	convs       map[string]wire.Conversation
	messages    map[string][]wire.Message
	threadToJID map[int64]waTypes.JID
	jidToThread map[string]int64
	threadTypes map[int64]table.ThreadType
	runCtx      context.Context
	cancel      context.CancelFunc
}

func New(log zerolog.Logger, paths *appStore.Paths, publish func(wire.Event)) *Backend {
	return &Backend{
		log: log.With().Str("svc", "messenger").Logger(), paths: paths, publish: publish,
		status: wire.Status{Network: wire.NetworkMessenger, State: wire.StateUnpaired, PhoneOK: true},
		convs:  make(map[string]wire.Conversation), messages: make(map[string][]wire.Message),
		threadToJID: make(map[int64]waTypes.JID), jidToThread: make(map[string]int64),
		threadTypes: make(map[int64]table.ThreadType),
	}
}

func (b *Backend) SetConfig(cs *appStore.ConfigStore)           { b.config = cs }
func (b *Backend) Status() wire.Status                          { b.mu.RLock(); defer b.mu.RUnlock(); return b.status }
func (b *Backend) SetState(state wire.ConnState, detail string) { b.setState(state, detail) }
func (b *Backend) setState(state wire.ConnState, detail string) {
	b.mu.Lock()
	b.status.State, b.status.Error = state, detail
	st := b.status
	b.mu.Unlock()
	if b.publish != nil {
		b.publish(wire.Event{Event: wire.EventStatus, Network: wire.NetworkMessenger, Data: st})
	}
}

func (b *Backend) saveCookies(values map[string]string) error {
	data, err := json.Marshal(sessionData{Cookies: values})
	if err != nil {
		return err
	}
	return appStore.WritePrivateJSON(b.paths.MessengerSessionFile(), data)
}
func (b *Backend) loadCookies() (map[string]string, error) {
	data, err := os.ReadFile(b.paths.MessengerSessionFile())
	if err != nil {
		return nil, err
	}
	var sess sessionData
	if err = json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	return sess.Cookies, nil
}
func makeCookies(values map[string]string) *cookies.Cookies {
	c := &cookies.Cookies{Platform: metaTypes.Facebook}
	m := make(map[cookies.MetaCookieName]string, len(values))
	for k, v := range values {
		m[cookies.MetaCookieName(k)] = v
	}
	c.UpdateValues(m)
	return c
}

func (b *Backend) PairFromBrowser(ctx context.Context) error {
	b.setState(wire.StateConnecting, "Finding browser profile...")
	profiles := browser.DiscoverProfiles()
	if len(profiles) == 0 {
		b.setState(wire.StateUnpaired, "No supported browser profile found")
		return errors.New("no supported browser profile found")
	}
	selected := &profiles[0]
	if b.config != nil {
		wanted := b.config.Get().BrowserProfile
		for i := range profiles {
			if profiles[i].Name == wanted {
				selected = &profiles[i]
				break
			}
		}
	}
	b.mu.Lock()
	b.status.Profile = selected.Name
	b.mu.Unlock()
	values, err := browser.ExtractMessengerCookiesContext(ctx, *selected)
	if err != nil {
		b.setState(wire.StateUnpaired, "Failed to read Messenger cookies: "+err.Error())
		return fmt.Errorf("read Messenger cookies: %w", err)
	}
	b.setState(wire.StateConnecting, "Authenticating with Messenger...")
	b.mu.RLock()
	runCtx := b.runCtx
	b.mu.RUnlock()
	if runCtx == nil {
		runCtx = ctx
	}
	if err = b.connect(runCtx, makeCookies(values)); err != nil {
		b.setState(wire.StateUnpaired, "Authentication failed: "+err.Error())
		return err
	}
	if err = b.saveCookies(values); err != nil {
		b.mu.Lock()
		e2ee, cli := b.e2eeClient, b.client
		b.e2eeClient, b.client = nil, nil
		b.mu.Unlock()
		if e2ee != nil {
			e2ee.Disconnect()
		}
		if cli != nil {
			cli.Disconnect()
		}
		return fmt.Errorf("save Messenger session: %w", err)
	}
	b.setState(wire.StateConnected, "")
	return nil
}

func (b *Backend) Start(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	b.mu.Lock()
	if b.cancel != nil {
		b.cancel()
	}
	b.cancel, b.runCtx = cancel, ctx
	b.mu.Unlock()
	values, err := b.loadCookies()
	if errors.Is(err, os.ErrNotExist) {
		b.setState(wire.StateUnpaired, "")
		return nil
	}
	if err != nil {
		b.setState(wire.StateError, "Failed to read session: "+err.Error())
		return err
	}
	b.setState(wire.StateConnecting, "")
	if err = b.connect(ctx, makeCookies(values)); err != nil {
		b.setState(wire.StateDisconnected, err.Error())
		return err
	}
	b.setState(wire.StateConnected, "")
	return nil
}

func (b *Backend) connect(ctx context.Context, mc *cookies.Cookies) error {
	if b.waStore == nil {
		container, err := sqlstore.New(ctx, "sqlite3", "file:"+b.paths.MessengerDBFile()+"?_foreign_keys=on", waLog.Zerolog(b.log.With().Str("component", "e2ee-db").Logger()))
		if err != nil {
			return fmt.Errorf("open E2EE store: %w", err)
		}
		b.waStore = container
	}
	cli := messagix.NewClient(mc, b.log.With().Str("component", "meta").Logger(), &messagix.Config{})
	connected := false
	defer func() {
		if connected {
			return
		}
		cli.Disconnect()
		b.mu.Lock()
		if b.client == cli {
			b.client = nil
		}
		b.mu.Unlock()
	}()
	cli.SetEventHandler(b.handleMetaEvent)
	user, initial, err := cli.LoadMessagesPage(ctx)
	if err != nil {
		return fmt.Errorf("load Messenger inbox: %w", err)
	}
	b.mu.Lock()
	b.selfID, b.client = user.GetFBID(), cli
	b.mu.Unlock()
	b.handleTable(initial)
	device, err := b.waStore.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("load E2EE device: %w", err)
	}
	newDevice := needsMessengerRegistration(device)
	if newDevice {
		if device == nil {
			device = b.waStore.NewDevice()
		}
	}
	cli.SetDevice(device)
	if newDevice {
		if err = cli.RegisterE2EE(ctx, user.GetFBID()); err != nil {
			return fmt.Errorf("register E2EE device: %w", err)
		}
		if err = device.Save(ctx); err != nil {
			return fmt.Errorf("save E2EE device: %w", err)
		}
	}
	if err = cli.Connect(ctx); err != nil {
		return fmt.Errorf("connect Messenger transport: %w", err)
	}
	e2ee, err := cli.PrepareE2EEClient()
	if err != nil {
		cli.Disconnect()
		return fmt.Errorf("prepare E2EE transport: %w", err)
	}
	e2ee.AddEventHandler(b.handleE2EEEvent)
	if err = e2ee.Connect(); err != nil {
		cli.Disconnect()
		return fmt.Errorf("connect E2EE transport: %w", err)
	}
	b.mu.Lock()
	b.e2eeClient = e2ee
	b.mu.Unlock()
	connected = true
	return nil
}

func needsMessengerRegistration(device *waStore.Device) bool {
	return device == nil || device.ID == nil
}

func (b *Backend) Stop() {
	b.mu.Lock()
	cancel, e2ee, cli, waStore := b.cancel, b.e2eeClient, b.client, b.waStore
	b.cancel, b.runCtx, b.e2eeClient, b.client, b.waStore = nil, nil, nil, nil, nil
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if e2ee != nil {
		e2ee.Disconnect()
	}
	if cli != nil {
		cli.Disconnect()
	}
	if waStore != nil {
		if err := waStore.Close(); err != nil {
			b.log.Warn().Err(err).Msg("Failed to close Messenger E2EE store")
		}
	}
}
func (b *Backend) Unpair(context.Context) error {
	b.Stop()
	b.setState(wire.StateUnpaired, "")
	return b.paths.ClearMessengerSession()
}

func (b *Backend) handleMetaEvent(_ context.Context, evt any) {
	switch v := evt.(type) {
	case *table.LSTable:
		b.handleTable(v)
	case *messagix.ConnectedEvent, *messagix.ReconnectedEvent:
		b.setState(wire.StateConnected, "")
	case *messagix.TransientDisconnectEvent:
		detail := "Messenger transport disconnected"
		if v.Err != nil {
			detail = v.Err.Error()
		}
		b.setState(wire.StateDisconnected, detail)
	case *messagix.PermanentErrorEvent:
		detail := "Messenger transport stopped"
		if v.Err != nil {
			detail = v.Err.Error()
		}
		b.setState(wire.StateError, detail)
	}
}
func (b *Backend) handleTable(tbl *table.LSTable) {
	if tbl == nil {
		return
	}
	b.mu.Lock()
	for _, m := range tbl.LSUpdateThreadAuthorityAndMappingWithOTIDFromJID {
		b.setMappingLocked(m.ThreadKey, m.ThreadJID, table.ENCRYPTED_OVER_WA_ONE_TO_ONE)
	}
	for _, m := range tbl.LSVerifyHybridThreadExists {
		b.setMappingLocked(m.ThreadKey, m.ThreadJID, m.ThreadType)
	}
	for _, t := range tbl.LSDeleteThenInsertThread {
		b.upsertThreadLocked(t.ThreadKey, t.ThreadName, t.Snippet, t.LastActivityTimestampMs, t.LastReadWatermarkTimestampMs, t.UnreadMessageCount > 0, t.MemberCount > 2, t.DisableComposerInput, t.ThreadPictureUrl, t.ThreadType)
	}
	for _, t := range tbl.LSUpdateOrInsertThread {
		b.upsertThreadLocked(t.ThreadKey, t.ThreadName, t.Snippet, t.LastActivityTimestampMs, t.LastReadWatermarkTimestampMs, false, false, t.DisableComposerInput, t.ThreadPictureUrl, t.ThreadType)
	}
	for _, m := range tbl.LSDeleteThenInsertMessage {
		b.addMessageLocked(m.ThreadKey, m.MessageId, m.Text, m.TimestampMs, m.SenderId, m.IsUnsent, m.ReplySourceId)
	}
	for _, m := range tbl.LSInsertMessage {
		b.addMessageLocked(m.ThreadKey, m.MessageId, m.Text, m.TimestampMs, m.SenderId, m.IsUnsent, m.ReplySourceId)
	}
	b.recountUnreadLocked()
	b.mu.Unlock()
	b.publishSnapshots()
}
func (b *Backend) setMappingLocked(threadKey, jid int64, typ table.ThreadType) {
	if jid == 0 {
		return
	}
	server := waTypes.MessengerServer
	if typ == table.ENCRYPTED_OVER_WA_GROUP {
		server = waTypes.GroupServer
	}
	j := waTypes.NewJID(strconv.FormatInt(jid, 10), server)
	b.threadToJID[threadKey] = j
	b.jidToThread[j.ToNonAD().String()] = threadKey
	b.threadTypes[threadKey] = typ
}
func (b *Backend) upsertThreadLocked(id int64, name, preview string, ts, readTS int64, unread, group, readOnly bool, avatar string, typ table.ThreadType) {
	key := strconv.FormatInt(id, 10)
	if name == "" {
		name = "Messenger conversation"
	}
	b.threadTypes[id] = typ
	_ = readTS
	// Messenger picture URLs are remote. Keep them out of AvatarPath, which is
	// reserved for daemon-controlled local files.
	_ = avatar
	b.convs[key] = wire.Conversation{ID: key, Name: name, Preview: preview, Timestamp: ts, Unread: unread, IsGroup: group, ReadOnly: readOnly, AvatarColor: "#0084ff", Initials: initials(name)}
}
func (b *Backend) addMessageLocked(thread int64, id, text string, ts, sender int64, deleted bool, reply string) {
	if id == "" {
		return
	}
	key := strconv.FormatInt(thread, 10)
	msg := wire.Message{ID: id, ConversationID: key, Text: text, Timestamp: ts, FromMe: sender == b.selfID, SenderID: strconv.FormatInt(sender, 10), Deleted: deleted, ReplyToID: reply}
	list := b.messages[key]
	for i := range list {
		if list[i].ID == id {
			list[i] = msg
			b.messages[key] = list
			return
		}
	}
	b.messages[key] = append(list, msg)
	sort.Slice(b.messages[key], func(i, j int) bool { return b.messages[key][i].Timestamp < b.messages[key][j].Timestamp })
}

func (b *Backend) handleE2EEEvent(raw any) {
	switch evt := raw.(type) {
	case *events.FBMessage:
		text := fbText(evt)
		if text == "" {
			text = "Unsupported Messenger message. Open Messenger to view it."
		}
		jid := evt.Info.Chat.ToNonAD()
		b.mu.Lock()
		thread := b.jidToThread[jid.String()]
		if thread == 0 {
			if parsed, err := strconv.ParseInt(jid.User, 10, 64); err == nil {
				thread = parsed
				b.setMappingLocked(thread, parsed, table.ENCRYPTED_OVER_WA_ONE_TO_ONE)
			}
		}
		key := strconv.FormatInt(thread, 10)
		if _, ok := b.convs[key]; !ok {
			b.upsertThreadLocked(thread, jid.User, text, evt.Info.Timestamp.UnixMilli(), 0, true, jid.Server == waTypes.GroupServer, false, "", table.ENCRYPTED_OVER_WA_ONE_TO_ONE)
		}
		b.addMessageLocked(thread, evt.Info.ID, text, evt.Info.Timestamp.UnixMilli(), parseUser(evt.Info.Sender.User), false, "")
		conv := b.convs[key]
		conv.Preview, conv.Timestamp, conv.Unread = text, evt.Info.Timestamp.UnixMilli(), !evt.Info.IsFromMe
		b.convs[key] = conv
		b.recountUnreadLocked()
		b.mu.Unlock()
		b.publishSnapshots()
	case *events.Connected:
		b.setState(wire.StateConnected, "")
	case *events.Disconnected:
		b.setState(wire.StateDisconnected, "Messenger encrypted transport disconnected")
	}
}
func fbText(evt *events.FBMessage) string {
	consumer, ok := evt.Message.(*waConsumerApplication.ConsumerApplication)
	if !ok {
		return ""
	}
	content := consumer.GetPayload().GetContent()
	switch v := content.GetContent().(type) {
	case *waConsumerApplication.ConsumerApplication_Content_MessageText:
		return v.MessageText.GetText()
	case *waConsumerApplication.ConsumerApplication_Content_ExtendedTextMessage:
		return v.ExtendedTextMessage.GetText().GetText()
	}
	return ""
}
func parseUser(v string) int64 { n, _ := strconv.ParseInt(v, 10, 64); return n }
func initials(name string) string {
	f := strings.Fields(name)
	if len(f) == 0 {
		return "?"
	}
	a := []rune(strings.ToUpper(f[0]))
	out := []rune{a[0]}
	if len(f) > 1 {
		z := []rune(strings.ToUpper(f[len(f)-1]))
		out = append(out, z[0])
	}
	return string(out)
}
func (b *Backend) recountUnreadLocked() {
	n := 0
	for _, c := range b.convs {
		if c.Unread {
			n++
		}
	}
	b.status.Unread = n
	b.status.LastSyncSec = time.Now().Unix()
}
func (b *Backend) publishSnapshots() {
	if b.publish == nil {
		return
	}
	for _, c := range b.Conversations(50) {
		b.publish(wire.Event{Event: wire.EventConversation, Network: wire.NetworkMessenger, Data: c})
	}
	b.publish(wire.Event{Event: wire.EventStatus, Network: wire.NetworkMessenger, Data: b.Status()})
}

func (b *Backend) Conversations(count int) []wire.Conversation {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]wire.Conversation, 0, len(b.convs))
	for _, c := range b.convs {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	if count > 0 && len(out) > count {
		out = out[:count]
	}
	return out
}
func (b *Backend) Messages(ctx context.Context, p wire.MessagesParams) (wire.MessagesResult, error) {
	thread, err := strconv.ParseInt(p.ConversationID, 10, 64)
	if err != nil {
		return wire.MessagesResult{}, errors.New("invalid Messenger conversation ID")
	}
	b.mu.RLock()
	cli := b.client
	b.mu.RUnlock()
	if cli != nil {
		resp, fetchErr := cli.ExecuteTasks(ctx, &socket.FetchMessagesTask{ThreadKey: thread, Direction: 0, ReferenceTimestampMs: p.CursorTime, ReferenceMessageId: p.CursorID, SyncGroup: 1})
		if fetchErr != nil {
			return wire.MessagesResult{}, fetchErr
		}
		b.handleTable(resp)
	}
	b.mu.RLock()
	list := append([]wire.Message(nil), b.messages[p.ConversationID]...)
	b.mu.RUnlock()
	count := int(p.Count)
	if count > 0 && len(list) > count {
		list = list[len(list)-count:]
	}
	return wire.MessagesResult{ConversationID: p.ConversationID, Messages: list}, nil
}
func (b *Backend) Send(ctx context.Context, p wire.SendParams) (*wire.Message, error) {
	thread, err := strconv.ParseInt(p.ConversationID, 10, 64)
	if err != nil {
		return nil, errors.New("invalid Messenger conversation ID")
	}
	text := strings.TrimSpace(p.Text)
	if text == "" {
		return nil, errors.New("message is empty")
	}
	b.mu.RLock()
	cli, e2ee, jid, self := b.client, b.e2eeClient, b.threadToJID[thread], b.selfID
	b.mu.RUnlock()
	if cli == nil {
		return nil, ErrNotConfigured
	}
	id := strconv.FormatInt(methods.GenerateEpochID(), 10)
	ts := time.Now()
	if !jid.IsEmpty() {
		if e2ee == nil {
			return nil, errors.New("Messenger encrypted transport is not connected")
		}
		content := &waConsumerApplication.ConsumerApplication{Payload: &waConsumerApplication.ConsumerApplication_Payload{Payload: &waConsumerApplication.ConsumerApplication_Payload_Content{Content: &waConsumerApplication.ConsumerApplication_Content{Content: &waConsumerApplication.ConsumerApplication_Content_MessageText{MessageText: &waCommon.MessageText{Text: proto.String(text)}}}}}}
		resp, e := e2ee.SendFBMessage(ctx, jid, content, nil, whatsmeow.SendRequestExtra{ID: id})
		if e != nil {
			return nil, e
		}
		ts = resp.Timestamp
	} else {
		otid := methods.GenerateEpochID()
		resp, e := cli.ExecuteTasks(ctx, &socket.SendMessageTask{ThreadId: thread, Otid: otid, Source: table.MESSENGER_INBOX, SendType: table.TEXT, SyncGroup: 1, Text: text, InitiatingSource: table.FACEBOOK_INBOX})
		if e != nil {
			return nil, e
		}
		for _, r := range resp.LSReplaceOptimsiticMessage {
			if r.MessageId != "" {
				id = r.MessageId
				break
			}
		}
	}
	msg := wire.Message{TmpID: p.TmpID, ID: id, ConversationID: p.ConversationID, Text: text, Timestamp: ts.UnixMilli(), FromMe: true, SenderID: strconv.FormatInt(self, 10), Delivery: wire.DeliverySent}
	b.mu.Lock()
	b.addMessageLocked(thread, id, text, msg.Timestamp, self, false, p.ReplyToID)
	if c, ok := b.convs[p.ConversationID]; ok {
		c.Preview = text
		c.PreviewMine = true
		c.Timestamp = msg.Timestamp
		b.convs[p.ConversationID] = c
	}
	b.mu.Unlock()
	b.publishSnapshots()
	return &msg, nil
}
func (b *Backend) MarkRead(ctx context.Context, p wire.MarkReadParams) error {
	thread, err := strconv.ParseInt(p.ConversationID, 10, 64)
	if err != nil {
		return err
	}
	b.mu.RLock()
	cli := b.client
	b.mu.RUnlock()
	if cli == nil {
		return ErrNotConfigured
	}
	_, err = cli.ExecuteTasks(ctx, &socket.ThreadMarkReadTask{ThreadId: thread, LastReadWatermarkTs: time.Now().UnixMilli(), SyncGroup: 1})
	if err == nil {
		b.mu.Lock()
		c := b.convs[p.ConversationID]
		c.Unread = false
		b.convs[p.ConversationID] = c
		b.recountUnreadLocked()
		b.mu.Unlock()
		b.publishSnapshots()
	}
	return err
}
func (b *Backend) Refresh(ctx context.Context) error {
	b.mu.RLock()
	cli := b.client
	b.mu.RUnlock()
	if cli == nil {
		return ErrNotConfigured
	}
	resp, err := cli.ExecuteTasks(ctx, &socket.FetchThreadsTask{ParentThreadKey: -1, ReferenceActivityTimestamp: time.Now().Add(24 * time.Hour).UnixMilli(), SyncGroup: 1})
	if err == nil {
		b.handleTable(resp)
	}
	return err
}
