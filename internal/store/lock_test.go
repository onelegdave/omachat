package store

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDataLockSubprocess(t *testing.T) {
	if dir := os.Getenv("OMACHAT_LOCK_TEST_DIR"); dir != "" {
		lock, err := LockData(dir)
		if os.Getenv("OMACHAT_LOCK_EXPECT_BUSY") == "1" {
			if !errors.Is(err, ErrDataLocked) {
				os.Exit(2)
			}
			os.Exit(0)
		}
		if err != nil {
			os.Exit(3)
		}
		_ = lock // deliberately exit without Close: process death releases flock
		os.Exit(0)
	}
	dir := t.TempDir()
	lock, err := LockData(dir)
	if err != nil {
		t.Fatal(err)
	}
	run := func(busy string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestDataLockSubprocess$")
		cmd.Env = append(os.Environ(), "OMACHAT_LOCK_TEST_DIR="+dir, "OMACHAT_LOCK_EXPECT_BUSY="+busy)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child: %v %s", err, out)
		}
	}
	run("1")
	lock.Close()
	run("0")
	lock, err = LockData(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	info, err := os.Stat(filepath.Join(dir, "daemon.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions: %v", info.Mode())
	}
}

func TestDataLockRejectsSymlinkHardlinkAndNonregular(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(t.TempDir(), "target")
			if err := os.WriteFile(target, []byte("untouched"), 0o644); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "daemon.lock")
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(target, path)
			case "hardlink":
				err = os.Link(target, path)
			case "directory":
				err = os.Mkdir(path, 0o700)
			}
			if err != nil {
				t.Fatal(err)
			}
			lock, err := LockData(dir)
			if lock != nil {
				lock.Close()
			}
			if err == nil {
				t.Fatal("unsafe lock accepted")
			}
			got, err := os.ReadFile(target)
			if err != nil || string(got) != "untouched" {
				t.Fatalf("target changed: %s %v", got, err)
			}
			info, _ := os.Stat(target)
			if info.Mode().Perm() != 0o644 {
				t.Fatal("target mode changed")
			}
		})
	}
}

func TestDifferentDataDirectoriesCanLockIndependently(t *testing.T) {
	for i := 0; i < 2; i++ {
		lock, err := LockData(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
	}
}
