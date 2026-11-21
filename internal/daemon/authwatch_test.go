package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/browser"
	"github.com/onelegdave/omachat/internal/wire"
)

type fakeBrowser struct {
	mu    sync.Mutex
	mod   time.Time
	metas []browser.CookieMeta
	err   error
	reads int
}

func (f *fakeBrowser) stat(string) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mod, nil
}

func (f *fakeBrowser) read(context.Context, browser.Profile) ([]browser.CookieMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	return append([]browser.CookieMeta(nil), f.metas...), f.err
}

func (f *fakeBrowser) set(mod time.Time, metas []browser.CookieMeta, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mod, f.metas, f.err = mod, metas, err
}

func (f *fakeBrowser) readCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

func newWatchDaemon(t *testing.T) (*Daemon, *fakeBrowser, string) {
	t.Helper()
	d, _, logPath := newDiagDaemon(t)
	fb := &fakeBrowser{mod: time.Unix(100, 0), metas: []browser.CookieMeta{
		{Name: "SID", Host: ".google.com", EncryptedHash: "h-sid-1", EncryptedLen: 40},
		{Name: "__Secure-1PSIDTS", Host: ".google.com", EncryptedHash: "h-ts-1", EncryptedLen: 90},
	}}
	w := d.diag.watcher
	w.interval = time.Hour // tests drive tick directly
	w.stat = fb.stat
	w.read = fb.read
	w.profiles = func() []browser.Profile {
		return []browser.Profile{{Name: "Chrome / Profile 1", CookieDB: "/p1/Cookies"}, {Name: "Chrome / Default", CookieDB: "/d/Cookies"}}
	}
	return d, fb, logPath
}

func setPaired(d *Daemon, paired bool) {
	d.mu.Lock()
	d.paired = paired
	d.mu.Unlock()
}

func TestWatcherReadsOnlyWhileOnEnabledAndPaired(t *testing.T) {
	d, fb, _ := newWatchDaemon(t)
	ctx := context.Background()
	w := d.diag.watcher

	setPaired(d, true)
	w.tick(ctx, d)
	if fb.readCount() != 0 {
		t.Fatal("watcher read the browser while diagnostics were off")
	}
	setPaired(d, false) // keep the watcher's own first tick from reading
	enableDiag(t, d)
	w.stop() // drive ticks by hand from here
	w.tick(ctx, d)
	if fb.readCount() != 0 {
		t.Fatal("watcher read the browser while unpaired")
	}
	setPaired(d, true)
	d.servicesMu.Lock()
	d.activeServices = map[string]bool{wire.NetworkWhatsApp: true}
	d.servicesMu.Unlock()
	w.tick(ctx, d)
	if fb.readCount() != 0 {
		t.Fatal("watcher read the browser with Google Messages disabled")
	}
	d.servicesMu.Lock()
	d.activeServices = map[string]bool{wire.NetworkGMessages: true}
	d.servicesMu.Unlock()
	w.tick(ctx, d)
	if fb.readCount() != 1 {
		t.Fatalf("reads = %d, want 1", fb.readCount())
	}
}

func TestWatcherLogsBaselineThenOnlyChanges(t *testing.T) {
	d, fb, logPath := newWatchDaemon(t)
	ctx := context.Background()
	w := d.diag.watcher
	enableDiag(t, d) // unpaired, so the watcher's own first tick reads nothing
	w.stop()
	setPaired(d, true)

	w.tick(ctx, d) // baseline
	w.tick(ctx, d) // same modification time: no read
	if fb.readCount() != 1 {
		t.Fatalf("reads = %d, want 1 (unchanged file must be skipped)", fb.readCount())
	}
	fb.set(time.Unix(200, 0), fb.metas, nil) // file touched, cookies unchanged
	w.tick(ctx, d)
	fb.set(time.Unix(300, 0), []browser.CookieMeta{
		{Name: "SID", Host: ".google.com", EncryptedHash: "h-sid-1", EncryptedLen: 40},
		{Name: "__Secure-1PSIDTS", Host: ".google.com", EncryptedHash: "h-ts-2", EncryptedLen: 90, LastUpdate: time.Unix(300, 0)},
		{Name: "SIDCC", Host: ".google.com", EncryptedHash: "h-cc-1", EncryptedLen: 70},
	}, nil)
	w.tick(ctx, d)

	entries := entriesOfKind(readLogEntries(t, logPath), "browserCookies")
	if len(entries) != 2 {
		t.Fatalf("browserCookies entries = %d, want baseline + one change: %v", len(entries), entries)
	}
	if entries[0]["baseline"] != true || entries[0]["profile"] != "Chrome / Profile 1" || len(entries[0]["changed"].([]any)) != 2 {
		t.Fatalf("baseline = %v", entries[0])
	}
	changed := entries[1]["changed"].([]any)
	var names []string
	for _, c := range changed {
		m := c.(map[string]any)
		names = append(names, m["name"].(string))
		if fp, _ := m["encryptedFP"].(string); fp == "" || strings.HasPrefix(fp, "h-") {
			t.Fatalf("encrypted hash logged without a salted fingerprint: %v", m)
		}
	}
	if entries[1]["baseline"] != false || fmt.Sprint(names) != "[SIDCC __Secure-1PSIDTS]" {
		t.Fatalf("change entry = %v", entries[1])
	}
}

func TestWatcherPrefersSelectedThenLastUsedProfile(t *testing.T) {
	d, _, _ := newWatchDaemon(t)
	if p, _ := d.diag.watcher.pickProfile(d); p.Name != "Chrome / Profile 1" {
		t.Fatalf("automatic default = %s", p.Name)
	}
	d.cookies.mu.Lock()
	d.cookies.lastProfile = "Chrome / Default"
	d.cookies.mu.Unlock()
	if p, _ := d.diag.watcher.pickProfile(d); p.Name != "Chrome / Default" {
		t.Fatalf("last-used profile = %s", p.Name)
	}
	if err := d.config.SetBrowserProfile("Chrome / Profile 1"); err != nil {
		t.Fatal(err)
	}
	if p, _ := d.diag.watcher.pickProfile(d); p.Name != "Chrome / Profile 1" {
		t.Fatalf("selected profile = %s", p.Name)
	}
}

func TestWatcherReadErrorsAreRateLimitedAndRetried(t *testing.T) {
	d, fb, logPath := newWatchDaemon(t)
	ctx := context.Background()
	w := d.diag.watcher
	enableDiag(t, d)
	w.stop()
	setPaired(d, true)
	fb.set(time.Unix(100, 0), nil, errors.New("database is locked"))
	w.tick(ctx, d)
	w.tick(ctx, d)
	if fb.readCount() != 2 {
		t.Fatal("a failed read must be retried on the next tick")
	}
	if errs := entriesOfKind(readLogEntries(t, logPath), "browserWatchError"); len(errs) != 1 {
		t.Fatalf("read errors = %d, want 1 per hour", len(errs))
	}
}

func TestWatcherStartsAndStopsWithTheSetting(t *testing.T) {
	d, _, _ := newWatchDaemon(t)
	if d.diag.watcher.running() {
		t.Fatal("watcher running before diagnostics were switched on")
	}
	enableDiag(t, d)
	if !d.diag.watcher.running() {
		t.Fatal("watcher did not start")
	}
	done := make(chan struct{})
	go func() {
		_ = d.applyDebugLogging(false, "")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("switching off did not stop the watcher promptly")
	}
	if d.diag.watcher.running() {
		t.Fatal("watcher still running after switching off")
	}
}

// Without a read override the watcher must go through the daemon's sqlite
// runner with an immutable, read-only database URI.
func TestWatcherUsesReadOnlySQLiteRunner(t *testing.T) {
	d, _, _ := newWatchDaemon(t)
	d.diag.watcher.read = nil
	var uris []string
	d.diag.sqlite = func(_ context.Context, uri, query string) ([]byte, error) {
		uris = append(uris, uri)
		if strings.Contains(query, "pragma_table_info") {
			return []byte("name\nhost_key\nencrypted_value\n"), nil
		}
		return nil, nil
	}
	enableDiag(t, d)
	d.diag.watcher.stop()
	setPaired(d, true)
	d.diag.watcher.tick(context.Background(), d)
	if len(uris) != 2 {
		t.Fatalf("sqlite calls = %d", len(uris))
	}
	for _, uri := range uris {
		if uri != "file:///p1/Cookies?mode=ro&immutable=1" {
			t.Fatalf("uri = %s", uri)
		}
	}
}
