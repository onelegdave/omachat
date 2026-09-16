package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMediaEvictionPreservesInFlightAndJustCommitted(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"old.jpg", "new.jpg", ".download-in-flight"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("12345678"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	old := filepath.Join(dir, "old.jpg")
	if err := os.Chtimes(old, time.Unix(1, 0), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "new.jpg")
	if err := pruneMedia(dir, keep, 8); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old file not evicted")
	}
	for _, name := range []string{"new.jpg", ".download-in-flight"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
}
