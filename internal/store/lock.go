package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrDataLocked means another helper owns this data directory, even if it has
// not reached Listen yet or uses a different runtime directory/socket override.
var ErrDataLocked = errors.New("another omachatd owns the data directory")

// LockData takes a process-lifetime, nonblocking exclusive lock. Call it before
// constructing a daemon: even constructors may read or migrate backend stores.
// The file is never removed. Unlinking it would let a third process lock a new
// inode while a contender still holds the old one. The kernel releases the lock
// on close or process death; a leftover lock file is not a stale lock.
func LockData(data string) (*os.File, error) {
	canonical, err := filepath.EvalSymlinks(data)
	if err != nil {
		return nil, fmt.Errorf("resolve data directory: %w", err)
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return nil, fmt.Errorf("resolve data directory: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(canonical, "daemon.lock"),
		os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open data lock: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			f.Close()
		}
	}()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat data lock: %w", err)
	}
	stat, valid := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !valid || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("data lock must be an owner-owned regular file with one link")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrDataLocked
		}
		return nil, fmt.Errorf("lock data directory: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		return nil, fmt.Errorf("secure data lock: %w", err)
	}
	ok = true
	return f, nil
}
