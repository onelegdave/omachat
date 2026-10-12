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

func TestStaleSocketNeverDeletesRegularFileOrSymlink(t *testing.T) {
	for _, link := range []bool{false, true} {
		dir := t.TempDir()
		path := filepath.Join(dir, "daemon.sock")
		target := filepath.Join(dir, "keep")
		if err := os.WriteFile(target, []byte("synthetic store"), 0o600); err != nil {
			t.Fatal(err)
		}
		if link {
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.WriteFile(path, []byte("synthetic store"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := removeStaleSocket(path); err == nil {
			t.Fatal("accepted a non-socket")
		}
		if _, err := os.Lstat(path); err != nil {
			t.Fatal("removed a non-socket")
		}
		got, err := os.ReadFile(target)
		if err != nil || string(got) != "synthetic store" {
			t.Fatal("changed target")
		}
	}
}
