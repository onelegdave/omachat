package telegram

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

func setupTestTelegram(t *testing.T) (*Backend, *store.Paths, chan wire.Event) {
	t.Helper()
	dataDir := t.TempDir()
	cacheDir := t.TempDir()
	runtimeDir := t.TempDir()

	paths := &store.Paths{
		Data:    dataDir,
		Cache:   cacheDir,
		Runtime: runtimeDir,
	}
	_ = os.MkdirAll(paths.MediaDir(), 0o700)
	_ = os.MkdirAll(paths.WhatsAppMediaDir(), 0o700)
	_ = os.MkdirAll(paths.TelegramMediaDir(), 0o700)

	events := make(chan wire.Event, 20)
	publish := func(evt wire.Event) {
		select {
		case events <- evt:
		default:
		}
	}

	b := New(zerolog.Nop(), paths, publish)
	mock := NewMockClient()
	b.SetClientFactory(func(creds store.TelegramCredentials, sessionPath string) (Client, error) {
		return mock, nil
	})
	return b, paths, events
}

func setupTestTelegramWithMock(t *testing.T) (*Backend, *store.Paths, chan wire.Event, *MockClient) {
	t.Helper()
	b, paths, events := setupTestTelegram(t)
	mock := NewMockClient()
	b.SetClientFactory(func(creds store.TelegramCredentials, sessionPath string) (Client, error) {
		return mock, nil
	})
	return b, paths, events, mock
}

func TestBackendInitialState(t *testing.T) {
	b, _, _ := setupTestTelegram(t)
	st := b.Status()

	if st.Network != wire.NetworkTelegram {
		t.Errorf("expected network %q, got %q", wire.NetworkTelegram, st.Network)
	}
	if st.State != wire.StateUnpaired {
		t.Errorf("expected state %q, got %q", wire.StateUnpaired, st.State)
	}
	if !st.PhoneOK {
		t.Error("expected PhoneOK to be true")
	}
	if st.Hint == "" {
		t.Error("expected non-empty Hint explaining pending integration")
	}
}

func TestBackendStartWithoutSession(t *testing.T) {
	b, paths, _ := setupTestTelegram(t)
	ctx := context.Background()

	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	st := b.Status()
	if st.State != wire.StateUnpaired {
		t.Errorf("expected state %q, got %q", wire.StateUnpaired, st.State)
	}
	if _, err := os.Stat(paths.TelegramMediaDir()); err != nil {
		t.Errorf("expected Telegram media dir to exist, got: %v", err)
	}
}

func TestBackendStartWithSession(t *testing.T) {
	b, paths, _ := setupTestTelegram(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	if err := cs.SetTelegramCredentials(1234567, "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	b.SetConfig(cs)
	ctx := context.Background()

	// Seed dummy session file
	if err := os.WriteFile(paths.TelegramSessionFile(), []byte("tg-session-data"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	st := b.Status()
	if st.State != wire.StateConnected {
		t.Errorf("expected state %q when session exists, got %q", wire.StateConnected, st.State)
	}
}

func TestBackendStartWithSessionMissingCredentials(t *testing.T) {
	t.Setenv("OMACHAT_TELEGRAM_API_ID", "")
	t.Setenv("OMACHAT_TELEGRAM_API_HASH", "")

	b, paths, _ := setupTestTelegram(t)
	ctx := context.Background()

	// Seed session file without credentials configured
	if err := os.WriteFile(paths.TelegramSessionFile(), []byte("tg-session-data"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	st := b.Status()
	if st.State != wire.StateUnpaired {
		t.Errorf("expected state %q when credentials missing, got %q", wire.StateUnpaired, st.State)
	}
	if st.Hint != hintCredentialsRequired {
		t.Errorf("hint = %q, want %q", st.Hint, hintCredentialsRequired)
	}
	if b.Client() != nil {
		t.Error("expected client to remain uninstantiated when credentials are missing")
	}
}

func TestBackendStartWithSessionRestoresViaAbstraction(t *testing.T) {
	b, paths, _, mock := setupTestTelegramWithMock(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	if err := cs.SetTelegramCredentials(1234567, "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	b.SetConfig(cs)

	startCalled := false
	mock.StartFunc = func(ctx context.Context) error {
		startCalled = true
		mock.SetConnected(true)
		return nil
	}

	if err := os.WriteFile(paths.TelegramSessionFile(), []byte("session-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !startCalled {
		t.Error("expected mock client Start to be called during session restore")
	}
	if b.Status().State != wire.StateConnected {
		t.Errorf("expected state %q, got %q", wire.StateConnected, b.Status().State)
	}
}

func TestBackendStartSessionRestoreFailure(t *testing.T) {
	b, paths, _, mock := setupTestTelegramWithMock(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	if err := cs.SetTelegramCredentials(1234567, "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	b.SetConfig(cs)

	mock.StartFunc = func(ctx context.Context) error {
		return errors.New("simulated restore failure")
	}

	if err := os.WriteFile(paths.TelegramSessionFile(), []byte("session-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if b.Status().State != wire.StateDisconnected {
		t.Errorf("expected StateDisconnected on restore failure, got %s", b.Status().State)
	}
}

func TestBackendUnconfiguredOperations(t *testing.T) {
	b, _, _ := setupTestTelegram(t)
	ctx := context.Background()

	// Send
	if _, err := b.Send(ctx, wire.SendParams{ConversationID: "123", Text: "hi"}); err != ErrNotConfigured {
		t.Errorf("expected ErrNotConfigured for Send, got %v", err)
	}

	// SendMedia
	if _, err := b.SendMedia(ctx, wire.SendMediaParams{ConversationID: "123", Path: "/tmp/img.png"}); err != ErrNotConfigured {
		t.Errorf("expected ErrNotConfigured for SendMedia, got %v", err)
	}

	// Media
	if _, err := b.Media(ctx, wire.MediaParams{Key: "k1"}); err != ErrNotConfigured {
		t.Errorf("expected ErrNotConfigured for Media, got %v", err)
	}

	// MarkRead
	if err := b.MarkRead(ctx, wire.MarkReadParams{ConversationID: "123"}); err != ErrNotConfigured {
		t.Errorf("expected ErrNotConfigured for MarkRead, got %v", err)
	}

	// StartPairing
	if _, err := b.StartPairing(ctx); err != ErrNotConfigured {
		t.Errorf("expected ErrNotConfigured for StartPairing, got %v", err)
	}

	// Refresh (no-op)
	if err := b.Refresh(ctx); err != nil {
		t.Errorf("expected nil for Refresh, got %v", err)
	}
}

func TestBackendConversationsAndMessages(t *testing.T) {
	b, _, _ := setupTestTelegram(t)
	ctx := context.Background()

	// Initially empty
	convs := b.Conversations(50)
	if len(convs) != 0 {
		t.Errorf("expected 0 conversations, got %d", len(convs))
	}

	// Add test conversation
	testConv := wire.Conversation{
		ID:      "tg-conv-1",
		Name:    "Telegram Group",
		Preview: "Hello Telegram",
	}
	b.AddTestConversation(testConv)

	convs = b.Conversations(50)
	if len(convs) != 1 || convs[0].ID != "tg-conv-1" {
		t.Errorf("expected 1 conversation tg-conv-1, got %+v", convs)
	}

	// Add test messages
	testMsgs := []wire.Message{
		{ID: "m1", ConversationID: "tg-conv-1", Text: "Hello"},
	}
	b.SetTestMessages("tg-conv-1", testMsgs)

	res, err := b.Messages(ctx, wire.MessagesParams{ConversationID: "tg-conv-1"})
	if err != nil {
		t.Fatalf("Messages returned error: %v", err)
	}
	if len(res.Messages) != 1 || res.Messages[0].ID != "m1" {
		t.Errorf("expected message m1, got %+v", res.Messages)
	}
}

func TestBackendUnpairCleanup(t *testing.T) {
	b, paths, events := setupTestTelegram(t)
	ctx := context.Background()

	// Seed files
	if err := os.WriteFile(paths.TelegramSessionFile(), []byte("tg-session"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TelegramStoreFile(), []byte("tg-store"), 0o600); err != nil {
		t.Fatal(err)
	}
	testMedia := filepath.Join(paths.TelegramMediaDir(), "media.jpg")
	if err := os.WriteFile(testMedia, []byte("media-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	b.AddTestConversation(wire.Conversation{ID: "tg-conv-1", Name: "Test"})

	// Unpair
	if err := b.Unpair(ctx); err != nil {
		t.Fatalf("Unpair failed: %v", err)
	}

	// Verify files deleted
	if _, err := os.Stat(paths.TelegramSessionFile()); !os.IsNotExist(err) {
		t.Errorf("Telegram session file was not removed: %v", err)
	}
	if _, err := os.Stat(paths.TelegramStoreFile()); !os.IsNotExist(err) {
		t.Errorf("Telegram store file was not removed: %v", err)
	}
	if _, err := os.Stat(testMedia); !os.IsNotExist(err) {
		t.Errorf("Telegram media file was not removed: %v", err)
	}

	// Verify in-memory state cleared
	if len(b.Conversations(50)) != 0 {
		t.Error("conversations were not cleared after unpair")
	}
	if b.Status().State != wire.StateUnpaired {
		t.Errorf("expected state %q, got %q", wire.StateUnpaired, b.Status().State)
	}

	// Verify status event published
	select {
	case evt := <-events:
		if evt.Event != wire.EventStatus || evt.Network != wire.NetworkTelegram {
			t.Errorf("unexpected event: %+v", evt)
		}
	default:
		t.Error("expected status event to be published on unpair")
	}
}

func TestTelegramStartUnconfiguredStatus(t *testing.T) {
	t.Setenv("OMACHAT_TELEGRAM_API_ID", "")
	t.Setenv("OMACHAT_TELEGRAM_API_HASH", "")

	b, _, _ := setupTestTelegram(t)
	ctx := context.Background()

	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	st := b.Status()
	if st.State != wire.StateUnpaired {
		t.Errorf("expected state %q for unconfigured Telegram, got %q", wire.StateUnpaired, st.State)
	}
	const wantHint = "Telegram API credentials required. Run: python3 ~/.config/omarchy/plugins/onelegdave.omachat/scripts/configure-telegram.py (without sudo). API hash input stays blank while typing. Then run: omarchy restart shell, reopen Telegram, and choose Pair with Telegram. Obtain credentials from my.telegram.org."
	if st.Hint != wantHint {
		t.Errorf("hint = %q, want %q", st.Hint, wantHint)
	}
	if st.Error != "" {
		t.Errorf("expected empty Error for clean unconfigured state, got %q", st.Error)
	}
	if b.Client() != nil {
		t.Error("expected Client to remain nil when unconfigured")
	}
}

func TestTelegramStartConfiguredStatus(t *testing.T) {
	t.Setenv("OMACHAT_TELEGRAM_API_ID", "")
	t.Setenv("OMACHAT_TELEGRAM_API_HASH", "")

	b, paths, _ := setupTestTelegram(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	if err := cs.SetTelegramCredentials(1234567, "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	b.SetConfig(cs)

	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	st := b.Status()
	if st.State != wire.StateUnpaired {
		t.Errorf("expected state %q, got %q", wire.StateUnpaired, st.State)
	}
	const wantHint = "Telegram API credentials configured; pairing not yet started"
	if st.Hint != wantHint {
		t.Errorf("hint = %q, want %q", st.Hint, wantHint)
	}
	if st.Error != "" {
		t.Errorf("expected empty Error, got %q", st.Error)
	}
	// Invariant: no client connection or QR pairing started
	if b.Client() != nil {
		t.Error("expected Client to remain nil in this milestone")
	}
}

func TestTelegramStartEnvironmentFallback(t *testing.T) {
	t.Setenv("OMACHAT_TELEGRAM_API_ID", "7654321")
	t.Setenv("OMACHAT_TELEGRAM_API_HASH", "fedcba9876543210fedcba9876543210")

	b, _, _ := setupTestTelegram(t)
	ctx := context.Background()

	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	st := b.Status()
	if st.State != wire.StateUnpaired {
		t.Errorf("expected state %q, got %q", wire.StateUnpaired, st.State)
	}
	const wantHint = "Telegram API credentials configured; pairing not yet started"
	if st.Hint != wantHint {
		t.Errorf("hint = %q, want %q", st.Hint, wantHint)
	}
	if b.Client() != nil {
		t.Error("expected Client to remain nil in this milestone")
	}
}

func TestTelegramStartMalformedCredentials(t *testing.T) {
	t.Setenv("OMACHAT_TELEGRAM_API_ID", "")
	t.Setenv("OMACHAT_TELEGRAM_API_HASH", "")

	b, paths, _ := setupTestTelegram(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	// Set invalid hash
	_ = cs.SetTelegramCredentials(12345, "invalid-hash-too-short")
	b.SetConfig(cs)

	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	st := b.Status()
	if st.State != wire.StateUnpaired {
		t.Errorf("expected state %q, got %q", wire.StateUnpaired, st.State)
	}
	if !strings.Contains(st.Hint, "Telegram API credentials required") {
		t.Errorf("expected credentials required in hint, got %q", st.Hint)
	}
	if st.Error == "" {
		t.Error("expected non-empty Error for malformed credentials")
	}
	if strings.Contains(st.Error, "invalid-hash-too-short") {
		t.Error("SECURITY: secret leaked into status Error message")
	}
}

func TestTelegramUnpairPreservesConfiguredHint(t *testing.T) {
	t.Setenv("OMACHAT_TELEGRAM_API_ID", "")
	t.Setenv("OMACHAT_TELEGRAM_API_HASH", "")

	b, paths, _ := setupTestTelegram(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	_ = cs.SetTelegramCredentials(1234567, "0123456789abcdef0123456789abcdef")
	b.SetConfig(cs)

	// Seed session file
	if err := os.WriteFile(paths.TelegramSessionFile(), []byte("tg-session"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if b.Status().State != wire.StateConnected {
		t.Fatalf("expected StateConnected with session, got %s", b.Status().State)
	}

	if err := b.Unpair(ctx); err != nil {
		t.Fatal(err)
	}
	st := b.Status()
	if st.State != wire.StateUnpaired {
		t.Errorf("expected StateUnpaired, got %s", st.State)
	}
	if st.Hint != "Telegram API credentials configured; pairing not yet started" {
		t.Errorf("expected configured hint after unpair with valid credentials, got %q", st.Hint)
	}
}

func TestTelegramRefreshLoadsAndPersistsReadOnlyData(t *testing.T) {
	b, paths, _, mock := setupTestTelegramWithMock(t)
	mock.DialogsFunc = func(context.Context, int) ([]Dialog, error) {
		return []Dialog{{ID: 7, Name: "Alice", Preview: "hello", Timestamp: 20}}, nil
	}
	mock.MessagesFunc = func(context.Context, int64, int) ([]Message, error) {
		return []Message{{ID: 2, ConversationID: 7, Text: "hello", Timestamp: 20}}, nil
	}
	b.SetClient(mock)
	if err := b.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := b.Conversations(10); len(got) != 1 || got[0].ID != "tg:7" {
		t.Fatalf("unexpected conversations: %+v", got)
	}
	data, err := os.ReadFile(paths.TelegramStoreFile())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "tg:7") {
		t.Fatalf("persisted data missing conversation: %s", data)
	}
	reloaded := New(zerolog.Nop(), paths, nil)
	if got := reloaded.Conversations(10); len(got) != 1 || got[0].ID != "tg:7" {
		t.Fatalf("reload failed: %+v", got)
	}
}

func TestTelegramMarkReadAcknowledgesAndPersists(t *testing.T) {
	b, paths, _, mock := setupTestTelegramWithMock(t)
	b.SetClient(mock)
	called := false
	var gotConversation, gotMessage int64
	mock.MarkReadFunc = func(_ context.Context, conversationID, messageID int64) error {
		called = true
		gotConversation, gotMessage = conversationID, messageID
		return nil
	}
	b.AddTestConversation(wire.Conversation{ID: "tg:7", Name: "Alice", Unread: true})
	if err := b.MarkRead(context.Background(), wire.MarkReadParams{ConversationID: "tg:7", MessageID: "tg:42"}); err != nil {
		t.Fatal(err)
	}
	if !called || gotConversation != 7 || gotMessage != 42 {
		t.Fatalf("unexpected mark read call: called=%v conversation=%d message=%d", called, gotConversation, gotMessage)
	}
	if got := b.Conversations(1)[0].Unread; got {
		t.Fatal("conversation remained unread")
	}
	data, err := os.ReadFile(paths.TelegramStoreFile())
	if err != nil || strings.Contains(string(data), `"unread":true`) {
		t.Fatalf("unread state was not persisted: err=%v data=%s", err, data)
	}
}

func TestTelegramMarkReadRejectsMalformedIDs(t *testing.T) {
	b, _, _, mock := setupTestTelegramWithMock(t)
	called := false
	mock.MarkReadFunc = func(context.Context, int64, int64) error { called = true; return nil }
	if err := b.MarkRead(context.Background(), wire.MarkReadParams{ConversationID: "alice", MessageID: "tg:1"}); err == nil {
		t.Fatal("expected malformed conversation ID error")
	}
	if called {
		t.Fatal("transport called for malformed ID")
	}
}

func TestTelegramIncomingMessageUpdatesCacheAndPublishes(t *testing.T) {
	b, paths, events := setupTestTelegram(t)
	b.ingestMessage(Message{ID: 9, ConversationID: 7, Text: "live", Timestamp: 123, SenderID: 7, SenderName: "Alice"})
	convs := b.Conversations(1)
	if len(convs) != 1 || convs[0].ID != "tg:7" || convs[0].Preview != "live" || !convs[0].Unread {
		t.Fatalf("unexpected incoming conversation: %+v", convs)
	}
	result, err := b.Messages(context.Background(), wire.MessagesParams{ConversationID: "tg:7"})
	if err != nil || len(result.Messages) != 1 || result.Messages[0].ID != "tg:9" {
		t.Fatalf("unexpected incoming message cache: %+v err=%v", result.Messages, err)
	}
	select {
	case event := <-events:
		if event.Event != wire.EventMessage || event.Network != wire.NetworkTelegram {
			t.Fatalf("unexpected event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("incoming message event was not published")
	}
	if _, err := os.Stat(paths.TelegramStoreFile()); err != nil {
		t.Fatalf("incoming message was not persisted: %v", err)
	}
}

func TestTelegramSendTextRoutesAndCaches(t *testing.T) {
	b, _, _, mock := setupTestTelegramWithMock(t)
	b.SetClient(mock)
	b.AddTestConversation(wire.Conversation{ID: "tg:7", Name: "Alice"})
	var gotID int64
	mock.SendTextFunc = func(_ context.Context, id int64, text string) (Message, error) {
		gotID = id
		return Message{ID: 12, ConversationID: id, Text: text, Timestamp: 44, FromMe: true}, nil
	}
	msg, err := b.Send(context.Background(), wire.SendParams{TmpID: "tx-1", ConversationID: "tg:7", Text: "hello"})
	if err != nil || msg == nil || msg.ID != "tg:12" || msg.TmpID != "tx-1" || msg.Text != "hello" || !msg.FromMe || msg.Delivery != wire.DeliverySent {
		t.Fatalf("unexpected send result: %+v err=%v", msg, err)
	}
	if gotID != 7 {
		t.Fatalf("transport received conversation %d", gotID)
	}
	result, _ := b.Messages(context.Background(), wire.MessagesParams{ConversationID: "tg:7"})
	if len(result.Messages) != 1 || result.Messages[0].ID != "tg:12" {
		t.Fatalf("sent message was not cached: %+v", result.Messages)
	}
}

func TestTelegramSendMediaReturnsAttachmentMetadata(t *testing.T) {
	b, paths, _, mock := setupTestTelegramWithMock(t)
	b.SetClient(mock)
	b.AddTestConversation(wire.Conversation{ID: "tg:7", Name: "Alice"})
	path := filepath.Join(paths.TelegramMediaDir(), "photo.jpg")
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	mock.SendImageFunc = func(_ context.Context, id int64, gotPath, caption string) (Message, error) {
		if id != 7 || gotPath != path || caption != "caption" {
			t.Fatalf("unexpected media args")
		}
		return Message{ID: 13, ConversationID: id, Text: caption, Timestamp: 44, FromMe: true}, nil
	}
	res, err := b.SendMedia(context.Background(), wire.SendMediaParams{TmpID: "tx-2", ConversationID: "tg:7", Path: path, Caption: "caption"})
	if err != nil || res == nil || res.Message == nil || len(res.Message.Attachments) != 1 || !res.Message.Attachments[0].IsImage {
		t.Fatalf("unexpected media result: %+v err=%v", res, err)
	}
}

func TestTelegramSendMediaRoutesVoiceNotes(t *testing.T) {
	b, paths, _, mock := setupTestTelegramWithMock(t)
	b.SetClient(mock)
	path := filepath.Join(paths.TelegramMediaDir(), "voice.ogg")
	if err := os.WriteFile(path, []byte("voice"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	mock.SendVoiceFunc = func(_ context.Context, id int64, gotPath, caption string) (Message, error) {
		called = id == 7 && gotPath == path && caption == "note"
		return Message{ID: 14, ConversationID: id, Text: caption, Timestamp: 45, FromMe: true}, nil
	}
	res, err := b.SendMedia(context.Background(), wire.SendMediaParams{ConversationID: "tg:7", Path: path, Caption: "note"})
	if err != nil || !called || res == nil || res.Message == nil || len(res.Message.Attachments) != 1 || res.Message.Attachments[0].Key == "" || !res.Message.Attachments[0].IsAudio || res.Message.Attachments[0].MimeType != "audio/ogg" {
		t.Fatalf("voice send failed: %+v err=%v called=%v", res, err, called)
	}
}

func TestStartPairingUnconfigured(t *testing.T) {
	t.Setenv("OMACHAT_TELEGRAM_API_ID", "")
	t.Setenv("OMACHAT_TELEGRAM_API_HASH", "")

	b, _, events := setupTestTelegram(t)
	ctx := context.Background()

	_, err := b.StartPairing(ctx)
	if err != ErrNotConfigured {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}

	st := b.Status()
	if st.State != wire.StateUnpaired {
		t.Errorf("expected StateUnpaired, got %s", st.State)
	}
	if st.Hint != hintCredentialsRequired {
		t.Errorf("hint = %q, want %q", st.Hint, hintCredentialsRequired)
	}
	if st.Error != "" {
		t.Errorf("expected empty Error to avoid duplicating hint, got %q", st.Error)
	}

	select {
	case evt := <-events:
		if evt.Event != wire.EventStatus || evt.Network != wire.NetworkTelegram {
			t.Errorf("unexpected event: %+v", evt)
		}
	default:
		t.Error("expected status event to be emitted on unconfigured pairing failure")
	}
}

func TestStartPairingMalformedCredentials(t *testing.T) {
	t.Setenv("OMACHAT_TELEGRAM_API_ID", "")
	t.Setenv("OMACHAT_TELEGRAM_API_HASH", "")

	b, paths, events := setupTestTelegram(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	_ = cs.SetTelegramCredentials(1234567, "not-a-valid-hex-hash-too-short")
	b.SetConfig(cs)

	ctx := context.Background()
	_, err := b.StartPairing(ctx)
	if err == nil {
		t.Fatal("expected error for malformed credentials in StartPairing")
	}

	st := b.Status()
	if st.State != wire.StateUnpaired {
		t.Errorf("expected StateUnpaired, got %s", st.State)
	}
	if strings.Contains(st.Error, "not-a-valid-hex") {
		t.Error("SECURITY: secret leaked into status Error message")
	}
	if st.Error == "" || st.Error == st.Hint || st.Hint != hintCredentialsRequired {
		t.Error("malformed credentials must retain a distinct error and setup guidance")
	}

	select {
	case evt := <-events:
		if evt.Event != wire.EventStatus {
			t.Errorf("unexpected event: %+v", evt)
		}
	default:
		t.Error("expected status event to be emitted")
	}
}

func TestStartPairingAlreadyPaired(t *testing.T) {
	b, paths, _ := setupTestTelegram(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	_ = cs.SetTelegramCredentials(1234567, "0123456789abcdef0123456789abcdef")
	b.SetConfig(cs)
	b.SetPaired(true)

	ctx := context.Background()
	_, err := b.StartPairing(ctx)
	if err == nil || !strings.Contains(err.Error(), "already paired") {
		t.Fatalf("expected already paired error, got: %v", err)
	}
}

func TestStartPairingSuccessFlow(t *testing.T) {
	b, paths, events, mock := setupTestTelegramWithMock(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	if err := cs.SetTelegramCredentials(1234567, "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	b.SetConfig(cs)

	qrChan := make(chan QRChannelItem, 4)
	mock.GetQRChannelFunc = func(ctx context.Context) (<-chan QRChannelItem, error) {
		return qrChan, nil
	}

	const initialURL = "tg://login?token=initial-token-sample"
	qrChan <- QRChannelItem{Event: QRChannelEventCode, Code: initialURL}

	ctx := context.Background()
	gotURL, err := b.StartPairing(ctx)
	if err != nil {
		t.Fatalf("StartPairing failed: %v", err)
	}
	if gotURL != initialURL {
		t.Errorf("got URL %q, want %q", gotURL, initialURL)
	}

	st := b.Status()
	if st.State != wire.StatePairing {
		t.Errorf("expected state %q, got %q", wire.StatePairing, st.State)
	}
	if st.QRURL != initialURL {
		t.Errorf("expected QRURL %q, got %q", initialURL, st.QRURL)
	}

	// Verify pairing status event
	var seenPairingEvent bool
	for len(events) > 0 {
		e := <-events
		if e.Event == wire.EventStatus && e.Network == wire.NetworkTelegram {
			if s, ok := e.Data.(wire.Status); ok && s.State == wire.StatePairing && s.QRURL == initialURL {
				seenPairingEvent = true
			}
		}
	}
	if !seenPairingEvent {
		t.Error("expected status event with StatePairing and initial QRURL")
	}

	// 2. Token refresh
	const refreshedURL = "tg://login?token=refreshed-token-sample"
	qrChan <- QRChannelItem{Event: QRChannelEventCode, Code: refreshedURL}

	// Allow listener goroutine to process
	time.Sleep(20 * time.Millisecond)

	st = b.Status()
	if st.QRURL != refreshedURL {
		t.Errorf("expected refreshed QRURL %q, got %q", refreshedURL, st.QRURL)
	}

	// 3. User accepts login on phone
	qrChan <- QRChannelItem{Event: QRChannelEventSuccess}
	time.Sleep(20 * time.Millisecond)

	st = b.Status()
	if st.State != wire.StateConnected {
		t.Errorf("expected state %q after success, got %q", wire.StateConnected, st.State)
	}
	if st.QRURL != "" {
		t.Errorf("expected empty QRURL after pairing success, got %q", st.QRURL)
	}
}

func TestStartPairingContextCancellation(t *testing.T) {
	b, paths, _, mock := setupTestTelegramWithMock(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	_ = cs.SetTelegramCredentials(1234567, "0123456789abcdef0123456789abcdef")
	b.SetConfig(cs)

	qrChan := make(chan QRChannelItem)
	mock.GetQRChannelFunc = func(ctx context.Context) (<-chan QRChannelItem, error) {
		return qrChan, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before first token arrives

	_, err := b.StartPairing(ctx)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}

	st := b.Status()
	if st.State != wire.StateUnpaired {
		t.Errorf("expected StateUnpaired after cancellation, got %s", st.State)
	}
	if st.QRURL != "" {
		t.Errorf("expected empty QRURL after cancellation, got %s", st.QRURL)
	}
	if !strings.Contains(st.Error, "cancelled") {
		t.Errorf("expected cancellation in Error, got %q", st.Error)
	}
}

func TestStartPairing2FAPasswordNeeded(t *testing.T) {
	b, paths, _, mock := setupTestTelegramWithMock(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	_ = cs.SetTelegramCredentials(1234567, "0123456789abcdef0123456789abcdef")
	b.SetConfig(cs)

	qrChan := make(chan QRChannelItem, 4)
	mock.GetQRChannelFunc = func(ctx context.Context) (<-chan QRChannelItem, error) {
		return qrChan, nil
	}

	qrChan <- QRChannelItem{Event: QRChannelEventCode, Code: "tg://login?token=sample"}

	ctx := context.Background()
	_, err := b.StartPairing(ctx)
	if err != nil {
		t.Fatalf("StartPairing failed: %v", err)
	}

	// Send 2FA error
	qrChan <- QRChannelItem{
		Event: QRChannelEventError,
		Error: errors.New("SESSION_PASSWORD_NEEDED: 2FA required"),
	}
	time.Sleep(20 * time.Millisecond)

	st := b.Status()
	if st.State != wire.StateUnpaired {
		t.Errorf("expected StateUnpaired after 2FA error, got %s", st.State)
	}
	if !strings.Contains(st.Error, "Telegram 2FA Cloud Password required") {
		t.Errorf("expected 2FA cloud password message in Error, got %q", st.Error)
	}
}

func TestStartPairingClientError(t *testing.T) {
	b, paths, _, mock := setupTestTelegramWithMock(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	_ = cs.SetTelegramCredentials(1234567, "0123456789abcdef0123456789abcdef")
	b.SetConfig(cs)

	mock.GetQRChannelFunc = func(ctx context.Context) (<-chan QRChannelItem, error) {
		return nil, errors.New("cannot connect to Telegram DC")
	}

	ctx := context.Background()
	_, err := b.StartPairing(ctx)
	if err == nil {
		t.Fatal("expected error when GetQRChannel fails")
	}

	st := b.Status()
	if st.State != wire.StateUnpaired {
		t.Errorf("expected StateUnpaired, got %s", st.State)
	}
	if !strings.Contains(st.Error, "Could not start Telegram QR pairing session") {
		t.Errorf("unexpected Error message: %q", st.Error)
	}
}

func TestStartPairingPrematureChannelClose(t *testing.T) {
	b, paths, _, mock := setupTestTelegramWithMock(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	_ = cs.SetTelegramCredentials(1234567, "0123456789abcdef0123456789abcdef")
	b.SetConfig(cs)

	qrChan := make(chan QRChannelItem)
	close(qrChan)
	mock.GetQRChannelFunc = func(ctx context.Context) (<-chan QRChannelItem, error) {
		return qrChan, nil
	}

	ctx := context.Background()
	_, err := b.StartPairing(ctx)
	if err == nil {
		t.Fatal("expected error on closed channel")
	}

	st := b.Status()
	if st.State != wire.StateUnpaired {
		t.Errorf("expected StateUnpaired, got %s", st.State)
	}
}

func TestStartPairingUnpairCancelsInFlight(t *testing.T) {
	b, paths, _, mock := setupTestTelegramWithMock(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	_ = cs.SetTelegramCredentials(1234567, "0123456789abcdef0123456789abcdef")
	b.SetConfig(cs)

	qrChan := make(chan QRChannelItem, 4)
	mock.GetQRChannelFunc = func(ctx context.Context) (<-chan QRChannelItem, error) {
		return qrChan, nil
	}

	qrChan <- QRChannelItem{Event: QRChannelEventCode, Code: "tg://login?token=token-active"}

	ctx := context.Background()
	_, err := b.StartPairing(ctx)
	if err != nil {
		t.Fatalf("StartPairing failed: %v", err)
	}
	if b.Status().State != wire.StatePairing {
		t.Fatalf("expected StatePairing, got %s", b.Status().State)
	}

	// Unpair while in-flight
	if err := b.Unpair(ctx); err != nil {
		t.Fatalf("Unpair failed: %v", err)
	}

	st := b.Status()
	if st.State != wire.StateUnpaired {
		t.Errorf("expected StateUnpaired, got %s", st.State)
	}
	if st.QRURL != "" {
		t.Errorf("expected empty QRURL, got %s", st.QRURL)
	}
}

func TestStartPairingGenerationMismatch(t *testing.T) {
	b, paths, _, mock := setupTestTelegramWithMock(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	_ = cs.SetTelegramCredentials(1234567, "0123456789abcdef0123456789abcdef")
	b.SetConfig(cs)

	qrChan1 := make(chan QRChannelItem, 4)
	qrChan2 := make(chan QRChannelItem, 4)

	callCount := 0
	mock.GetQRChannelFunc = func(ctx context.Context) (<-chan QRChannelItem, error) {
		callCount++
		if callCount == 1 {
			return qrChan1, nil
		}
		return qrChan2, nil
	}

	qrChan1 <- QRChannelItem{Event: QRChannelEventCode, Code: "tg://login?token=gen1"}
	qrChan2 <- QRChannelItem{Event: QRChannelEventCode, Code: "tg://login?token=gen2"}

	ctx := context.Background()
	url1, err := b.StartPairing(ctx)
	if err != nil || url1 != "tg://login?token=gen1" {
		t.Fatalf("StartPairing 1 failed: url=%s, err=%v", url1, err)
	}

	// Start second pairing (supersedes generation 1)
	url2, err := b.StartPairing(ctx)
	if err != nil || url2 != "tg://login?token=gen2" {
		t.Fatalf("StartPairing 2 failed: url=%s, err=%v", url2, err)
	}

	// Stale event from gen1 should be ignored
	qrChan1 <- QRChannelItem{Event: QRChannelEventSuccess}
	time.Sleep(20 * time.Millisecond)

	// Backend must still be in StatePairing with gen2 URL, not StateConnected!
	st := b.Status()
	if st.State != wire.StatePairing {
		t.Errorf("stale event mutated state: got %s, want %s", st.State, wire.StatePairing)
	}
	if st.QRURL != "tg://login?token=gen2" {
		t.Errorf("stale event mutated QRURL: got %s", st.QRURL)
	}

	// Gen2 success works
	qrChan2 <- QRChannelItem{Event: QRChannelEventSuccess}
	time.Sleep(20 * time.Millisecond)

	st = b.Status()
	if st.State != wire.StateConnected {
		t.Errorf("gen2 success failed: got %s, want %s", st.State, wire.StateConnected)
	}
}

func TestBackendStopMethod(t *testing.T) {
	b, _, _, mock := setupTestTelegramWithMock(t)
	stopCalled := false
	mock.StopFunc = func() error {
		stopCalled = true
		return nil
	}
	b.SetClient(mock)

	b.Stop()
	if !stopCalled {
		t.Error("expected Stop to call client Stop")
	}
	if b.Client() != nil {
		t.Error("expected client to be nil after Stop")
	}
}
