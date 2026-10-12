package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A GIPHY key or notification preview setting saved by an earlier release
// must not survive on disk once the feature is gone, and the other settings
// around it must be untouched.
func TestNewConfigStoreForgetsRetiredKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"giphyApiKey":"secret","notificationPreviews":true,"uiScale":1.2,"telegramApiID":7}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cs := NewConfigStore(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"giphyApiKey", "notificationPreviews"} {
		if _, ok := raw[key]; ok {
			t.Fatalf("%s still present: %s", key, data)
		}
	}
	if raw["uiScale"] != 1.2 || raw["telegramApiID"] != float64(7) {
		t.Fatalf("other settings changed: %s", data)
	}
	if got := cs.Get(); got.UiScale != 1.2 || got.TelegramAPIID != 7 {
		t.Fatalf("loaded config lost settings: %+v", got)
	}

	// A clean file is left alone rather than rewritten.
	before, _ := os.Stat(path)
	NewConfigStore(path)
	after, _ := os.Stat(path)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("config rewritten although nothing was retired")
	}
}
