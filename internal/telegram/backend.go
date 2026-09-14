package telegram

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/rs/zerolog"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

// ErrNotConfigured is returned by operations that require a live MTProto client
// or reviewed API credentials.
var ErrNotConfigured = errors.New("Telegram client is not configured: protocol library integration pending")

// Backend manages the Telegram service state, local data isolation, and protocol routing scaffold.
type Backend struct {
	log     zerolog.Logger
	paths   *appStore.Paths
	publish func(wire.Event)

	mu        sync.RWMutex
	sessionMu sync.Mutex

	status wire.Status
	paired bool

	convs    map[string]wire.Conversation
	order    []string
	messages map[string][]wire.Message
}

// New creates an unstarted Telegram backend scaffold.
func New(log zerolog.Logger, paths *appStore.Paths, publish func(wire.Event)) *Backend {
	return &Backend{
		log:      log.With().Str("network", wire.NetworkTelegram).Logger(),
		paths:    paths,
		publish:  publish,
		convs:    make(map[string]wire.Conversation),
		messages: make(map[string][]wire.Message),
		status: wire.Status{
			Network: wire.NetworkTelegram,
			State:   wire.StateUnpaired,
			PhoneOK: true,
			Hint:    "Telegram protocol client integration pending",
		},
	}
}

// Start initializes the Telegram backend. If a persisted session file exists,
// it marks the backend state accordingly; otherwise it parks in StateUnpaired.
func (b *Backend) Start(ctx context.Context) error {
	b.sessionMu.Lock()
	defer b.sessionMu.Unlock()

	// Ensure private media directory exists.
	if err := os.MkdirAll(b.paths.TelegramMediaDir(), 0o700); err != nil {
		b.log.Error().Err(err).Msg("Failed to create Telegram media directory")
		b.setState(wire.StateDisconnected, "create media dir: "+err.Error())
		return err
	}

	// Check for existing session file (for future protocol client or testing).
	if _, err := os.Stat(b.paths.TelegramSessionFile()); err == nil {
		b.setState(wire.StateConnecting, "saved session detected; protocol client pending")
		return nil
	}

	b.setState(wire.StateUnpaired, "")
	b.log.Info().Msg("Telegram backend initialized in unpaired scaffold mode")
	return nil
}

// Status returns the current Telegram status.
func (b *Backend) Status() wire.Status {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.status
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

// Send rejects sending as the real protocol client is not yet configured.
func (b *Backend) Send(ctx context.Context, p wire.SendParams) (*wire.Message, error) {
	return nil, ErrNotConfigured
}

// SendMedia rejects sending media as the real protocol client is not yet configured.
func (b *Backend) SendMedia(ctx context.Context, p wire.SendMediaParams) (*wire.SendMediaResult, error) {
	return nil, ErrNotConfigured
}

// Media rejects fetching media as the real protocol client is not yet configured.
func (b *Backend) Media(ctx context.Context, p wire.MediaParams) (*wire.MediaResult, error) {
	return nil, ErrNotConfigured
}

// MarkRead rejects mark read as the real protocol client is not yet configured.
func (b *Backend) MarkRead(ctx context.Context, p wire.MarkReadParams) error {
	return ErrNotConfigured
}

// StartPairing reports that live pairing is not yet supported in this scaffold.
func (b *Backend) StartPairing(ctx context.Context) (string, error) {
	return "", ErrNotConfigured
}

// Unpair wipes local Telegram session, store, and media files, and transitions to StateUnpaired.
func (b *Backend) Unpair(ctx context.Context) error {
	b.sessionMu.Lock()
	defer b.sessionMu.Unlock()

	b.mu.Lock()
	b.convs = make(map[string]wire.Conversation)
	b.order = nil
	b.messages = make(map[string][]wire.Message)
	b.paired = false
	b.mu.Unlock()

	clearErr := b.paths.ClearTelegramSession()
	var statusErr string
	if clearErr != nil {
		b.log.Error().Err(clearErr).Msg("Failed to clear local Telegram session files")
		statusErr = "Local Telegram files could not be removed: " + clearErr.Error()
	}
	b.setState(wire.StateUnpaired, statusErr)
	if clearErr != nil {
		return fmt.Errorf("telegram storage cleanup failed: %w", clearErr)
	}
	return nil
}

// Refresh performs a no-op refresh while unpaired.
func (b *Backend) Refresh(ctx context.Context) error {
	return nil
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
