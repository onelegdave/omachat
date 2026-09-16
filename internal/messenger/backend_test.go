package messenger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-meta/pkg/messagix"
	waStore "go.mau.fi/whatsmeow/store"
	waTypes "go.mau.fi/whatsmeow/types"
)

func TestBackendRequiresConnection(t *testing.T) {
	log := zerolog.Nop()

	// Create mock paths
	tmp, err := os.MkdirTemp("", "messenger-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	os.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	os.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	os.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmp, "run"))
	defer os.Unsetenv("XDG_DATA_HOME")
	defer os.Unsetenv("XDG_CACHE_HOME")
	defer os.Unsetenv("XDG_RUNTIME_DIR")

	paths, err := appStore.NewPaths()
	if err != nil {
		t.Fatal(err)
	}

	events := make(chan wire.Event, 10)
	publish := func(e wire.Event) {
		events <- e
	}

	b := New(log, paths, publish)
	if b.Status().State != wire.StateUnpaired {
		t.Errorf("expected unpaired state, got %v", b.Status().State)
	}

	ctx := context.Background()

	if err := b.Refresh(ctx); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("expected ErrNotConfigured for Refresh, got %v", err)
	}

	if _, err := b.Send(ctx, wire.SendParams{ConversationID: "1", Text: "hello"}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("expected ErrNotConfigured for Send, got %v", err)
	}
}

func TestBackend_Unpair(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "messenger-test")
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	os.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	paths, _ := appStore.NewPaths()

	b := New(zerolog.Nop(), paths, func(wire.Event) {})
	if err := os.WriteFile(paths.MessengerSessionFile(), []byte(`{"cookies":{"c_user":"1"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	b.setState(wire.StateConnected, "")

	if err := b.Unpair(context.Background()); err != nil {
		t.Errorf("unpair failed: %v", err)
	}
	if b.Status().State != wire.StateUnpaired {
		t.Errorf("expected unpaired after Unpair, got %v", b.Status().State)
	}
	if _, err := os.Stat(paths.MessengerSessionFile()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Messenger session was not removed: %v", err)
	}
}

func TestNilDisconnectEventsDoNotPanic(t *testing.T) {
	b := New(zerolog.Nop(), nil, func(wire.Event) {})

	b.handleMetaEvent(context.Background(), &messagix.TransientDisconnectEvent{})
	if got := b.Status(); got.State != wire.StateDisconnected || got.Error != "Messenger transport disconnected" {
		t.Fatalf("unexpected transient disconnect status: %#v", got)
	}

	b.handleMetaEvent(context.Background(), &messagix.PermanentErrorEvent{})
	if got := b.Status(); got.State != wire.StateError || got.Error != "Messenger transport stopped" {
		t.Fatalf("unexpected permanent disconnect status: %#v", got)
	}
}

func TestMessengerDeviceRegistrationDetection(t *testing.T) {
	if !needsMessengerRegistration(nil) {
		t.Fatal("nil device must be registered")
	}
	if !needsMessengerRegistration(&waStore.Device{}) {
		t.Fatal("new unsaved device without an ID must be registered")
	}
	if needsMessengerRegistration(&waStore.Device{ID: &waTypes.JID{User: "1", Device: 1, Server: waTypes.MessengerServer}}) {
		t.Fatal("saved device with an ID must be reused")
	}
}

func TestEnsureMessengerSQLiteFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messenger.db")
	if err := os.WriteFile(path, []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureMessengerSQLiteFile(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("Messenger database mode = %04o, want 0600", got)
	}

	target := filepath.Join(t.TempDir(), "target.db")
	if err := os.WriteFile(target, []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "messenger.db")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := ensureMessengerSQLiteFile(link); err == nil {
		t.Fatal("expected Messenger database symlink to be rejected")
	}
}
