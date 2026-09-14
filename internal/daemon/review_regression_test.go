package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"

	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

func syntheticTestAuth() *libgm.AuthData {
	auth := libgm.NewAuthData()
	auth.TachyonAuthToken = []byte("synthetic-tachyon-token-daemon-test")
	auth.Browser = &gmproto.Device{
		SourceID: "browser-dev-1",
	}
	auth.Mobile = &gmproto.Device{
		SourceID: "mobile-dev-1",
	}
	auth.SetCookies(map[string]string{
		"SID":              "synthetic-sid",
		"HSID":             "synthetic-hsid",
		"SSID":             "synthetic-ssid",
		"APISID":           "synthetic-apisid",
		"SAPISID":          "synthetic-sapisid",
		"__Secure-1PSID":   "synthetic-1psid",
		"__Secure-1PSIDTS": "synthetic-1psidts-initial",
		"__Secure-3PSID":   "synthetic-3psid",
		"__Secure-3PSIDTS": "synthetic-3psidts-initial",
		"OSID":             "synthetic-osid",
	})
	return auth
}

func newTestDaemon(t *testing.T) (*Daemon, *store.Paths) {
	t.Helper()
	dir := t.TempDir()
	paths := &store.Paths{
		Data:    filepath.Join(dir, "data"),
		Cache:   filepath.Join(dir, "cache"),
		Runtime: filepath.Join(dir, "runtime"),
	}
	for _, d := range []string{paths.Data, paths.Cache, paths.Runtime, paths.MediaDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	if err := store.NewConfigStore(paths.ConfigFile()).SetEnabledServices([]string{"gmessages", "whatsapp", "telegram"}); err != nil {
		t.Fatal(err)
	}
	d := New(zerolog.Nop(), paths)
	ctx := context.Background()
	d.maintCtx, d.maintCancel = context.WithCancel(ctx)
	d.sessionCtx, d.sessionCancel = context.WithCancel(d.maintCtx)
	d.auth = syntheticTestAuth()
	d.client = libgm.NewClient(d.auth, nil, d.log.With().Str("component", "libgm").Logger())
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "external networking disabled in tests", http.StatusForbidden)
	}))
	t.Cleanup(proxy.Close)
	if err := d.client.SetProxy(proxy.URL); err != nil {
		t.Fatal(err)
	}
	d.bindClient(d.client, d.sessionCtx)
	t.Cleanup(func() { d.maintCancel(); d.sessionCancel() })
	return d, paths
}

// TestUnpairPersistence verifies that unpairing clears the on-disk session and that
// subsequent calls to saveSession() or Stop() do NOT recreate credentials on disk.
func TestUnpairPersistence(t *testing.T) {
	d, paths := newTestDaemon(t)

	// Save synthetic session on disk
	auth := syntheticTestAuth()
	if err := paths.SaveSession(auth); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	d.mu.Lock()
	d.paired = true
	d.auth = auth
	d.mu.Unlock()

	// Verify session file exists before unpair
	if _, err := os.Stat(paths.SessionFile()); err != nil {
		t.Fatalf("session file must exist before unpair: %v", err)
	}

	// Perform unpair with bounded timeout
	unpairCtx, unpairCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer unpairCancel()
	if err := d.Unpair(unpairCtx); err == nil {
		t.Fatal("expected remote revocation failure from the rejecting test proxy")
	}

	// Verify session file is gone
	if _, err := os.Stat(paths.SessionFile()); !os.IsNotExist(err) {
		t.Fatalf("session file must be removed after Unpair")
	}

	// Simulate periodic maintenance save
	d.saveSession()

	// Session file must NOT be recreated
	if _, err := os.Stat(paths.SessionFile()); !os.IsNotExist(err) {
		t.Fatalf("saveSession() after Unpair recreated the session file on disk")
	}

	// Simulate daemon Stop()
	d.Stop()

	// Session file must still NOT be recreated
	if _, err := os.Stat(paths.SessionFile()); !os.IsNotExist(err) {
		t.Fatalf("Stop() after Unpair recreated the session file on disk")
	}
}

// TestConcurrentUnpairAndSaveSession verifies that racing saves and an unpair
// operation leave the daemon completely unpaired and without credentials on disk.
func TestConcurrentUnpairAndSaveSession(t *testing.T) {
	d, paths := newTestDaemon(t)
	auth := syntheticTestAuth()
	if err := paths.SaveSession(auth); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	d.mu.Lock()
	d.paired = true
	d.auth = auth
	d.mu.Unlock()

	var wg sync.WaitGroup
	// Run concurrent save attempts
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				d.saveSession()
			}
		}()
	}

	// Run Unpair concurrently
	unpairCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = d.Unpair(unpairCtx)

	wg.Wait()

	// After all saves and unpair finish, session file must not exist
	if _, err := os.Stat(paths.SessionFile()); !os.IsNotExist(err) {
		t.Errorf("session file exists on disk after concurrent Unpair and saveSession")
	}
	d.mu.RLock()
	paired := d.paired
	d.mu.RUnlock()
	if paired {
		t.Errorf("daemon is still paired after Unpair")
	}
}

// TestMediaAndAvatarCacheResetAfterUnpair verifies that Unpair() resets the
// in-memory media and avatar caches and recreates the on-disk media directory.
func TestMediaAndAvatarCacheResetAfterUnpair(t *testing.T) {
	d, paths := newTestDaemon(t)

	// Seed in-memory media cache
	d.media.mu.Lock()
	d.media.secrets["test-key"] = mediaSecret{mediaID: "media-1", key: []byte("key-1")}
	d.media.requested["req-key"] = true
	d.media.mu.Unlock()

	// Seed in-memory avatar store
	d.avatars.mu.Lock()
	d.avatars.paths["conv-1"] = filepath.Join(paths.MediaDir(), "avatar-conv-1.png")
	d.avatars.tried["conv-1"] = true
	d.avatars.mu.Unlock()

	unpairCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := d.Unpair(unpairCtx); err == nil {
		t.Fatal("expected remote revocation failure from the rejecting test proxy")
	}

	// Verify in-memory media cache is cleared
	d.media.mu.Lock()
	secretsLen := len(d.media.secrets)
	reqLen := len(d.media.requested)
	d.media.mu.Unlock()
	if secretsLen != 0 || reqLen != 0 {
		t.Errorf("mediaCache not reset: secrets=%d, requested=%d", secretsLen, reqLen)
	}

	// Verify in-memory avatar cache is cleared
	d.avatars.mu.Lock()
	pathsLen := len(d.avatars.paths)
	triedLen := len(d.avatars.tried)
	d.avatars.mu.Unlock()
	if pathsLen != 0 || triedLen != 0 {
		t.Errorf("avatarStore not reset: paths=%d, tried=%d", pathsLen, triedLen)
	}

	// Verify MediaDir exists and is writable
	info, err := os.Stat(paths.MediaDir())
	if err != nil || !info.IsDir() {
		t.Fatalf("MediaDir missing or not a directory after Unpair: %v", err)
	}
	testFile := filepath.Join(paths.MediaDir(), "write-test.bin")
	if err := os.WriteFile(testFile, []byte("data"), 0o600); err != nil {
		t.Fatalf("failed to write file in MediaDir after Unpair: %v", err)
	}
}

// TestCanceledObsoleteEvents verifies that events arriving from an obsolete/unpaired
// client or after session context cancellation are discarded and do not alter daemon state.
func TestCanceledObsoleteEvents(t *testing.T) {
	d, _ := newTestDaemon(t)
	d.mu.Lock()
	d.paired = true
	d.mu.Unlock()

	oldClient := d.client
	oldCtx := d.sessionCtx

	// Subscribe to daemon events
	ch, unsub := d.Subscribe()
	defer unsub()

	// Drain initial status event
drain:
	for {
		select {
		case <-ch:
		default:
			break drain
		}
	}

	// Unpair the daemon - this cancels old sessionCtx, creates a new client, and resets
	unpairCtx, unpairCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer unpairCancel()
	if err := d.Unpair(unpairCtx); err == nil {
		t.Fatal("expected remote revocation failure from the rejecting test proxy")
	}

	// Drain unpair status events
drainUnpair:
	for {
		select {
		case <-ch:
		default:
			break drainUnpair
		}
	}

	// Invoke event handler on the old client with obsolete events
	handler := d.clientEventHandler(oldClient, oldCtx)
	handler(&events.PairSuccessful{PhoneID: "obsolete-phone-id"})
	handler(&gmproto.Message{
		MessageID:      "obsolete-msg-id",
		ConversationID: "obsolete-conv-id",
	})

	// Verify no obsolete events reached subscribers
	select {
	case evt := <-ch:
		t.Errorf("unexpected event received from obsolete client callback: %+v", evt)
	case <-time.After(50 * time.Millisecond):
		// Expected: event was discarded
	}

	// Verify daemon did NOT flip to paired
	d.mu.RLock()
	paired := d.paired
	state := d.status.State
	d.mu.RUnlock()
	if paired || state == wire.StateConnected {
		t.Errorf("daemon adopted obsolete state: paired=%v state=%s", paired, state)
	}
}

// TestGaiaPairingIgnoresCanceledContextEvent verifies that PairSuccessful is dropped
// if gaiaActive is true but gaiaCtx is canceled or timed out.
func TestGaiaPairingIgnoresCanceledContextEvent(t *testing.T) {
	d, _ := newTestDaemon(t)
	d.mu.Lock()
	d.paired = false
	d.mu.Unlock()

	// Simulate active Gaia pairing whose context timed out
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled immediately

	d.sessionMu.Lock()
	d.gaiaActive = true
	d.gaiaCtx = ctx
	d.sessionMu.Unlock()

	// Directly deliver PairSuccessful event
	d.clientEventHandler(d.client, d.sessionCtx)(&events.PairSuccessful{PhoneID: "phone-1"})

	d.mu.RLock()
	paired := d.paired
	d.mu.RUnlock()
	if paired {
		t.Error("PairSuccessful should be ignored when gaiaCtx is canceled")
	}
}

// TestCanceledInitialSyncReturnsImmediately verifies that initialSync honors context
// cancellation and returns immediately instead of sleeping through 3-second delay or retry loops.
func TestCanceledInitialSyncReturnsImmediately(t *testing.T) {
	d, _ := newTestDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled

	start := time.Now()
	d.initialSync(ctx)
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Errorf("initialSync took %v on canceled context, want immediate return", elapsed)
	}

	d.mu.RLock()
	state := d.status.State
	d.mu.RUnlock()
	if state == wire.StateConnected {
		t.Error("initialSync must not set StateConnected when context is canceled")
	}
}

// TestCanceledWithAuthRetry verifies that withAuthRetry does not call op() on canceled context.
func TestCanceledWithAuthRetry(t *testing.T) {
	d, _ := newTestDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled

	invoked := false
	op := func() (string, error) {
		invoked = true
		return "result", nil
	}

	res, err := withAuthRetry(ctx, d, op)
	if invoked {
		t.Error("withAuthRetry should not invoke op when context is canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if res != "" {
		t.Errorf("expected zero value, got %q", res)
	}
}

func TestCanceledRecoveryPreservesCooldown(t *testing.T) {
	d, _ := newTestDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if d.refreshBrowserCookies(ctx) {
		t.Fatal("canceled recovery succeeded")
	}
	if !d.cookies.last.IsZero() {
		t.Fatal("canceled recovery consumed cooldown")
	}
}

func TestRequestCancellationDoesNotCancelRevocation(t *testing.T) {
	d, _ := newTestDaemon(t)
	d.sessionCancel()
	// Intercept the actual revocation transport locally. It must be attempted
	// with a usable context, and must never reach a network destination.
	var attempted atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempted.Store(true)
		http.Error(w, "external networking disabled in tests", http.StatusForbidden)
	}))
	defer proxy.Close()
	if err := d.client.SetProxy(proxy.URL); err != nil {
		t.Fatal(err)
	}

	resp := d.dispatch(context.Background(), wire.Request{ID: "unpair", Method: wire.MethodUnpair})
	if resp.OK || resp.Error == "" {
		t.Fatal("unpair must report the proxy rejection")
	}
	if !attempted.Load() {
		t.Fatal("revocation was canceled before the transport")
	}
}

func TestSessionBoundContextSeesCancellationImmediately(t *testing.T) {
	session, cancel := context.WithCancel(context.Background())
	ctx := sessionBoundContext{Context: context.Background(), session: session}
	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("stale request could still write account data")
	}
}

func TestUnpairFailureDoesNotInterruptNewQRPairing(t *testing.T) {
	d, _ := newTestDaemon(t)
	denied := errors.New("synthetic revocation denial")
	err := d.unpair(d.sessionContext(), func(_ context.Context, _ *libgm.Client) error {
		// QR pairing reuses the reset session context; an epoch check alone
		// cannot protect its state from a late revocation reply.
		d.sessionMu.Lock()
		d.setState(wire.StatePairing, "")
		d.sessionMu.Unlock()
		return denied
	})
	if !errors.Is(err, denied) {
		t.Fatalf("revocation error was lost: %v", err)
	}
	if got := d.Status(); got.State != wire.StatePairing || got.Error != "" {
		t.Fatalf("late unpair failure overwrote QR pairing: %+v", got)
	}
}
