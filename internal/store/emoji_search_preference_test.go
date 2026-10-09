package store

import (
	"encoding/json"
	"os"
	"testing"
)

func TestEmojiSearchPreferenceLoadsFromConfig(t *testing.T) {
	path := (&Paths{Data: t.TempDir()}).ConfigFile()
	if err := os.WriteFile(path, []byte(`{"keepPreviousEmojiSearchText":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	config := NewConfigStore(path)
	data, err := json.Marshal(config.Get())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["keepPreviousEmojiSearchText"] != true {
		t.Fatalf("emoji search preference was not loaded: %#v", got)
	}

	config = NewConfigStore(path)
	if err := config.SetKeepPreviousEmojiSearchText(false); err != nil {
		t.Fatal(err)
	}
	if got := NewConfigStore(path).Get().KeepPreviousEmojiSearchText; got {
		t.Fatalf("disabled emoji search preference was not persisted: %v", got)
	}
	persisted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persistedConfig map[string]any
	if err := json.Unmarshal(persisted, &persistedConfig); err != nil {
		t.Fatal(err)
	}
	if got, ok := persistedConfig["keepPreviousEmojiSearchText"]; !ok || got != false {
		t.Fatalf("false preference was not written explicitly: %#v", persistedConfig)
	}
}

func TestEmojiSearchPreferenceDefaultsToFalse(t *testing.T) {
	config := NewConfigStore((&Paths{Data: t.TempDir()}).ConfigFile())
	if config.Get().KeepPreviousEmojiSearchText {
		t.Fatal("emoji search preference should default to false")
	}
}
