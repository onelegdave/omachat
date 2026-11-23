package daemon

import (
	"context"
	"time"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
)

// A session check settles a refusal of a Google account session that a
// browser read could not fix at once: a request that stays refused, or a
// refused ping. While the session lasts, it ends in one of two ways, so a
// session is never left refused without a pairing prompt:
//
//   - fixed: the session gets a newer __Secure-1PSIDTS from the browser, or
//     Google accepts its cookies again (the refusal was a one-off);
//   - pairing required: Google still refuses the session's cookies, and the
//     browser has nothing newer to offer.
//
// (A long-poll recovery running at the same time decides instead, and a
// check of a session that is unpaired or replaced just stops.)
//
// It first waits recoveryFreshWait for a newer cookie, reading the browser as
// the throttle allows, because Chromium writes a renewal to disk about 30 s
// after making it. It stops waiting early when every profile was read and
// none holds the session's sign-in any more (signed out), since nothing newer
// can come then. Then it looks at the pings libgm sends every minute while
// connected: they use the same cookies as every request, so a ping refused
// with the same cookie means Google still refuses the session, and none
// refused within sessionCheckWait means it accepts it again. Pairing is only
// ever required on a refusal from Google seen during the check; what the
// browser holds alone never ends a session, and a failed read never counts
// as signed out.

// refusedCookie is the __Secure-1PSIDTS that a refusal arriving now was most
// likely sent with: the session's, unless a read replaced it moments ago.
func (d *Daemon) refusedCookie() string {
	refused := d.sessionRenewedCookie()
	d.cookies.mu.Lock()
	if time.Since(d.cookies.installedAt) < recentInstall && d.cookies.replaced != "" {
		refused = d.cookies.replaced
	}
	d.cookies.mu.Unlock()
	return refused
}

// notePingRefused records that Google refused a ping, for a session check
// that is watching the pings.
func (d *Daemon) notePingRefused() {
	d.check.mu.Lock()
	d.check.pingRefusedAt = time.Now()
	d.check.mu.Unlock()
}

// startSessionCheck begins a session check unless one is running already, in
// which case that one settles this refusal too. used is the refused cookie.
func (d *Daemon) startSessionCheck(ctx context.Context, trigger, used string) {
	if ctx.Err() != nil || !d.followsBrowser() {
		return
	}
	d.mu.RLock()
	c := d.client
	d.mu.RUnlock()
	if c == nil {
		return
	}
	d.check.mu.Lock()
	// A check of this client settles this refusal too. One left over from a
	// session that has ended is stopping and does not count.
	if d.check.running && d.check.client == c {
		d.check.mu.Unlock()
		return
	}
	d.check.gen++
	gen := d.check.gen
	d.check.running, d.check.client = true, c
	// Pings refused from now on count, including any before the goroutine
	// runs; the refusal that started the check was recorded just before.
	started := time.Now()
	d.check.mu.Unlock()
	d.diagEvent("sessionCheck", logFields{"trigger": trigger, "result": "started"})
	go d.runSessionCheck(ctx, c, gen, trigger, used, started)
}

func (d *Daemon) runSessionCheck(ctx context.Context, c *libgm.Client, gen int, trigger, used string, started time.Time) {
	defer func() {
		d.check.mu.Lock()
		if d.check.gen == gen {
			d.check.running, d.check.client = false, nil
		}
		d.check.mu.Unlock()
	}()
	got := d.ensureNewCookie(ctx, used, d.timing.recoveryFreshWait, true, func(ctx context.Context) refreshOutcome {
		return d.refreshSameAccount(ctx, "sessionCheck")
	})
	if ctx.Err() != nil {
		return
	}
	if got == newCookie {
		d.diagEvent("sessionCheck", logFields{"trigger": trigger, "result": "renewed"})
		return
	}
	// Nothing newer in the browser, or no profile holds the session's
	// sign-in any more. Does Google still refuse these cookies?
	result := "stillRefused"
	if got == signedOut {
		result = "signedOut"
	}
	deadline := time.Now().Add(d.timing.sessionCheckWait)
	for {
		if cur := d.sessionRenewedCookie(); cur != "" && cur != used {
			d.diagEvent("sessionCheck", logFields{"trigger": trigger, "result": "renewed"})
			return
		}
		d.check.mu.Lock()
		refusedSince := d.check.pingRefusedAt.After(started)
		d.check.mu.Unlock()
		if refusedSince {
			d.endSessionCheck(ctx, c, trigger, result)
			return
		}
		if !time.Now().Before(deadline) {
			d.diagEvent("sessionCheck", logFields{"trigger": trigger, "result": "accepted"})
			return
		}
		if !waitContext(ctx, d.timing.sessionCheckPoll) || !d.recoveryStillWanted(c) {
			return
		}
	}
}

// endSessionCheck asks the user to pair again, unless the session has moved
// on meanwhile or a long-poll recovery is deciding instead.
func (d *Daemon) endSessionCheck(ctx context.Context, c *libgm.Client, trigger, result string) {
	d.inSession(ctx, func() {
		if !d.recoveryStillWanted(c) {
			return
		}
		if d.listenRecovering {
			d.diagEvent("sessionCheck", logFields{"trigger": trigger, "result": result, "requirePairing": false, "reason": "listenRecovery"})
			return
		}
		d.diagEvent("sessionCheck", logFields{"trigger": trigger, "result": result, "requirePairing": true})
		d.requirePairingLocked()
	})
}
