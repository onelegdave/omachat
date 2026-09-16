package telegram

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

func TestStoppedClientCannotRestoreSessionFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telegram.session")
	g := NewGotdClient(1, "hash", path, zerolog.Nop())
	if err := g.sessionStore.StoreSession(context.Background(), []byte("fixture")); err != nil {
		t.Fatal(err)
	}
	if err := g.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := g.sessionStore.StoreSession(context.Background(), []byte("late")); !errors.Is(err, context.Canceled) {
		t.Fatalf("late session save: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("credential file restored: %v", err)
	}
}
