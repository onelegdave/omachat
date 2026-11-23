package daemon

import (
	"context"
	"time"
)

// Why this exists.
//
// Two credentials keep a paired session alive:
//
//   - The tachyon auth token refreshes on its own. libgm signs a refresh with
//     the stored ECDSA key roughly an hour before expiry, needing no cookies.
//
//   - The Google cookies of a Google account pairing come from the browser.
//     libgm applies the Set-Cookie headers Google returns, but Google only
//     ever sends SIDCC that way, never a new __Secure-1PSIDTS. Meanwhile the
//     browser renews its own __Secure-1PSIDTS about every 10 minutes, and
//     Google refuses OmaChat's copy roughly two renewals later. Once the long
//     poll is refused, the session is gone unless OmaChat recovers it.
//
// So OmaChat follows the browser (cookiefollow.go): it reads the cookies again
// soon after each renewal, which keeps its copy at most one renewal behind.
// It also reads them at startup, after a refused request or ping, and before
// recovering a refused long poll (authretry.go, daemon.go). Reading on a fixed
// timer is not enough on its own: a copy taken just before a renewal goes out
// of date about 10 minutes later, so the timer would need to be well under
// that, and it would decrypt the cookies far more often than needed.

const (
	// sessionSaveInterval captures the token and cookie updates libgm applies
	// in place, so an unclean shutdown loses at most this much.
	sessionSaveInterval = 10 * time.Minute
)

// runMaintenance persists the session periodically so refreshed credentials
// survive a restart, and keeps a Google account session's cookies in step
// with the browser.
func (d *Daemon) runMaintenance(ctx context.Context) {
	saveTicker := time.NewTicker(sessionSaveInterval)
	defer saveTicker.Stop()
	followTicker := time.NewTicker(d.follow.interval)
	defer followTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-saveTicker.C:
			d.saveSession()
		case <-followTicker.C:
			d.followBrowserCookies(d.sessionContext())
		}
	}
}
