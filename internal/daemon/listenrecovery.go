package daemon

import (
	"context"
	"time"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/util/exhttp"

	"github.com/onelegdave/omachat/internal/wire"
)

// listenRecoveryWindow allows one in-place recovery of a refused long poll per
// window. A second refusal within it means the browser's cookies did not
// help, so OmaChat asks the user to pair again as before.
const listenRecoveryWindow = 10 * time.Minute

// recentInstall is how recently a read must have put a new cookie in the
// session for a refused long poll to count as sent with the cookie before it.
const recentInstall = 5 * time.Second

// startListenRecoveryLocked begins recovering a Google account session whose
// long poll Google refused. It reports false when no attempt is allowed, and
// the caller then requires pairing. Caller holds sessionMu.
//
// A refused long poll stops libgm's listener, but the pairing itself survives
// it: once the session holds a newer cookie from the browser, saved, a
// reconnect brings it back. Restarting with the saved copy does not work,
// which is what the user would otherwise end up doing.
func (d *Daemon) startListenRecoveryLocked() bool {
	if !d.followsBrowser() {
		return false
	}
	if !d.listenRecoveryAt.IsZero() && time.Since(d.listenRecoveryAt) < listenRecoveryWindow {
		d.diagEvent("listenRecovery", logFields{"result": "skipped", "reason": "recentAttempt"})
		return false
	}
	d.mu.RLock()
	c, ctx := d.client, d.sessionCtx
	d.mu.RUnlock()
	if c == nil || ctx == nil {
		return false
	}
	// The long poll was refused with the cookie the session holds now,
	// unless a read replaced it moments ago: then the refused poll was most
	// likely sent with the one that read replaced.
	refused := d.sessionRenewedCookie()
	d.cookies.mu.Lock()
	if time.Since(d.cookies.installedAt) < recentInstall && d.cookies.replaced != "" {
		refused = d.cookies.replaced
	}
	d.cookies.mu.Unlock()
	d.listenRecoveryAt = time.Now()
	d.diagEvent("listenRecovery", logFields{"result": "started"})
	d.setState(wire.StateConnecting, "")
	go d.recoverListen(ctx, c, refused)
	return true
}

// recoverListen waits for the session to hold a cookie other than the
// refused one, reading the browser as needed (a successful read also saves
// the session), and then reconnects the same client in place.
func (d *Daemon) recoverListen(ctx context.Context, c *libgm.Client, refused string) {
	fresh := d.ensureNewCookie(ctx, refused, d.timing.recoveryFreshWait, true, func(ctx context.Context) refreshOutcome {
		return d.refreshSameAccount(ctx, "listenFatal")
	})
	if ctx.Err() != nil {
		return
	}
	if !fresh {
		d.endListenRecovery(ctx, c, "notRefreshed", nil)
		return
	}
	reconnect := d.reconnectClient
	if reconnect == nil {
		reconnect = (*libgm.Client).Reconnect
	}
	delay := d.timing.recoveryDelay
	backoff := d.timing.reconnectBackoff
	for attempt := 0; ; attempt++ {
		if !waitContext(ctx, delay) || !d.recoveryStillWanted(c) {
			return
		}
		err := reconnect(c)
		if ctx.Err() != nil || !d.recoveryStillWanted(c) {
			// Unpaired, replaced by a new pairing, or required to pair while
			// reconnecting: stop what Reconnect started so no hidden client
			// keeps polling Google. A QR pairing started meanwhile uses this
			// same client, so leave it to that.
			if !d.pairingWithClient(c) {
				d.disconnect(c)
			}
			return
		}
		if err == nil {
			d.diagEvent("listenRecovery", logFields{"result": "reconnected", "attempts": attempt + 1})
			d.inSession(ctx, func() {
				if d.recoveryStillWanted(c) {
					d.setState(wire.StateConnecting, "")
				}
			})
			// ListenRecovered starts a sync too; initialSync runs once at a time.
			go d.initialSync(ctx)
			return
		}
		// Network failures are retried for as long as the session lasts.
		// Anything else, a refusal included, means the reconnect itself does
		// not work: a refusal ends recovery at once, other errors after the
		// backoff steps.
		if isAuthError(err) || (!exhttp.IsNetworkError(err) && attempt+1 >= len(backoff)) {
			d.endListenRecovery(ctx, c, "reconnectFailed", err)
			return
		}
		d.diagEvent("listenRecovery", logFields{"result": "reconnectRetry", "attempts": attempt + 1, "err": errText(err)})
		d.inSession(ctx, func() {
			if d.recoveryStillWanted(c) {
				d.setState(wire.StateDisconnected, "Can't reach Google Messages. Retrying…")
			}
		})
		delay = backoff[min(attempt, len(backoff)-1)]
	}
}

// recoveryStillWanted reports whether c is still the session's client and the
// session still counts as paired.
func (d *Daemon) recoveryStillWanted(c *libgm.Client) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.client == c && d.paired
}

// endListenRecovery gives up and asks the user to pair again, as OmaChat did
// before recovery existed.
func (d *Daemon) endListenRecovery(ctx context.Context, c *libgm.Client, result string, err error) {
	d.diagEvent("listenRecovery", logFields{"result": result, "err": errText(err)})
	d.inSession(ctx, func() {
		if d.recoveryStillWanted(c) {
			d.requirePairingLocked()
		}
	})
}

// pairingWithClient reports whether a pairing in progress is using c.
func (d *Daemon) pairingWithClient(c *libgm.Client) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.client == c && (d.status.State == wire.StatePairing || d.status.State == wire.StateGaiaPairing)
}

func (d *Daemon) isCurrentClient(c *libgm.Client) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.client == c
}

func (d *Daemon) disconnect(c *libgm.Client) {
	if d.disconnectClient != nil {
		d.disconnectClient(c)
		return
	}
	c.Disconnect()
}
