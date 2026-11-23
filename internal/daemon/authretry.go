package daemon

import (
	"context"
	"maps"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"

	"github.com/onelegdave/omachat/internal/wire"
)

// Google rotates session cookies (notably __Secure-1PSIDTS) as the user
// browses, so the copy captured at pairing time goes stale on its own. Every
// authenticated call then fails with HTTP 401 and the UI fills with
// "invalid authentication credentials".
//
// Rather than make the user re-pair, re-read the cookies from the browser
// profile and retry once. The browser is already keeping them fresh.

// authTimings are the waits around reading the browser and recovering a
// refused long poll. They live on the daemon so a test can shorten them for
// its own daemon without racing other tests' goroutines.
type authTimings struct {
	// refreshInterval throttles browser reads so a burst of failing calls
	// cannot hammer the cookie database.
	refreshInterval time.Duration
	// rpcFreshWait bounds how long a refused request waits for a new cookie,
	// and recoveryFreshWait how long a refused long poll waits. The longer
	// one covers Chromium writing a renewal to disk about 30 s after making it.
	rpcFreshWait      time.Duration
	recoveryFreshWait time.Duration
	// recoveryDelay lets libgm's stopped long-poll loop finish returning
	// before recovery reconnects; reconnectBackoff spaces out reconnects that
	// fail for reasons other than a refusal, such as the network. Its last
	// step repeats for as long as the session lasts.
	recoveryDelay    time.Duration
	reconnectBackoff []time.Duration
}

func defaultAuthTimings() authTimings {
	return authTimings{
		refreshInterval:   30 * time.Second,
		rpcFreshWait:      10 * time.Second,
		recoveryFreshWait: 90 * time.Second,
		recoveryDelay:     time.Second,
		reconnectBackoff:  []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute},
	}
}

// automaticReadTimeout bounds a browser read OmaChat starts on its own, so a
// keyring waiting on an unlock prompt cannot hold anything up for long.
const automaticReadTimeout = 30 * time.Second

type cookieRefresher struct {
	mu   sync.Mutex
	last time.Time
	// running is closed when the read in progress ends; nil when idle. Only
	// one read runs at a time.
	running chan struct{}

	// Diagnostics only: what the last refresh was for and how it ended, and
	// which profile last supplied cookies.
	lastTrigger string
	lastResult  string
	lastProfile string

	// installedAt is when a read last put a new __Secure-1PSIDTS in the
	// session, and replaced the one it put there instead. Compared, never
	// logged.
	installedAt time.Time
	replaced    string

	// missingWarned is the pinned profile last reported missing, so the
	// warning is logged once rather than at every check.
	missingWarned string
}

// refreshOutcome is how one attempt to read the browser's cookies ended.
type refreshOutcome int

const (
	refreshOK refreshOutcome = iota
	// refreshThrottled: another read is running or ran moments ago.
	refreshThrottled
	refreshFailed
)

// changedCookies names the cookies whose values differ. Names only: the values
// are live credentials and must never reach a log.
func changedCookies(auth *libgm.AuthData, fresh map[string]string) []string {
	auth.CookiesLock.RLock()
	defer auth.CookiesLock.RUnlock()

	var changed []string
	for name, value := range fresh {
		if auth.Cookies[name] != value {
			changed = append(changed, name)
		}
	}
	sort.Strings(changed)
	return changed
}

func isSessionInvalid(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "session_cookie_invalid")
}

// authErrorMarkers are matched, in order, against the lowercased error text.
var authErrorMarkers = []string{"401", "403", "invalid authentication", "unauthenticated", "oauth 2 access token", "session_cookie_invalid"}

func isAuthError(err error) bool {
	return authErrorMatch(err) != ""
}

// authErrorMatch names the marker that makes err a sign-in failure, or "".
func authErrorMatch(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range authErrorMarkers {
		if strings.Contains(msg, marker) {
			return marker
		}
	}
	return ""
}

// refreshBrowserCookies re-reads Google cookies from the browser and installs
// them on the live client. Returns true when a complete cookie set was read.
func (d *Daemon) refreshBrowserCookies(ctx context.Context) bool {
	return d.refreshBrowserCookiesFor(ctx, "unspecified", "")
}

// refreshBrowserCookiesFor is refreshBrowserCookies with a trigger and call
// ID for the diagnostics log. They do not affect what it does.
func (d *Daemon) refreshBrowserCookiesFor(ctx context.Context, trigger, callID string) bool {
	return d.refreshFromBrowser(ctx, trigger, callID, false) == refreshOK
}

// refreshSameAccount is refreshBrowserCookiesFor for reads OmaChat starts on
// its own rather than after a refused request or a profile choice. It takes
// cookies only from a profile signed in with the same Google sign-in as the
// session, so a second profile with another account is never swapped in, and
// it gives up after automaticReadTimeout.
func (d *Daemon) refreshSameAccount(ctx context.Context, trigger string) refreshOutcome {
	ctx, cancel := context.WithTimeout(ctx, automaticReadTimeout)
	defer cancel()
	return d.refreshFromBrowser(ctx, trigger, "", true)
}

// sameGoogleSignIn reports whether browser cookies belong to the session's
// Google sign-in. SID and SSID stay the same while the browser renews
// __Secure-1PSIDTS, and change when someone signs in again or with another
// account. The values are compared in memory and never logged.
func sameGoogleSignIn(auth *libgm.AuthData, cookies map[string]string) bool {
	auth.CookiesLock.RLock()
	defer auth.CookiesLock.RUnlock()
	for _, name := range []string{"SID", "SSID"} {
		have := auth.Cookies[name]
		if have == "" || have != cookies[name] {
			return false
		}
	}
	return true
}

// sessionRenewedCookie is the session's current __Secure-1PSIDTS. It is
// compared, never logged.
func (d *Daemon) sessionRenewedCookie() string {
	d.mu.RLock()
	auth := d.auth
	d.mu.RUnlock()
	if auth == nil {
		return ""
	}
	auth.CookiesLock.RLock()
	defer auth.CookiesLock.RUnlock()
	return auth.Cookies[followedCookie]
}

func (d *Daemon) refreshFromBrowser(ctx context.Context, trigger, callID string, sameAccount bool) refreshOutcome {
	if ctx.Err() != nil {
		return refreshFailed
	}
	d.cookies.mu.Lock()
	if since := time.Since(d.cookies.last); d.cookies.running != nil || since < d.timing.refreshInterval {
		lastTrigger, lastResult := d.cookies.lastTrigger, d.cookies.lastResult
		d.cookies.mu.Unlock()
		d.diagCookieRefresh(logFields{
			"trigger": trigger, "callID": callID, "result": "throttled",
			"sinceLastSec": since.Seconds(), "lastTrigger": lastTrigger, "lastResult": lastResult,
		})
		return refreshThrottled
	}
	running := make(chan struct{})
	d.cookies.last = time.Now()
	d.cookies.running = running
	d.cookies.lastTrigger = trigger
	d.cookies.lastResult = "running"
	preferred := d.cookies.lastProfile
	d.cookies.mu.Unlock()
	result := "failed"
	defer func() {
		d.cookies.mu.Lock()
		d.cookies.lastResult = result
		d.cookies.running = nil
		d.cookies.mu.Unlock()
		close(running)
	}()
	ended := func(r string) refreshOutcome {
		result = r
		d.diagCookieRefresh(logFields{"trigger": trigger, "callID": callID, "result": r})
		return refreshFailed
	}

	profiles := d.candidateProfiles()
	if sameAccount {
		// Try the profile the session's cookies came from first, so the
		// usual read decrypts only that one.
		sort.SliceStable(profiles, func(i, j int) bool {
			return profiles[i].Name == preferred && profiles[j].Name != preferred
		})
	}
	var attempts []logFields
	for _, p := range profiles {
		if ctx.Err() != nil {
			return ended("canceled")
		}
		// What the browser's renewed cookie looks like before the read: the
		// follower's next baseline if this profile is used.
		base := d.follow.snapshot(ctx, p)
		cookies, err := d.readBrowserCookies(ctx, p)
		if err != nil {
			attempts = append(attempts, logFields{"profile": p.Name, "error": err.Error()})
			continue
		}
		if missing := wire.MissingGaiaCookies(cookies); len(missing) > 0 {
			attempts = append(attempts, logFields{"profile": p.Name, "missing": missing})
			continue
		}
		d.sessionMu.Lock()
		if ctx.Err() != nil {
			d.sessionMu.Unlock()
			return ended("canceled")
		}
		d.mu.RLock()
		auth := d.auth
		d.mu.RUnlock()
		if auth == nil {
			d.sessionMu.Unlock()
			return ended("noSession")
		}
		if sameAccount && (cookies[followedCookie] == "" || !sameGoogleSignIn(auth, cookies)) {
			d.sessionMu.Unlock()
			attempts = append(attempts, logFields{"profile": p.Name, "skipped": "otherSignIn"})
			continue
		}
		changed := changedCookies(auth, cookies)
		var before map[string]string
		if d.diagOn() {
			before = snapshotCookies(auth)
		}
		auth.CookiesLock.RLock()
		previous := auth.Cookies[followedCookie]
		auth.CookiesLock.RUnlock()
		// The session gets its own copy: libgm writes Set-Cookie updates
		// into it while the diagnostics below still read this one.
		auth.SetCookies(maps.Clone(cookies))
		if cookies[followedCookie] != previous {
			d.cookies.mu.Lock()
			d.cookies.installedAt, d.cookies.replaced = time.Now(), previous
			d.cookies.mu.Unlock()
		}
		d.saveSessionLocked()
		d.sessionMu.Unlock()
		d.log.Info().
			Str("profile", p.Name).
			Strs("updated", changed).
			Msg("Refreshed Google cookies from browser")
		result = "ok"
		d.cookies.mu.Lock()
		d.cookies.lastProfile = p.Name
		d.cookies.mu.Unlock()
		d.follow.setBaseline(p, base)
		d.diagCookieRefreshSuccess(ctx, trigger, callID, p, attempts, before, cookies, changed)
		return refreshOK
	}

	d.log.Warn().Msg("Could not refresh cookies from any browser profile")
	d.diagCookieRefresh(logFields{"trigger": trigger, "callID": callID, "result": "noUsableProfile", "profiles": attempts})
	return refreshFailed
}

// ensureNewCookie makes sure the session holds a __Secure-1PSIDTS other than
// used, the one a refused request or long poll was sent with, reading the
// browser as needed. Concurrent callers all see the first read that brings a
// new cookie, whoever started it, because reads run one at a time.
//
// A request (persistent false) only waits for a read already in progress:
// if its own read finds nothing newer, or fails, it gives up at once rather
// than holding the UI for the whole wait. Recovery (persistent true) keeps
// trying until wait runs out, reading again whenever the throttle allows,
// because the browser may not have written a renewal to disk yet and a
// failed read may be a torn copy of a file being written.
//
// A session with no __Secure-1PSIDTS at all counts any successful read as
// fresh, as before this cookie was tracked.
func (d *Daemon) ensureNewCookie(ctx context.Context, used string, wait time.Duration, persistent bool, read func(context.Context) refreshOutcome) bool {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	renewed := func() bool {
		v := d.sessionRenewedCookie()
		return v != "" && v != used
	}
	for {
		if renewed() {
			return true
		}
		switch read(ctx) {
		case refreshOK:
			if renewed() || used == "" {
				return true
			}
			if !persistent {
				return false
			}
		case refreshFailed:
			if renewed() {
				return true
			}
			if !persistent || ctx.Err() != nil {
				return false
			}
		case refreshThrottled:
			if !persistent && !d.readRunning() {
				return renewed()
			}
		}
		if !d.waitForNextRead(ctx) {
			return renewed()
		}
	}
}

func (d *Daemon) readRunning() bool {
	d.cookies.mu.Lock()
	defer d.cookies.mu.Unlock()
	return d.cookies.running != nil
}

// waitForNextRead waits until another browser read may start: the read in
// progress has ended, or the throttle interval has passed.
func (d *Daemon) waitForNextRead(ctx context.Context) bool {
	d.cookies.mu.Lock()
	running := d.cookies.running
	wait := d.timing.refreshInterval - time.Since(d.cookies.last)
	d.cookies.mu.Unlock()
	if running != nil {
		select {
		case <-running:
			return ctx.Err() == nil
		case <-ctx.Done():
			return false
		}
	}
	return waitContext(ctx, wait+10*time.Millisecond)
}

// withAuthRetry runs op and, on an authentication failure, tries to restore
// access by refreshing browser cookies and retrying once. It never initiates
// re-pairing automatically; re-pairing requires explicit user action.
func withAuthRetry[T any](ctx context.Context, d *Daemon, op func() (T, error)) (T, error) {
	return withAuthRetryAt(ctx, d, "rpc", op)
}

// withAuthRetryAt is withAuthRetry with a request name for the diagnostics
// log. The name does not affect what it does.
//
// For a session that follows the browser (a Google account pairing), a
// request that stays refused no longer requires pairing: the long poll
// decides that, after its own recovery attempt (listenrecovery.go). A refused
// request is far weaker evidence, and a burst of them during a renewal would
// otherwise end a session that recovery could save.
func withAuthRetryAt[T any](ctx context.Context, d *Daemon, where string, op func() (T, error)) (T, error) {
	if err := ctx.Err(); err != nil {
		var zero T
		return zero, err
	}
	used := d.sessionRenewedCookie()
	result, err := op()
	if !isAuthError(err) {
		return result, err
	}
	callID := d.diagAuthFailure(where, err, true)
	follows := d.followsBrowser()

	if isSessionInvalid(err) && !follows {
		d.requirePairing(ctx)
		d.diagAuthOutcome(callID, where, logFields{"refresh": "skipped", "reason": "sessionInvalidMatched", "retried": false, "requirePairing": true})
		return result, err
	}
	d.log.Warn().Err(err).Msg("Request failed authentication; refreshing cookies")
	// A session that follows the browser takes cookies only from a profile
	// with its own Google sign-in, as the automatic reads do.
	refreshed := d.ensureNewCookie(ctx, used, d.timing.rpcFreshWait, false, func(ctx context.Context) refreshOutcome {
		return d.refreshFromBrowser(ctx, where, callID, follows)
	})
	retried := false
	if refreshed {
		if err := ctx.Err(); err != nil {
			var zero T
			d.diagAuthOutcome(callID, where, logFields{"refresh": "ok", "retried": false, "canceled": true, "requirePairing": false})
			return zero, err
		}
		result, err = op()
		retried = true
	}
	needsPairing := isAuthError(err) && !follows
	if needsPairing {
		d.requirePairing(ctx)
	}
	if callID != "" {
		outcome := logFields{"refresh": map[bool]string{true: "ok", false: "notRefreshed"}[refreshed], "retried": retried, "requirePairing": needsPairing}
		if retried {
			if err != nil {
				outcome["retry"] = describeAuthError(err)
			} else {
				outcome["retry"] = logFields{"ok": true}
			}
		}
		d.diagAuthOutcome(callID, where, outcome)
	}
	return result, err
}

// Re-pairing is an explicit user action. Latch the failed session so late
// transport events and in-flight sync responses cannot announce readiness.
func (d *Daemon) requirePairing(ctx context.Context) {
	d.inSession(ctx, d.requirePairingLocked)
}

func (d *Daemon) requirePairingLocked() {
	if d.gaiaActive {
		d.diagEvent("requirePairing", logFields{"ignored": "pairingInProgress"})
		return
	}
	d.diagEvent("requirePairing", logFields{"ignored": ""})
	d.mu.Lock()
	d.paired = false
	d.mu.Unlock()
	d.setState(wire.StateError, "Google sign-in needs to be renewed. Select Pair with Google in OmaChat, then confirm the matching emoji on your phone.")
}
