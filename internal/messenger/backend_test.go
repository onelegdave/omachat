package messenger

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
	"github.com/rs/zerolog"
)

func TestBackend_Blockers(t *testing.T) {
	log := zerolog.Nop()

	// Create mock paths
	tmp, err := os.MkdirTemp("", "messenger-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	os.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	os.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	os.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmp, "run"))
	defer os.Unsetenv("XDG_DATA_HOME")
	defer os.Unsetenv("XDG_CACHE_HOME")
	defer os.Unsetenv("XDG_RUNTIME_DIR")

	paths, err := appStore.NewPaths()
	if err != nil {
		t.Fatal(err)
	}

	events := make(chan wire.Event, 10)
	publish := func(e wire.Event) {
		events <- e
	}

	b := New(log, paths, publish)
	if b.Status().State != wire.StateUnpaired {
		t.Errorf("expected unpaired state, got %v", b.Status().State)
	}

	ctx := context.Background()

	if err := b.Refresh(ctx); err == nil {
		t.Error("expected blocker error for Refresh")
	}

	if _, err := b.Messages(ctx, wire.MessagesParams{}); err == nil {
		t.Error("expected blocker error for Messages")
	}
}

func TestBackend_Unpair(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "messenger-test")
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	os.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	paths, _ := appStore.NewPaths()

	b := New(zerolog.Nop(), paths, func(wire.Event) {})
	b.setPaired(true)
	b.setState(wire.StateConnected, "")

	if err := b.Unpair(context.Background()); err != nil {
		t.Errorf("unpair failed: %v", err)
	}
	if b.Status().State != wire.StateUnpaired {
		t.Errorf("expected unpaired after Unpair, got %v", b.Status().State)
	}
}
