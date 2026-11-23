package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"

	"github.com/onelegdave/omachat/internal/wire"
)

// Tests for the session check (sessioncheck.go): every refusal a browser read
// cannot fix at once ends with the session working or with pairing required.

func refusedPing() *events.PingFailed {
	return &events.PingFailed{Error: errors.New("failed to notify ditto activity: HTTP 401"), ErrorCount: 1}
}

func checkRunning(d *Daemon) bool {
	d.check.mu.Lock()
	defer d.check.mu.Unlock()
	return d.check.running
}

// quickSessionCheck shortens one daemon's session check to fractions of a
// second: the wait for a newer cookie, the reads within it, and the wait for
// a refused ping.
func quickSessionCheck(d *Daemon) {
	d.timing.recoveryFreshWait = 200 * time.Millisecond
	d.timing.refreshInterval = 20 * time.Millisecond
	d.timing.sessionCheckWait = 300 * time.Millisecond
}

// browserHasNothingNewer gives the browser profile the cookie the session
// already holds.
func browserHasNothingNewer(b *fakeCookieStore, profile string) {
	b.renew(profile, "omachat-copy")
}

// signOut removes every Google sign-in cookie from a browser profile, as
// signing out of Google there does.
func signOut(b *fakeCookieStore, profile string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cookies[profile] = map[string]string{}
}

func requirePaired(t *testing.T, d *Daemon) {
	t.Helper()
	if st, paired := stateOf(d); st == wire.StateError || !paired {
		t.Fatalf("state=%q paired=%v; the session should still be paired", st, paired)
	}
}

func eventuallyPairingRequired(t *testing.T, d *Daemon) {
	t.Helper()
	eventually(t, "pairing to be required", func() bool {
		st, paired := stateOf(d)
		return st == wire.StateError && !paired
	})
}

func TestSessionCheckTakesNewerCookie(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	quickSessionCheck(d)
	d.clientEventHandler(d.client, d.sessionContext())(refusedPing())
	eventually(t, "the check to end", func() bool { return !checkRunning(d) })
	if got := currentPSIDTS(d); got != "browser-Chromium-1" {
		t.Fatalf("session cookie = %q; want the browser's newer one", got)
	}
	requirePaired(t, d)
}

func TestSessionCheckRequiresPairingWhilePingsStayRefused(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	browserHasNothingNewer(b, "Chromium")
	d := newGoogleAccountDaemon(t, b)
	quickSessionCheck(d)
	handler := d.clientEventHandler(d.client, d.sessionContext())
	handler(refusedPing())
	time.Sleep(50 * time.Millisecond)
	handler(refusedPing()) // the next minute's ping, refused again
	// Still waiting for the browser to offer a newer cookie.
	requirePaired(t, d)
	eventuallyPairingRequired(t, d)
}

func TestSessionCheckEndsWhenGoogleAcceptsAgain(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	browserHasNothingNewer(b, "Chromium")
	d := newGoogleAccountDaemon(t, b)
	quickSessionCheck(d)
	d.clientEventHandler(d.client, d.sessionContext())(refusedPing())
	eventually(t, "the check to end", func() bool { return !checkRunning(d) })
	requirePaired(t, d)
}

func TestSessionCheckEndsWhenAnotherReadBringsNewerCookie(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	browserHasNothingNewer(b, "Chromium")
	d := newGoogleAccountDaemon(t, b)
	quickSessionCheck(d)
	d.timing.sessionCheckWait = time.Hour
	handler := d.clientEventHandler(d.client, d.sessionContext())
	handler(refusedPing())
	time.Sleep(250 * time.Millisecond) // past the wait for a newer cookie
	d.auth.CookiesLock.Lock()
	d.auth.Cookies[followedCookie] = "renewed-by-the-follower"
	d.auth.CookiesLock.Unlock()
	eventually(t, "the check to end", func() bool { return !checkRunning(d) })
	requirePaired(t, d)
}

// A profile signed out of Google ends the wait for a newer cookie at once,
// but pairing still takes Google refusing the session again.
func TestSignedOutBrowserRequiresPairingOnNextRefusedPing(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	signOut(b, "Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.timing.recoveryFreshWait = time.Hour // so only skipping the wait passes
	d.timing.sessionCheckWait = time.Hour
	handler := d.clientEventHandler(d.client, d.sessionContext())
	handler(refusedPing())
	waitRead(t, b)
	time.Sleep(50 * time.Millisecond)
	requirePaired(t, d)
	handler(refusedPing()) // the next minute's ping
	eventuallyPairingRequired(t, d)
}

func TestSessionSignedInToAnotherAccountRequiresPairingOnNextRefusedPing(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	b.cookies["Chromium"]["SID"] = "a-new-sign-in"
	d := newGoogleAccountDaemon(t, b)
	d.timing.recoveryFreshWait = time.Hour
	d.timing.sessionCheckWait = time.Hour
	handler := d.clientEventHandler(d.client, d.sessionContext())
	handler(refusedPing())
	waitRead(t, b)
	time.Sleep(50 * time.Millisecond)
	requirePaired(t, d)
	handler(refusedPing())
	eventuallyPairingRequired(t, d)
}

// Cookies pasted at pairing never came from the browser's profiles, so no
// profile holds their sign-in. A request refused for its own reasons, such as
// an expired attachment, must not end that session while Google accepts the
// rest.
func TestNoMatchingProfileAloneNeverEndsSession(t *testing.T) {
	b := newFakeCookieStore("Someone else")
	b.cookies["Someone else"]["SID"] = "another-sid"
	d := newGoogleAccountDaemon(t, b)
	quickSessionCheck(d)
	_, err := withAuthRetryAt(context.Background(), d, "rpc:downloadMedia", func() (string, error) {
		return "", errors.New("HTTP 403")
	})
	if err == nil {
		t.Fatal("the refused request succeeded")
	}
	requirePaired(t, d)
	eventually(t, "the check to end", func() bool { return !checkRunning(d) })
	requirePaired(t, d)
}

func TestUnreadableSignInIsNotSignedOut(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	b.undecryptable["Chromium"] = true
	d := newGoogleAccountDaemon(t, b)
	if got := d.refreshSameAccount(context.Background(), "test"); got != refreshFailed {
		t.Fatalf("outcome = %v; a SID that did not decrypt may still be the session's", got)
	}
}

func TestFailedReadNeverCountsAsSignedOut(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	b.errs["Chromium"] = errors.New("keyring locked")
	d := newGoogleAccountDaemon(t, b)
	quickSessionCheck(d)
	d.clientEventHandler(d.client, d.sessionContext())(refusedPing())
	eventually(t, "the check to end", func() bool { return !checkRunning(d) })
	requirePaired(t, d)
	if b.readCount() < 2 {
		t.Fatalf("read %d times; the check should keep trying failed reads", b.readCount())
	}
}

func TestSignedOutNeedsEveryProfileRead(t *testing.T) {
	b := newFakeCookieStore("Mine", "Other")
	signOut(b, "Mine")
	b.errs["Other"] = errors.New("keyring locked")
	d := newGoogleAccountDaemon(t, b)
	if got := d.refreshSameAccount(context.Background(), "test"); got != refreshFailed {
		t.Fatalf("outcome = %v; a profile that could not be read might hold the sign-in", got)
	}

	b2 := newFakeCookieStore("Mine", "Other")
	signOut(b2, "Mine")
	b2.cookies["Other"]["SID"] = "another-sid"
	d2 := newGoogleAccountDaemon(t, b2)
	if got := d2.refreshSameAccount(context.Background(), "test"); got != refreshSignedOut {
		t.Fatalf("outcome = %v; no profile holds the session's sign-in", got)
	}
}

func TestSameSignInWithoutRenewedCookieIsNotSignedOut(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	delete(b.cookies["Chromium"], followedCookie) // a cookie file caught mid-write
	d := newGoogleAccountDaemon(t, b)
	if got := d.refreshSameAccount(context.Background(), "test"); got != refreshFailed {
		t.Fatalf("outcome = %v; the profile still has the session's sign-in", got)
	}
}

func TestRefusedRequestWithSignedOutBrowserRequiresPairingOnRefusedPing(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	signOut(b, "Chromium")
	d := newGoogleAccountDaemon(t, b)
	// The check reads again once the request's read is past the throttle.
	d.timing.refreshInterval = 20 * time.Millisecond
	d.timing.recoveryFreshWait = time.Hour
	d.timing.sessionCheckWait = time.Hour
	_, err := withAuthRetryAt(context.Background(), d, "rpc:test", func() (string, error) {
		return "", errors.New("HTTP 401")
	})
	if err == nil {
		t.Fatal("the refused request succeeded")
	}
	requirePaired(t, d)
	if !checkRunning(d) {
		t.Fatal("the refused request did not start a session check")
	}
	d.clientEventHandler(d.client, d.sessionContext())(refusedPing())
	eventuallyPairingRequired(t, d)
}

func TestRefusedRequestStartsSessionCheck(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	browserHasNothingNewer(b, "Chromium")
	d := newGoogleAccountDaemon(t, b)
	quickSessionCheck(d)
	d.timing.sessionCheckWait = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	_, err := withAuthRetryAt(ctx, d, "rpc:test", func() (string, error) {
		return "", errors.New("HTTP 401")
	})
	cancel() // the request is over; the check carries on in the session
	if err == nil {
		t.Fatal("the refused request succeeded")
	}
	requirePaired(t, d)
	if !checkRunning(d) {
		t.Fatal("a request that stayed refused did not start a session check")
	}
	d.clientEventHandler(d.client, d.sessionContext())(refusedPing())
	eventuallyPairingRequired(t, d)
}

func TestRefusedRetryStartsSessionCheckWithNewCookie(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	quickSessionCheck(d)
	d.timing.sessionCheckWait = time.Hour
	calls := 0
	_, err := withAuthRetryAt(context.Background(), d, "rpc:test", func() (string, error) {
		calls++
		return "", errors.New("HTTP 401")
	})
	if err == nil || calls != 2 {
		t.Fatalf("err=%v calls=%d; want a refused retry with the browser's cookie", err, calls)
	}
	// The browser's newest cookie was refused too, and it has nothing newer.
	time.Sleep(50 * time.Millisecond)
	d.clientEventHandler(d.client, d.sessionContext())(refusedPing())
	eventuallyPairingRequired(t, d)
}

func TestSessionCheckLeavesDecisionToListenRecovery(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	browserHasNothingNewer(b, "Chromium")
	d := newGoogleAccountDaemon(t, b)
	quickSessionCheck(d)
	d.sessionMu.Lock()
	d.listenRecovering = true
	d.sessionMu.Unlock()
	handler := d.clientEventHandler(d.client, d.sessionContext())
	handler(refusedPing())
	time.Sleep(50 * time.Millisecond)
	handler(refusedPing())
	eventually(t, "the check to end", func() bool { return !checkRunning(d) })
	requirePaired(t, d)
}

func TestSessionCheckStopsWhenUnpaired(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	browserHasNothingNewer(b, "Chromium")
	d := newGoogleAccountDaemon(t, b)
	quickSessionCheck(d)
	handler := d.clientEventHandler(d.client, d.sessionContext())
	handler(refusedPing())
	time.Sleep(50 * time.Millisecond)
	handler(refusedPing())
	d.mu.Lock()
	d.paired = false
	d.status.State = wire.StateUnpaired
	d.mu.Unlock()
	eventually(t, "the check to end", func() bool { return !checkRunning(d) })
	if st, _ := stateOf(d); st != wire.StateUnpaired {
		t.Fatalf("state = %q; an unpaired session was asked to pair again", st)
	}
}

func TestQRPairingStartsNoSessionCheck(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.auth.DestRegID = [16]byte{}
	d.clientEventHandler(d.client, d.sessionContext())(refusedPing())
	if checkRunning(d) || b.readCount() != 0 {
		t.Fatal("a refused ping of a QR pairing started a session check")
	}
}

func TestRecoveryWithSignedOutBrowserRequiresPairingAtOnce(t *testing.T) {
	b := newFakeCookieStore("Chromium")
	signOut(b, "Chromium")
	d := newGoogleAccountDaemon(t, b)
	d.timing.recoveryFreshWait = time.Hour
	d.reconnectClient = func(c *libgm.Client) error { t.Error("reconnected a signed-out session"); return nil }
	d.clientEventHandler(d.client, d.sessionContext())(refusedLongPoll())
	eventuallyPairingRequired(t, d)
	eventually(t, "recovery to end", func() bool {
		d.sessionMu.Lock()
		defer d.sessionMu.Unlock()
		return !d.listenRecovering
	})
}
