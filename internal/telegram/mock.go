package telegram

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/gotd/td/telegram"
)

// MockClient provides a synthetic test seam for Telegram unit and regression tests.
type MockClient struct {
	connected  atomic.Bool
	authorized atomic.Bool

	StartFunc         func(ctx context.Context) error
	StopFunc          func() error
	IsConnectedFunc   func() bool
	IsAuthorizedFunc  func(ctx context.Context) (bool, error)
	GetQRChannelFunc  func(ctx context.Context) (<-chan QRChannelItem, error)
	PingFunc          func(ctx context.Context) error
	UnderlyingFunc    func() *telegram.Client
	DialogsFunc       func(ctx context.Context, limit int) ([]Dialog, error)
	MessagesFunc      func(ctx context.Context, conversationID int64, limit int) ([]Message, error)
	MarkReadFunc      func(ctx context.Context, conversationID int64, messageID int64) error
	SendTextFunc      func(ctx context.Context, conversationID int64, text string) (Message, error)
	SendImageFunc     func(ctx context.Context, conversationID int64, path, caption string) (Message, error)
	SendVoiceFunc     func(ctx context.Context, conversationID int64, path, caption string) (Message, error)
	DownloadMediaFunc func(ctx context.Context, key, dir string) (string, error)
}

func (m *MockClient) SendText(ctx context.Context, conversationID int64, text string) (Message, error) {
	if m.SendTextFunc != nil {
		return m.SendTextFunc(ctx, conversationID, text)
	}
	return Message{}, nil
}

func (m *MockClient) SendImage(ctx context.Context, conversationID int64, path, caption string) (Message, error) {
	if m.SendImageFunc != nil {
		return m.SendImageFunc(ctx, conversationID, path, caption)
	}
	return Message{}, nil
}

func (m *MockClient) SendVoice(ctx context.Context, conversationID int64, path, caption string) (Message, error) {
	if m.SendVoiceFunc != nil {
		return m.SendVoiceFunc(ctx, conversationID, path, caption)
	}
	return Message{}, errors.New("mock voice send not configured")
}

func (m *MockClient) DownloadMedia(ctx context.Context, key, dir string) (string, error) {
	if m.DownloadMediaFunc != nil {
		return m.DownloadMediaFunc(ctx, key, dir)
	}
	return "", errors.New("mock media download not configured")
}

func (m *MockClient) MarkRead(ctx context.Context, conversationID int64, messageID int64) error {
	if m.MarkReadFunc != nil {
		return m.MarkReadFunc(ctx, conversationID, messageID)
	}
	return nil
}

func (m *MockClient) Dialogs(ctx context.Context, limit int) ([]Dialog, error) {
	if m.DialogsFunc != nil {
		return m.DialogsFunc(ctx, limit)
	}
	return nil, nil
}

func (m *MockClient) Messages(ctx context.Context, conversationID int64, limit int) ([]Message, error) {
	if m.MessagesFunc != nil {
		return m.MessagesFunc(ctx, conversationID, limit)
	}
	return nil, nil
}

var _ Client = (*MockClient)(nil)

// NewMockClient constructs a MockClient.
func NewMockClient() *MockClient {
	m := &MockClient{}
	m.connected.Store(false)
	m.authorized.Store(false)
	return m
}

// Start invokes StartFunc or marks the client connected.
func (m *MockClient) Start(ctx context.Context) error {
	if m.StartFunc != nil {
		return m.StartFunc(ctx)
	}
	m.connected.Store(true)
	return nil
}

// Stop invokes StopFunc or marks the client disconnected.
func (m *MockClient) Stop() error {
	if m.StopFunc != nil {
		return m.StopFunc()
	}
	m.connected.Store(false)
	return nil
}

// IsConnected reports whether the mock client is connected.
func (m *MockClient) IsConnected() bool {
	if m.IsConnectedFunc != nil {
		return m.IsConnectedFunc()
	}
	return m.connected.Load()
}

// SetConnected manually updates the connected state.
func (m *MockClient) SetConnected(v bool) {
	m.connected.Store(v)
}

// IsAuthorized reports whether the mock client is authorized.
func (m *MockClient) IsAuthorized(ctx context.Context) (bool, error) {
	if m.IsAuthorizedFunc != nil {
		return m.IsAuthorizedFunc(ctx)
	}
	return m.authorized.Load(), nil
}

// SetAuthorized manually updates the authorized state.
func (m *MockClient) SetAuthorized(v bool) {
	m.authorized.Store(v)
}

// GetQRChannel invokes GetQRChannelFunc or yields a default mock QR URL.
func (m *MockClient) GetQRChannel(ctx context.Context) (<-chan QRChannelItem, error) {
	if m.GetQRChannelFunc != nil {
		return m.GetQRChannelFunc(ctx)
	}
	ch := make(chan QRChannelItem, 2)
	ch <- QRChannelItem{
		Event: QRChannelEventCode,
		Code:  "tg://login?token=bW9jay1xci1sb2dpbi10b2tlbg",
	}
	return ch, nil
}

// Ping invokes PingFunc or returns nil.
func (m *MockClient) Ping(ctx context.Context) error {
	if m.PingFunc != nil {
		return m.PingFunc(ctx)
	}
	return nil
}

// Underlying invokes UnderlyingFunc or returns nil.
func (m *MockClient) Underlying() *telegram.Client {
	if m.UnderlyingFunc != nil {
		return m.UnderlyingFunc()
	}
	return nil
}
