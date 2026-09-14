package telegram

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"
	"github.com/rs/zerolog"

	"github.com/onelegdave/omachat/internal/store"
)

func TestFileSessionStorage(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "telegram.session")
	storage := NewFileSessionStorage(sessionPath)
	ctx := context.Background()

	// 1. Loading non-existent session returns session.ErrNotFound
	data, err := storage.LoadSession(ctx)
	if err != session.ErrNotFound {
		t.Fatalf("expected ErrNotFound for missing session, got err=%v, data=%v", err, data)
	}

	// 2. Storing session writes file atomically with 0600 permissions
	payload := []byte("mtproto-session-key-data-sample")
	if err := storage.StoreSession(ctx, payload); err != nil {
		t.Fatalf("StoreSession failed: %v", err)
	}

	fi, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("session file does not exist after store: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected file mode 0600, got %04o", perm)
	}

	// 3. Loading existing session returns payload
	loaded, err := storage.LoadSession(ctx)
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}
	if string(loaded) != string(payload) {
		t.Errorf("expected session data %q, got %q", string(payload), string(loaded))
	}

	// 4. Overwriting session works
	updated := []byte("updated-mtproto-session-data")
	if err := storage.StoreSession(ctx, updated); err != nil {
		t.Fatalf("StoreSession (update) failed: %v", err)
	}
	loaded, err = storage.LoadSession(ctx)
	if err != nil {
		t.Fatalf("LoadSession after update failed: %v", err)
	}
	if string(loaded) != string(updated) {
		t.Errorf("expected session data %q, got %q", string(updated), string(loaded))
	}

	// 5. Empty session file returns ErrNotFound
	if err := os.WriteFile(sessionPath, []byte{}, 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if _, err := storage.LoadSession(ctx); err != session.ErrNotFound {
		t.Errorf("expected ErrNotFound for empty session file, got %v", err)
	}
}

func TestMTProtoClientInitialization(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "telegram.session")
	storage := NewFileSessionStorage(sessionPath)

	// Verify gotd/td client can be initialized in offline mode with options
	// without initiating any network connection or panic.
	client := telegram.NewClient(1234567, "0123456789abcdef0123456789abcdef", telegram.Options{
		SessionStorage: storage,
	})
	if client == nil {
		t.Fatal("expected NewClient to return non-nil client")
	}
}

func TestQRLoginTokenParsing(t *testing.T) {
	// Sample URL generated according to MTProto qr login spec: tg://login?token=<base64url>
	initial := qrlogin.NewToken([]byte("sample-mtproto-login-token-bytes"), 0)
	rawURL := initial.URL()
	token, err := qrlogin.ParseTokenURL(rawURL)
	if err != nil {
		t.Fatalf("ParseTokenURL failed: %v", err)
	}
	if token.URL() != rawURL {
		t.Errorf("expected URL %q, got %q", rawURL, token.URL())
	}

	// Test invalid URLs
	if _, err := qrlogin.ParseTokenURL("https://example.com"); err == nil {
		t.Error("expected error for non-tg URL scheme")
	}
	if _, err := qrlogin.ParseTokenURL("tg://wronghost?token=abc"); err == nil {
		t.Error("expected error for wrong host")
	}
	if _, err := qrlogin.ParseTokenURL("tg://login?token="); err == nil {
		t.Error("expected error for empty token")
	}
}

func TestTGTypesInstantiable(t *testing.T) {
	// Verify core tg types are compilable and referenceable
	req := &tg.AuthExportLoginTokenRequest{
		APIID:   12345,
		APIHash: "dummyhash",
	}
	if req.APIID != 12345 || req.APIHash != "dummyhash" {
		t.Errorf("unexpected field values on AuthExportLoginTokenRequest: %+v", req)
	}
}

func TestDefaultClientFactory(t *testing.T) {
	factory := DefaultClientFactory(zerolog.Nop())
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "telegram.session")

	// 1. Invalid credentials reject
	if _, err := factory(store.TelegramCredentials{APIID: 0, APIHash: ""}, sessionPath); err == nil {
		t.Error("expected error for empty credentials")
	}

	// 2. Valid credentials instantiate GotdClient offline without network
	cli, err := factory(store.TelegramCredentials{APIID: 1234567, APIHash: "0123456789abcdef0123456789abcdef"}, sessionPath)
	if err != nil {
		t.Fatalf("unexpected error from DefaultClientFactory: %v", err)
	}
	if cli == nil {
		t.Fatal("expected non-nil client from factory")
	}
	if cli.Underlying() == nil {
		t.Error("expected Underlying gotd client to be non-nil")
	}
	if cli.IsConnected() {
		t.Error("expected fresh GotdClient to not be connected")
	}
}
