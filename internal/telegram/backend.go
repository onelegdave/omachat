package telegram

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/rs/zerolog"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

// ErrNotConfigured is returned by operations that require a live MTProto client
// or reviewed API credentials.
var ErrNotConfigured = errors.New("Telegram client is not configured: protocol library integration pending")

const (
	hintCredentialsRequired   = "Telegram API credentials required: configure api_id and api_hash in ~/.local/share/omachat/config.json (obtain from my.telegram.org)"
	hintCredentialsConfigured = "Telegram API credentials configured; pairing not yet started"
)

// Backend manages the Telegram service state, local data isolation, and protocol routing.
type Backend struct {
	log     zerolog.Logger
	paths   *appStore.Paths
	config  *appStore.ConfigStore
	publish func(wire.Event)

	mu        sync.RWMutex
	sessionMu sync.Mutex

	status wire.Status
	paired bool

	client        Client
	clientFactory ClientFactory

	ctx        context.Context
	cancel     context.CancelFunc
	pairCancel context.CancelFunc
	gen        uint64 // pairing attempt generation to prevent stale goroutine races

	convs    map[string]wire.Conversation
	order    []string
	messages map[string][]wire.Message
}

type incomingHandlerClient interface {
	SetMessageHandler(func(Message))
}

// New creates an unstarted Telegram backend.
func New(log zerolog.Logger, paths *appStore.Paths, publish func(wire.Event), cfg ...*appStore.ConfigStore) *Backend {
	var configStore *appStore.ConfigStore
	if len(cfg) > 0 && cfg[0] != nil {
		configStore = cfg[0]
	} else if paths != nil {
		configStore = appStore.NewConfigStore(paths.ConfigFile())
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &Backend{
		log:           log.With().Str("network", wire.NetworkTelegram).Logger(),
		paths:         paths,
		config:        configStore,
		publish:       publish,
		clientFactory: DefaultClientFactory(log),
		ctx:           ctx,
		cancel:        cancel,
		convs:         make(map[string]wire.Conversation),
		messages:      make(map[string][]wire.Message),
		status: wire.Status{
			Network: wire.NetworkTelegram,
			State:   wire.StateUnpaired,
			PhoneOK: true,
			Hint:    hintCredentialsRequired,
		},
	}
	if paths != nil {
		stored := loadStoredData(paths.TelegramStoreFile())
		b.convs, b.order, b.messages = stored.Conversations, stored.Order, stored.Messages
		// Telegram text sending is supported now. Normalize caches written by
		// the earlier read-only milestone so restored conversations are writable.
		for id, conv := range b.convs {
			if strings.HasPrefix(id, "tg:") {
				conv.ReadOnly = false
				b.convs[id] = conv
			}
		}
	}
	return b
}

// SetConfig updates the configuration store for the backend.
func (b *Backend) SetConfig(cs *appStore.ConfigStore) {
	b.mu.Lock()
	b.config = cs
	b.mu.Unlock()
}

// SetClient overrides the Telegram client instance (primarily used for unit testing).
func (b *Backend) SetClient(c Client) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.client = c
}

// SetClientFactory overrides the client factory (primarily used for unit testing).
func (b *Backend) SetClientFactory(f ClientFactory) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clientFactory = f
}

// Credentials inspects and returns the configured Telegram credentials, or an error if unconfigured or invalid.
// Secrets are never logged or exposed.
func (b *Backend) Credentials() (appStore.TelegramCredentials, error) {
	b.mu.RLock()
	cs := b.config
	b.mu.RUnlock()

	if cs != nil {
		return cs.TelegramCredentials()
	}
	if b.paths != nil {
		return appStore.NewConfigStore(b.paths.ConfigFile()).TelegramCredentials()
	}
	return appStore.Config{}.TelegramCredentials()
}

// Start initializes the Telegram backend. If a persisted session file exists,
// it restores the session only through the client abstraction and does not make
// network calls in tests. Otherwise it inspects credentials and reports an honest
// unpaired hint.
func (b *Backend) Start(ctx context.Context) error {
	b.sessionMu.Lock()
	defer b.sessionMu.Unlock()

	// Ensure private media directory exists.
	if err := os.MkdirAll(b.paths.TelegramMediaDir(), 0o700); err != nil {
		b.log.Error().Err(err).Msg("Failed to create Telegram media directory")
		b.setState(wire.StateDisconnected, "create media dir: "+err.Error())
		return err
	}

	creds, credErr := b.Credentials()
	if credErr != nil {
		if errors.Is(credErr, appStore.ErrTelegramUnconfigured) {
			b.setStatusWithHint(wire.StateUnpaired, hintCredentialsRequired, "")
			b.log.Info().Msg("Telegram credentials unconfigured; remaining in unpaired scaffold mode")
			return nil
		}
		b.setStatusWithHint(wire.StateUnpaired, hintCredentialsRequired, credErr.Error())
		b.log.Warn().Msg("Telegram credentials invalid; remaining in unpaired scaffold mode")
		return nil
	}

	// Credentials are valid. Check for an existing session file.
	sessionFile := b.paths.TelegramSessionFile()
	sessionExists := false
	if fi, err := os.Stat(sessionFile); err == nil && fi.Size() > 0 {
		sessionExists = true
	}

	if !sessionExists {
		b.setStatusWithHint(wire.StateUnpaired, hintCredentialsConfigured, "")
		b.log.Info().Msg("Telegram backend initialized with valid credentials; pairing pending")
		return nil
	}

	// Restore persisted session through the client abstraction
	cli, err := b.getOrCreateClientLocked(creds)
	if err != nil {
		b.setState(wire.StateDisconnected, "init client: "+err.Error())
		return err
	}

	b.mu.Lock()
	b.paired = true
	b.mu.Unlock()

	b.setState(wire.StateConnecting, "")
	// Keep the client attached to the backend lifetime. The separate wait
	// context bounds startup without canceling the transport after it becomes
	// ready.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 20*time.Second)
	restoreErr := make(chan error, 1)
	go func() { restoreErr <- cli.Start(b.ctx) }()
	var startErr error
	select {
	case startErr = <-restoreErr:
	case <-waitCtx.Done():
		startErr = waitCtx.Err()
	}
	waitCancel()
	if startErr != nil {
		b.log.Warn().Err(startErr).Msg("Telegram session restore connection failed")
		if strings.Contains(strings.ToLower(startErr.Error()), "unauthorized") || strings.Contains(strings.ToLower(startErr.Error()), "revoked") || errors.Is(startErr, context.DeadlineExceeded) {
			// A canceled or revoked QR attempt can leave a session blob behind.
			// Remove only Telegram's local state and return to a fresh pairing
			// screen instead of trapping the panel in reconnecting.
			_ = cli.Stop()
			b.mu.Lock()
			b.client = nil
			b.paired = false
			b.mu.Unlock()
			if b.paths != nil {
				_ = b.paths.ClearTelegramSession()
			}
			b.setStatusWithHint(wire.StateUnpaired, hintCredentialsConfigured, "")
			return nil
		}
		b.setState(wire.StateDisconnected, "restore session: "+startErr.Error())
		return nil
	}
	// A successful client.Start means the persisted session is authorized and
	// the transport is ready. Publish the connected state and hydrate the local
	// inbox before the panel asks for conversations.
	b.setState(wire.StateConnected, "")
	syncCtx, syncCancel := context.WithTimeout(b.ctx, 30*time.Second)
	if err := b.Refresh(syncCtx); err != nil {
		b.log.Warn().Err(err).Msg("Telegram initial dialog refresh failed")
	}
	syncCancel()
	return nil
}

func (b *Backend) getOrCreateClientLocked(creds appStore.TelegramCredentials) (Client, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.client != nil {
		return b.client, nil
	}
	if b.clientFactory == nil {
		return nil, errors.New("telegram client factory not configured")
	}
	cli, err := b.clientFactory(creds, b.paths.TelegramSessionFile())
	if err != nil {
		return nil, err
	}
	b.client = cli
	if incoming, ok := cli.(incomingHandlerClient); ok {
		incoming.SetMessageHandler(b.ingestMessage)
	}
	return cli, nil
}

func (b *Backend) ingestMessage(msg Message) {
	if msg.ConversationID == 0 || msg.ID == 0 {
		return
	}
	converted := mapMessage(msg)
	conversationID := fmt.Sprintf("tg:%d", msg.ConversationID)
	b.mu.Lock()
	if _, exists := b.convs[conversationID]; !exists {
		name := msg.SenderName
		if name == "" {
			name = fmt.Sprintf("Telegram chat %d", msg.ConversationID)
		}
		b.convs[conversationID] = mapDialog(Dialog{ID: msg.ConversationID, Name: name, Preview: msg.Text, Timestamp: msg.Timestamp})
		b.order = append([]string{conversationID}, b.order...)
	}
	items := b.messages[conversationID]
	seen := false
	for _, item := range items {
		if item.ID == converted.ID {
			seen = true
			break
		}
	}
	if !seen {
		items = append(items, converted)
	}
	b.messages[conversationID] = items
	conv := b.convs[conversationID]
	conv.Preview, conv.Timestamp, conv.Unread = msg.Text, msg.Timestamp, !msg.FromMe
	b.convs[conversationID] = conv
	snapshot := storedData{Conversations: b.convs, Order: b.order, Messages: b.messages}
	b.mu.Unlock()
	if b.paths != nil {
		_ = saveStoredData(b.paths.TelegramStoreFile(), snapshot)
	}
	if b.publish != nil {
		b.publish(wire.Event{Event: wire.EventMessage, Network: wire.NetworkTelegram, Data: converted})
	}
}

// Status returns the current Telegram status.
func (b *Backend) Status() wire.Status {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.status
}

// Client returns the active Client interface, or nil if not configured.
func (b *Backend) Client() Client {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.client
}

// TelegramClient returns the underlying gotd MTProto client, or nil if using mock or not configured.
func (b *Backend) TelegramClient() *telegram.Client {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.client == nil {
		return nil
	}
	return b.client.Underlying()
}

// SetState updates the backend status and publishes a status event.
func (b *Backend) SetState(state wire.ConnState, errStr string) {
	b.sessionMu.Lock()
	defer b.sessionMu.Unlock()
	b.setState(state, errStr)
}

func (b *Backend) setState(state wire.ConnState, errStr string) {
	b.mu.Lock()
	b.status.State = state
	b.status.Error = errStr
	st := b.status
	b.mu.Unlock()

	if b.publish != nil {
		b.publish(wire.Event{
			Event:   wire.EventStatus,
			Network: wire.NetworkTelegram,
			Data:    st,
		})
	}
}

func (b *Backend) setStatusWithHint(state wire.ConnState, hint string, errStr string) {
	b.mu.Lock()
	b.status.State = state
	b.status.Hint = hint
	b.status.Error = errStr
	if state == wire.StateUnpaired {
		b.status.QRURL = ""
	}
	st := b.status
	b.mu.Unlock()

	if b.publish != nil {
		b.publish(wire.Event{
			Event:   wire.EventStatus,
			Network: wire.NetworkTelegram,
			Data:    st,
		})
	}
}

func (b *Backend) setQRURL(url string) {
	b.mu.Lock()
	b.status.QRURL = url
	st := b.status
	b.mu.Unlock()

	if b.publish != nil {
		b.publish(wire.Event{
			Event:   wire.EventStatus,
			Network: wire.NetworkTelegram,
			Data:    st,
		})
	}
}

// Conversations returns the list of cached Telegram conversations.
func (b *Backend) Conversations(count int) []wire.Conversation {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.order) == 0 {
		return []wire.Conversation{}
	}
	limit := count
	if limit <= 0 || limit > len(b.order) {
		limit = len(b.order)
	}
	out := make([]wire.Conversation, 0, limit)
	for _, id := range b.order[:limit] {
		if c, ok := b.convs[id]; ok {
			out = append(out, c)
		}
	}
	return out
}

// Messages returns the cached messages for a conversation.
func (b *Backend) Messages(ctx context.Context, p wire.MessagesParams) (wire.MessagesResult, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	msgs := b.messages[p.ConversationID]
	if msgs == nil {
		msgs = []wire.Message{}
	}
	return wire.MessagesResult{
		ConversationID: p.ConversationID,
		Messages:       msgs,
	}, nil
}

func (b *Backend) Send(ctx context.Context, p wire.SendParams) (*wire.Message, error) {
	b.mu.RLock()
	cli := b.client
	b.mu.RUnlock()
	sender, ok := cli.(SendClient)
	if !ok {
		return nil, ErrNotConfigured
	}
	id, err := parseTelegramID(p.ConversationID)
	if err != nil {
		return nil, err
	}
	msg, err := sender.SendText(ctx, id, p.Text)
	if err != nil {
		return nil, err
	}
	converted := mapMessage(msg)
	converted.TmpID = p.TmpID
	converted.Status = wire.DeliverySent
	converted.Delivery = wire.DeliverySent
	b.ingestMessage(msg)
	return &converted, nil
}

func (b *Backend) SendMedia(ctx context.Context, p wire.SendMediaParams) (*wire.SendMediaResult, error) {
	b.mu.RLock()
	cli := b.client
	b.mu.RUnlock()
	sender, ok := cli.(MediaClient)
	if !ok {
		return nil, ErrNotConfigured
	}
	id, err := parseTelegramID(p.ConversationID)
	if err != nil {
		return nil, err
	}
	var msg Message
	lowerPath := strings.ToLower(p.Path)
	isVoice := strings.HasSuffix(lowerPath, ".ogg") || strings.HasSuffix(lowerPath, ".opus") || strings.HasSuffix(lowerPath, ".m4a")
	if isVoice {
		msg, err = sender.SendVoice(ctx, id, p.Path, p.Caption)
	} else {
		msg, err = sender.SendImage(ctx, id, p.Path, p.Caption)
	}
	if err != nil {
		return nil, err
	}
	out := mapMessage(msg)
	out.TmpID = p.TmpID
	out.Status = wire.DeliverySent
	out.Delivery = wire.DeliverySent
	if isVoice {
		out.Attachments = []wire.Attachment{{Key: out.ID, MimeType: "audio/ogg", IsAudio: true}}
		if strings.HasSuffix(lowerPath, ".m4a") {
			out.Attachments[0].MimeType = "audio/mp4"
		}
	}
	if info, statErr := os.Stat(p.Path); statErr == nil && info.Mode().IsRegular() {
		mediaType := mime.TypeByExtension(strings.ToLower(filepath.Ext(p.Path)))
		if mediaType == "" && isVoice {
			mediaType = "audio/ogg"
			if strings.HasSuffix(lowerPath, ".m4a") {
				mediaType = "audio/mp4"
			}
		}
		attachmentKey := p.TmpID
		if attachmentKey == "" {
			attachmentKey = out.ID
		}
		out.Attachments = []wire.Attachment{{
			Key: attachmentKey, MediaID: attachmentKey, Name: filepath.Base(p.Path),
			MimeType: mediaType, Size: info.Size(), IsImage: !isVoice && strings.HasPrefix(mediaType, "image/"), IsAudio: isVoice || strings.HasPrefix(mediaType, "audio/"),
			Path: p.Path,
		}}
	}
	b.ingestMessage(msg)
	return &wire.SendMediaResult{Message: &out}, nil
}

func (b *Backend) Media(ctx context.Context, p wire.MediaParams) (*wire.MediaResult, error) {
	key := p.Key
	if key == "" {
		key = p.MediaID
	}
	if key == "" {
		return nil, fmt.Errorf("empty Telegram media key: %w", ErrNotConfigured)
	}
	b.mu.RLock()
	cli := b.client
	b.mu.RUnlock()
	media, ok := cli.(MediaClient)
	if !ok {
		return nil, ErrNotConfigured
	}
	path, err := media.DownloadMedia(ctx, key, b.paths.TelegramMediaDir())
	if err != nil {
		return nil, err
	}
	return &wire.MediaResult{Key: key, Path: path}, nil
}

// MarkRead acknowledges Telegram history and clears the local unread flag.
func (b *Backend) MarkRead(ctx context.Context, p wire.MarkReadParams) error {
	b.mu.RLock()
	cli := b.client
	b.mu.RUnlock()
	syncClient, ok := cli.(SyncClient)
	if !ok {
		return ErrNotConfigured
	}
	conversationID, err := parseTelegramID(p.ConversationID)
	if err != nil {
		return err
	}
	messageID := int64(0)
	if p.MessageID != "" {
		messageID, err = parseTelegramID(p.MessageID)
		if err != nil {
			return err
		}
	}
	if err := syncClient.MarkRead(ctx, conversationID, messageID); err != nil {
		return err
	}
	b.mu.Lock()
	if conv, exists := b.convs[p.ConversationID]; exists {
		conv.Unread = false
		b.convs[p.ConversationID] = conv
	}
	snapshot := storedData{Conversations: b.convs, Order: b.order, Messages: b.messages}
	b.mu.Unlock()
	if b.paths != nil {
		return saveStoredData(b.paths.TelegramStoreFile(), snapshot)
	}
	return nil
}

// StartPairing initiates the gotd QR authentication flow in a context-safe way.
// It yields the first QR token URL synchronously, updates status to StatePairing,
// and manages background token updates, acceptance, and failure recovery.
func (b *Backend) StartPairing(ctx context.Context) (string, error) {
	b.sessionMu.Lock()
	defer b.sessionMu.Unlock()

	// 1. Reject if already paired
	b.mu.RLock()
	if b.paired {
		b.mu.RUnlock()
		return "", errors.New("already paired; unpair first before starting a new QR session")
	}
	b.mu.RUnlock()

	// 2. Validate Telegram credentials
	creds, credErr := b.Credentials()
	if credErr != nil {
		var userErr string
		if errors.Is(credErr, appStore.ErrTelegramUnconfigured) {
			userErr = "Telegram API credentials required. Configure api_id and api_hash in ~/.local/share/omachat/config.json."
			b.setStatusWithHint(wire.StateUnpaired, hintCredentialsRequired, userErr)
			return "", ErrNotConfigured
		}
		userErr = "Telegram credentials invalid. Check your configuration."
		b.setStatusWithHint(wire.StateUnpaired, hintCredentialsRequired, credErr.Error())
		b.log.Warn().Err(credErr).Msg("Cannot start pairing: credentials invalid")
		return "", fmt.Errorf("telegram credentials: %w", credErr)
	}

	// 3. Ensure media directory exists
	if err := os.MkdirAll(b.paths.TelegramMediaDir(), 0o700); err != nil {
		b.setState(wire.StateUnpaired, "Cannot pair: could not create media directory: "+err.Error())
		return "", err
	}

	// 4. Cancel any prior in-flight pairing
	if b.pairCancel != nil {
		b.pairCancel()
		b.pairCancel = nil
	}

	// 5. Obtain or construct client via abstraction
	cli, err := b.getOrCreateClientLocked(creds)
	if err != nil {
		b.setStatusWithHint(wire.StateUnpaired, hintCredentialsConfigured, "Failed to initialize Telegram client: "+err.Error())
		return "", fmt.Errorf("init telegram client: %w", err)
	}

	b.mu.Lock()
	b.gen++
	gen := b.gen
	b.client = cli
	b.mu.Unlock()

	pairCtx, pairCancel := context.WithCancel(b.ctx)
	b.pairCancel = pairCancel

	pairingFailed := func(userMsg string, retErr error) (string, error) {
		pairCancel()
		b.pairCancel = nil

		b.mu.Lock()
		if b.gen == gen {
			b.gen++
		}
		b.status.State = wire.StateUnpaired
		b.status.Hint = hintCredentialsConfigured
		b.status.Error = userMsg
		b.status.QRURL = ""
		st := b.status
		b.mu.Unlock()

		if b.publish != nil {
			b.publish(wire.Event{
				Event:   wire.EventStatus,
				Network: wire.NetworkTelegram,
				Data:    st,
			})
		}
		b.log.Warn().Err(retErr).Msg("Telegram pairing failed; returned to unpaired")
		return "", retErr
	}

	qrChan, err := cli.GetQRChannel(pairCtx)
	if err != nil {
		return pairingFailed(
			"Could not start Telegram QR pairing session. Select Use a QR code to try again.",
			fmt.Errorf("get qr channel: %w", err),
		)
	}

	b.setStatusWithHint(wire.StatePairing, "", "")

	// Wait synchronously for the first QR token or error/cancellation
	select {
	case item, ok := <-qrChan:
		if !ok {
			return pairingFailed(
				"Telegram QR pairing channel closed prematurely. Try again.",
				errors.New("qr channel closed prematurely"),
			)
		}
		switch item.Event {
		case QRChannelEventCode:
			b.setQRURL(item.Code)
			go b.listenQRChannel(qrChan, pairCancel, gen)
			return item.Code, nil
		case QRChannelEventError:
			pairingErr := item.Error
			if pairingErr == nil {
				pairingErr = errors.New("Telegram QR pairing returned an unspecified error")
			}
			return pairingFailed(
				"Telegram pairing error: "+pairingErr.Error(),
				pairingErr,
			)
		default:
			return pairingFailed(
				fmt.Sprintf("Unexpected pairing event: %s", item.Event),
				fmt.Errorf("unexpected event: %s", item.Event),
			)
		}
	case <-ctx.Done():
		return pairingFailed(
			"Telegram QR pairing was cancelled. Select Use a QR code to try again.",
			ctx.Err(),
		)
	case <-time.After(15 * time.Second):
		return pairingFailed(
			"Timed out waiting for Telegram QR code. Try again.",
			errors.New("timeout waiting for initial qr code"),
		)
	}
}

func (b *Backend) listenQRChannel(qrChan <-chan QRChannelItem, cancel context.CancelFunc, gen uint64) {
	for item := range qrChan {
		b.mu.Lock()
		if b.gen != gen {
			b.mu.Unlock()
			return
		}
		b.mu.Unlock()

		switch item.Event {
		case QRChannelEventCode:
			b.setQRURL(item.Code)
		case QRChannelEventSuccess:
			b.mu.Lock()
			if b.gen != gen {
				b.mu.Unlock()
				return
			}
			b.paired = true
			b.status.State = wire.StateConnected
			b.status.QRURL = ""
			b.status.Hint = ""
			b.status.Error = ""
			st := b.status
			b.mu.Unlock()

			if b.publish != nil {
				b.publish(wire.Event{
					Event:   wire.EventStatus,
					Network: wire.NetworkTelegram,
					Data:    st,
				})
			}
			b.log.Info().Msg("Telegram pairing completed successfully")
			return
		case QRChannelEventError:
			cancel()
			b.mu.Lock()
			if b.gen != gen {
				b.mu.Unlock()
				return
			}
			b.status.State = wire.StateUnpaired
			b.status.Hint = hintCredentialsConfigured
			b.status.QRURL = ""
			if item.Error != nil && strings.Contains(item.Error.Error(), "SESSION_PASSWORD_NEEDED") {
				b.status.Error = "Telegram 2FA Cloud Password required; cloud password authentication is not yet supported."
			} else if item.Error != nil {
				b.status.Error = "Telegram pairing failed: " + item.Error.Error()
			}
			st := b.status
			b.mu.Unlock()

			if b.publish != nil {
				b.publish(wire.Event{
					Event:   wire.EventStatus,
					Network: wire.NetworkTelegram,
					Data:    st,
				})
			}
			b.log.Warn().Err(item.Error).Msg("Telegram QR pairing failed")
			return
		}
	}
	cancel()
}

// Unpair wipes local Telegram session, store, and media files, and transitions to StateUnpaired.
func (b *Backend) Unpair(ctx context.Context) error {
	b.sessionMu.Lock()
	defer b.sessionMu.Unlock()

	if b.pairCancel != nil {
		b.pairCancel()
		b.pairCancel = nil
	}

	b.mu.Lock()
	b.gen++
	cli := b.client
	b.convs = make(map[string]wire.Conversation)
	b.order = nil
	b.messages = make(map[string][]wire.Message)
	b.client = nil
	b.paired = false
	b.mu.Unlock()

	if cli != nil {
		if err := cli.Stop(); err != nil {
			b.log.Warn().Err(err).Msg("Error stopping client during unpair")
		}
	}

	clearErr := b.paths.ClearTelegramSession()
	var statusErr string
	if clearErr != nil {
		b.log.Error().Err(clearErr).Msg("Failed to clear local Telegram session files")
		statusErr = "Local Telegram files could not be removed: " + clearErr.Error()
	}

	creds, credErr := b.Credentials()
	hint := hintCredentialsRequired
	if credErr == nil && creds.APIID > 0 {
		hint = hintCredentialsConfigured
	}
	b.setStatusWithHint(wire.StateUnpaired, hint, statusErr)
	if clearErr != nil {
		return fmt.Errorf("telegram storage cleanup failed: %w", clearErr)
	}
	return nil
}

// Stop cleanly stops any running pairing or client connection.
func (b *Backend) Stop() {
	b.sessionMu.Lock()
	defer b.sessionMu.Unlock()

	if b.pairCancel != nil {
		b.pairCancel()
		b.pairCancel = nil
	}
	if b.cancel != nil {
		b.cancel()
	}

	b.mu.Lock()
	cli := b.client
	b.client = nil
	b.paired = false
	b.mu.Unlock()

	if cli != nil {
		_ = cli.Stop()
	}
}

// Refresh performs a no-op refresh while unpaired.
func (b *Backend) Refresh(ctx context.Context) error {
	b.mu.RLock()
	cli := b.client
	b.mu.RUnlock()
	reader, ok := cli.(ReadClient)
	if !ok {
		return nil
	}
	dialogs, err := reader.Dialogs(ctx, 50)
	if err != nil {
		return err
	}
	convs := mapDialogs(dialogs, 50)
	msgs := make(map[string][]wire.Message)
	order := make([]string, 0, len(convs))
	for _, conv := range convs {
		order = append(order, conv.ID)
		id, _ := strconv.ParseInt(strings.TrimPrefix(conv.ID, "tg:"), 10, 64)
		items, e := reader.Messages(ctx, id, 100)
		if e != nil {
			return e
		}
		msgs[conv.ID] = mapMessages(items, id)
	}
	b.mu.Lock()
	b.convs, b.order, b.messages = make(map[string]wire.Conversation, len(convs)), order, msgs
	for _, conv := range convs {
		b.convs[conv.ID] = conv
	}
	snapshot := storedData{Conversations: b.convs, Order: b.order, Messages: b.messages}
	b.mu.Unlock()
	if b.paths != nil {
		return saveStoredData(b.paths.TelegramStoreFile(), snapshot)
	}
	return nil
}

func parseTelegramID(value string) (int64, error) {
	value = strings.TrimPrefix(value, "tg:")
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid Telegram ID %q", value)
	}
	return id, nil
}

// AddTestConversation adds a conversation to the in-memory cache for testing.
func (b *Backend) AddTestConversation(conv wire.Conversation) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.convs[conv.ID]; !exists {
		b.order = append([]string{conv.ID}, b.order...)
	}
	b.convs[conv.ID] = conv
}

// SetTestMessages sets messages for a conversation for testing.
func (b *Backend) SetTestMessages(convID string, msgs []wire.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages[convID] = msgs
}

// SetPaired sets the paired flag for testing.
func (b *Backend) SetPaired(paired bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.paired = paired
}
