package daemon

import (
	"context"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/mautrix-gmessages/pkg/libgm"
)

func waitContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}

func (d *Daemon) sessionContext() context.Context {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.sessionCtx == nil {
		return context.Background()
	}
	return d.sessionCtx
}

// inSession prevents an operation from publishing or writing old account
// data after unpairing. Reset cancels the context while holding this lock.
func (d *Daemon) inSession(ctx context.Context, f func()) bool {
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	if ctx.Err() != nil {
		return false
	}
	f()
	return true
}

func (d *Daemon) bindClient(c *libgm.Client, ctx context.Context) {
	c.SetEventHandler(d.clientEventHandler(c, ctx))
}

func (d *Daemon) clientEventHandler(c *libgm.Client, ctx context.Context) libgm.EventHandler {
	return func(evt any) {
		d.sessionMu.Lock()
		defer d.sessionMu.Unlock()
		if ctx.Err() != nil {
			return
		}
		d.mu.RLock()
		current := d.client == c
		d.mu.RUnlock()
		if current {
			d.handleEvent(evt)
		}
	}
}

// resetSessionLocked invalidates old callbacks before clearing credentials.
// Caller holds sessionMu. Disconnect the returned client outside that lock.
func (d *Daemon) resetSessionLocked() (*libgm.Client, error) {
	if d.sessionCancel != nil {
		d.sessionCancel()
	}
	if d.gaiaCancel != nil {
		d.gaiaCancel()
	}
	d.gaiaCancel = nil
	d.gaiaActive = false
	d.syncing = false
	d.stopPairRefresh()
	parent := d.maintCtx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	auth := libgm.NewAuthData()
	c := libgm.NewClient(auth, nil, d.log.With().Str("component", "libgm").Logger())
	d.mu.Lock()
	old := d.client
	d.paired = false
	d.client, d.auth = c, auth
	d.sessionCtx, d.sessionCancel = ctx, cancel
	d.convs = make(map[string]wire.Conversation)
	d.order = nil
	d.selfIDs = make(map[string]bool)
	d.status = wire.Status{State: wire.StateUnpaired, PhoneOK: true}
	d.mu.Unlock()
	d.bindClient(c, ctx)
	d.cookies.mu.Lock()
	d.cookies.last = time.Time{}
	d.cookies.mu.Unlock()
	d.media.reset()
	d.avatars.reset()
	d.reactMu.Lock()
	d.reactions = make(map[string][]reactionRecord)
	d.reactionOrder = nil
	d.reactMu.Unlock()
	err := d.paths.ClearSession()
	d.setState(wire.StateUnpaired, "")
	return old, err
}

func (d *Daemon) resetLoggedOut(ctx context.Context) {
	d.sessionMu.Lock()
	if ctx.Err() != nil {
		d.sessionMu.Unlock()
		return
	}
	old, err := d.resetSessionLocked()
	if err != nil {
		d.log.Error().Err(err).Msg("Could not clear logged-out session")
	}
	d.setState(wire.StateUnpaired, "Google signed this device out. Pair again.")
	d.sessionMu.Unlock()
	if old != nil {
		old.Disconnect()
	}
}

// Err observes session cancellation synchronously, even before AfterFunc has
// delivered it to the request's Done channel.
type sessionBoundContext struct {
	context.Context
	session context.Context
}

func (c sessionBoundContext) Err() error {
	if err := c.session.Err(); err != nil {
		return err
	}
	return c.Context.Err()
}
