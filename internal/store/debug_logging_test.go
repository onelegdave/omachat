package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDebugLoggingDefaultsOff(t *testing.T) {
	dir := t.TempDir()
	if NewConfigStore(filepath.Join(dir, "missing.json")).Get().DebugLogging {
		t.Fatal("a missing config must leave diagnostics off")
	}
	old := filepath.Join(dir, "old.json")
	if err := os.WriteFile(old, []byte(`{"browserProfile":"Chrome / Default","unifiedInboxEnabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if NewConfigStore(old).Get().DebugLogging {
		t.Fatal("a config written before the setting existed must leave diagnostics off")
	}
}

func TestDebugLoggingPersistsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := NewConfigStore(path)
	if err := cfg.SetDebugLogging(true); err != nil {
		t.Fatal(err)
	}
	if !cfg.Get().DebugLogging || !NewConfigStore(path).Get().DebugLogging {
		t.Fatal("enabled setting did not persist")
	}
	var raw map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &raw); err != nil || raw["debugLogging"] != true {
		t.Fatalf("config file = %s", data)
	}
	if err := cfg.SetDebugLogging(false); err != nil {
		t.Fatal(err)
	}
	if cfg.Get().DebugLogging || NewConfigStore(path).Get().DebugLogging {
		t.Fatal("disabled setting did not persist")
	}
}

func TestDebugLogFileRequiresAbsoluteState(t *testing.T) {
	for _, p := range []*Paths{nil, {}, {Data: "/tmp/x"}, {State: "relative/state"}} {
		if path, err := p.DebugLogFile(); err == nil {
			t.Fatalf("DebugLogFile(%+v) = %q, want an error", p, path)
		}
	}
	path, err := (&Paths{State: "/abs/state/omachat"}).DebugLogFile()
	if err != nil || path != "/abs/state/omachat/auth.log" {
		t.Fatalf("DebugLogFile = %q, %v", path, err)
	}
}

func TestNewPathsResolvesButDoesNotCreateStateDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "run"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	p, err := NewPaths()
	if err != nil {
		t.Fatal(err)
	}
	if p.State != filepath.Join(root, "state", "omachat") {
		t.Fatalf("State = %q", p.State)
	}
	if _, err := os.Stat(p.State); !os.IsNotExist(err) {
		t.Fatalf("state folder must not exist until diagnostics are switched on: %v", err)
	}
}
