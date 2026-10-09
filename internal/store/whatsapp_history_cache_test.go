package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWhatsAppHistoryCachePreferenceDefaultsAndPersistsAllowedSizes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"browserProfile":"Profile 1","uiScale":1.2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config := NewConfigStore(path)
	if got := config.Get().WhatsAppHistoryCacheMB; got != 128 {
		t.Fatalf("default history cache = %d MB, want 128 MB", got)
	}
	for _, size := range []int{64, 128, 256, 512} {
		if err := config.SetWhatsAppHistoryCacheMB(size); err != nil {
			t.Fatalf("set %d MB: %v", size, err)
		}
		reopened := NewConfigStore(path)
		if got := reopened.Get().WhatsAppHistoryCacheMB; got != size {
			t.Fatalf("reopened history cache = %d MB, want %d MB", got, size)
		}
	}
	if got := NewConfigStore(path).Get(); got.BrowserProfile != "Profile 1" || got.UiScale != 1.2 {
		t.Fatalf("saving history cache changed unrelated settings: %+v", got)
	}
}

func TestWhatsAppHistoryCachePreferenceRejectsUnsupportedSize(t *testing.T) {
	config := NewConfigStore(filepath.Join(t.TempDir(), "config.json"))
	if err := config.SetWhatsAppHistoryCacheMB(96); err == nil {
		t.Fatal("setting unsupported history cache size succeeded")
	}
	if got := config.Get().WhatsAppHistoryCacheMB; got != 128 {
		t.Fatalf("rejected setting changed preference to %d MB", got)
	}
}
