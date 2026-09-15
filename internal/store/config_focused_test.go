package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConfigStore_StaleDaemonSetter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Daemon loads empty
	daemon := NewConfigStore(path)

	// External script writes credentials
	initial := map[string]any{
		"telegramApiID":   123,
		"telegramApiHash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	b, _ := json.Marshal(initial)
	os.WriteFile(path, b, 0600)

	// Stale daemon writes something else
	if err := daemon.SetBrowserProfile("Profile 1"); err != nil {
		t.Fatal(err)
	}

	// Verify both exist
	b, _ = os.ReadFile(path)
	var merged map[string]any
	json.Unmarshal(b, &merged)
	if merged["telegramApiID"].(float64) != 123 || merged["browserProfile"].(string) != "Profile 1" {
		t.Errorf("merged incorrectly: %v", merged)
	}
	if daemon.Get().TelegramAPIID != 123 {
		t.Errorf("daemon memory not updated after its own write: %+v", daemon.Get())
	}
}

func TestConfigStore_ReverseOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// External creates empty
	os.WriteFile(path, []byte("{}"), 0600)

	// Daemon writes
	daemon := NewConfigStore(path)
	daemon.SetBrowserProfile("Profile 1")

	// External writes credentials via another ConfigStore instance
	external := NewConfigStore(path)
	external.SetTelegramCredentials(456, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	// Check final disk
	b, _ := os.ReadFile(path)
	var merged map[string]any
	json.Unmarshal(b, &merged)
	if merged["telegramApiID"].(float64) != 456 || merged["browserProfile"].(string) != "Profile 1" {
		t.Errorf("merged incorrectly: %v", merged)
	}
}

func TestConfigStore_ConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := NewConfigStore(path)
			for n := 0; n < 10; n++ {
				var err error
				if i == 0 {
					err = c.SetBrowserProfile("Profile 1")
				} else {
					err = c.SetUiScale(1.2)
				}
				if err != nil {
					t.Errorf("concurrent write failed: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	got := NewConfigStore(path).Get()
	if got.BrowserProfile != "Profile 1" || got.UiScale != 1.2 {
		t.Fatal("concurrent writers lost independent settings")
	}
}

func TestConfigStore_FailedWriteDoesNotMutateMemory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	c := NewConfigStore(path)
	// Make directory read-only to fail the write
	os.Chmod(dir, 0500)
	defer os.Chmod(dir, 0700)

	err := c.SetBrowserProfile("Profile 1")
	if err == nil {
		t.Fatal("expected write to fail")
	}

	if c.Get().BrowserProfile == "Profile 1" {
		t.Error("memory mutated on failed write")
	}
}
