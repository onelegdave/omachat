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

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "401") ||
		strings.Contains(msg, "403") ||
		strings.Contains(msg, "invalid authentication") ||
		strings.Contains(msg, "unauthenticated") ||
		strings.Contains(msg, "oauth 2 access token") ||
		strings.Contains(msg, "session_cookie_invalid")
}

// refreshBrowserCookies re-reads Google cookies from the browser and installs
// them on the live client. Returns true when a complete cookie set was read.
func (d *Daemon) refreshBrowserCookies(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	d.cookies.mu.Lock()
	if time.Since(d.cookies.last) < cookieRefreshInterval {
		d.cookies.mu.Unlock()
		return false
	}
	d.cookies.last = time.Now()
	d.cookies.mu.Unlock()

	for _, p := range d.candidateProfiles() {
		if ctx.Err() != nil {
			return false
		}
		cookies, err := browser.ExtractGoogleCookiesContext(ctx, p)
		if err != nil || len(wire.MissingGaiaCookies(cookies)) > 0 {
			continue
		}
		d.sessionMu.Lock()
		if ctx.Err() != nil {
			d.sessionMu.Unlock()
			return false
		}
		d.mu.RLock()
		auth := d.auth
		d.mu.RUnlock()
		if auth == nil {
			d.sessionMu.Unlock()
			return false
		}
		changed := changedCookies(auth, cookies)
		auth.SetCookies(cookies)
		d.saveSessionLocked()
		d.sessionMu.Unlock()
		d.log.Info().
			Str("profile", p.Name).
			Strs("updated", changed).
			Msg("Refreshed Google cookies from browser")
		return true
	}

	d.log.Warn().Msg("Could not refresh cookies from any browser profile")
	return false
}

// withAuthRetry runs op and, on an authentication failure, tries to restore
// access by refreshing browser cookies and retrying once. It never initiates
// re-pairing automatically; re-pairing requires explicit user action.
func withAuthRetry[T any](ctx context.Context, d *Daemon, op func() (T, error)) (T, error) {
	if err := ctx.Err(); err != nil {
		var zero T
		return zero, err
	}
	result, err := op()
	if !isAuthError(err) {
		return result, err
	}

	if isSessionInvalid(err) {
		d.requirePairing(ctx)
		return result, err
	}
	d.log.Warn().Err(err).Msg("Request failed authentication; refreshing cookies")
	if d.refreshBrowserCookies(ctx) {
		if err := ctx.Err(); err != nil {
			var zero T
			return zero, err
		}
		result, err = op()
	}
	if isAuthError(err) {
		d.requirePairing(ctx)
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
		return
	}
	d.mu.Lock()
	d.paired = false
	d.mu.Unlock()
	d.setState(wire.StateError, "Google sign-in needs to be renewed. Select Pair with Google in OmaChat, then confirm the matching emoji on your phone.")
}
