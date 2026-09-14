package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
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
}

var _ Client = (*GotdClient)(nil)
var _ ReadClient = (*GotdClient)(nil)

// NewGotdClient constructs a GotdClient wrapping gotd/td MTProto client.
func NewGotdClient(appID int, appHash string, sessionPath string, log zerolog.Logger) *GotdClient {
	dispatcher := tg.NewUpdateDispatcher()
	storage := NewFileSessionStorage(sessionPath)
	client := telegram.NewClient(appID, appHash, telegram.Options{
		UpdateHandler:  dispatcher,
		SessionStorage: storage,
	})
	return &GotdClient{
		appID:       appID,
		appHash:     appHash,
		sessionPath: sessionPath,
		log:         log.With().Str("component", "gotd").Logger(),
		client:      client,
		peers:       make(map[int64]tg.InputPeerClass),
		dispatcher:  dispatcher,
	}
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
			previews[id] = Message{ID: int64(m.ID), ConversationID: id, Text: m.Message, Timestamp: int64(m.Date), FromMe: m.Out}
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
		out = append(out, Message{ID: int64(m.ID), ConversationID: conversationID, Text: m.Message, Timestamp: int64(m.Date), FromMe: m.Out})
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
