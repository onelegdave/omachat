package daemon

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/onelegdave/omachat/internal/browser"
	"github.com/onelegdave/omachat/internal/wire"
)

// PairFromBrowser finds a signed-in browser profile, reads the Google cookies
// out of it, and starts Gaia pairing — the whole flow the widget needs, with
// no terminal step.
//
// When it cannot proceed it sets a specific hint rather than a bare error:
// "missing OSID" is meaningless on its own, but "sign in to Messages in
// Chrome / Profile 1" is something a person can act on.
func (d *Daemon) PairFromBrowser() error {
	return d.pairFromBrowser(d.sessionContext())
}

func (d *Daemon) pairFromBrowser(ctx context.Context) error {
	profiles := d.candidateProfiles()
	if len(profiles) == 0 {
		d.setPairErrorFor(ctx, "No browser profile found",
			"Install or sign in to Chrome, Chromium, or Brave, then try again.")
		return fmt.Errorf("no browser profile found")
	}

	var (
		bestCookies map[string]string
		bestProfile string
		bestMissing []string
		readErrs    []string
	)

	for _, p := range profiles {
		if err := ctx.Err(); err != nil {
			return err
		}
		cookies, err := browser.ExtractGoogleCookiesContext(ctx, p)
		if err != nil {
			readErrs = append(readErrs, p.Name+": "+err.Error())
			continue
		}
		missing := wire.MissingGaiaCookies(cookies)
		if len(missing) == 0 {
			d.log.Info().Str("profile", p.Name).Msg("Using browser profile for pairing")
			d.inSession(ctx, func() {
				d.mu.Lock()
				d.status.Profile = p.Name
				d.mu.Unlock()
			})
			return d.startGaiaPairing(ctx, cookies)
		}
		// Track the closest profile so the hint can name it.
		if bestCookies == nil || len(missing) < len(bestMissing) {
			bestCookies, bestProfile, bestMissing = cookies, p.Name, missing
		}
	}

	if bestCookies == nil {
		detail := strings.Join(readErrs, "; ")
		d.setPairErrorFor(ctx, "Could not read browser cookies",
			"Make sure your login keyring is unlocked. "+detail)
		return fmt.Errorf("could not read cookies: %s", detail)
	}

	sort.Strings(bestMissing)
	hint := fmt.Sprintf("Open messages.google.com/web in %s, sign in, let it load, then try again.", bestProfile)
	if len(bestMissing) == 1 && bestMissing[0] == "OSID" {
		// The overwhelmingly common case: signed in to Google generally, but
		// never to Messages specifically, which is the only thing that sets OSID.
		hint = fmt.Sprintf("You're signed in to Google in %s but not to Messages. "+
			"Open messages.google.com/web there, let your conversations load, then try again.", bestProfile)
	}
	d.setPairErrorFor(ctx, "Not signed in to Google Messages", hint)
	return fmt.Errorf("missing cookies: %s", strings.Join(bestMissing, ", "))
}

func (d *Daemon) setPairErrorFor(ctx context.Context, msg, hint string) {
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	if ctx.Err() != nil {
		return
	}
	d.mu.Lock()
	d.status.State = wire.StateError
	d.status.Error = msg
	d.status.Hint = hint
	st := d.status
	d.mu.Unlock()
	d.publish(wire.EventStatus, st)
}
