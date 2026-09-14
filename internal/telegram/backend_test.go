package telegram

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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

	events := make(chan wire.Event, 10)
	publish := func(evt wire.Event) {
		select {
		case events <- evt:
		default:
		}
	}

	b := New(zerolog.Nop(), paths, publish)
	return b, paths, events
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
	ctx := context.Background()

	// Seed dummy session file
	if err := os.WriteFile(paths.TelegramSessionFile(), []byte("tg-session-data"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	st := b.Status()
	if st.State != wire.StateConnecting {
		t.Errorf("expected state %q when session exists, got %q", wire.StateConnecting, st.State)
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
