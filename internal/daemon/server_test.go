package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

func TestServeChmodFailure(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	originalChmod := osChmod
	osChmod = func(name string, mode os.FileMode) error {
		return errors.New("mock chmod error")
	}
	defer func() { osChmod = originalChmod }()

	d := &Daemon{log: zerolog.Nop()}
	err := d.Serve(context.Background(), socketPath)
	if err == nil {
		t.Fatal("expected error from Serve due to chmod failure, got nil")
	}
	if err.Error() != "secure socket: mock chmod error" {
		t.Fatalf("unexpected error message: %v", err)
	}

	// verify socket file was removed
	if _, err := os.Stat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket file should have been removed, stat err: %v", err)
	}
}
