package telegram

import (
	"context"
	"errors"
	"fmt"
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
	client     *telegram.Client
	dispatcher tg.UpdateDispatcher
	cancel     context.CancelFunc
	running    bool
	connected  bool
}

var _ Client = (*GotdClient)(nil)

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
		defer close(qrChan)
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
			select {
			case qrChan <- QRChannelItem{Event: QRChannelEventError, Error: sendErr}:
			default:
			}
		}
	}()

	return qrChan, nil
}

// Ping pings the Telegram DC to check connectivity.
func (g *GotdClient) Ping(ctx context.Context) error {
	return g.client.Ping(ctx)
}

// Underlying returns the underlying gotd *telegram.Client.
func (g *GotdClient) Underlying() *telegram.Client {
	return g.client
}
