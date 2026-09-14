package whatsapp

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// MockClient provides a synthetic test seam for WhatsApp unit and regression tests.
type MockClient struct {
	mu          sync.RWMutex
	handlers    map[uint32]whatsmeow.EventHandler
	nextHandler atomic.Uint32
	connected   atomic.Bool
	loggedIn    atomic.Bool

	ConnectFunc        func() error
	DisconnectFunc     func()
	GetQRChannelFunc   func(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error)
	SendMessageFunc    func(ctx context.Context, to types.JID, message *waE2E.Message, extra ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error)
	UploadFunc         func(ctx context.Context, plaintext []byte, appInfo whatsmeow.MediaType) (whatsmeow.UploadResponse, error)
	DownloadAnyFunc    func(ctx context.Context, msg *waE2E.Message) ([]byte, error)
	DownloadToFileFunc func(ctx context.Context, msg whatsmeow.DownloadableMessage, file whatsmeow.File) error
	LogoutFunc         func(ctx context.Context) error
	MarkReadFunc       func(ctx context.Context, ids []types.MessageID, timestamp time.Time, chat, sender types.JID, receiptTypeExtra ...types.ReceiptType) error
}

// NewMockClient constructs a MockClient.
func NewMockClient() *MockClient {
	return &MockClient{
		handlers: make(map[uint32]whatsmeow.EventHandler),
	}
}

func (m *MockClient) AddEventHandler(handler whatsmeow.EventHandler) uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.nextHandler.Add(1)
	m.handlers[id] = handler
	return id
}

func (m *MockClient) RemoveEventHandler(id uint32) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.handlers, id)
	return true
}

func (m *MockClient) TriggerEvent(evt any) {
	// Hold the handler read-lock across callbacks, matching whatsmeow.dispatchEvent.
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, h := range m.handlers {
		if h != nil {
			h(evt)
		}
	}
}

func (m *MockClient) Connect() error {
	if m.ConnectFunc != nil {
		return m.ConnectFunc()
	}
	m.connected.Store(true)
	m.TriggerEvent(&events.Connected{})
	return nil
}

func (m *MockClient) Disconnect() {
	if m.DisconnectFunc != nil {
		m.DisconnectFunc()
		return
	}
	m.connected.Store(false)
	m.TriggerEvent(&events.Disconnected{})
}

func (m *MockClient) IsConnected() bool {
	return m.connected.Load()
}

func (m *MockClient) IsLoggedIn() bool {
	return m.loggedIn.Load()
}

func (m *MockClient) SetLoggedIn(v bool) {
	m.loggedIn.Store(v)
}

func (m *MockClient) SetConnected(v bool) {
	m.connected.Store(v)
}

func (m *MockClient) GetQRChannel(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	if m.GetQRChannelFunc != nil {
		return m.GetQRChannelFunc(ctx)
	}
	ch := make(chan whatsmeow.QRChannelItem, 2)
	ch <- whatsmeow.QRChannelItem{Event: whatsmeow.QRChannelEventCode, Code: "mock-qr-code-payload"}
	return ch, nil
}

func (m *MockClient) SendMessage(ctx context.Context, to types.JID, message *waE2E.Message, extra ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error) {
	if m.SendMessageFunc != nil {
		return m.SendMessageFunc(ctx, to, message, extra...)
	}
	return whatsmeow.SendResponse{
		ID:        "mock-msg-id-123",
		Timestamp: time.Now(),
	}, nil
}

func (m *MockClient) Upload(ctx context.Context, plaintext []byte, appInfo whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
	if m.UploadFunc != nil {
		return m.UploadFunc(ctx, plaintext, appInfo)
	}
	return whatsmeow.UploadResponse{
		URL:           "https://mock.whatsapp.net/upload/123",
		DirectPath:    "/mock/direct/path",
		MediaKey:      []byte("mock-32-byte-media-key-12345678"),
		FileSHA256:    []byte("mock-sha256-hash-bytes-12345678"),
		FileEncSHA256: []byte("mock-encsha-hash-bytes-12345678"),
	}, nil
}

func (m *MockClient) DownloadToFile(ctx context.Context, msg whatsmeow.DownloadableMessage, file whatsmeow.File) error {
	if m.DownloadToFileFunc != nil {
		return m.DownloadToFileFunc(ctx, msg, file)
	}
	data, err := m.DownloadAny(ctx, nil)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	return err
}

func (m *MockClient) DownloadAny(ctx context.Context, msg *waE2E.Message) ([]byte, error) {
	if m.DownloadAnyFunc != nil {
		return m.DownloadAnyFunc(ctx, msg)
	}
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
		0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
		0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}, nil
}

func (m *MockClient) Logout(ctx context.Context) error {
	if m.LogoutFunc != nil {
		return m.LogoutFunc(ctx)
	}
	m.connected.Store(false)
	m.loggedIn.Store(false)
	return nil
}

func (m *MockClient) MarkRead(ctx context.Context, ids []types.MessageID, timestamp time.Time, chat, sender types.JID, receiptTypeExtra ...types.ReceiptType) error {
	if m.MarkReadFunc != nil {
		return m.MarkReadFunc(ctx, ids, timestamp, chat, sender, receiptTypeExtra...)
	}
	return nil
}
