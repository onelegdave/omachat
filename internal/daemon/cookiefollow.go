package daemon

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/onelegdave/omachat/internal/browser"
	"github.com/onelegdave/omachat/internal/wire"
)

// cookieFollowInterval is how often OmaChat checks whether the browser has
// renewed its __Secure-1PSIDTS cookie. Chromium renews it about every 10
// minutes while a Google page is open, and Google refuses OmaChat's copy
// roughly two renewals later, so a check every minute leaves plenty of margin.
const cookieFollowInterval = time.Minute

// cookieFollowRetry spaces out reads after one failed, so a locked keyring
// or a browser that cannot be read is not asked again every minute.
const cookieFollowRetry = 5 * time.Minute

// followedCookie is the browser cookie whose renewal makes OmaChat's copy go
// out of date. Google never sends OmaChat a new one, so the browser is the
// only source.
const followedCookie = "__Secure-1PSIDTS"

// cookieFollower keeps a Google account session's cookies in step with the
// browser it was paired from, without decrypting anything until it must.
//
// Each check costs a stat of the cookie database. Only when the file has
// changed does it read cookie metadata, which needs sqlite3 but not the
// keyring, and compares a hash of the still-encrypted __Secure-1PSIDTS with
// the baseline. Only when that differs does it read the cookies again, which
// decrypts them.
//
// The baseline is the encrypted hash of the browser's cookie at the moment
// OmaChat took its copy: a stand-in for "the cookie OmaChat holds" that can
// be compared without the browser's key. It is recorded just before each
// read (snapshot, then setBaseline once the read succeeded), so a renewal
// that lands during the read makes the baseline older than OmaChat's copy,
// costing one extra read, rather than newer, which would hide a renewal.
type cookieFollower struct {
	interval time.Duration
	stat     func(path string) (time.Time, error)
	read     func(ctx context.Context, p browser.Profile) ([]browser.CookieMeta, error)

	mu         sync.Mutex
	lastMod    map[string]time.Time // per profile: cookie file time last checked
	baseline   map[string]string    // per profile: encrypted hash OmaChat's copy matches
	retryAfter time.Time
}

// followSnapshot is a profile's cookie file time and __Secure-1PSIDTS hash at
// one moment. An empty fp means it could not be taken.
type followSnapshot struct {
	mod time.Time
	fp  string
}

func newCookieFollower() *cookieFollower {
	return &cookieFollower{
		interval: cookieFollowInterval,
		stat: func(path string) (time.Time, error) {
			st, err := os.Stat(path)
			if err != nil {
				return time.Time{}, err
			}
			return st.ModTime(), nil
		},
		read: func(ctx context.Context, p browser.Profile) ([]browser.CookieMeta, error) {
			return browser.ReadSignInMetadata(ctx, p, browserSQLite)
		},
	}
}

// reset forgets every baseline, for a new or ended session.
func (f *cookieFollower) reset() {
	f.mu.Lock()
	f.lastMod, f.baseline = nil, nil
	f.retryAfter = time.Time{}
	f.mu.Unlock()
}

// snapshot reads profile p's cookie file time and the encrypted hash of its
// __Secure-1PSIDTS, without decrypting anything.
func (f *cookieFollower) snapshot(ctx context.Context, p browser.Profile) followSnapshot {
	mod, err := f.stat(p.CookieDB)
	if err != nil {
		return followSnapshot{}
	}
	metas, err := f.read(ctx, p)
	if err != nil {
		return followSnapshot{}
	}
	return followSnapshot{mod: mod, fp: followedFingerprint(metas)}
}

// setBaseline records, after OmaChat took its copy from profile p, what that
// copy looked like in the browser beforehand. Without a usable snapshot the
// profile's baseline is dropped, and the next check takes a new one.
func (f *cookieFollower) setBaseline(p browser.Profile, s followSnapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastMod == nil {
		f.lastMod = make(map[string]time.Time)
		f.baseline = make(map[string]string)
	}
	f.retryAfter = time.Time{}
	if s.fp == "" {
		delete(f.lastMod, p.Name)
		delete(f.baseline, p.Name)
		return
	}
	f.lastMod[p.Name] = s.mod
	f.baseline[p.Name] = s.fp
}

func followedFingerprint(metas []browser.CookieMeta) string {
	for _, m := range metas {
		if m.Name == followedCookie && m.Host == ".google.com" {
			return m.EncryptedHash
		}
	}
	return ""
}

// followsBrowser reports whether the current session takes its cookies from
// a browser: a confirmed Google account pairing. QR-code pairings carry no
// browser cookies.
func (d *Daemon) followsBrowser() bool {
	if !d.serviceEnabled(wire.NetworkGMessages) {
		return false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.paired && d.auth != nil && d.auth.IsGoogleAccount()
}

// followedProfiles lists the profiles to watch: the one OmaChat's cookies
// came from when it is known in this run, otherwise every candidate. A
// renewal in any of them leads to a read that only takes cookies with the
// session's own Google sign-in (refreshSameAccount).
func (d *Daemon) followedProfiles() []browser.Profile {
	candidates := d.candidateProfiles()
	d.cookies.mu.Lock()
	last := d.cookies.lastProfile
	d.cookies.mu.Unlock()
	for _, p := range candidates {
		if p.Name == last {
			return []browser.Profile{p}
		}
	}
	return candidates
}

// followBrowserCookies reads the browser's cookies again when the browser has
// renewed __Secure-1PSIDTS since OmaChat took its copy, so that copy is never
// more than one renewal behind. Every successful read records the baseline;
// a watched profile without one is read too.
func (d *Daemon) followBrowserCookies(ctx context.Context) {
	if ctx.Err() != nil || !d.followsBrowser() {
		return
	}
	f := d.follow
	f.mu.Lock()
	backingOff := time.Now().Before(f.retryAfter)
	f.mu.Unlock()
	if backingOff {
		return
	}
	renewed := false
	for _, p := range d.followedProfiles() {
		if d.profileRenewed(ctx, p) {
			renewed = true
		}
	}
	if !renewed {
		return
	}
	// A successful read records new baselines itself. Otherwise the old ones
	// stay, so a later check tries again: the next one after a throttled
	// read, or after cookieFollowRetry after a failed one.
	if d.refreshSameAccount(ctx, "browserRenewed") == refreshFailed {
		f.mu.Lock()
		f.retryAfter = time.Now().Add(cookieFollowRetry)
		f.mu.Unlock()
	}
}

// profileRenewed reports whether profile p needs a read: its
// __Secure-1PSIDTS differs from the baseline, or it has no baseline. It
// records the file time when nothing needs doing.
func (d *Daemon) profileRenewed(ctx context.Context, p browser.Profile) bool {
	f := d.follow
	mod, err := f.stat(p.CookieDB)
	if err != nil {
		return false
	}
	f.mu.Lock()
	if last, seen := f.lastMod[p.Name]; seen && last.Equal(mod) {
		f.mu.Unlock()
		return false
	}
	f.mu.Unlock()

	metas, err := f.read(ctx, p)
	if err != nil || ctx.Err() != nil {
		// The file time is not recorded, so the next check reads again.
		return false
	}
	fp := followedFingerprint(metas)

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastMod == nil {
		f.lastMod = make(map[string]time.Time)
		f.baseline = make(map[string]string)
	}
	base, hasBaseline := f.baseline[p.Name]
	if fp != "" && (!hasBaseline || fp != base) {
		// Renewed, or nothing is known about the copy OmaChat holds (the read
		// at startup or pairing did not record one): read to be sure, rather
		// than adopt the browser's current cookie as the baseline unread.
		// Leave the file time unrecorded until a read records a baseline, so
		// a throttled or failed read is tried again.
		return true
	}
	f.lastMod[p.Name] = mod
	return false
}
