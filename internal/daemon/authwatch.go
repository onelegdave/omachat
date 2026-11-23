package daemon

import (
	"context"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/onelegdave/omachat/internal/browser"
	"github.com/onelegdave/omachat/internal/wire"
)

const (
	cookieWatchInterval   = 3 * time.Minute
	cookieWatchFirstDelay = 15 * time.Second
)

// cookieWatcher records when the browser renews its Google sign-in cookies.
// It runs only while diagnostics are on and Google Messages is enabled and
// paired. It reads metadata and a hash of each still-encrypted value through
// sqlite3; it never decrypts, never uses the keyring, never copies the
// database, and never hands anything to the daemon.
type cookieWatcher struct {
	interval time.Duration
	stat     func(path string) (time.Time, error)
	profiles func() []browser.Profile
	read     func(ctx context.Context, p browser.Profile) ([]browser.CookieMeta, error)

	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	lastMod map[string]time.Time
	lastSet map[string]map[string]browser.CookieMeta
}

func newCookieWatcher() *cookieWatcher {
	return &cookieWatcher{
		interval: cookieWatchInterval,
		stat: func(path string) (time.Time, error) {
			st, err := os.Stat(path)
			if err != nil {
				return time.Time{}, err
			}
			return st.ModTime(), nil
		},
	}
}

func (w *cookieWatcher) running() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cancel != nil
}

func (w *cookieWatcher) start(parent context.Context, d *Daemon) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	w.cancel, w.done = cancel, done
	interval := w.interval
	go func() {
		defer close(done)
		// The helper switches diagnostics on before it has loaded its
		// session, so take the first reading shortly after, not at once.
		first := cookieWatchFirstDelay
		if interval < first {
			first = interval
		}
		if !waitContext(ctx, first) {
			return
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			w.tick(ctx, d)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// stop cancels the watcher and waits for it. Callers must not hold the log's
// lock, which a tick in progress may need to finish.
func (w *cookieWatcher) stop() {
	w.mu.Lock()
	cancel, done := w.cancel, w.done
	w.cancel, w.done = nil, nil
	w.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func (w *cookieWatcher) resetBaseline() {
	w.mu.Lock()
	w.lastMod = nil
	w.lastSet = nil
	w.mu.Unlock()
}

func (w *cookieWatcher) pickProfile(d *Daemon) (browser.Profile, bool) {
	discover := w.profiles
	if discover == nil {
		discover = d.diag.discover
	}
	all := discover()
	if len(all) == 0 {
		return browser.Profile{}, false
	}
	want := d.config.Get().BrowserProfile
	if want == "" {
		d.cookies.mu.Lock()
		want = d.cookies.lastProfile
		d.cookies.mu.Unlock()
	}
	for _, p := range all {
		if p.Name == want {
			return p, true
		}
	}
	return all[0], true
}

func (w *cookieWatcher) tick(ctx context.Context, d *Daemon) {
	if ctx.Err() != nil || !d.diagOn() || !d.serviceEnabled(wire.NetworkGMessages) {
		return
	}
	d.mu.RLock()
	paired := d.paired
	d.mu.RUnlock()
	if !paired {
		return
	}
	p, ok := w.pickProfile(d)
	if !ok {
		d.diagWriteLimited("browserWatch:noProfile", 1, time.Hour, "browserWatchError", logFields{"err": "no browser profile found"})
		return
	}
	mod, err := w.stat(p.CookieDB)
	if err != nil {
		d.diagWriteLimited("browserWatch:stat:"+p.Name, 1, time.Hour, "browserWatchError", logFields{"profile": p.Name, "err": err.Error()})
		return
	}
	w.mu.Lock()
	if last, seen := w.lastMod[p.Name]; seen && last.Equal(mod) {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	read := w.read
	if read == nil {
		read = func(ctx context.Context, p browser.Profile) ([]browser.CookieMeta, error) {
			return browser.ReadSignInMetadata(ctx, p, d.diag.sqlite)
		}
	}
	metas, err := read(ctx, p)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		// The modification time is not recorded, so the next tick retries.
		d.diagWriteLimited("browserWatch:read:"+p.Name, 1, time.Hour, "browserWatchError", logFields{"profile": p.Name, "err": err.Error()})
		return
	}

	current := make(map[string]browser.CookieMeta, len(metas))
	for _, m := range metas {
		current[m.Name+"|"+m.Host] = m
	}
	w.mu.Lock()
	if w.lastMod == nil {
		w.lastMod = make(map[string]time.Time)
		w.lastSet = make(map[string]map[string]browser.CookieMeta)
	}
	previous, hadBaseline := w.lastSet[p.Name]
	w.lastMod[p.Name] = mod
	w.lastSet[p.Name] = current
	w.mu.Unlock()

	var changed []browser.CookieMeta
	var removed []string
	for key, m := range current {
		if old, ok := previous[key]; !ok || old.EncryptedHash != m.EncryptedHash || !old.Expires.Equal(m.Expires) {
			changed = append(changed, m)
		}
	}
	for key, old := range previous {
		if _, ok := current[key]; !ok {
			removed = append(removed, old.Name+" ("+old.Host+")")
		}
	}
	if hadBaseline && len(changed) == 0 && len(removed) == 0 {
		return
	}
	sort.Slice(changed, func(i, j int) bool {
		if changed[i].Name != changed[j].Name {
			return changed[i].Name < changed[j].Name
		}
		return changed[i].Host < changed[j].Host
	})
	sort.Strings(removed)
	d.diagWrite("browserCookies", logFields{
		"profile":      p.Name,
		"baseline":     !hadBaseline,
		"fileModified": diagTime(mod),
		"changed":      d.diagCookieMetas(changed),
		"removed":      removed,
		"cookiesTotal": len(current),
	})
}
