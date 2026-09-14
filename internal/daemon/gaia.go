package daemon

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/mautrix-gmessages/pkg/libgm"
)

// gaiaPairTimeout bounds how long we wait for the phone to confirm. The phone's
// own verification window is shorter than this, so hitting it means the prompt
// was never seen rather than that it needs longer.
const gaiaPairTimeout = 3 * time.Minute

// StartGaiaPairing runs the Google-account pairing flow.
//
// Newer Google Messages builds have dropped the QR scanner entirely — the
// phone's "Device pairing" screen only offers "sign in to the same Google
// Account". That flow authenticates with cookies lifted from a signed-in
// messages.google.com session and confirms with an emoji shown on both ends,
// rather than a scannable code.
//
// FinishGaiaPairing blocks until the phone answers, so the whole exchange runs
// in the background and reports progress through events.
func (d *Daemon) StartGaiaPairing(cookies map[string]string) error {
	return d.startGaiaPairing(d.sessionContext(), cookies)
}

func (d *Daemon) startGaiaPairing(parent context.Context, cookies map[string]string) error {
	if err := validateGaiaCookies(cookies); err != nil {
		return err
	}

	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	if err := parent.Err(); err != nil {
		return err
	}
	if d.gaiaActive {
		return errors.New("Google pairing is already in progress")
	}
	// A fresh client and context isolate this explicit attempt from old
	// requests, callbacks, and background syncs. Keep the saved pairing
	// untouched until the phone confirms the replacement.
	d.stopPairRefresh()
	if d.sessionCancel != nil {
		d.sessionCancel()
	}
	base := d.maintCtx
	if base == nil {
		base = context.Background()
	}
	session, sessionCancel := context.WithCancel(base)
	auth := libgm.NewAuthData()
	c := libgm.NewClient(auth, nil, d.log.With().Str("component", "libgm").Logger())
	d.mu.Lock()
	old := d.client
	d.auth, d.client = auth, c
	d.sessionCtx, d.sessionCancel = session, sessionCancel
	d.mu.Unlock()
	d.bindClient(c, session)
	d.syncing = false
	if old != nil {
		go old.Disconnect()
	}

	auth.SetCookies(cookies)
	d.mu.Lock()
	d.paired = false
	d.mu.Unlock()
	d.gaiaActive = true
	ctx, cancel := context.WithTimeout(session, gaiaPairTimeout)
	d.gaiaCancel = cancel
	d.gaiaCtx = ctx

	d.mu.Lock()
	d.status.State = wire.StateGaiaPairing
	d.status.Error = ""
	d.status.QRURL = ""
	d.status.Emoji = ""
	st := d.status
	d.mu.Unlock()
	d.publish(wire.EventStatus, st)

	go func() {
		// FinishGaiaPairing blocks on the phone with no timeout of its own, so
		// an unanswered prompt would otherwise wedge the daemon in
		// "gaiaPairing" forever with no way back except a restart.
		defer cancel()
		defer func() {
			if session.Err() != nil {
				c.Disconnect()
			}
		}()
		defer d.inSession(session, func() { d.gaiaActive = false; d.gaiaCancel = nil })

		err := c.DoGaiaPairing(ctx, func(emoji string) {
			d.inSession(session, func() {
				if ctx.Err() != nil {
					return
				}
				d.mu.RLock()
				currentState := d.status.State
				d.mu.RUnlock()
				if currentState != wire.StateGaiaPairing {
					d.log.Warn().Str("state", string(currentState)).Msg("Ignoring emoji callback in non-pairing state")
					return
				}
				d.log.Info().Str("emoji", emoji).Msg("Gaia pairing emoji")
				d.mu.Lock()
				d.status.Emoji = emoji
				st := d.status
				d.mu.Unlock()
				d.publish(wire.EventStatus, st)
				d.publish(wire.EventEmoji, map[string]string{"emoji": emoji})
			})
		})
		if err != nil {
			d.inSession(session, func() {
				d.mu.Lock()
				d.status.Emoji = ""
				d.mu.Unlock()
				if errors.Is(err, context.DeadlineExceeded) {
					d.log.Warn().Msg("Gaia pairing timed out waiting for the phone")
					d.setState(wire.StateUnpaired,
						"the phone never confirmed. Open Google Messages on the phone "+
							"(Settings > Device pairing) and keep it on screen, then pair again.")
					return
				}
				d.log.Error().Err(err).Msg("Gaia pairing failed")
				// Cookies that fail here are usually stale rather than wrong, so
				// say so plainly instead of leaving a bare API error.
				d.setState(wire.StateError, gaiaErrorHint(err))
			})
			return
		}
		// PairSuccessful from DoGaiaPairing drives the rest through
		// handleEvent, which persists the session and reconnects.
		d.inSession(session, func() { d.gaiaActive = false; d.saveSessionLocked() })
		go d.initialSync(session)
	}()

	return nil
}

// validateGaiaCookies reports the specific cookies that are missing, since a
// generic failure here is very hard to act on.
func validateGaiaCookies(cookies map[string]string) error {
	if len(cookies) == 0 {
		return fmt.Errorf("no cookies supplied")
	}
	var missing []string
	for _, name := range wire.RequiredGaiaCookies {
		if strings.TrimSpace(cookies[name]) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("missing required cookie(s): %s. copy them from a signed-in messages.google.com session", strings.Join(missing, ", "))
	}
	return nil
}

func gaiaErrorHint(err error) string {
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "401"), strings.Contains(low, "403"), strings.Contains(low, "unauthorized"):
		return "Google rejected the cookies (" + msg + "). They expire quickly. re-copy them from a freshly loaded messages.google.com and try again."
	case strings.Contains(low, "no cookies"):
		return "No cookies were supplied. Run: omachatd pair --curl <file>"
	default:
		return msg
	}
}
