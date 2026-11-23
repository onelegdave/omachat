package daemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"net"
	"syscall"

	"github.com/google/uuid"
	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"

	"github.com/onelegdave/omachat/internal/browser"
	"github.com/onelegdave/omachat/internal/wire"
)

// fakeCookieStore stands in for the browser's cookie store. Every read returns
// its current cookies; renew replaces __Secure-1PSIDTS as Chromium does.
type fakeCookieStore struct {
	mu       sync.Mutex
	profiles []browser.Profile
	cookies  map[string]map[string]string // profile name -> cookies
	errs     map[string]error
	// undecryptable profiles are signed in, but their SID and SSID do not
	// decrypt, so a read returns the rest without them.
	undecryptable map[string]bool
	reads         []string
	readCh        chan string
	mod           time.Time
	fp            string
	metaRead      int
}

func newFakeCookieStore(names ...string) *fakeCookieStore {
	b := &fakeCookieStore{cookies: map[string]map[string]string{}, errs: map[string]error{}, undecryptable: map[string]bool{}, readCh: make(chan string, 16), mod: time.Unix(1000, 0), fp: "fp-1"}
	for _, n := range names {
		b.profiles = append(b.profiles, browser.Profile{Name: n, CookieDB: "/fake/" + n + "/Cookies"})
		b.cookies[n] = fullCookies("browser-" + n + "-1")
	}
	return b
}

func fullCookies(psidts string) map[string]string {
	c := map[string]string{}
	for _, name := range wire.RequiredGaiaCookies {
		c[name] = "synthetic-" + name
	}
	c["__Secure-1PSIDTS"] = psidts
	return c
}

func (b *fakeCookieStore) renew(profile, psidts string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c := map[string]string{}
	for k, v := range b.cookies[profile] {
		c[k] = v
	}
	c["__Secure-1PSIDTS"] = psidts
	b.cookies[profile] = c
	b.mod = b.mod.Add(time.Minute)
	b.fp = "fp-" + psidts
}

// touch changes the file without renewing __Secure-1PSIDTS, as browsing does.
func (b *fakeCookieStore) touch() {
	b.mu.Lock()
	b.mod = b.mod.Add(time.Minute)
	b.mu.Unlock()
}

func (b *fakeCookieStore) readCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.reads)
}

func (b *fakeCookieStore) install(d *Daemon) {
	d.discoverProfiles = func() []browser.Profile {
		b.mu.Lock()
		defer b.mu.Unlock()
		return append([]browser.Profile(nil), b.profiles...)
	}
	d.extractCookies = func(ctx context.Context, p browser.Profile) (map[string]string, error) {
		b.mu.Lock()
		b.reads = append(b.reads, p.Name)
		err := b.errs[p.Name]
		undecryptable := b.undecryptable[p.Name]
		c := map[string]string{}
		for k, v := range b.cookies[p.Name] {
			c[k] = v
		}
		b.mu.Unlock()
		b.readCh <- p.Name
		if err != nil {
			return nil, err
		}
		if undecryptable {
			delete(c, "SID")
			delete(c, "SSID")
			return c, nil
		}
		// As the real extractor: a profile with no SID at all is not
		// signed in to Google.
		if c["SID"] == "" {
			return nil, browser.ErrNotSignedIn
		}
		return c, nil
	}
	d.follow.stat = func(string) (time.Time, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.mod, nil
	}
	d.follow.read = func(ctx context.Context, p browser.Profile) ([]browser.CookieMeta, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.metaRead++
		return []browser.CookieMeta{
			{Name: "SIDCC", Host: ".google.com", EncryptedHash: "changes-all-the-time"},
			{Name: "__Secure-1PSIDTS", Host: ".google.com", EncryptedHash: b.fp},
		}, nil
	}
}

// newGoogleAccountDaemon is a paired, connected Google account session whose
// cookies came from the fake browser's first profile.
func newGoogleAccountDaemon(t *testing.T, b *fakeCookieStore) *Daemon {
	t.Helper()
	d, _ := newTestDaemon(t)
	d.mu.Lock()
	d.auth.DestRegID = uuid.New()
	d.auth.SetCookies(fullCookies("omachat-copy"))
	d.paired = true
	d.status.State = wire.StateConnected
	d.mu.Unlock()
	b.install(d)
	shortenRecoveryTimings(d)
	return d
}

// shortenRecoveryTimings makes one daemon's recovery waits instant.
func shortenRecoveryTimings(d *Daemon) {
	d.timing.recoveryDelay = 0
	d.timing.reconnectBackoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	d.timing.sessionCheckPoll = 5 * time.Millisecond
}

func currentPSIDTS(d *Daemon) string {
	d.mu.RLock()
	auth := d.auth
	d.mu.RUnlock()
	auth.CookiesLock.RLock()
	defer auth.CookiesLock.RUnlock()
	return auth.Cookies["__Secure-1PSIDTS"]
}

func savedPSIDTS(t *testing.T, d *Daemon) string {
	t.Helper()
	auth, _, err := d.paths.LoadSession()
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if auth == nil {
		return ""
	}
	return auth.Cookies["__Secure-1PSIDTS"]
}

func waitRead(t *testing.T, b *fakeCookieStore) string {
	t.Helper()
	select {
	case name := <-b.readCh:
		return name
	case <-time.After(5 * time.Second):
		t.Fatal("the browser's cookies were never read")
		return ""
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// --- refreshBrowserCookiesFor ---

func TestRefreshUsesFirstUsableProfileAndSaves(t *testing.T) {
	b := newFakeCookieStore("Broken", "Incomplete", "Good")
	b.errs["Broken"] = errors.New("cannot read cookie database")
	delete(b.cookies["Incomplete"], "SAPISID")
	d := newGoogleAccountDaemon(t, b)

	if !d.refreshBrowserCookiesFor(context.Background(), "test", "") {
		t.Fatal("refresh failed although one profile was usable")
	}
	if got := currentPSIDTS(d); got != "browser-Good-1" {
		t.Fatalf("session cookie = %q, want the usable profile's", got)
	}
	if got := savedPSIDTS(t, d); got != "browser-Good-1" {
		t.Fatalf("saved cookie = %q, want the usable profile's", got)
	}
	if d.cookies.lastProfile != "Good" || d.cookies.lastResult != "ok" {
		t.Fatalf("lastProfile=%q lastResult=%q", d.cookies.lastProfile, d.cookies.lastResult)
	}
}

func TestRefreshIsThrottled(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.cookies.last = time.Now()
	if d.refreshBrowserCookiesFor(context.Background(), "test", "") {
		t.Fatal("a refresh within the throttle interval ran")
	}
	if b.readCount() != 0 {
		t.Fatal("a throttled refresh read the browser")
	}
}

func TestRefreshStopsWhenCanceled(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if d.refreshBrowserCookiesFor(ctx, "test", "") || b.readCount() != 0 {
		t.Fatal("a canceled refresh read the browser")
	}
}

func TestRefreshWithoutUsableProfileKeepsCookies(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	b.errs["Chromium"] = errors.New("keyring locked")
	d := newGoogleAccountDaemon(t, b)
	if d.refreshBrowserCookiesFor(context.Background(), "test", "") {
		t.Fatal("refresh reported success with no usable profile")
	}
	if got := currentPSIDTS(d); got != "omachat-copy" {
		t.Fatalf("session cookie changed to %q", got)
	}
	if d.cookies.lastResult != "failed" {
		t.Fatalf("lastResult = %q, want failed", d.cookies.lastResult)
	}
}

func TestRefreshWithoutSession(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.mu.Lock()
	d.auth = nil
	d.mu.Unlock()
	if d.refreshBrowserCookiesFor(context.Background(), "test", "") {
		t.Fatal("refresh succeeded without a session")
	}
	if d.cookies.lastResult != "noSession" {
		t.Fatalf("lastResult = %q, want noSession", d.cookies.lastResult)
	}
}

// --- fix 1: follow the browser's renewals ---

func TestFollowerReadsBrowserAfterRenewal(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	ctx := context.Background()

	// Nothing is known about the copy OmaChat holds yet, so the first check
	// reads rather than adopt the browser's cookie as the baseline unread.
	d.followBrowserCookies(ctx)
	waitRead(t, b)
	if got := currentPSIDTS(d); got != "browser-Chromium-1" {
		t.Fatalf("session cookie = %q after the first check", got)
	}
	d.cookies.last = time.Now().Add(-time.Hour)
	metas := b.metaRead
	d.followBrowserCookies(ctx) // file unchanged
	if b.metaRead != metas {
		t.Fatal("metadata was read although the file had not changed")
	}
	b.touch() // file changed, __Secure-1PSIDTS did not
	d.followBrowserCookies(ctx)
	if b.readCount() != 1 {
		t.Fatal("cookies were read although __Secure-1PSIDTS had not changed")
	}

	b.renew("Chromium", "browser-Chromium-2")
	d.followBrowserCookies(ctx)
	waitRead(t, b)
	if got := currentPSIDTS(d); got != "browser-Chromium-2" {
		t.Fatalf("session cookie = %q after the browser renewed it", got)
	}
	if got := savedPSIDTS(t, d); got != "browser-Chromium-2" {
		t.Fatalf("saved cookie = %q after the browser renewed it", got)
	}
}

func TestFollowerRetriesWhenRefreshWasThrottled(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	ctx := context.Background()
	d.followBrowserCookies(ctx) // first check reads and records the baseline
	waitRead(t, b)

	b.renew("Chromium", "browser-Chromium-2")
	d.cookies.last = time.Now() // a refresh ran moments ago
	d.followBrowserCookies(ctx)
	if b.readCount() != 1 {
		t.Fatal("a throttled check read the browser")
	}
	d.cookies.last = time.Now().Add(-time.Hour)
	d.followBrowserCookies(ctx)
	waitRead(t, b)
	if got := currentPSIDTS(d); got != "browser-Chromium-2" {
		t.Fatalf("the renewal was lost after a throttled check: cookie = %q", got)
	}
}

func TestFollowerWatchesProfileThatSuppliedCookies(t *testing.T) {
	b := newFakeCookieStore("Work", "Personal")
	d := newGoogleAccountDaemon(t, b)
	if got := d.followedProfiles(); len(got) != 2 {
		t.Fatalf("watching %d profiles before the source is known; want all of them", len(got))
	}
	d.cookies.lastProfile = "Personal"
	if got := d.followedProfiles(); len(got) != 1 || got[0].Name != "Personal" {
		t.Fatalf("watching %v; want only the profile that supplied the cookies", got)
	}
}

func TestFollowerNoticesRenewalInAnyProfileUntilSourceKnown(t *testing.T) {
	b := newFakeCookieStore("Other account", "Mine")
	b.cookies["Other account"]["SID"] = "another-sid"
	d := newGoogleAccountDaemon(t, b)
	ctx := context.Background()
	// No profile is known yet: the first check reads, skips the other
	// account's profile, and settles on the session's own.
	d.followBrowserCookies(ctx)
	eventually(t, "the session's own cookie", func() bool { return currentPSIDTS(d) == "browser-Mine-1" })
	if d.cookies.lastProfile != "Mine" {
		t.Fatalf("lastProfile = %q; the read should settle on the profile with the session's sign-in", d.cookies.lastProfile)
	}
	b.renew("Mine", "browser-Mine-2")
	d.cookies.last = time.Now().Add(-time.Hour)
	d.followBrowserCookies(ctx)
	eventually(t, "the renewed cookie", func() bool { return currentPSIDTS(d) == "browser-Mine-2" })
}

func TestFollowerIgnoresSessionsNotFromBrowser(t *testing.T) {
	cases := map[string]func(d *Daemon){
		"QR pairing": func(d *Daemon) { d.auth.DestRegID = uuid.Nil },
		"unpaired":   func(d *Daemon) { d.paired = false },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			b := newFakeCookieStore("Chromium")
			d := newGoogleAccountDaemon(t, b)
			d.mu.Lock()
			setup(d)
			d.mu.Unlock()
			d.followBrowserCookies(context.Background())
			b.renew("Chromium", "browser-Chromium-2")
			d.followBrowserCookies(context.Background())
			if b.metaRead != 0 || b.readCount() != 0 {
				t.Fatal("the browser was read for a session that does not use its cookies")
			}
		})
	}
}

// --- fix 2: a refused ping reads the browser ---

func TestRefusedPingReadsBrowser(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.clientEventHandler(d.client, d.sessionContext())(&events.PingFailed{Error: errors.New("failed to notify ditto activity: HTTP 401"), ErrorCount: 1})
	waitRead(t, b)
	eventually(t, "the renewed cookie", func() bool { return currentPSIDTS(d) == "browser-Chromium-1" })
}

func TestPingFailureWithoutAuthErrorDoesNotReadBrowser(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.clientEventHandler(d.client, d.sessionContext())(&events.PingFailed{Error: errors.New("connection reset by peer"), ErrorCount: 1})
	time.Sleep(50 * time.Millisecond)
	if b.readCount() != 0 {
		t.Fatal("a network error read the browser")
	}
}

// --- fix 3: read the browser before the first connect ---

func TestStartupReadsBrowserBeforeConnecting(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.readBrowserBeforeConnect(context.Background())
	if got := currentPSIDTS(d); got != "browser-Chromium-1" {
		t.Fatalf("session cookie = %q; startup did not read the browser", got)
	}
	if d.cookies.lastTrigger != "startup" {
		t.Fatalf("lastTrigger = %q", d.cookies.lastTrigger)
	}
}

func TestStartupSkipsQRPairing(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.auth.DestRegID = uuid.Nil
	d.readBrowserBeforeConnect(context.Background())
	if b.readCount() != 0 {
		t.Fatal("startup read the browser for a QR pairing")
	}
}

// --- fix 4: recover a refused long poll in place ---

func refusedLongPoll() *events.ListenFatalError {
	return &events.ListenFatalError{Error: events.HTTPError{Action: "polling", Resp: httpResp(401, "application/json+protobuf"), Body: []byte("SESSION_COOKIE_INVALID")}}
}

func stateOf(d *Daemon) (wire.ConnState, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.status.State, d.paired
}

func TestRefusedLongPollRecoversInPlace(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	reconnected := make(chan *libgm.Client, 1)
	d.reconnectClient = func(c *libgm.Client) error { reconnected <- c; return nil }

	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	if st, paired := stateOf(d); st != wire.StateConnecting || !paired {
		t.Fatalf("state=%q paired=%v; want connecting while recovering", st, paired)
	}
	select {
	case c := <-reconnected:
		if c != d.client {
			t.Fatal("reconnected a different client")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("never reconnected")
	}
	if got := savedPSIDTS(t, d); got != "browser-Chromium-1" {
		t.Fatalf("saved cookie = %q; the browser's cookie must be saved before reconnecting", got)
	}
	if st, paired := stateOf(d); st == wire.StateError || !paired {
		t.Fatalf("state=%q paired=%v after recovery", st, paired)
	}
}

func TestSecondRefusalWithinWindowRequiresPairing(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.reconnectClient = func(c *libgm.Client) error { return nil }
	d.listenRecoveryAt = time.Now().Add(-time.Minute)

	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	if st, paired := stateOf(d); st != wire.StateError || paired {
		t.Fatalf("state=%q paired=%v; a second refusal must require pairing", st, paired)
	}
	if b.readCount() != 0 {
		t.Fatal("a second refusal read the browser again")
	}
}

func TestRecoveryWithoutUsableBrowserRequiresPairing(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	b.errs["Chromium"] = errors.New("keyring locked")
	d := newGoogleAccountDaemon(t, b)
	d.timing.recoveryFreshWait = 300 * time.Millisecond
	d.timing.refreshInterval = 50 * time.Millisecond
	d.reconnectClient = func(c *libgm.Client) error { t.Error("reconnected without fresh cookies"); return nil }

	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	eventually(t, "pairing to be required", func() bool { st, paired := stateOf(d); return st == wire.StateError && !paired })
	if b.readCount() < 2 {
		t.Fatalf("read %d times; recovery should keep trying failed reads within its wait", b.readCount())
	}
}

// A newer cookie that another read installs while recovery waits is used
// without a read of its own.
func TestRecoveryUsesNewCookieFromAnotherRead(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.timing.refreshInterval = time.Hour // recovery's own read stays throttled
	d.cookies.last = time.Now()
	reconnected := make(chan struct{}, 1)
	d.reconnectClient = func(c *libgm.Client) error { reconnected <- struct{}{}; return nil }
	running := make(chan struct{})
	d.cookies.mu.Lock()
	d.cookies.running = running
	d.cookies.mu.Unlock()

	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	// The other read finishes and installs a newer cookie.
	d.auth.SetCookies(fullCookies("installed-by-another-read"))
	d.cookies.mu.Lock()
	d.cookies.running = nil
	d.cookies.mu.Unlock()
	close(running)
	select {
	case <-reconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("recovery did not use the newer cookie another read installed")
	}
	if b.readCount() != 0 {
		t.Fatal("recovery read the browser although the session already had a newer cookie")
	}
}

// A long poll refused with the cookie the browser still has is not
// reconnected with that same cookie.
func TestRecoveryDoesNotReconnectWithRefusedCookie(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	b.cookies["Chromium"] = fullCookies("omachat-copy") // the browser has nothing newer
	d := newGoogleAccountDaemon(t, b)
	d.timing.recoveryFreshWait = 300 * time.Millisecond
	d.timing.refreshInterval = 50 * time.Millisecond
	d.reconnectClient = func(c *libgm.Client) error { t.Error("reconnected with the refused cookie"); return nil }
	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	eventually(t, "pairing to be required", func() bool { st, paired := stateOf(d); return st == wire.StateError && !paired })
	if b.readCount() < 2 {
		t.Fatalf("read the browser %d times; want it to keep checking for a renewal until the wait ran out", b.readCount())
	}
}

func TestRecoveryReconnectRefusedRequiresPairing(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.reconnectClient = func(c *libgm.Client) error { return errors.New("failed to refresh auth token: HTTP 401") }
	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	eventually(t, "pairing to be required", func() bool { st, paired := stateOf(d); return st == wire.StateError && !paired })
}

// A network failure never ends recovery: it keeps retrying for as long as
// the session lasts, showing that it is retrying.
func TestRecoveryKeepsRetryingNetworkFailures(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	var mu sync.Mutex
	calls := 0
	d.reconnectClient = func(c *libgm.Client) error {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	}
	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	eventually(t, "more attempts than backoff steps", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls > len(d.timing.reconnectBackoff)+2
	})
	if st, paired := stateOf(d); st != wire.StateDisconnected || !paired {
		t.Fatalf("state=%q paired=%v while retrying", st, paired)
	}
}

// Recovery stops, and stops the client it reconnected, once the session no
// longer counts as paired.
func TestRecoveryStopsWhenPairingRequiredMeanwhile(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	disconnected := make(chan struct{}, 1)
	d.disconnectClient = func(c *libgm.Client) { disconnected <- struct{}{} }
	d.reconnectClient = func(c *libgm.Client) error {
		d.requirePairing(d.sessionContext())
		return nil
	}
	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	select {
	case <-disconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("a client reconnected after pairing was required kept running")
	}
}

func TestQRPairingStillRequiresPairingOnRefusedLongPoll(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.auth.DestRegID = uuid.Nil
	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	if st, paired := stateOf(d); st != wire.StateError || paired {
		t.Fatalf("state=%q paired=%v; a QR pairing has no browser to recover from", st, paired)
	}
}

// --- withAuthRetryAt ---

func TestRefusedRequestIsRetriedWithBrowserCookies(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	calls := 0
	got, err := withAuthRetryAt(context.Background(), d, "rpc:test", func() (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("HTTP 401: Request had invalid authentication credentials")
		}
		return "ok", nil
	})
	if err != nil || got != "ok" || calls != 2 {
		t.Fatalf("got %q, %v after %d calls; want a successful retry", got, err, calls)
	}
	if st, paired := stateOf(d); st == wire.StateError || !paired {
		t.Fatalf("state=%q paired=%v after a successful retry", st, paired)
	}
}

// For a Google account session a refused request does not require pairing
// at once: it starts a session check, which decides (sessioncheck_test.go).
func TestRefusedRequestDoesNotEndPairingAtOnce(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	b.errs["Chromium"] = errors.New("keyring locked")
	d := newGoogleAccountDaemon(t, b)
	calls := 0
	_, err := withAuthRetryAt(context.Background(), d, "rpc:test", func() (string, error) {
		calls++
		return "", errors.New("HTTP 401")
	})
	if err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d; want one failed call and no retry", err, calls)
	}
	if st, paired := stateOf(d); st == wire.StateError || !paired {
		t.Fatalf("state=%q paired=%v; a refused request must not end a Google account pairing", st, paired)
	}
}

func TestRefusedRequestStillRequiresPairingForQRPairing(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	b.errs["Chromium"] = errors.New("keyring locked")
	d := newGoogleAccountDaemon(t, b)
	d.auth.DestRegID = uuid.Nil
	_, _ = withAuthRetryAt(context.Background(), d, "rpc:test", func() (string, error) {
		return "", errors.New("HTTP 401")
	})
	if st, paired := stateOf(d); st != wire.StateError || paired {
		t.Fatalf("state=%q paired=%v; a QR pairing keeps the old behaviour", st, paired)
	}
}

func TestSessionInvalidErrorSkipsRefresh(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.auth.DestRegID = uuid.Nil // the old rule, which a QR pairing keeps
	_, _ = withAuthRetryAt(context.Background(), d, "rpc:test", func() (string, error) {
		return "", errors.New("HTTP 401: SESSION_COOKIE_INVALID")
	})
	if b.readCount() != 0 {
		t.Fatal("a SESSION_COOKIE_INVALID error in the error text read the browser")
	}
	if st, paired := stateOf(d); st != wire.StateError || paired {
		t.Fatalf("state=%q paired=%v; want pairing required", st, paired)
	}
}

func TestNonAuthErrorIsNotRetried(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	calls := 0
	_, err := withAuthRetryAt(context.Background(), d, "rpc:test", func() (string, error) {
		calls++
		return "", errors.New("connection reset by peer")
	})
	if err == nil || calls != 1 || b.readCount() != 0 {
		t.Fatalf("err=%v calls=%d reads=%d; a network error must not refresh or retry", err, calls, b.readCount())
	}
}

func TestAutomaticReadsSkipOtherGoogleSignIns(t *testing.T) {
	b := newFakeCookieStore("Other account", "Mine")
	b.cookies["Other account"]["SID"] = "another-sid"
	b.cookies["Other account"]["SSID"] = "another-ssid"
	d := newGoogleAccountDaemon(t, b)

	if d.refreshSameAccount(context.Background(), "startup") != refreshOK {
		t.Fatal("no refresh although one profile has the session's sign-in")
	}
	if got := currentPSIDTS(d); got != "browser-Mine-1" {
		t.Fatalf("session cookie = %q; want the profile with the same sign-in", got)
	}
	if d.cookies.lastProfile != "Mine" {
		t.Fatalf("lastProfile = %q", d.cookies.lastProfile)
	}
}

func TestAutomaticReadsNeverSwapAccounts(t *testing.T) {
	b := newFakeCookieStore("Other account")
	b.cookies["Other account"]["SID"] = "another-sid"
	d := newGoogleAccountDaemon(t, b)
	if d.refreshSameAccount(context.Background(), "browserRenewed") == refreshOK {
		t.Fatal("cookies from another Google sign-in were taken")
	}
	if got := currentPSIDTS(d); got != "omachat-copy" {
		t.Fatalf("session cookie changed to %q", got)
	}
}

// --- review findings ---

// A request refused right after a read failed does not wait out the
// throttle: it fails, but leaves a Google account pairing in place for the
// cookie check, the session check and recovery to fix.
func TestRefusedRequestInsideThrottleFailsWithoutEndingPairing(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.cookies.last, d.cookies.lastResult = time.Now(), "failed"
	start := time.Now()
	calls := 0
	_, err := withAuthRetryAt(context.Background(), d, "rpc:test", func() (string, error) {
		calls++
		return "", errors.New("HTTP 401")
	})
	if err == nil || calls != 1 || b.readCount() != 0 {
		t.Fatalf("err=%v calls=%d reads=%d", err, calls, b.readCount())
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("waited %v", took)
	}
	if st, paired := stateOf(d); st == wire.StateError || !paired {
		t.Fatalf("state=%q paired=%v; a throttled read cost the pairing", st, paired)
	}
}

// Several requests refused at once all retry with the one read that brings a
// new cookie; none of them ends the pairing.
func TestConcurrentRefusedRequestsShareOneRead(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	release := make(chan struct{})
	extract := d.extractCookies
	d.extractCookies = func(ctx context.Context, p browser.Profile) (map[string]string, error) {
		<-release
		return extract(ctx, p)
	}
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = withAuthRetryAt(context.Background(), d, "rpc:test", func() (string, error) {
				if currentPSIDTS(d) == "omachat-copy" {
					return "", errors.New("HTTP 401")
				}
				return "ok", nil
			})
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
	}
	if n := b.readCount(); n != 1 {
		t.Fatalf("browser read %d times; want one shared read", n)
	}
	if st, paired := stateOf(d); st == wire.StateError || !paired {
		t.Fatalf("state=%q paired=%v", st, paired)
	}
}

func TestRecoveryWaitsForReadInProgress(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	release := make(chan struct{})
	extract := d.extractCookies
	d.extractCookies = func(ctx context.Context, p browser.Profile) (map[string]string, error) {
		<-release
		return extract(ctx, p)
	}
	reconnected := make(chan struct{}, 1)
	d.reconnectClient = func(c *libgm.Client) error { reconnected <- struct{}{}; return nil }
	go d.refreshSameAccount(context.Background(), "pingFailed") // slow read in progress
	eventually(t, "the read to start", func() bool { d.cookies.mu.Lock(); defer d.cookies.mu.Unlock(); return d.cookies.running != nil })

	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	time.Sleep(50 * time.Millisecond)
	close(release)
	select {
	case <-reconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("recovery gave up while a read was still running")
	}
	if n := b.readCount(); n != 1 {
		t.Fatalf("browser read %d times; recovery should have used the read in progress", n)
	}
}

func TestRecoveryReadsAgainAfterFailedRead(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.timing.refreshInterval = 100 * time.Millisecond
	d.cookies.last, d.cookies.lastResult = time.Now(), "failed"
	reconnected := make(chan struct{}, 1)
	d.reconnectClient = func(c *libgm.Client) error { reconnected <- struct{}{}; return nil }
	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	select {
	case <-reconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("recovery gave up after an earlier failed read instead of reading again")
	}
	if got := savedPSIDTS(t, d); got != "browser-Chromium-1" {
		t.Fatalf("saved cookie = %q", got)
	}
}

func TestRecoveryRetriesReconnectAfterNetworkError(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	var mu sync.Mutex
	calls := 0
	d.reconnectClient = func(c *libgm.Client) error {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls < 3 {
			return &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
		}
		return nil
	}
	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	eventually(t, "three reconnect attempts", func() bool { mu.Lock(); defer mu.Unlock(); return calls == 3 })
	time.Sleep(20 * time.Millisecond)
	if st, paired := stateOf(d); st == wire.StateError || st == wire.StateDisconnected || !paired {
		t.Fatalf("state=%q paired=%v after a successful third attempt", st, paired)
	}
}

func TestClientReplacedDuringReconnectIsDisconnected(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	old := d.client
	disconnected := make(chan *libgm.Client, 1)
	d.disconnectClient = func(c *libgm.Client) { disconnected <- c }
	d.reconnectClient = func(c *libgm.Client) error {
		// A new pairing replaces the client while Reconnect runs.
		d.mu.Lock()
		d.client = libgm.NewClient(libgm.NewAuthData(), nil, d.log)
		d.mu.Unlock()
		return nil
	}
	d.clientEventHandler(old, d.sessionContext())(refusedLongPoll())
	select {
	case c := <-disconnected:
		if c != old {
			t.Fatal("disconnected the wrong client")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the replaced client was left running")
	}
}

func TestFollowerRetriesAfterFailedRead(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	ctx := context.Background()
	d.followBrowserCookies(ctx) // first check reads and records the baseline
	waitRead(t, b)
	b.renew("Chromium", "browser-Chromium-2")
	b.mu.Lock()
	b.errs["Chromium"] = errors.New("keyring locked")
	b.mu.Unlock()
	d.cookies.last = time.Now().Add(-time.Hour)
	d.followBrowserCookies(ctx)
	waitRead(t, b)

	d.cookies.last = time.Now().Add(-time.Hour)
	d.followBrowserCookies(ctx)
	if b.readCount() != 2 {
		t.Fatal("a failed read was retried at once instead of after the backoff")
	}
	b.mu.Lock()
	delete(b.errs, "Chromium")
	b.mu.Unlock()
	d.follow.mu.Lock()
	d.follow.retryAfter = time.Now().Add(-time.Second)
	d.follow.mu.Unlock()
	d.followBrowserCookies(ctx)
	waitRead(t, b)
	if got := currentPSIDTS(d); got != "browser-Chromium-2" {
		t.Fatalf("the renewal was lost after a failed read: cookie = %q", got)
	}
}

// Every successful read records the baseline, so a renewal right after it is
// noticed rather than absorbed into the first check's baseline.
func TestReadRecordsBaseline(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	if !d.refreshBrowserCookiesFor(context.Background(), "startup", "") {
		t.Fatal("setup read failed")
	}
	<-b.readCh
	b.renew("Chromium", "browser-Chromium-2")
	d.cookies.last = time.Now().Add(-time.Hour)
	d.followBrowserCookies(context.Background()) // first check, but a baseline exists
	waitRead(t, b)
	if got := currentPSIDTS(d); got != "browser-Chromium-2" {
		t.Fatalf("a renewal between the read and the first check was missed: cookie = %q", got)
	}
}

func TestRefreshGivesSessionItsOwnCopy(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	var handed map[string]string
	extract := d.extractCookies
	d.extractCookies = func(ctx context.Context, p browser.Profile) (map[string]string, error) {
		c, err := extract(ctx, p)
		handed = c
		return c, err
	}
	if !d.refreshBrowserCookiesFor(context.Background(), "test", "") {
		t.Fatal("refresh failed")
	}
	d.auth.CookiesLock.Lock()
	d.auth.Cookies["SIDCC"] = "set-by-a-response"
	d.auth.CookiesLock.Unlock()
	if _, shared := handed["SIDCC"]; shared {
		t.Fatal("the session shares its cookie map with the diagnostics' copy")
	}
}

// A pairing that does not start leaves the profile OmaChat follows alone.
func TestFailedBrowserPairingDoesNotChangeFollowedProfile(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.cookies.lastProfile = "Earlier"
	d.sessionMu.Lock()
	d.gaiaActive = true // "Google pairing is already in progress"
	d.sessionMu.Unlock()
	if err := d.pairFromBrowser(context.Background()); err == nil {
		t.Fatal("pairing started although one was already in progress")
	}
	if d.cookies.lastProfile != "Earlier" {
		t.Fatalf("lastProfile = %q after a pairing that never started", d.cookies.lastProfile)
	}
}

func TestAutomaticReadsTryFollowedProfileFirst(t *testing.T) {
	b := newFakeCookieStore("Other", "Mine")
	d := newGoogleAccountDaemon(t, b)
	d.cookies.lastProfile = "Mine"
	if d.refreshSameAccount(context.Background(), "browserRenewed") != refreshOK {
		t.Fatal("refresh failed")
	}
	if first := <-b.readCh; first != "Mine" {
		t.Fatalf("read %q first; want the followed profile", first)
	}
}

func TestAutomaticReadsNeedTheRenewedCookie(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	delete(b.cookies["Chromium"], "__Secure-1PSIDTS")
	d := newGoogleAccountDaemon(t, b)
	if d.refreshSameAccount(context.Background(), "browserRenewed") == refreshOK {
		t.Fatal("an automatic read took cookies without __Secure-1PSIDTS")
	}
	if got := currentPSIDTS(d); got != "omachat-copy" {
		t.Fatalf("session cookie changed to %q", got)
	}
}

// --- final review findings ---

func connRefused() error { return &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED} }

// A. The read after a refused request takes only the session's own sign-in.
func TestRefusedRequestReadsOnlySameSignIn(t *testing.T) {
	b := newFakeCookieStore("Other account", "Mine")
	b.cookies["Other account"]["SID"] = "another-sid"
	d := newGoogleAccountDaemon(t, b)
	calls := 0
	_, err := withAuthRetryAt(context.Background(), d, "rpc:test", func() (string, error) {
		calls++
		if currentPSIDTS(d) == "omachat-copy" {
			return "", errors.New("HTTP 401")
		}
		return "ok", nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	if got := currentPSIDTS(d); got != "browser-Mine-1" {
		t.Fatalf("session cookie = %q; a refused request swapped in another account's cookies", got)
	}
	if d.cookies.lastProfile != "Mine" {
		t.Fatalf("lastProfile = %q", d.cookies.lastProfile)
	}
}

// B. A refused request whose read finds nothing newer gives up at once.
func TestRefusedRequestDoesNotWaitWhenBrowserHasNothingNewer(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	b.cookies["Chromium"] = fullCookies("omachat-copy")
	d := newGoogleAccountDaemon(t, b)
	d.timing.rpcFreshWait = 5 * time.Second
	start := time.Now()
	calls := 0
	_, err := withAuthRetryAt(context.Background(), d, "rpc:test", func() (string, error) {
		calls++
		return "", errors.New("HTTP 401")
	})
	if err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d; want one failed call and no retry", err, calls)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("a request the browser could not help waited %v", took)
	}
	if n := b.readCount(); n != 1 {
		t.Fatalf("browser read %d times", n)
	}
}

// D. Recovery tries again after its own read fails, within its wait.
func TestRecoveryRetriesAfterItsOwnReadFails(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.timing.refreshInterval = 50 * time.Millisecond
	failures := 1
	extract := d.extractCookies
	d.extractCookies = func(ctx context.Context, p browser.Profile) (map[string]string, error) {
		c, err := extract(ctx, p)
		if failures > 0 {
			failures--
			return nil, errors.New("file is not a database (torn copy)")
		}
		return c, err
	}
	reconnected := make(chan struct{}, 1)
	d.reconnectClient = func(c *libgm.Client) error { reconnected <- struct{}{}; return nil }
	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	select {
	case <-reconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("recovery gave up after one failed read")
	}
}

// E. Errors that are neither refusals nor network failures end recovery
// after the backoff steps instead of retrying forever.
func TestRecoveryGivesUpOnOtherReconnectErrors(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	var mu sync.Mutex
	calls := 0
	d.reconnectClient = func(c *libgm.Client) error {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return errors.New("failed to refresh auth token: HTTP 400")
	}
	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	eventually(t, "pairing to be required", func() bool { st, paired := stateOf(d); return st == wire.StateError && !paired })
	mu.Lock()
	defer mu.Unlock()
	if calls != len(d.timing.reconnectBackoff) {
		t.Fatalf("%d attempts; want one per backoff step", calls)
	}
}

// F. A session without __Secure-1PSIDTS still retries after a successful read.
func TestRefusedRequestRetriedWhenSessionHasNoRenewedCookie(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.auth.DestRegID = uuid.Nil
	c := fullCookies("x")
	delete(c, "__Secure-1PSIDTS")
	d.auth.SetCookies(c)
	calls := 0
	_, err := withAuthRetryAt(context.Background(), d, "rpc:test", func() (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("HTTP 401")
		}
		return "ok", nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d; want a retry after the read", err, calls)
	}
}

// G. A long poll refused moments after a read installed a new cookie was
// most likely sent with the old one: reconnect with the new one at once.
func TestRecoveryUsesCookieInstalledJustBeforeRefusal(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	if !d.refreshBrowserCookiesFor(context.Background(), "browserRenewed", "") {
		t.Fatal("setup read failed")
	}
	<-b.readCh
	reconnected := make(chan struct{}, 1)
	d.reconnectClient = func(c *libgm.Client) error { reconnected <- struct{}{}; return nil }
	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	select {
	case <-reconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("recovery waited for a cookie other than the one installed moments before")
	}
	if n := b.readCount(); n != 1 {
		t.Fatalf("browser read %d times; the installed cookie needed no new read", n)
	}
}

// H. A QR pairing started while recovery reconnects keeps its client.
func TestRecoveryLeavesClientOfPairingInProgress(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.disconnectClient = func(c *libgm.Client) { t.Error("disconnected the client a pairing is using") }
	done := make(chan struct{})
	d.reconnectClient = func(c *libgm.Client) error {
		defer close(done)
		d.mu.Lock()
		d.paired = false
		d.status.State = wire.StatePairing
		d.mu.Unlock()
		return nil
	}
	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	<-done
	time.Sleep(50 * time.Millisecond)
	if st, _ := stateOf(d); st != wire.StatePairing {
		t.Fatalf("state = %q; recovery overwrote the pairing screen", st)
	}
}
