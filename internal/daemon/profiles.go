package daemon

import (
	"context"
	"fmt"

	"github.com/onelegdave/omachat/internal/browser"
	"github.com/onelegdave/omachat/internal/wire"
)

// ListProfiles reports every browser profile that could supply Google cookies,
// with enough detail for the user to tell why one will not work.
//
// Automatic selection guesses the most recently used profile with a complete
// cookie set, which is right for a single-profile machine and wrong the moment
// someone keeps work and personal accounts apart.
func (d *Daemon) ListProfiles() []wire.BrowserProfile {
	selected := d.config.Get().BrowserProfile

	var out []wire.BrowserProfile
	for _, p := range d.browserProfiles() {
		entry := wire.BrowserProfile{
			Name:     p.Name,
			Browser:  p.BrowserName,
			Selected: p.Name == selected,
		}

		cookies, err := d.readBrowserCookies(context.Background(), p)
		switch {
		case err != nil:
			entry.Reason = err.Error()
		default:
			entry.Cookies = len(cookies)
			entry.Missing = wire.MissingGaiaCookies(cookies)
			entry.Usable = len(entry.Missing) == 0
			if !entry.Usable {
				if len(entry.Missing) == 1 && entry.Missing[0] == "OSID" {
					entry.Reason = "Signed in to Google, but not to Messages. Open messages.google.com/web in this profile."
				} else {
					entry.Reason = fmt.Sprintf("Missing %v", entry.Missing)
				}
			}
		}
		out = append(out, entry)
	}
	return out
}

// SetProfile pins the profile cookies are read from. An empty name restores
// automatic selection.
func (d *Daemon) SetProfile(name string) error {
	if name != "" {
		found := false
		for _, p := range d.browserProfiles() {
			if p.Name == name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("no browser profile named %q", name)
		}
	}
	if err := d.config.SetBrowserProfile(name); err != nil {
		return fmt.Errorf("save profile choice: %w", err)
	}
	d.log.Info().Str("profile", name).Msg("Browser profile selection changed")
	d.diagEvent("profileSelected", logFields{"profile": name})

	// Adopt the newly chosen profile's cookies immediately: the user picked it
	// precisely because the current ones are not working.
	d.refreshBrowserCookiesFor(d.sessionContext(), "setProfile", "")
	return nil
}

// candidateProfiles returns the profiles to try, honouring an explicit choice.
// A pinned profile that has since disappeared falls back to automatic rather
// than leaving the daemon with nothing to read.
func (d *Daemon) candidateProfiles() []browser.Profile {
	all := d.browserProfiles()
	selected := d.config.Get().BrowserProfile
	if selected == "" {
		return all
	}
	for _, p := range all {
		if p.Name == selected {
			d.cookies.mu.Lock()
			d.cookies.missingWarned = ""
			d.cookies.mu.Unlock()
			return []browser.Profile{p}
		}
	}
	// The cookie check asks every minute; warn once per missing profile.
	d.cookies.mu.Lock()
	warn := d.cookies.missingWarned != selected
	d.cookies.missingWarned = selected
	d.cookies.mu.Unlock()
	if warn {
		d.log.Warn().Str("profile", selected).Msg("Selected browser profile not found; falling back to automatic")
	}
	return all
}

// The real browser access, as package variables so the package's tests can
// switch all of it off (see TestMain) and never read the browser or keyring of
// the machine running them.
var (
	discoverBrowserProfiles = browser.DiscoverProfiles
	extractBrowserCookies   = browser.ExtractGoogleCookiesContext
	browserSQLite           = browser.DefaultSQLiteRunner
)

// browserProfiles lists the installed browser profiles.
func (d *Daemon) browserProfiles() []browser.Profile {
	if d.discoverProfiles != nil {
		return d.discoverProfiles()
	}
	return discoverBrowserProfiles()
}

// readBrowserCookies reads and decrypts a profile's Google cookies.
func (d *Daemon) readBrowserCookies(ctx context.Context, p browser.Profile) (map[string]string, error) {
	if d.extractCookies != nil {
		return d.extractCookies(ctx, p)
	}
	return extractBrowserCookies(ctx, p)
}
