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
	"syscall"
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

const unsupportedMessageText = "Unsupported Messenger message. Open Messenger to view it."

const encryptedHistoryNotice = "Older messages in this end-to-end encrypted conversation are not currently supported. New messages will appear while OmaChat is connected."

type sessionData struct {
	Cookies map[string]string `json:"cookies"`
}

type Backend struct {
	log            zerolog.Logger
	paths          *appStore.Paths
	publish        func(wire.Event)
	config         *appStore.ConfigStore
	mu             sync.RWMutex
	status         wire.Status
	client         *messagix.Client
	e2eeClient     *whatsmeow.Client
	waStore        *sqlstore.Container
	selfID         int64
	convs          map[string]wire.Conversation
	messages       map[string][]wire.Message
	contactNames   map[int64]string
	threadNames    map[int64]string
	participants   map[int64][]int64
	threadToJID    map[int64]waTypes.JID
	jidToThread    map[string]int64
	threadTypes    map[int64]table.ThreadType
	media          map[string]*messengerMedia
	contactAvatars map[int64]string
	threadAvatars  map[int64]string
	avatarCache    map[string]string
	avatarPending  map[string]bool
	runCtx         context.Context
	cancel         context.CancelFunc
}

func New(log zerolog.Logger, paths *appStore.Paths, publish func(wire.Event)) *Backend {
	return &Backend{
		log: log.With().Str("svc", "messenger").Logger(), paths: paths, publish: publish,
		status: wire.Status{Network: wire.NetworkMessenger, State: wire.StateUnpaired, PhoneOK: true},
		convs:  make(map[string]wire.Conversation), messages: make(map[string][]wire.Message), contactNames: make(map[int64]string),
		threadNames: make(map[int64]string), participants: make(map[int64][]int64),
		threadToJID: make(map[int64]waTypes.JID), jidToThread: make(map[string]int64),
		threadTypes:    make(map[int64]table.ThreadType),
		media:          make(map[string]*messengerMedia),
		contactAvatars: make(map[int64]string),
		threadAvatars:  make(map[int64]string),
		avatarCache:    make(map[string]string),
		avatarPending:  make(map[string]bool),
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
	if b.paths != nil {
		b.mu.Lock()
		b.mergeStoredMessengerDataLocked(loadStoredMessengerData(b.paths.MessengerStoreFile()))
		b.mu.Unlock()
	}
	if b.waStore == nil {
		if err := ensureMessengerSQLiteFile(b.paths.MessengerDBFile()); err != nil {
			return fmt.Errorf("secure E2EE store: %w", err)
		}
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

func ensureMessengerSQLiteFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
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
	publish := make(map[string]wire.Message)
	queue := func(msg wire.Message) {
		if msg.ID == "" {
			return
		}
		publish[msg.ConversationID+"\x00"+msg.ID] = msg
	}
	b.mu.Lock()
	for _, contact := range tbl.LSVerifyContactRowExists {
		if contact.ProfilePictureUrl != "" {
			b.contactAvatars[contact.ContactId] = contact.ProfilePictureUrl
		}
		for _, msg := range b.setContactNameLocked(contact.ContactId, contact.Name) {
			queue(msg)
		}
	}
	for _, contact := range tbl.LSDeleteThenInsertContact {
		if avatar := contact.GetAvatarURL(); avatar != "" {
			b.contactAvatars[contact.Id] = avatar
		}
		for _, msg := range b.setContactNameLocked(contact.Id, contact.Name) {
			queue(msg)
		}
	}
	for _, m := range tbl.LSUpdateThreadAuthorityAndMappingWithOTIDFromJID {
		b.setMappingLocked(m.ThreadKey, m.ThreadJID, table.ENCRYPTED_OVER_WA_ONE_TO_ONE)
	}
	for _, m := range tbl.LSVerifyHybridThreadExists {
		b.setMappingLocked(m.ThreadKey, m.ThreadJID, m.ThreadType)
	}
	for _, participant := range tbl.LSAddParticipantIdToGroupThread {
		b.addParticipantLocked(participant.ThreadKey, participant.ContactId)
	}
	for _, t := range tbl.LSDeleteThenInsertThread {
		b.upsertThreadLocked(t.ThreadKey, t.ThreadName, t.Snippet, t.LastActivityTimestampMs, t.LastReadWatermarkTimestampMs, t.UnreadMessageCount > 0, messengerGroupThread(t.ThreadType, t.MemberCount), t.DisableComposerInput, t.ThreadPictureUrl, t.ThreadType)
	}
	for _, t := range tbl.LSUpdateOrInsertThread {
		b.upsertThreadLocked(t.ThreadKey, t.ThreadName, t.Snippet, t.LastActivityTimestampMs, t.LastReadWatermarkTimestampMs, false, messengerGroupThread(t.ThreadType, 0), t.DisableComposerInput, t.ThreadPictureUrl, t.ThreadType)
	}
	attachments := b.tableAttachmentsLocked(tbl)
	for _, m := range tbl.LSDeleteThenInsertMessage {
		queue(b.addMessageWithAttachmentsLocked(m.ThreadKey, m.MessageId, m.Text, m.TimestampMs, m.SenderId, m.IsUnsent, m.ReplySourceId, attachments[m.MessageId]))
	}
	for _, m := range tbl.LSUpsertMessage {
		queue(b.addMessageWithAttachmentsLocked(m.ThreadKey, m.MessageId, m.Text, m.TimestampMs, m.SenderId, m.IsUnsent, m.ReplySourceId, attachments[m.MessageId]))
	}
	for _, m := range tbl.LSInsertMessage {
		queue(b.addMessageWithAttachmentsLocked(m.ThreadKey, m.MessageId, m.Text, m.TimestampMs, m.SenderId, m.IsUnsent, m.ReplySourceId, attachments[m.MessageId]))
	}
	for messageID, media := range attachments {
		for conversationID, list := range b.messages {
			for i := range list {
				if list[i].ID != messageID || len(media) == 0 || len(list[i].Attachments) != 0 {
					continue
				}
				list[i].Attachments = media
				if list[i].Text == unsupportedMessageText {
					list[i].Text = ""
				}
				b.messages[conversationID] = list
				queue(list[i])
			}
		}
	}
	b.refreshConversationNamesLocked()
	avatarJobs := b.avatarJobsLocked()
	b.recountUnreadLocked()
	b.saveStoredMessengerDataLocked()
	b.mu.Unlock()
	if len(avatarJobs) > 0 {
		go b.fetchAvatars(avatarJobs)
	}
	for _, msg := range publish {
		b.publishMessage(msg)
	}
	b.publishSnapshots()
}
func messengerGroupThread(typ table.ThreadType, memberCount int64) bool {
	switch typ {
	case table.GROUP_THREAD, table.TINCAN_GROUP_DISAPPEARING, table.CARRIER_MESSAGING_GROUP, table.ENCRYPTED_OVER_WA_GROUP:
		return true
	default:
		return memberCount > 2
	}
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
	if name = strings.TrimSpace(name); name != "" {
		b.threadNames[id] = name
	}
	if current, ok := b.convs[key]; ok {
		group = group || current.IsGroup
	}
	// A later, non-hybrid-aware thread row (e.g. a plain inbox refresh) can
	// report a generic type for a thread already known to be an encrypted
	// WhatsApp bridge. Losing that classification would hide historyNotice
	// and make the thread look like ordinary Messenger, so never downgrade.
	if current, ok := b.threadTypes[id]; !ok || !current.IsWhatsApp() || typ.IsWhatsApp() {
		b.threadTypes[id] = typ
	}
	_ = readTS
	if avatar != "" {
		b.threadAvatars[id] = avatar
	}
	name = b.conversationNameLocked(id, group)
	b.convs[key] = wire.Conversation{ID: key, Name: name, Preview: preview, Timestamp: messengerTimestamp(ts), Unread: unread, IsGroup: group, ReadOnly: readOnly, AvatarColor: "#0084ff", Initials: initials(name)}
}

func (b *Backend) addParticipantLocked(thread, contact int64) {
	if thread == 0 || contact == 0 {
		return
	}
	for _, existing := range b.participants[thread] {
		if existing == contact {
			return
		}
	}
	b.participants[thread] = append(b.participants[thread], contact)
}

func (b *Backend) conversationNameLocked(thread int64, group bool) string {
	if name := b.threadNames[thread]; name != "" {
		return name
	}
	if !group {
		contact := thread
		if jid := b.threadToJID[thread]; !jid.IsEmpty() {
			if parsed := parseUser(jid.User); parsed != 0 {
				contact = parsed
			}
		}
		if name := b.contactNames[contact]; name != "" {
			return name
		}
		return "Messenger conversation"
	}
	var names []string
	for _, contact := range b.participants[thread] {
		if contact == b.selfID {
			continue
		}
		if name := b.contactNames[contact]; name != "" {
			names = append(names, name)
		}
	}
	if len(names) > 3 {
		return strings.Join(names[:3], ", ") + " +" + strconv.Itoa(len(names)-3)
	}
	if len(names) > 0 {
		return strings.Join(names, ", ")
	}
	return "Messenger group"
}

func (b *Backend) refreshConversationNamesLocked() {
	for key, conversation := range b.convs {
		thread, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			continue
		}
		conversation.Name = b.conversationNameLocked(thread, conversation.IsGroup)
		conversation.Initials = initials(conversation.Name)
		b.convs[key] = conversation
	}
}
func (b *Backend) setContactNameLocked(id int64, name string) []wire.Message {
	name = strings.TrimSpace(name)
	if id == 0 || name == "" || b.contactNames[id] == name {
		return nil
	}
	b.contactNames[id] = name
	senderID := strconv.FormatInt(id, 10)
	var updated []wire.Message
	for conversationID, list := range b.messages {
		for i := range list {
			if list[i].SenderID != senderID || list[i].SenderName == name {
				continue
			}
			list[i].SenderName = name
			updated = append(updated, list[i])
		}
		b.messages[conversationID] = list
	}
	return updated
}

func (b *Backend) addMessageLocked(thread int64, id, text string, ts, sender int64, deleted bool, reply string) wire.Message {
	return b.addMessageWithAttachmentsLocked(thread, id, text, ts, sender, deleted, reply, nil)
}

func (b *Backend) addMessageWithAttachmentsLocked(thread int64, id, text string, ts, sender int64, deleted bool, reply string, attachments []wire.Attachment) wire.Message {
	if id == "" {
		return wire.Message{}
	}
	if !deleted && strings.TrimSpace(text) == "" && len(attachments) == 0 {
		text = unsupportedMessageText
	}
	key := strconv.FormatInt(thread, 10)
	msg := wire.Message{ID: id, ConversationID: key, Text: text, Timestamp: messengerTimestamp(ts), FromMe: sender == b.selfID, SenderID: strconv.FormatInt(sender, 10), SenderName: b.contactNames[sender], Deleted: deleted, ReplyToID: reply, Attachments: attachments}
	list := b.messages[key]
	for i := range list {
		if list[i].ID == id {
			if len(msg.Attachments) == 0 {
				msg.Attachments = list[i].Attachments
			}
			list[i] = msg
			b.messages[key] = list
			return msg
		}
	}
	b.messages[key] = append(list, msg)
	sort.Slice(b.messages[key], func(i, j int) bool { return b.messages[key][i].Timestamp < b.messages[key][j].Timestamp })
	return msg
}

func (b *Backend) handleE2EEEvent(raw any) {
	switch evt := raw.(type) {
	case *events.FBMessage:
		text, media := fbContent(evt)
		if text == "" && media == nil {
			text = unsupportedMessageText
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
			b.upsertThreadLocked(thread, "", text, evt.Info.Timestamp.UnixMilli(), 0, true, jid.Server == waTypes.GroupServer, false, "", table.ENCRYPTED_OVER_WA_ONE_TO_ONE)
		}
		var attachments []wire.Attachment
		if media != nil {
			media.key = messengerMediaKey(evt.Info.ID, "e2ee", 0)
			b.media[media.key] = media
			attachments = []wire.Attachment{media.attachment()}
		}
		msg := b.addMessageWithAttachmentsLocked(thread, evt.Info.ID, text, evt.Info.Timestamp.UnixMilli(), parseUser(evt.Info.Sender.User), false, "", attachments)
		conv := b.convs[key]
		preview := text
		if preview == "" && media != nil {
			preview = media.name
			if preview == "" {
				preview = "Attachment"
			}
		}
		conv.Preview, conv.Timestamp, conv.Unread = preview, messengerTimeTimestamp(evt.Info.Timestamp), !evt.Info.IsFromMe
		b.convs[key] = conv
		b.recountUnreadLocked()
		b.saveStoredMessengerDataLocked()
		b.mu.Unlock()
		b.publishMessage(msg)
		b.publishSnapshots()
	case *events.Connected:
		b.setState(wire.StateConnected, "")
	case *events.Disconnected:
		b.setState(wire.StateDisconnected, "Messenger encrypted transport disconnected")
	}
}
func messengerTimestamp(milliseconds int64) int64 {
	return milliseconds * int64(time.Millisecond/time.Microsecond)
}
func messengerTimeTimestamp(timestamp time.Time) int64 { return timestamp.UnixMicro() }
func fbText(evt *events.FBMessage) string {
	text, _ := fbContent(evt)
	return text
}

func fbContent(evt *events.FBMessage) (string, *messengerMedia) {
	consumer, ok := evt.Message.(*waConsumerApplication.ConsumerApplication)
	if !ok {
		return "", nil
	}
	content := consumer.GetPayload().GetContent()
	switch v := content.GetContent().(type) {
	case *waConsumerApplication.ConsumerApplication_Content_MessageText:
		return v.MessageText.GetText(), nil
	case *waConsumerApplication.ConsumerApplication_Content_ExtendedTextMessage:
		return v.ExtendedTextMessage.GetText().GetText(), nil
	case *waConsumerApplication.ConsumerApplication_Content_ImageMessage:
		decoded, err := v.ImageMessage.Decode()
		if err == nil {
			return v.ImageMessage.GetCaption().GetText(), mediaFromFB(decoded.GetIntegral().GetTransport(), whatsmeow.MediaImage, true, false, false, false, "")
		}
	case *waConsumerApplication.ConsumerApplication_Content_VideoMessage:
		decoded, err := v.VideoMessage.Decode()
		if err == nil {
			return v.VideoMessage.GetCaption().GetText(), mediaFromFB(decoded.GetIntegral().GetTransport(), whatsmeow.MediaVideo, false, decoded.GetAncillary().GetGifPlayback(), false, true, "")
		}
	case *waConsumerApplication.ConsumerApplication_Content_AudioMessage:
		decoded, err := v.AudioMessage.Decode()
		if err == nil {
			return "", mediaFromFB(decoded.GetIntegral().GetTransport(), whatsmeow.MediaAudio, false, false, true, false, "Voice message")
		}
	case *waConsumerApplication.ConsumerApplication_Content_DocumentMessage:
		decoded, err := v.DocumentMessage.Decode()
		if err == nil {
			return "", mediaFromFB(decoded.GetIntegral().GetTransport(), whatsmeow.MediaDocument, false, false, false, false, v.DocumentMessage.GetFileName())
		}
	case *waConsumerApplication.ConsumerApplication_Content_StickerMessage:
		decoded, err := v.StickerMessage.Decode()
		if err == nil {
			return "", mediaFromFB(decoded.GetIntegral().GetTransport(), whatsmeow.MediaImage, true, false, false, false, "Sticker")
		}
	}
	return "", nil
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

func (b *Backend) publishMessage(msg wire.Message) {
	if b.publish == nil || msg.ID == "" {
		return
	}
	b.publish(wire.Event{Event: wire.EventMessage, Network: wire.NetworkMessenger, Data: msg})
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
	threadType := b.threadTypes[thread]
	b.mu.RUnlock()
	if cli != nil && !threadType.IsWhatsApp() {
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
	result := wire.MessagesResult{ConversationID: p.ConversationID, Messages: list}
	if threadType.IsWhatsApp() {
		result.HistoryNotice = encryptedHistoryNotice
	}
	return result, nil
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
	msg := wire.Message{TmpID: p.TmpID, ID: id, ConversationID: p.ConversationID, Text: text, Timestamp: messengerTimeTimestamp(ts), FromMe: true, SenderID: strconv.FormatInt(self, 10), Delivery: wire.DeliverySent}
	b.mu.Lock()
	b.addMessageLocked(thread, id, text, ts.UnixMilli(), self, false, p.ReplyToID)
	if c, ok := b.convs[p.ConversationID]; ok {
		c.Preview = text
		c.PreviewMine = true
		c.Timestamp = msg.Timestamp
		b.convs[p.ConversationID] = c
	}
	b.saveStoredMessengerDataLocked()
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
		b.saveStoredMessengerDataLocked()
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
