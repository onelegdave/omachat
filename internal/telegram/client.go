package telegram

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/rs/zerolog"

	appStore "github.com/onelegdave/omachat/internal/store"
)

// QRChannelItem represents an event emitted during Telegram QR code pairing.
type QRChannelItem struct {
	Event string // "code", "success", "error"
	Code  string // tg://login?token=... URL when Event == "code"
	Error error  // Non-nil when Event == "error"
}

const (
	QRChannelEventCode    = "code"
	QRChannelEventSuccess = "success"
	QRChannelEventError   = "error"
)

// Client defines the interface for MTProto client operations required by Backend.
// Both the real gotd MTProto client wrapper and synthetic mock implementations satisfy this.
type Client interface {
	// Start connects or restores the MTProto client in the background.
	Start(ctx context.Context) error

	// Stop cleanly stops the MTProto client connection.
	Stop() error

	// IsConnected reports whether the client connection is currently active.
	IsConnected() bool

	// IsAuthorized checks whether the current session is authorized.
	IsAuthorized(ctx context.Context) (bool, error)

	// GetQRChannel initiates the MTProto QR login flow and returns a channel
	// yielding QRChannelItems.
	GetQRChannel(ctx context.Context) (<-chan QRChannelItem, error)

	// Ping checks connection health.
	Ping(ctx context.Context) error

	// Underlying returns the gotd *telegram.Client, or nil if using a mock.
	Underlying() *telegram.Client
}

// ClientFactory instantiates a Client for the given credentials and session file path.
type ClientFactory func(creds appStore.TelegramCredentials, sessionPath string) (Client, error)

// GotdClient wraps a gotd/td MTProto client, implementing the Client interface.
type GotdClient struct {
	appID       int
	appHash     string
	sessionPath string
	log         zerolog.Logger

	mu         sync.RWMutex
	peers      map[int64]tg.InputPeerClass
	client     *telegram.Client
	dispatcher tg.UpdateDispatcher
	cancel     context.CancelFunc
	running    bool
	connected  bool
	onMessage  func(Message)
	mediaRefs  map[string]tg.InputFileLocationClass
	mediaExts  map[string]string
}

// SetMessageHandler registers the callback used for live incoming updates.
// It is intentionally a small seam so Backend can own cache and event policy.
func (g *GotdClient) SetMessageHandler(handler func(Message)) {
	g.mu.Lock()
	g.onMessage = handler
	g.mu.Unlock()
}

var _ Client = (*GotdClient)(nil)
var _ ReadClient = (*GotdClient)(nil)

// NewGotdClient constructs a GotdClient wrapping gotd/td MTProto client.
func NewGotdClient(appID int, appHash string, sessionPath string, log zerolog.Logger) *GotdClient {
	dispatcher := tg.NewUpdateDispatcher()
	var wrapper *GotdClient
	dispatch := func(_ context.Context, entities tg.Entities, raw tg.MessageClass) error {
		m, ok := raw.(*tg.Message)
		if !ok || m.PeerID == nil {
			return nil
		}
		id := peerID(m.PeerID)
		if id == 0 {
			return nil
		}
		wrapper.mu.Lock()
		for userID, user := range entities.Users {
			if user.AccessHash != 0 {
				wrapper.peers[userID] = &tg.InputPeerUser{UserID: userID, AccessHash: user.AccessHash}
			}
		}
		for chatID := range entities.Chats {
			wrapper.peers[chatID] = &tg.InputPeerChat{ChatID: chatID}
		}
		for channelID, channel := range entities.Channels {
			if channel.AccessHash != 0 {
				wrapper.peers[channelID] = &tg.InputPeerChannel{ChannelID: channelID, AccessHash: channel.AccessHash}
			}
		}
		converted := wrapper.mediaMessage(m, id)
		handler := wrapper.onMessage
		wrapper.mu.Unlock()
		if handler != nil {
			handler(converted)
		}
		return nil
	}
	dispatcher.OnNewMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		return dispatch(ctx, e, u.Message)
	})
	dispatcher.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		return dispatch(ctx, e, u.Message)
	})
	storage := NewFileSessionStorage(sessionPath)
	client := telegram.NewClient(appID, appHash, telegram.Options{
		UpdateHandler:  dispatcher,
		SessionStorage: storage,
	})
	wrapper = &GotdClient{
		appID:       appID,
		appHash:     appHash,
		sessionPath: sessionPath,
		log:         log.With().Str("component", "gotd").Logger(),
		client:      client,
		peers:       make(map[int64]tg.InputPeerClass),
		mediaRefs:   make(map[string]tg.InputFileLocationClass),
		mediaExts:   make(map[string]string),
		dispatcher:  dispatcher,
	}
	return wrapper
}

// DefaultClientFactory constructs GotdClient instances for live MTProto operations.
func DefaultClientFactory(log zerolog.Logger) ClientFactory {
	return func(creds appStore.TelegramCredentials, sessionPath string) (Client, error) {
		if creds.APIID <= 0 || creds.APIHash == "" {
			return nil, errors.New("invalid telegram credentials")
		}
		return NewGotdClient(creds.APIID, creds.APIHash, sessionPath, log), nil
	}
}

// Start connects the client in the background and verifies whether the session is authorized.
func (g *GotdClient) Start(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	g.cancel = cancel
	g.running = true
	g.mu.Unlock()

	ready := make(chan error, 1)

	go func() {
		err := g.client.Run(runCtx, func(clientCtx context.Context) error {
			g.mu.Lock()
			g.connected = true
			g.mu.Unlock()

			st, authErr := g.client.Auth().Status(clientCtx)
			if authErr != nil {
				ready <- authErr
				return authErr
			}
			if !st.Authorized {
				errUnauth := errors.New("telegram session unauthorized or revoked")
				ready <- errUnauth
				return errUnauth
			}
			ready <- nil

			<-clientCtx.Done()
			return clientCtx.Err()
		})

		g.mu.Lock()
		g.running = false
		g.connected = false
		g.mu.Unlock()

		if err != nil && !errors.Is(err, context.Canceled) {
			select {
			case ready <- err:
			default:
			}
			g.log.Warn().Err(err).Msg("Telegram MTProto connection exited")
		}
	}()

	select {
	case err := <-ready:
		return err
	case <-time.After(15 * time.Second):
		return errors.New("timed out waiting for Telegram connection")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop terminates the MTProto connection.
func (g *GotdClient) Stop() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cancel != nil {
		g.cancel()
		g.cancel = nil
	}
	g.running = false
	g.connected = false
	return nil
}

// IsConnected reports whether the client connection is active.
func (g *GotdClient) IsConnected() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.connected
}

// IsAuthorized reports whether the current MTProto session is authorized.
func (g *GotdClient) IsAuthorized(ctx context.Context) (bool, error) {
	st, err := g.client.Auth().Status(ctx)
	if err != nil {
		if auth.IsUnauthorized(err) {
			return false, nil
		}
		return false, err
	}
	return st.Authorized, nil
}

// GetQRChannel starts MTProto client connection and drives gotd QR login flow,
// yielding QRChannelItems onto the returned channel until completed, cancelled, or failed.
func (g *GotdClient) GetQRChannel(ctx context.Context) (<-chan QRChannelItem, error) {
	qrChan := make(chan QRChannelItem, 4)
	loggedIn := qrlogin.OnLoginToken(g.dispatcher)

	runCtx, cancel := context.WithCancel(ctx)
	g.mu.Lock()
	g.cancel = cancel
	g.running = true
	g.mu.Unlock()

	go func() {
		err := g.client.Run(runCtx, func(clientCtx context.Context) error {
			g.mu.Lock()
			g.connected = true
			g.mu.Unlock()

			qr := g.client.QR()
			authRes, authErr := qr.Auth(clientCtx, loggedIn, func(showCtx context.Context, token qrlogin.Token) error {
				select {
				case qrChan <- QRChannelItem{Event: QRChannelEventCode, Code: token.URL()}:
					return nil
				case <-showCtx.Done():
					return showCtx.Err()
				case <-clientCtx.Done():
					return clientCtx.Err()
				}
			})
			if authErr != nil {
				return authErr
			}
			_ = authRes

			select {
			case qrChan <- QRChannelItem{Event: QRChannelEventSuccess}:
			case <-clientCtx.Done():
			}

			// Keep connection open after pairing
			<-clientCtx.Done()
			return clientCtx.Err()
		})

		g.mu.Lock()
		g.running = false
		g.connected = false
		g.mu.Unlock()

		if err != nil && !errors.Is(err, context.Canceled) {
			var sendErr error
			if tgerr.Is(err, "SESSION_PASSWORD_NEEDED") || errors.Is(err, auth.ErrPasswordAuthNeeded) {
				sendErr = fmt.Errorf("SESSION_PASSWORD_NEEDED: %w", err)
			} else {
				sendErr = err
			}
			// Deliver the transport error before closing. A non-blocking send can
			// drop it when token refreshes filled the channel, leaving callers with
			// the misleading "channel closed" error.
			// qrChan is buffered, so always preserve the underlying error even
			// when the request context has already been canceled.
			qrChan <- QRChannelItem{Event: QRChannelEventError, Error: sendErr}
		}
		close(qrChan)
	}()

	return qrChan, nil
}

// Ping pings the Telegram DC to check connectivity.
func (g *GotdClient) Ping(ctx context.Context) error {
	return g.client.Ping(ctx)
}

// Dialogs fetches a bounded read-only dialog page from Telegram.
func (g *GotdClient) Dialogs(ctx context.Context, limit int) ([]Dialog, error) {
	if limit <= 0 {
		limit = 50
	}
	// gotd requires OffsetPeer to be present even for the initial page;
	// Telegram uses InputPeerEmpty as the no-offset sentinel.
	res, err := g.client.API().MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{OffsetPeer: &tg.InputPeerEmpty{}, Limit: limit})
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	peers := make(map[int64]tg.InputPeerClass)
	var raws []tg.DialogClass
	var lastMessages []tg.MessageClass
	var users []tg.UserClass
	var chats []tg.ChatClass
	switch x := res.(type) {
	case *tg.MessagesDialogs:
		raws, lastMessages, users, chats = x.Dialogs, x.Messages, x.Users, x.Chats
	case *tg.MessagesDialogsSlice:
		raws, lastMessages, users, chats = x.Dialogs, x.Messages, x.Users, x.Chats
	}
	for _, u := range users {
		if x, ok := u.(*tg.User); ok {
			names[fmt.Sprintf("tg:%d", x.ID)] = strings.TrimSpace(x.FirstName + " " + x.LastName)
			if x.AccessHash != 0 {
				peers[x.ID] = &tg.InputPeerUser{UserID: x.ID, AccessHash: x.AccessHash}
			}
		}
	}
	for _, c := range chats {
		switch x := c.(type) {
		case *tg.Chat:
			names[fmt.Sprintf("tg:%d", x.ID)] = x.Title
			peers[x.ID] = &tg.InputPeerChat{ChatID: x.ID}
		case *tg.Channel:
			names[fmt.Sprintf("tg:%d", x.ID)] = x.Title
			if x.AccessHash != 0 {
				peers[x.ID] = &tg.InputPeerChannel{ChannelID: x.ID, AccessHash: x.AccessHash}
			}
		}
	}
	out := make([]Dialog, 0, len(raws))
	previews := make(map[int64]Message, len(lastMessages))
	for _, raw := range lastMessages {
		m, ok := raw.(*tg.Message)
		if !ok || m.PeerID == nil {
			continue
		}
		id := peerID(m.PeerID)
		if id != 0 {
			g.mu.Lock()
			previews[id] = g.mediaMessage(m, id)
			g.mu.Unlock()
		}
	}
	for _, raw := range raws {
		d, ok := raw.(*tg.Dialog)
		if !ok || d.Peer == nil {
			continue
		}
		id := peerID(d.Peer)
		if id == 0 {
			continue
		}
		preview := previews[id]
		out = append(out, Dialog{ID: id, Name: names[fmt.Sprintf("tg:%d", id)], Preview: preview.Text, Unread: d.UnreadCount > 0, Timestamp: preview.Timestamp, IsGroup: isGroupPeer(d.Peer)})
	}
	g.mu.Lock()
	g.peers = peers
	g.mu.Unlock()
	return out, nil
}

// Messages fetches text messages for a user or basic group peer.
func (g *GotdClient) Messages(ctx context.Context, conversationID int64, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}
	g.mu.RLock()
	peer := g.peers[conversationID]
	g.mu.RUnlock()
	if peer == nil {
		return nil, fmt.Errorf("telegram peer %d is not available", conversationID)
	}
	res, err := g.client.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: peer, Limit: limit})
	if err != nil {
		return nil, err
	}
	var raws []tg.MessageClass
	switch x := res.(type) {
	case *tg.MessagesMessages:
		raws = x.Messages
	case *tg.MessagesMessagesSlice:
		raws = x.Messages
	}
	out := make([]Message, 0, len(raws))
	for _, raw := range raws {
		m, ok := raw.(*tg.Message)
		if !ok {
			continue
		}
		g.mu.Lock()
		out = append(out, g.mediaMessage(m, conversationID))
		g.mu.Unlock()
	}
	return out, nil
}

// MarkRead acknowledges Telegram history up to messageID. A zero message ID
// asks Telegram to mark the whole known history for the peer as read.
func (g *GotdClient) MarkRead(ctx context.Context, conversationID int64, messageID int64) error {
	g.mu.RLock()
	peer := g.peers[conversationID]
	g.mu.RUnlock()
	if peer == nil {
		return fmt.Errorf("telegram peer %d is not available", conversationID)
	}
	maxID := 0
	if messageID > 0 {
		maxID = int(messageID)
	}
	_, err := g.client.API().MessagesReadHistory(ctx, &tg.MessagesReadHistoryRequest{Peer: peer, MaxID: maxID})
	return err
}

func (g *GotdClient) SendText(ctx context.Context, conversationID int64, text string) (Message, error) {
	if strings.TrimSpace(text) == "" {
		return Message{}, errors.New("Telegram message cannot be empty")
	}
	g.mu.RLock()
	peer := g.peers[conversationID]
	g.mu.RUnlock()
	if peer == nil {
		return Message{}, fmt.Errorf("telegram peer %d is not available", conversationID)
	}
	var randomID int64
	if err := binary.Read(rand.Reader, binary.LittleEndian, &randomID); err != nil {
		return Message{}, fmt.Errorf("generate Telegram message ID: %w", err)
	}
	updates, err := g.client.API().MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{Peer: peer, Message: text, RandomID: randomID})
	if err != nil {
		return Message{}, err
	}
	if u, ok := updates.(*tg.Updates); ok {
		for _, raw := range u.Updates {
			if update, ok := raw.(*tg.UpdateNewMessage); ok {
				if m, ok := update.Message.(*tg.Message); ok {
					return Message{ID: int64(m.ID), ConversationID: conversationID, Text: m.Message, Timestamp: telegramTimestamp(m.Date), FromMe: true}, nil
				}
			}
			if update, ok := raw.(*tg.UpdateNewChannelMessage); ok {
				if m, ok := update.Message.(*tg.Message); ok {
					return Message{ID: int64(m.ID), ConversationID: conversationID, Text: m.Message, Timestamp: telegramTimestamp(m.Date), FromMe: true}, nil
				}
			}
		}
	}
	if u, ok := updates.(*tg.UpdatesCombined); ok {
		for _, raw := range u.Updates {
			if update, ok := raw.(*tg.UpdateNewMessage); ok {
				if m, ok := update.Message.(*tg.Message); ok {
					return Message{ID: int64(m.ID), ConversationID: conversationID, Text: m.Message, Timestamp: telegramTimestamp(m.Date), FromMe: true}, nil
				}
			}
			if update, ok := raw.(*tg.UpdateNewChannelMessage); ok {
				if m, ok := update.Message.(*tg.Message); ok {
					return Message{ID: int64(m.ID), ConversationID: conversationID, Text: m.Message, Timestamp: telegramTimestamp(m.Date), FromMe: true}, nil
				}
			}
		}
	}
	if u, ok := updates.(*tg.UpdateShortSentMessage); ok {
		return Message{ID: int64(u.ID), ConversationID: conversationID, Text: text, Timestamp: telegramTimestamp(u.Date), FromMe: u.Out}, nil
	}
	return Message{}, errors.New("Telegram send succeeded without a message response")
}

func (g *GotdClient) SendImage(ctx context.Context, conversationID int64, path, caption string) (Message, error) {
	g.mu.RLock()
	peer := g.peers[conversationID]
	g.mu.RUnlock()
	if peer == nil {
		return Message{}, fmt.Errorf("telegram peer %d is not available", conversationID)
	}
	info, err := os.Stat(path)
	if err != nil {
		return Message{}, fmt.Errorf("stat image: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Message{}, errors.New("image is not a regular file")
	}
	if info.Size() <= 0 || info.Size() > 16*1024*1024 {
		return Message{}, errors.New("image must be between 1 byte and 16 MB")
	}
	file, err := uploader.NewUploader(g.client.API()).FromPath(ctx, path)
	if err != nil {
		return Message{}, fmt.Errorf("upload image: %w", err)
	}
	var randomID int64
	if err := binary.Read(rand.Reader, binary.LittleEndian, &randomID); err != nil {
		return Message{}, err
	}
	updates, err := g.client.API().MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{Peer: peer, Media: &tg.InputMediaUploadedPhoto{File: file}, Message: caption, RandomID: randomID})
	if err != nil {
		return Message{}, err
	}
	if u, ok := updates.(*tg.Updates); ok {
		for _, raw := range u.Updates {
			if x, ok := raw.(*tg.UpdateNewMessage); ok {
				if m, ok := x.Message.(*tg.Message); ok {
					return Message{ID: int64(m.ID), ConversationID: conversationID, Text: m.Message, Timestamp: telegramTimestamp(m.Date), FromMe: true}, nil
				}
			}
			if x, ok := raw.(*tg.UpdateNewChannelMessage); ok {
				if m, ok := x.Message.(*tg.Message); ok {
					return Message{ID: int64(m.ID), ConversationID: conversationID, Text: m.Message, Timestamp: telegramTimestamp(m.Date), FromMe: true}, nil
				}
			}
		}
	}
	return Message{}, errors.New("Telegram image send succeeded without a message response")
}

func (g *GotdClient) mediaMessage(m *tg.Message, conversationID int64) Message {
	out := Message{ID: int64(m.ID), ConversationID: conversationID, Text: m.Message, Timestamp: telegramTimestamp(m.Date), FromMe: m.Out}
	if photo, ok := m.Media.(*tg.MessageMediaPhoto); ok {
		if p, ok := photo.Photo.(*tg.Photo); ok {
			for _, raw := range p.Sizes {
				if s, ok := raw.(*tg.PhotoSize); ok {
					key := fmt.Sprintf("tg:%d", m.ID)
					g.mediaRefs[key] = &tg.InputPhotoFileLocation{ID: p.ID, AccessHash: p.AccessHash, FileReference: p.FileReference, ThumbSize: s.Type}
					out.MediaKey = key
					break
				}
			}
		}
	}
	if document, ok := m.Media.(*tg.MessageMediaDocument); ok {
		if d, ok := document.Document.(*tg.Document); ok {
			isAudio := false
			for _, attr := range d.Attributes {
				if _, ok := attr.(*tg.DocumentAttributeAudio); ok {
					isAudio = true
					break
				}
			}
			if isAudio {
				key := fmt.Sprintf("tg:%d", m.ID)
				g.mediaRefs[key] = &tg.InputDocumentFileLocation{ID: d.ID, AccessHash: d.AccessHash, FileReference: d.FileReference}
				mimeType := d.MimeType
				if mimeType == "" {
					mimeType = "audio/ogg"
				}
				g.mediaExts[key] = telegramMediaExt(mimeType)
				out.MediaKey, out.MediaMime, out.MediaAudio = key, mimeType, true
			}
		}
	}
	return out
}

func telegramMediaExt(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "audio/mp4", "audio/aac":
		return ".m4a"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/amr":
		return ".amr"
	case "audio/3gpp":
		return ".3ga"
	default:
		return ".ogg"
	}
}

func (g *GotdClient) DownloadMedia(ctx context.Context, key, dir string) (string, error) {
	g.mu.RLock()
	location := g.mediaRefs[key]
	g.mu.RUnlock()
	if location == nil {
		return "", errors.New("Telegram media reference is unavailable; refresh the conversation")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	ext := ".jpg"
	g.mu.RLock()
	if value := g.mediaExts[key]; value != "" {
		ext = value
	}
	g.mu.RUnlock()
	final := filepath.Join(dir, safeMediaName(key)+ext)
	if st, err := os.Stat(final); err == nil && st.Mode().IsRegular() {
		return final, nil
	}
	tmp, err := os.CreateTemp(dir, ".telegram-media-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	_, err = g.client.Download(location).Stream(ctx, io.Writer(tmp))
	closeErr := tmp.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	st, err := os.Stat(tmpName)
	if err != nil || st.Size() == 0 || st.Size() > 32*1024*1024 {
		return "", errors.New("downloaded Telegram media is invalid or too large")
	}
	if err := os.Rename(tmpName, final); err != nil {
		return "", err
	}
	return final, nil
}

func safeMediaName(key string) string {
	var b strings.Builder
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "media"
	}
	return b.String()
}

// gotd exposes Telegram dates as Unix seconds; OmaChat wire timestamps use
// Unix microseconds so the shared QML formatter can render every network.
func telegramTimestamp(seconds int) int64 {
	return int64(seconds) * 1_000_000
}

func peerID(p tg.PeerClass) int64 {
	switch x := p.(type) {
	case *tg.PeerUser:
		return x.UserID
	case *tg.PeerChat:
		return x.ChatID
	case *tg.PeerChannel:
		return x.ChannelID
	}
	return 0
}
func isGroupPeer(p tg.PeerClass) bool {
	switch p.(type) {
	case *tg.PeerChat, *tg.PeerChannel:
		return true
	}
	return false
}

// Underlying returns the underlying gotd *telegram.Client.
func (g *GotdClient) Underlying() *telegram.Client {
	return g.client
}
