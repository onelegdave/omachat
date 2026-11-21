package daemon

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"

	"github.com/onelegdave/omachat/internal/browser"
	"github.com/onelegdave/omachat/internal/wire"
)

// Google rotates session cookies (notably __Secure-1PSIDTS) as the user
// browses, so the copy captured at pairing time goes stale on its own. Every
// authenticated call then fails with HTTP 401 and the UI fills with
// "invalid authentication credentials".
//
// Rather than make the user re-pair, re-read the cookies from the browser
// profile and retry once. The browser is already keeping them fresh.

// cookieRefreshInterval throttles re-reads so a burst of failing calls cannot
// hammer the cookie database.
const cookieRefreshInterval = 30 * time.Second

type cookieRefresher struct {
	mu   sync.Mutex
	last time.Time

	// Diagnostics only: what the last refresh was for and how it ended, and
	// which profile last supplied cookies.
	lastTrigger string
	lastResult  string
	lastProfile string
}

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
	if ctx.Err() != nil {
		return false
	}
	d.cookies.mu.Lock()
	if since := time.Since(d.cookies.last); since < cookieRefreshInterval {
		lastTrigger, lastResult := d.cookies.lastTrigger, d.cookies.lastResult
		d.cookies.mu.Unlock()
		d.diagCookieRefresh(logFields{
			"trigger": trigger, "callID": callID, "result": "throttled",
			"sinceLastSec": since.Seconds(), "lastTrigger": lastTrigger, "lastResult": lastResult,
		})
		return false
	}
	d.cookies.last = time.Now()
	d.cookies.lastTrigger = trigger
	d.cookies.lastResult = "running"
	d.cookies.mu.Unlock()
	result := "failed"
	defer func() {
		d.cookies.mu.Lock()
		d.cookies.lastResult = result
		d.cookies.mu.Unlock()
	}()
	ended := func(r string) bool {
		result = r
		d.diagCookieRefresh(logFields{"trigger": trigger, "callID": callID, "result": r})
		return false
	}

	var attempts []logFields
	for _, p := range d.candidateProfiles() {
		if ctx.Err() != nil {
			return ended("canceled")
		}
		cookies, err := browser.ExtractGoogleCookiesContext(ctx, p)
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
		changed := changedCookies(auth, cookies)
		var before map[string]string
		if d.diagOn() {
			before = snapshotCookies(auth)
		}
		auth.SetCookies(cookies)
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
		d.diagCookieRefreshSuccess(ctx, trigger, callID, p, attempts, before, cookies, changed)
		return true
	}

	d.log.Warn().Msg("Could not refresh cookies from any browser profile")
	d.diagCookieRefresh(logFields{"trigger": trigger, "callID": callID, "result": "noUsableProfile", "profiles": attempts})
	return false
}

// withAuthRetry runs op and, on an authentication failure, tries to restore
// access by refreshing browser cookies and retrying once. It never initiates
// re-pairing automatically; re-pairing requires explicit user action.
func withAuthRetry[T any](ctx context.Context, d *Daemon, op func() (T, error)) (T, error) {
	return withAuthRetryAt(ctx, d, "rpc", op)
}

// withAuthRetryAt is withAuthRetry with a request name for the diagnostics
// log. The name does not affect what it does.
func withAuthRetryAt[T any](ctx context.Context, d *Daemon, where string, op func() (T, error)) (T, error) {
	if err := ctx.Err(); err != nil {
		var zero T
		return zero, err
	}
	result, err := op()
	if !isAuthError(err) {
		return result, err
	}
	callID := d.diagAuthFailure(where, err, true)

	if isSessionInvalid(err) {
		d.requirePairing(ctx)
		d.diagAuthOutcome(callID, where, logFields{"refresh": "skipped", "reason": "sessionInvalidMatched", "retried": false, "requirePairing": true})
		return result, err
	}
	d.log.Warn().Err(err).Msg("Request failed authentication; refreshing cookies")
	refreshed := d.refreshBrowserCookiesFor(ctx, where, callID)
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
	needsPairing := isAuthError(err)
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
