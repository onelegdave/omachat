package whatsapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	_ "github.com/mattn/go-sqlite3"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

const (
	maxConversations       = 50
	maxInboundMediaBytes   = 25 * 1024 * 1024 // 25 MB
	maxOutboundMediaBytes  = 16 * 1024 * 1024 // 16 MB
	maxImageDimensionPixel = 8192
)

// Backend manages the WhatsApp client, local state, and protocol isolation.
type Backend struct {
	log     zerolog.Logger
	paths   *appStore.Paths
	publish func(wire.Event)

	mu        sync.RWMutex
	sessionMu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc

	pairCancel context.CancelFunc

	container *sqlstore.Container
	client    Client
	paired    bool
	status    wire.Status
	gen       uint64
	handlerID uint32

	convs    map[string]wire.Conversation
	order    []string
	messages map[string][]wire.Message
	rawMsgs  map[string]*waE2E.Message

	pairBlocked bool

	// mediaCommitStall is a test hook invoked after a download and before the
	// generation-checked cache commit.
	mediaCommitStall func()
}

// New creates an unstarted WhatsApp backend.
func New(log zerolog.Logger, paths *appStore.Paths, publish func(wire.Event)) *Backend {
	return &Backend{
		log:      log.With().Str("network", wire.NetworkWhatsApp).Logger(),
		paths:    paths,
		publish:  publish,
		convs:    make(map[string]wire.Conversation),
		messages: make(map[string][]wire.Message),
		rawMsgs:  make(map[string]*waE2E.Message),
		status: wire.Status{
			Network: wire.NetworkWhatsApp,
			State:   wire.StateUnpaired,
			PhoneOK: true,
		},
	}
}

// ensureSQLiteFile guarantees the sqlite file exists with private 0600 permissions before opening.
func ensureSQLiteFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	_ = f.Chmod(0o600)
	return f.Close()
}

// mediaKeyToOpaque hashes a media key to an opaque hex string to prevent path traversal or glob escapes.
func mediaKeyToOpaque(key string) string {
	h := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", h)
}

// SetClient overrides the WhatsApp client instance. Primarily used by unit tests
// to supply a synthetic test seam. The registered handler captures the session
// generation so a later Unpair cannot be resurrected by the retired client.
func (b *Backend) SetClient(c Client, paired bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gen++
	b.bindClientLocked(c, paired)
}

// bindClientLocked stores c as the active client and registers an event handler
// that captures the current generation. Caller must hold b.mu.
func (b *Backend) bindClientLocked(c Client, paired bool) {
	b.client = c
	b.paired = paired
	b.handlerID = 0
	if c == nil {
		return
	}
	gen := b.gen
	b.handlerID = c.AddEventHandler(func(evt any) {
		b.handleEventFor(gen, evt)
	})
}

// SetState surfaces state updates to the daemon.
func (b *Backend) SetState(state wire.ConnState, errStr string) {
	b.setState(state, errStr)
}

// Start initializes the database store and connects if a paired session exists.
func (b *Backend) Start(ctx context.Context) error {
	b.sessionMu.Lock()
	defer b.sessionMu.Unlock()

	b.ctx, b.cancel = context.WithCancel(ctx)

	// Load persisted chat store from disk, including media protobufs needed
	// to download attachments after a restart. View-once payloads are omitted.
	storePath := b.paths.WhatsAppStoreFile()
	if loaded, err := loadChatStore(storePath); err != nil {
		b.log.Warn().Err(err).Msg("Failed to load WhatsApp chat store; starting with empty state")
	} else {
		raw := restoreRawMedia(loaded.RawMedia)
		b.mu.Lock()
		b.convs = loaded.Conversations
		b.order = loaded.Order
		b.messages = loaded.Messages
		b.rawMsgs = raw
		b.mu.Unlock()
	}

	b.mu.RLock()
	hasClient := b.client != nil
	paired := b.paired
	cli := b.client
	gen := b.gen
	b.mu.RUnlock()

	if hasClient {
		if paired {
			b.setState(wire.StateConnecting, "")
			go b.connectGeneration(cli, gen)
		} else {
			b.setState(wire.StateUnpaired, "")
		}
		return nil
	}

	dbPath := b.paths.WhatsAppDBFile()
	if err := ensureSQLiteFile(dbPath); err != nil {
		b.setState(wire.StateDisconnected, err.Error())
		return fmt.Errorf("create whatsapp db file: %w", err)
	}

	container, err := sqlstore.New(b.ctx, "sqlite3", "file:"+dbPath+"?_foreign_keys=on", waLog.Zerolog(b.log))
	if err != nil {
		b.setState(wire.StateDisconnected, err.Error())
		return fmt.Errorf("open whatsapp db: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(b.ctx)
	if err != nil {
		_ = container.Close()
		b.setState(wire.StateDisconnected, err.Error())
		return fmt.Errorf("get whatsapp device: %w", err)
	}
	if deviceStore == nil {
		deviceStore = container.NewDevice()
	}

	live := whatsmeow.NewClient(deviceStore, waLog.Zerolog(b.log))

	// Detect persisted pairing via deviceStore.ID != nil. IsLoggedIn() is
	// live authentication only and is false on a fresh client with saved keys.
	isPaired := deviceStore.ID != nil

	b.mu.Lock()
	b.gen++
	b.container = container
	b.bindClientLocked(live, isPaired)
	gen = b.gen
	b.mu.Unlock()

	if !isPaired {
		b.setState(wire.StateUnpaired, "")
		b.log.Info().Msg("No stored WhatsApp session; waiting for QR pairing")
		return nil
	}

	b.setState(wire.StateConnecting, "")
	go b.connectGeneration(live, gen)

	return nil
}

func (b *Backend) connectGeneration(c Client, gen uint64) {
	if c == nil {
		return
	}
	if err := c.Connect(); err != nil {
		if b.commitStatus(gen, wire.StateDisconnected, err.Error()) {
			b.log.Warn().Err(err).Msg("WhatsApp reconnect failed")
		}
	}
}

func (b *Backend) generationCurrent(gen uint64) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.gen == gen
}

// Stop cleanly disconnects the WhatsApp client and releases resources.
func (b *Backend) Stop() {
	b.sessionMu.Lock()
	defer b.sessionMu.Unlock()

	b.mu.Lock()
	b.gen++
	if b.cancel != nil {
		b.cancel()
	}
	if b.pairCancel != nil {
		b.pairCancel()
	}
	cli := b.client
	container := b.container
	b.mu.Unlock()

	if cli != nil {
		cli.Disconnect()
	}
	if container != nil {
		_ = container.Close()
	}
}

func (b *Backend) initClientAndStoreLocked() error {
	if b.client != nil {
		return nil
	}
	dbPath := b.paths.WhatsAppDBFile()
	if err := ensureSQLiteFile(dbPath); err != nil {
		return fmt.Errorf("create whatsapp db file: %w", err)
	}
	container, err := sqlstore.New(b.ctx, "sqlite3", "file:"+dbPath+"?_foreign_keys=on", waLog.Zerolog(b.log))
	if err != nil {
		return fmt.Errorf("open whatsapp db: %w", err)
	}
	deviceStore, err := container.GetFirstDevice(b.ctx)
	if err != nil {
		_ = container.Close()
		return fmt.Errorf("get whatsapp device: %w", err)
	}
	if deviceStore == nil {
		deviceStore = container.NewDevice()
	}
	cli := whatsmeow.NewClient(deviceStore, waLog.Zerolog(b.log))
	b.gen++
	b.container = container
	b.bindClientLocked(cli, deviceStore.ID != nil)
	return nil
}

// StartPairing starts the explicit QR pairing flow and yields the QR pairing code.
// Returns an error immediately if the account is already paired; the caller
// must Unpair first before requesting a new QR session.
//
// All early failures set UNPAIRED state with a user-visible error so PairingView
// shows the retry button even when the RPC callback is null. Each failure also
// increments the generation so any in-flight goroutine from that attempt sees
// the mismatch and stops, guaranteeing the next explicit retry gets a fresh
// handler with no stale QR callbacks.
func (b *Backend) StartPairing(ctx context.Context) (string, error) {
	b.sessionMu.Lock()
	defer b.sessionMu.Unlock()

	if b.pairCancel != nil {
		b.pairCancel()
		b.pairCancel = nil
	}

	b.mu.Lock()
	if b.paired {
		b.mu.Unlock()
		return "", errors.New("already paired; unpair first before starting a new QR session")
	}
	if b.pairBlocked {
		b.mu.Unlock()
		if err := b.paths.ClearWhatsAppSession(); err != nil {
			b.setState(wire.StateUnpaired, "Cannot pair: leftover WhatsApp files could not be removed. Try again or restart.")
			return "", fmt.Errorf("cannot pair: leftover WhatsApp files could not be removed: %w", err)
		}
		b.mu.Lock()
		b.pairBlocked = false
	}
	if err := b.initClientAndStoreLocked(); err != nil {
		b.mu.Unlock()
		b.setState(wire.StateUnpaired, "Could not open WhatsApp database. Select Use a QR code to try again.")
		return "", fmt.Errorf("init whatsapp client: %w", err)
	}
	cli := b.client
	gen := b.gen
	b.mu.Unlock()

	if cli == nil {
		b.setState(wire.StateUnpaired, "WhatsApp client not ready. Select Use a QR code to try again.")
		return "", errors.New("whatsapp client not initialized")
	}

	pairCtx, pairCancel := context.WithCancel(b.ctx)
	b.pairCancel = pairCancel

	// pairingFailed is called on every early exit after pairCtx is live.
	// It cancels the context, disconnects the client if it was connected only
	// for pairing, retires the generation (so any goroutine using the old gen
	// sees a mismatch and stops), and publishes a retryable UNPAIRED status.
	pairingFailed := func(userMsg string, retErr error) (string, error) {
		pairCancel()
		b.pairCancel = nil
		// Disconnect only if not yet paired (pairing attempt cleanup).
		if cli != nil && cli.IsConnected() {
			cli.Disconnect()
		}
		// Retire generation so stale QR goroutines from this attempt cannot
		// overwrite the new state after the next StartPairing call.
		b.mu.Lock()
		oldHandler := b.handlerID
		if b.gen == gen {
			b.gen++
		}
		b.handlerID = 0
		b.mu.Unlock()
		// Removal waits for upstream callbacks: never hold our state lock here.
		if oldHandler != 0 {
			cli.RemoveEventHandler(oldHandler)
		}
		b.mu.Lock()
		b.bindClientLocked(cli, false)
		b.status.State = wire.StateUnpaired
		b.status.Error = userMsg
		b.status.QRURL = ""
		st := b.status
		st.Unread = b.unreadCountLocked()
		b.emitLocked(wire.EventStatus, st)
		b.mu.Unlock()
		b.log.Warn().Err(retErr).Msg("StartPairing failed; returned to unpaired")
		return "", retErr
	}

	qrChan, err := cli.GetQRChannel(pairCtx)
	if err != nil {
		return pairingFailed(
			"Could not start WhatsApp QR session. Select Use a QR code to try again.",
			fmt.Errorf("get qr channel: %w", err),
		)
	}

	if !cli.IsConnected() {
		if err := cli.Connect(); err != nil {
			return pairingFailed(
				"Could not connect to WhatsApp. Check your internet connection and select Use a QR code to try again.",
				fmt.Errorf("connect for pairing: %w", err),
			)
		}
	}

	b.setState(wire.StatePairing, "")

	// Wait synchronously for the first QR code to return immediately.
	select {
	case item, ok := <-qrChan:
		if !ok {
			return pairingFailed(
				"WhatsApp QR channel closed unexpectedly. Select Use a QR code to try again.",
				errors.New("qr channel closed prematurely"),
			)
		}
		if item.Event == whatsmeow.QRChannelEventCode {
			b.setQRURL(item.Code)
			go b.listenQRChannel(qrChan, pairCancel, gen)
			return item.Code, nil
		}
		return pairingFailed(
			fmt.Sprintf("WhatsApp QR pairing failed (%s). Select Use a QR code to try again.", item.Event),
			fmt.Errorf("unexpected pairing event: %s", item.Event),
		)
	case <-ctx.Done():
		return pairingFailed(
			"WhatsApp QR pairing was cancelled. Select Use a QR code to try again.",
			ctx.Err(),
		)
	case <-time.After(10 * time.Second):
		return pairingFailed(
			"Timed out waiting for WhatsApp QR code. Select Use a QR code to try again.",
			errors.New("timeout waiting for initial qr code"),
		)
	}
}

func (b *Backend) listenQRChannel(qrChan <-chan whatsmeow.QRChannelItem, cancel context.CancelFunc, gen uint64) {
	defer cancel()
	for item := range qrChan {
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			if !b.commitQR(gen, item.Code) {
				return
			}
		case "success":
			b.mu.Lock()
			if b.gen != gen {
				b.mu.Unlock()
				return
			}
			b.paired = true
			// Only advance to StateConnecting if we are not already fully
			// connected. If a *events.Connected fired before we acquired the
			// lock here, the state is already StateConnected and overwriting
			// it with StateConnecting would regress the UI unnecessarily.
			if b.status.State != wire.StateConnected {
				b.status.State = wire.StateConnecting
			}
			b.status.QRURL = ""
			b.status.Error = ""
			st := b.status
			st.Unread = b.unreadCountLocked()
			// Emit EventStatus and EventPaired under lock while generation is
			// still validated, preventing retirement between unlock and emit.
			b.emitLocked(wire.EventStatus, st)
			b.emitLocked(wire.EventPaired, nil)
			b.mu.Unlock()
			b.log.Info().Msg("WhatsApp QR pairing succeeded")
			return
		case "timeout":
			// Return to unpaired (retryable) so the QR retry button appears in
			// PairingView. StateDisconnected is not in needsPair and hides the
			// button.
			if !b.commitStatus(gen, wire.StateUnpaired, "Pairing timed out. Select Use a QR code to try again.") {
				return
			}
			b.log.Warn().Msg("WhatsApp QR code timed out")
			return
		case "err-client-outdated":
			if !b.commitStatus(gen, wire.StateUnpaired, "WhatsApp client version outdated; update OmaChat.") {
				return
			}
			b.log.Error().Msg("WhatsApp client version outdated")
			return
		default:
			// Generic QR error events (e.g. "err-scan-without-multidevice",
			// unexpected signals): return to unpaired so the retry button shows.
			if !b.commitStatus(gen, wire.StateUnpaired, fmt.Sprintf("QR pairing failed (%s). Select Use a QR code to try again.", item.Event)) {
				return
			}
			b.log.Warn().Str("qr_event", item.Event).Msg("WhatsApp QR pairing failed with unexpected event")
			return
		}
	}
}

func (b *Backend) commitQR(gen uint64, code string) bool {
	b.mu.Lock()
	if b.gen != gen {
		b.mu.Unlock()
		return false
	}
	b.status.State = wire.StatePairing
	b.status.QRURL = code
	b.status.Error = ""
	b.mu.Unlock()
	b.publishStatus()
	return true
}

func (b *Backend) commitStatus(gen uint64, state wire.ConnState, errStr string) bool {
	b.mu.Lock()
	if b.gen != gen {
		b.mu.Unlock()
		return false
	}
	b.status.State = state
	b.status.Error = errStr
	if state == wire.StateUnpaired {
		b.status.QRURL = ""
	}
	b.mu.Unlock()
	b.publishStatus()
	return true
}

// Unpair revokes the linked device over the network while still connected,
// then disconnects, closes the database, and clears local credentials.
// Generation is incremented first so stale callbacks cannot republish state
// or rewrite deleted credentials. The next pairing is explicit only.
func (b *Backend) Unpair(ctx context.Context) error {
	b.sessionMu.Lock()
	defer b.sessionMu.Unlock()

	cli, container, pairCancel, handlerID, paired, _ := b.retireGeneration(0, false)

	if pairCancel != nil {
		pairCancel()
	}

	var logoutErr error
	if cli != nil {
		if paired || cli.IsLoggedIn() || cli.IsConnected() {
			logoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			logoutErr = cli.Logout(logoutCtx)
			cancel()
		}
		cli.Disconnect()
		if handlerID != 0 {
			cli.RemoveEventHandler(handlerID)
		}
	}

	if container != nil {
		_ = container.Close()
	}

	clearErr := b.paths.ClearWhatsAppSession()
	statusErr := ""
	if clearErr != nil {
		b.log.Error().Err(clearErr).Msg("Failed to clear local WhatsApp session files")
		statusErr = "Local WhatsApp files could not be removed. Pairing is blocked until they can be deleted. Google Messages is unchanged."
		b.mu.Lock()
		b.pairBlocked = true
		b.mu.Unlock()
	}
	if logoutErr != nil && statusErr == "" {
		statusErr = "Remote logout unavailable (offline or unreachable); local session cleared. Check WhatsApp on your phone under Linked devices."
	}
	b.setState(wire.StateUnpaired, statusErr)

	if clearErr != nil {
		return fmt.Errorf("local storage cleanup failed: %w", clearErr)
	}
	if logoutErr != nil {
		return fmt.Errorf("remote logout unavailable (offline or unreachable); local session cleared. Check WhatsApp on your phone under Linked devices: %w", logoutErr)
	}
	return nil
}

func (b *Backend) retireGeneration(expect uint64, match bool) (cli Client, container *sqlstore.Container, pairCancel context.CancelFunc, handlerID uint32, paired bool, ok bool) {
	b.mu.Lock()
	if match && b.gen != expect {
		b.mu.Unlock()
		return
	}
	b.gen++
	cli = b.client
	container = b.container
	pairCancel = b.pairCancel
	handlerID = b.handlerID
	paired = b.paired
	b.pairCancel = nil
	b.paired = false
	b.client = nil
	b.handlerID = 0
	b.container = nil
	b.convs = make(map[string]wire.Conversation)
	b.order = nil
	b.messages = make(map[string][]wire.Message)
	b.rawMsgs = make(map[string]*waE2E.Message)
	b.mu.Unlock()
	ok = true
	return
}

func (b *Backend) applyRemoteLogout(gen uint64, reason string) {
	b.sessionMu.Lock()
	defer b.sessionMu.Unlock()

	cli, container, pairCancel, handlerID, _, ok := b.retireGeneration(gen, true)
	if !ok {
		return
	}
	if pairCancel != nil {
		pairCancel()
	}
	if cli != nil {
		cli.Disconnect()
		if handlerID != 0 {
			cli.RemoveEventHandler(handlerID)
		}
	}
	if container != nil {
		_ = container.Close()
	}
	statusErr := "Logged out from phone. Select Use a QR code to pair again."
	if reason != "" {
		statusErr = "Logged out from phone (" + reason + "). Select Use a QR code to pair again."
	}
	if err := b.paths.ClearWhatsAppSession(); err != nil {
		b.log.Error().Err(err).Msg("Failed to clear local WhatsApp session files after remote logout")
		statusErr = "Logged out from phone, but local WhatsApp files could not be removed. Pairing is blocked until they can be deleted."
		b.mu.Lock()
		b.pairBlocked = true
		b.mu.Unlock()
	}
	b.setState(wire.StateUnpaired, statusErr)
}

// Status returns the current WhatsApp connection state and unread count.
func (b *Backend) Status() wire.Status {
	b.mu.RLock()
	defer b.mu.RUnlock()
	st := b.status
	st.Unread = b.unreadCountLocked()
	return st
}

// Conversations returns a defensive copy of the active WhatsApp conversation list.
func (b *Backend) Conversations(count int) []wire.Conversation {
	b.mu.RLock()
	defer b.mu.RUnlock()

	limit := count
	if limit <= 0 {
		limit = len(b.order)
	}
	out := make([]wire.Conversation, 0, min(limit, len(b.order)))
	for _, id := range b.order {
		if len(out) >= limit {
			break
		}
		// Companion history can include contact stubs with no messages. Keep
		// those out of the inbox until a real message creates the chat.
		if len(b.messages[id]) == 0 {
			continue
		}
		out = append(out, b.convs[id])
	}
	return out
}

// Messages returns cached and synced messages for a WhatsApp chat with stable timestamp+ID tie breaking.
func (b *Backend) Messages(ctx context.Context, p wire.MessagesParams) (wire.MessagesResult, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	msgs := b.messages[p.ConversationID]
	limit := int(p.Count)
	if limit <= 0 {
		limit = 60
	}

	var filtered []wire.Message
	if p.CursorTime > 0 {
		for _, m := range msgs {
			if m.Timestamp < p.CursorTime || (m.Timestamp == p.CursorTime && p.CursorID != "" && m.ID < p.CursorID) {
				filtered = append(filtered, m)
			}
		}
	} else {
		filtered = append(filtered, msgs...)
	}

	// Slice newest messages up to limit
	start := len(filtered) - limit
	if start < 0 {
		start = 0
	}
	slice := filtered[start:]
	resMsgs := make([]wire.Message, len(slice))
	copy(resMsgs, slice)

	var cursorID string
	var cursorTime int64
	if len(resMsgs) > 0 {
		cursorID = resMsgs[0].ID
		cursorTime = resMsgs[0].Timestamp
	}

	// OmaChat initial WhatsApp implementation pages from cached synced history.
	// On-demand phone history sync requests are not yet implemented.
	hasMore := start > 0

	return wire.MessagesResult{
		ConversationID: p.ConversationID,
		Messages:       resMsgs,
		CursorID:       cursorID,
		CursorTime:     cursorTime,
		HasMore:        hasMore,
	}, nil
}

// Send sends a plain text message to a WhatsApp chat.
func (b *Backend) Send(ctx context.Context, p wire.SendParams) (wire.Message, error) {
	b.mu.RLock()
	cli := b.client
	connected := b.status.State == wire.StateConnected
	gen := b.gen
	b.mu.RUnlock()

	if !connected || cli == nil {
		return wire.Message{}, errors.New("not connected to WhatsApp")
	}

	toJID, err := types.ParseJID(p.ConversationID)
	if err != nil {
		return wire.Message{}, fmt.Errorf("invalid recipient JID: %w", err)
	}
	if toJID.Server != types.DefaultUserServer && toJID.Server != types.GroupServer && toJID.Server != "lid" {
		return wire.Message{}, fmt.Errorf("unsupported recipient server: %s", toJID.Server)
	}

	waMsg := &waE2E.Message{}
	if p.ReplyToID != "" {
		b.mu.RLock()
		rawQuoted := b.rawMsgs[rawMediaKey(p.ConversationID, p.ReplyToID)]
		if rawQuoted == nil {
			rawQuoted = b.rawMsgs[p.ReplyToID]
		}
		var quotedParticipant string
		if rawQuoted == nil {
			for _, m := range b.messages[p.ConversationID] {
				if m.ID == p.ReplyToID {
					quotedParticipant = m.SenderID
					break
				}
			}
		}
		b.mu.RUnlock()

		if rawQuoted == nil && quotedParticipant == "" {
			return wire.Message{}, fmt.Errorf("reply target message %q not found", p.ReplyToID)
		}

		waMsg.ExtendedTextMessage = &waE2E.ExtendedTextMessage{
			Text: proto.String(p.Text),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:      proto.String(p.ReplyToID),
				Participant:   proto.String(quotedParticipant),
				QuotedMessage: rawQuoted,
			},
		}
	} else {
		waMsg.Conversation = proto.String(p.Text)
	}

	resp, err := cli.SendMessage(ctx, toJID, waMsg)
	if err != nil {
		return wire.Message{}, fmt.Errorf("send message: %w", err)
	}

	timestamp := resp.Timestamp.UnixMicro()
	if timestamp == 0 {
		timestamp = time.Now().UnixMicro()
	}

	out := wire.Message{
		ID:             resp.ID,
		TmpID:          p.TmpID,
		ConversationID: p.ConversationID,
		Text:           p.Text,
		Timestamp:      timestamp,
		FromMe:         true,
		Delivery:       wire.DeliverySent,
		ReplyToID:      p.ReplyToID,
	}

	if !b.commitMessage(gen, out, waMsg) {
		return wire.Message{}, errors.New("session changed during send")
	}
	return out, nil
}

// SendMedia uploads an image file and sends it to a WhatsApp chat.
func (b *Backend) SendMedia(ctx context.Context, p wire.SendMediaParams) (wire.SendMediaResult, error) {
	b.mu.RLock()
	cli := b.client
	connected := b.status.State == wire.StateConnected
	gen := b.gen
	b.mu.RUnlock()

	if !connected || cli == nil {
		return wire.SendMediaResult{}, errors.New("not connected to WhatsApp")
	}

	cleanPath := filepath.Clean(p.Path)
	fi, err := os.Stat(cleanPath)
	if err != nil {
		return wire.SendMediaResult{}, fmt.Errorf("stat media file: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return wire.SendMediaResult{}, errors.New("media file is not a regular file")
	}
	if fi.Size() > maxOutboundMediaBytes {
		return wire.SendMediaResult{}, errors.New("media file exceeds 16MB limit")
	}

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return wire.SendMediaResult{}, fmt.Errorf("read media file: %w", err)
	}

	// Validate exact supported image types and dimensions
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		// Allow webp magic detection if standard decoder lacks it
		if bytes.HasPrefix(data, []byte("RIFF")) && len(data) >= 12 && string(data[8:12]) == "WEBP" {
			format = "webp"
		} else {
			return wire.SendMediaResult{}, fmt.Errorf("unsupported image format: %w", err)
		}
	} else {
		if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxImageDimensionPixel || cfg.Height > maxImageDimensionPixel {
			return wire.SendMediaResult{}, fmt.Errorf("invalid image dimensions: %dx%d", cfg.Width, cfg.Height)
		}
	}

	mimeType := "image/" + format
	if format == "jpg" {
		mimeType = "image/jpeg"
	}

	toJID, err := types.ParseJID(p.ConversationID)
	if err != nil {
		return wire.SendMediaResult{}, fmt.Errorf("invalid recipient JID: %w", err)
	}
	if toJID.Server != types.DefaultUserServer && toJID.Server != types.GroupServer && toJID.Server != "lid" {
		return wire.SendMediaResult{}, fmt.Errorf("unsupported recipient server: %s", toJID.Server)
	}

	uploadResp, err := cli.Upload(ctx, data, whatsmeow.MediaImage)
	if err != nil {
		return wire.SendMediaResult{}, fmt.Errorf("upload image: %w", err)
	}

	waMsg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			Caption:       proto.String(p.Caption),
			Mimetype:      proto.String(mimeType),
			URL:           &uploadResp.URL,
			DirectPath:    &uploadResp.DirectPath,
			MediaKey:      uploadResp.MediaKey,
			FileEncSHA256: uploadResp.FileEncSHA256,
			FileSHA256:    uploadResp.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(data))),
		},
	}

	resp, err := cli.SendMessage(ctx, toJID, waMsg)
	if err != nil {
		return wire.SendMediaResult{}, fmt.Errorf("send media: %w", err)
	}

	timestamp := resp.Timestamp.UnixMicro()
	if timestamp == 0 {
		timestamp = time.Now().UnixMicro()
	}

	out := wire.Message{
		ID:             resp.ID,
		TmpID:          p.TmpID,
		ConversationID: p.ConversationID,
		Text:           p.Caption,
		Timestamp:      timestamp,
		FromMe:         true,
		Delivery:       wire.DeliverySent,
		Attachments: []wire.Attachment{
			{
				Key:      rawMediaKey(p.ConversationID, resp.ID),
				MediaID:  resp.ID,
				MimeType: mimeType,
				Size:     int64(len(data)),
				IsImage:  true,
			},
		},
	}

	if !b.commitMessage(gen, out, waMsg) {
		return wire.SendMediaResult{}, errors.New("session changed during send media")
	}

	return wire.SendMediaResult{
		Message: &out,
	}, nil
}

// Media retrieves or downloads an attachment file into the local WhatsApp cache.
func (b *Backend) Media(ctx context.Context, p wire.MediaParams) (wire.MediaResult, error) {
	key := p.Key
	if key == "" {
		key = p.MediaID
	}
	if key == "" {
		return wire.MediaResult{}, errors.New("empty media key")
	}

	cacheDir := b.paths.WhatsAppMediaDir()
	opaque := mediaKeyToOpaque(key)

	// Check for existing cached file safely without globbing user input
	for _, ext := range []string{".jpg", ".png", ".webp", ".gif", ".bin"} {
		candidate := filepath.Join(cacheDir, opaque+ext)
		fi, err := os.Lstat(candidate)
		if err != nil || fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		return wire.MediaResult{Key: key, Path: candidate}, nil
	}

	b.mu.RLock()
	cli := b.client
	rawMsg := b.rawMsgs[key]
	gen := b.gen
	b.mu.RUnlock()

	if rawMsg == nil {
		return wire.MediaResult{}, errors.New("unauthorized or unknown media key")
	}
	if isViewOnce(rawMsg) || isEphemeralWrapped(rawMsg) {
		return wire.MediaResult{}, errors.New("view-once media cannot be cached or reopened")
	}
	if cli == nil {
		return wire.MediaResult{}, errors.New("whatsapp client not available")
	}

	dl, declared := downloadableMedia(rawMsg)
	if dl == nil {
		return wire.MediaResult{}, errors.New("unauthorized or unknown media key")
	}
	if declared > uint64(maxInboundMediaBytes) {
		return wire.MediaResult{}, errInboundMediaTooLarge
	}

	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return wire.MediaResult{}, fmt.Errorf("create media cache: %w", err)
	}
	tmp, err := os.CreateTemp(cacheDir, ".dl-*")
	if err != nil {
		return wire.MediaResult{}, fmt.Errorf("create media tempfile: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	capped := &cappedFile{File: tmp, max: maxInboundMediaBytes}
	if err := cli.DownloadToFile(ctx, dl, capped); err != nil {
		_ = tmp.Close()
		return wire.MediaResult{}, fmt.Errorf("download media: %w", err)
	}
	if _, err := tmp.Seek(0, 0); err != nil {
		_ = tmp.Close()
		return wire.MediaResult{}, err
	}
	head := make([]byte, 512)
	n, _ := tmp.Read(head)
	_ = tmp.Close()

	mimeType := http.DetectContentType(head[:n])
	ext := ".bin"
	switch {
	case strings.Contains(mimeType, "jpeg"):
		ext = ".jpg"
	case strings.Contains(mimeType, "png"):
		ext = ".png"
	case strings.Contains(mimeType, "webp"):
		ext = ".webp"
	case strings.Contains(mimeType, "gif"):
		ext = ".gif"
	}
	if ext == ".bin" {
		inner, _ := unwrapMessage(rawMsg)
		if inner != nil && inner.GetStickerMessage() != nil {
			m := strings.ToLower(inner.GetStickerMessage().GetMimetype())
			if strings.Contains(m, "webp") || m == "" {
				ext = ".webp"
			}
		}
	}

	if stall := b.mediaCommitStall; stall != nil {
		stall()
	}

	targetPath := filepath.Join(cacheDir, opaque+ext)
	if rel, err := filepath.Rel(cacheDir, targetPath); err != nil || strings.HasPrefix(rel, "..") {
		return wire.MediaResult{}, errors.New("refusing media path outside the WhatsApp cache")
	}

	b.mu.Lock()
	if b.gen != gen {
		b.mu.Unlock()
		return wire.MediaResult{}, errors.New("download aborted: session changed")
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		b.mu.Unlock()
		return wire.MediaResult{}, fmt.Errorf("write cached media: %w", err)
	}
	_ = os.Chmod(targetPath, 0o600)
	if err := appStore.PruneMedia(cacheDir, targetPath); err != nil {
		b.log.Warn().Err(err).Msg("Could not trim WhatsApp media")
	}
	b.mu.Unlock()

	return wire.MediaResult{Key: key, Path: targetPath}, nil
}

// MarkRead marks messages as read for a given conversation.
func (b *Backend) MarkRead(ctx context.Context, p wire.MarkReadParams) error {
	b.mu.Lock()
	conv, ok := b.convs[p.ConversationID]
	if ok {
		conv.Unread = false
		b.convs[p.ConversationID] = conv
		b.saveStoreLocked()
	}
	cli := b.client
	var senderJID types.JID
	if p.MessageID != "" {
		for _, m := range b.messages[p.ConversationID] {
			if m.ID == p.MessageID && m.SenderID != "" {
				senderJID, _ = types.ParseJID(m.SenderID)
				break
			}
		}
	}
	b.mu.Unlock()

	if ok {
		b.emit(wire.EventConversation, conv)
		b.publishStatus()
	}

	if cli != nil && cli.IsConnected() && p.MessageID != "" {
		chatJID, err := types.ParseJID(p.ConversationID)
		if err != nil {
			return fmt.Errorf("invalid conversation JID: %w", err)
		}
		if senderJID.IsEmpty() || chatJID.Server != types.GroupServer {
			senderJID = chatJID
		}
		if err := cli.MarkRead(ctx, []types.MessageID{p.MessageID}, time.Now(), chatJID, senderJID); err != nil {
			return fmt.Errorf("mark read: %w", err)
		}
	}
	return nil
}

// Refresh triggers an explicit connection or conversation synchronization check.
func (b *Backend) Refresh(ctx context.Context) error {
	b.mu.RLock()
	cli := b.client
	paired := b.paired
	b.mu.RUnlock()

	if !paired || cli == nil {
		return nil
	}

	if !cli.IsConnected() {
		return cli.Connect()
	}
	return nil
}

func (b *Backend) handleEvent(evt any) {
	b.mu.RLock()
	gen := b.gen
	b.mu.RUnlock()
	b.handleEventFor(gen, evt)
}

func (b *Backend) handleEventFor(gen uint64, evt any) {
	switch e := evt.(type) {
	case *events.Connected:
		b.mu.Lock()
		if b.gen != gen {
			b.mu.Unlock()
			return
		}
		b.status.State = wire.StateConnected
		b.status.Error = ""
		b.mu.Unlock()
		b.log.Info().Msg("WhatsApp connected")
		b.publishStatus()

	case *events.Disconnected:
		b.mu.Lock()
		if b.gen != gen || !b.paired {
			b.mu.Unlock()
			return
		}
		b.status.State = wire.StateDisconnected
		b.status.Error = "WhatsApp disconnected"
		b.mu.Unlock()
		b.log.Info().Msg("WhatsApp disconnected")
		b.publishStatus()

	case *events.LoggedOut:
		b.log.Info().Str("reason", e.Reason.String()).Msg("WhatsApp remote logout received")
		go b.applyRemoteLogout(gen, e.Reason.String())

	case *events.Message:
		msg, displayable := convertEventMessage(e)
		if !displayable {
			return
		}
		raw := e.Message
		if restrictedLifetime(e, e.Message) {
			raw = nil
		}
		b.commitMessage(gen, msg, raw)

	case *events.Receipt:
		b.handleReceipt(gen, e)

	case *events.HistorySync:
		b.ingestHistorySync(gen, e.Data)
	}
}

func (b *Backend) appendMessage(msg wire.Message, raw *waE2E.Message) {
	b.mu.Lock()
	gen := b.gen
	b.mu.Unlock()
	b.commitMessage(gen, msg, raw)
}

func (b *Backend) commitMessage(gen uint64, msg wire.Message, raw *waE2E.Message) bool {
	b.mu.Lock()
	if b.gen != gen {
		b.mu.Unlock()
		return false
	}
	if raw != nil && msg.ID != "" && !isViewOnce(raw) && !isEphemeralWrapped(raw) {
		b.rawMsgs[rawMediaKey(msg.ConversationID, msg.ID)] = raw
	}

	list := b.messages[msg.ConversationID]
	found := false
	for i, m := range list {
		if m.ID == msg.ID || (msg.TmpID != "" && m.TmpID == msg.TmpID) {
			list[i] = msg
			found = true
			break
		}
	}
	if !found {
		list = append(list, msg)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Timestamp != list[j].Timestamp {
			return list[i].Timestamp < list[j].Timestamp
		}
		return list[i].ID < list[j].ID
	})
	if len(list) > maxPersistedMessages {
		list = list[len(list)-maxPersistedMessages:]
	}
	b.messages[msg.ConversationID] = list

	// Update conversation entry
	conv, ok := b.convs[msg.ConversationID]
	if !ok {
		jid, _ := types.ParseJID(msg.ConversationID)
		name := formatConversationName(jid, msg.SenderName)
		conv = wire.Conversation{
			ID:          msg.ConversationID,
			Name:        name,
			AvatarColor: avatarColor(msg.ConversationID),
			Initials:    initials(name),
			IsGroup:     jid.Server == types.GroupServer,
		}
	}

	conv.Preview = msg.Text
	if conv.Preview == "" && len(msg.Attachments) > 0 {
		conv.Preview = "Attachment"
	}
	conv.PreviewMine = msg.FromMe
	conv.Timestamp = msg.Timestamp
	if !msg.FromMe {
		conv.Unread = true
	}

	b.convs[msg.ConversationID] = conv
	b.reorderLocked()
	b.saveStoreLocked()
	updatedConv := b.convs[msg.ConversationID]
	st := b.status
	st.Unread = b.unreadCountLocked()
	// Publish all three events under the lock before releasing. b.publish is a
	// non-blocking channel enqueue so this does not risk deadlock. Publishing
	// here closes the retirement race: Unpair increments gen and clears maps
	// under the same lock, so it cannot observe these emits after retiring.
	b.emitLocked(wire.EventMessage, msg)
	b.emitLocked(wire.EventConversation, updatedConv)
	b.emitLocked(wire.EventStatus, st)
	b.mu.Unlock()
	return true
}

func (b *Backend) ingestHistorySync(gen uint64, data *waHistorySync.HistorySync) {
	if data == nil {
		return
	}

	b.mu.Lock()
	if b.gen != gen {
		b.mu.Unlock()
		return
	}

	for _, c := range data.GetConversations() {
		chatID := c.GetID()
		if chatID == "" {
			continue
		}
		jid, _ := types.ParseJID(chatID)
		name := formatConversationName(jid, c.GetName())
		conv, ok := b.convs[chatID]
		if !ok {
			conv = wire.Conversation{
				ID:          chatID,
				Name:        name,
				AvatarColor: avatarColor(chatID),
				Initials:    initials(name),
				IsGroup:     jid.Server == types.GroupServer,
				Unread:      c.GetUnreadCount() > 0,
			}
		}

		for _, hMsg := range c.GetMessages() {
			webMsg := hMsg.GetMessage()
			if webMsg == nil || webMsg.GetMessage() == nil {
				continue
			}
			raw := webMsg.GetMessage()
			unwrapped, _ := unwrapMessage(raw)
			id := webMsg.GetKey().GetID()
			restricted := isViewOnce(raw) || isEphemeralWrapped(raw)
			var text string
			var atts []wire.Attachment
			if restricted {
				text = lifetimePlaceholder()
			} else {
				text = extractText(unwrapped)
				atts = extractAttachments(raw, chatID, id)
			}

			if !isDisplayableMessage(unwrapped, text, atts) {
				continue
			}

			fromMe := webMsg.GetKey().GetFromMe()
			ts := int64(webMsg.GetMessageTimestamp()) * 1000000 // Convert sec to microsec
			if ts == 0 {
				ts = 1
			}

			m := wire.Message{
				ID:             id,
				ConversationID: chatID,
				Text:           text,
				Timestamp:      ts,
				FromMe:         fromMe,
				Attachments:    atts,
			}
			if fromMe {
				m.Delivery = wire.DeliverySent
			}

			if id != "" && raw != nil && !restricted {
				b.rawMsgs[rawMediaKey(chatID, id)] = raw
			}

			existing := b.messages[chatID]
			dup := false
			for _, ex := range existing {
				if ex.ID == id {
					dup = true
					break
				}
			}
			if !dup {
				existing = append(existing, m)
				b.messages[chatID] = existing
			}

			if ts > conv.Timestamp {
				conv.Timestamp = ts
				conv.Preview = text
				if conv.Preview == "" && len(atts) > 0 {
					conv.Preview = "Attachment"
				}
				conv.PreviewMine = fromMe
			}
		}

		sort.Slice(b.messages[chatID], func(i, j int) bool {
			if b.messages[chatID][i].Timestamp != b.messages[chatID][j].Timestamp {
				return b.messages[chatID][i].Timestamp < b.messages[chatID][j].Timestamp
			}
			return b.messages[chatID][i].ID < b.messages[chatID][j].ID
		})
		if len(b.messages[chatID]) > maxPersistedMessages {
			b.messages[chatID] = b.messages[chatID][len(b.messages[chatID])-maxPersistedMessages:]
		}

		b.convs[chatID] = conv
	}

	b.reorderLocked()
	b.saveStoreLocked()

	list := make([]wire.Conversation, 0, len(b.order))
	for _, id := range b.order {
		list = append(list, b.convs[id])
	}
	st := b.status
	st.Unread = b.unreadCountLocked()
	// Emit all events under lock to prevent Unpair retiring the account
	// between the unlock and the emit calls.
	for _, c := range list {
		b.emitLocked(wire.EventConversation, c)
	}
	b.emitLocked(wire.EventStatus, st)
	b.mu.Unlock()
}

func (b *Backend) handleReceipt(gen uint64, evt *events.Receipt) {
	b.mu.Lock()
	if b.gen != gen {
		b.mu.Unlock()
		return
	}

	chatJID := evt.Chat.String()
	msgs, ok := b.messages[chatJID]
	if !ok {
		b.mu.Unlock()
		return
	}

	receiptState := wire.DeliveryDelivered
	if evt.Type == types.ReceiptTypeRead || evt.Type == types.ReceiptTypeReadSelf {
		receiptState = wire.DeliveryRead
	}

	var updated []wire.Message
	targetIDs := make(map[string]struct{}, len(evt.MessageIDs))
	for _, id := range evt.MessageIDs {
		targetIDs[id] = struct{}{}
	}

	for i := range msgs {
		if _, matches := targetIDs[msgs[i].ID]; matches && msgs[i].FromMe {
			msgs[i].Delivery = receiptState
			updated = append(updated, msgs[i])
		}
	}
	b.messages[chatJID] = msgs
	b.saveStoreLocked()
	// Emit receipt updates under lock to prevent Unpair retiring the account
	// between the unlock and the emit calls.
	for _, m := range updated {
		b.emitLocked(wire.EventMessage, m)
	}
	b.mu.Unlock()
}

func (b *Backend) saveStoreLocked() {
	stored := &StoredChatData{
		Conversations: b.convs,
		Order:         b.order,
		Messages:      b.messages,
		RawMedia:      snapshotRawMedia(b.rawMsgs),
	}
	if err := saveChatStore(b.paths.WhatsAppStoreFile(), stored); err != nil {
		b.log.Warn().Err(err).Msg("Failed to persist WhatsApp chat store")
	}
}

func (b *Backend) unreadCountLocked() int {
	total := 0
	for _, conv := range b.convs {
		if conv.Unread {
			total++
		}
	}
	return total
}

func (b *Backend) reorderLocked() {
	type entry struct {
		id string
		ts int64
	}
	entries := make([]entry, 0, len(b.convs))
	for id, conv := range b.convs {
		entries = append(entries, entry{id: id, ts: conv.Timestamp})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ts != entries[j].ts {
			return entries[i].ts > entries[j].ts
		}
		return entries[i].id < entries[j].id
	})
	limit := len(entries)
	if limit > maxConversations {
		limit = maxConversations
	}
	b.order = make([]string, limit)
	keep := make(map[string]struct{}, limit)
	for i := 0; i < limit; i++ {
		b.order[i] = entries[i].id
		keep[entries[i].id] = struct{}{}
	}
	for id := range b.convs {
		if _, ok := keep[id]; !ok {
			delete(b.convs, id)
			delete(b.messages, id)
		}
	}
	for key := range b.rawMsgs {
		chat, _, ok := strings.Cut(key, "\x1f")
		if ok {
			if _, keepChat := keep[chat]; !keepChat {
				delete(b.rawMsgs, key)
			}
		}
	}
}

func (b *Backend) setState(state wire.ConnState, errStr string) {
	b.mu.Lock()
	b.status.State = state
	b.status.Error = errStr
	if state == wire.StateUnpaired {
		b.status.QRURL = ""
	}
	b.mu.Unlock()
	b.publishStatus()
}

func (b *Backend) setQRURL(url string) {
	b.mu.Lock()
	b.status.State = wire.StatePairing
	b.status.QRURL = url
	b.status.Error = ""
	b.mu.Unlock()
	b.publishStatus()
}

func (b *Backend) publishStatus() {
	b.mu.RLock()
	st := b.status
	st.Unread = b.unreadCountLocked()
	b.mu.RUnlock()

	b.emit(wire.EventStatus, st)
}

func (b *Backend) emit(evtType string, payload any) {
	if b.publish == nil {
		return
	}
	b.publish(wire.Event{
		Network: wire.NetworkWhatsApp,
		Event:   evtType,
		Data:    payload,
	})
}

// emitLocked publishes an event while b.mu is already held by the caller.
// This is safe only because b.publish is a non-blocking channel enqueue that
// never acquires b.mu, so there is no re-entrant deadlock risk.  Publishing
// under the lock closes the retirement gap: Unpair cannot retire the account
// and clear state between the gen check and the emit call.
func (b *Backend) emitLocked(evtType string, payload any) {
	if b.publish == nil {
		return
	}
	b.publish(wire.Event{
		Network: wire.NetworkWhatsApp,
		Event:   evtType,
		Data:    payload,
	})
}
