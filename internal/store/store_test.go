package store

import (
	"os"
	"testing"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

func TestIsPairedRejectsAbandonedPairing(t *testing.T) {
	// Starting a QR pairing mints a tachyon token before any phone accepts.
	// Treating that as paired makes the daemon boot into "not logged in".
	abandoned := &libgm.AuthData{TachyonAuthToken: []byte("tok")}
	if IsPaired(abandoned) {
		t.Error("a token without a browser identity must not count as paired")
	}

	complete := &libgm.AuthData{
		TachyonAuthToken: []byte("tok"),
		Browser:          &gmproto.Device{},
	}
	if !IsPaired(complete) {
		t.Error("token plus browser identity should count as paired")
	}

	if IsPaired(&libgm.AuthData{Browser: &gmproto.Device{}}) {
		t.Error("browser without a token must not count as paired")
	}
	if IsPaired(nil) {
		t.Error("nil auth is not paired")
	}
}

func TestSaveAndLoadSessionRoundTrip(t *testing.T) {
	p := &Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}

	// A missing file is a normal cold start, not an error.
	auth, paired, err := p.LoadSession()
	if err != nil || paired || auth == nil {
		t.Fatalf("cold start: auth=%v paired=%v err=%v", auth != nil, paired, err)
	}

	auth.TachyonAuthToken = []byte("tok")
	auth.Browser = &gmproto.Device{}
	if err := p.SaveSession(auth); err != nil {
		t.Fatal(err)
	}

	got, paired, err := p.LoadSession()
	if err != nil {
		t.Fatal(err)
	}
	if !paired {
		t.Error("expected a completed session to load as paired")
	}
	if string(got.TachyonAuthToken) != "tok" {
		t.Errorf("token did not round-trip: %q", got.TachyonAuthToken)
	}
}

func TestConfigStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.json"

	// A missing file is a normal first run, not an error.
	cs := NewConfigStore(path)
	if cs.Get().BrowserProfile != "" {
		t.Error("expected empty default (automatic selection)")
	}

	if err := cs.SetBrowserProfile("Chrome / Profile 1"); err != nil {
		t.Fatal(err)
	}
	if got := cs.Get().BrowserProfile; got != "Chrome / Profile 1" {
		t.Errorf("in-memory value = %q", got)
	}

	// The choice must survive a daemon restart, or the background cookie sync
	// would silently revert to guessing.
	reopened := NewConfigStore(path)
	if got := reopened.Get().BrowserProfile; got != "Chrome / Profile 1" {
		t.Errorf("persisted value = %q, want the selected profile", got)
	}

	// Clearing restores automatic selection.
	if err := reopened.SetBrowserProfile(""); err != nil {
		t.Fatal(err)
	}
	if NewConfigStore(path).Get().BrowserProfile != "" {
		t.Error("clearing should restore automatic selection")
	}
}

func TestConfigStoreSurvivesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.json"
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A damaged config must never stop the daemon starting.
	if got := NewConfigStore(path).Get().BrowserProfile; got != "" {
		t.Errorf("corrupt config should fall back to defaults, got %q", got)
	}
}

func TestStorageIsolation(t *testing.T) {
	dataDir := t.TempDir()
	cacheDir := t.TempDir()
	runtimeDir := t.TempDir()

	p := &Paths{Data: dataDir, Cache: cacheDir, Runtime: runtimeDir}
	if err := os.MkdirAll(p.MediaDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.WhatsAppMediaDir(), 0o700); err != nil {
		t.Fatal(err)
	}

	// Create Google files
	if err := os.WriteFile(p.SessionFile(), []byte("google-session"), 0o600); err != nil {
		t.Fatal(err)
	}
	googleMedia := p.MediaDir() + "/image.png"
	if err := os.WriteFile(googleMedia, []byte("google-media"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create WhatsApp files
	if err := os.WriteFile(p.WhatsAppDBFile(), []byte("whatsapp-db"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.WhatsAppDBFile()+"-wal", []byte("whatsapp-wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.WhatsAppStoreFile(), []byte("whatsapp-store"), 0o600); err != nil {
		t.Fatal(err)
	}
	waMedia := p.WhatsAppMediaDir() + "/photo.png"
	if err := os.WriteFile(waMedia, []byte("whatsapp-media"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Clearing Google session must NOT touch WhatsApp files
	if err := p.ClearSession(); err != nil {
		t.Fatalf("ClearSession: %v", err)
	}
	if _, err := os.Stat(p.SessionFile()); !os.IsNotExist(err) {
		t.Errorf("expected Google session file to be deleted, got %v", err)
	}
	if _, err := os.Stat(googleMedia); !os.IsNotExist(err) {
		t.Errorf("expected Google media file to be deleted, got %v", err)
	}
	if b, err := os.ReadFile(p.WhatsAppDBFile()); err != nil || string(b) != "whatsapp-db" {
		t.Errorf("WhatsApp DB file was modified or deleted by ClearSession: %v", err)
	}
	if b, err := os.ReadFile(p.WhatsAppDBFile() + "-wal"); err != nil || string(b) != "whatsapp-wal" {
		t.Errorf("WhatsApp WAL file was modified or deleted by ClearSession: %v", err)
	}
	if b, err := os.ReadFile(p.WhatsAppStoreFile()); err != nil || string(b) != "whatsapp-store" {
		t.Errorf("WhatsApp store file was modified or deleted by ClearSession: %v", err)
	}
	if b, err := os.ReadFile(waMedia); err != nil || string(b) != "whatsapp-media" {
		t.Errorf("WhatsApp media file was modified or deleted by ClearSession: %v", err)
	}

	// Now re-create Google files and clear WhatsApp session
	if err := os.WriteFile(p.SessionFile(), []byte("google-session-2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.ClearWhatsAppSession(); err != nil {
		t.Fatalf("ClearWhatsAppSession: %v", err)
	}
	if _, err := os.Stat(p.WhatsAppDBFile()); !os.IsNotExist(err) {
		t.Errorf("expected WhatsApp DB file to be deleted, got %v", err)
	}
	if _, err := os.Stat(p.WhatsAppDBFile() + "-wal"); !os.IsNotExist(err) {
		t.Errorf("expected WhatsApp WAL file to be deleted, got %v", err)
	}
	if _, err := os.Stat(p.WhatsAppStoreFile()); !os.IsNotExist(err) {
		t.Errorf("expected WhatsApp store file to be deleted, got %v", err)
	}
	if _, err := os.Stat(waMedia); !os.IsNotExist(err) {
		t.Errorf("expected WhatsApp media file to be deleted, got %v", err)
	}
	if b, err := os.ReadFile(p.SessionFile()); err != nil || string(b) != "google-session-2" {
		t.Errorf("Google session file was modified or deleted by ClearWhatsAppSession: %v", err)
	}
}
