package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/daemon"
	"github.com/onelegdave/omachat/internal/store"
	"github.com/rs/zerolog"
)

type fakeHelper struct {
	start func() error
	serve func(string) error
	stop  func()
}

func (f *fakeHelper) Start(context.Context) error                { return f.start() }
func (f *fakeHelper) Serve(_ context.Context, path string) error { return f.serve(path) }
func (f *fakeHelper) Stop()                                      { f.stop() }
func (f *fakeHelper) RestartRequested() <-chan struct{}          { return nil }

func waitFor(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("fake helper did not reach checkpoint")
	}
}

func TestLockBeforeConstructionThroughSlowStartAndStop(t *testing.T) {
	paths := &store.Paths{Data: t.TempDir()}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(paths.Data, alias); err != nil {
		t.Fatal(err)
	}
	starting, resume, stopping, finish := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	// Always release blocked goroutines, including when a checkpoint fails.
	var resumeOnce, finishOnce sync.Once
	defer resumeOnce.Do(func() { close(resume) })
	defer finishOnce.Do(func() { close(finish) })
	done := make(chan error, 1)
	go func() {
		done <- runLocked(context.Background(), zerolog.Nop(), paths, "first.sock", func() helper {
			return &fakeHelper{
				start: func() error { close(starting); <-resume; return nil },
				serve: func(string) error { return nil },
				stop:  func() { close(stopping); <-finish },
			}
		})
	}()
	waitFor(t, starting)
	var opened atomic.Int32
	contend := func() {
		err := runLocked(context.Background(), zerolog.Nop(), &store.Paths{Data: alias}, "different.sock", func() helper {
			opened.Add(1)
			return &fakeHelper{start: func() error { return nil }, serve: func(string) error { return nil }, stop: func() {}}
		})
		if !errors.Is(err, store.ErrDataLocked) {
			t.Errorf("contender: %v", err)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); contend() }()
	}
	wg.Wait()
	resumeOnce.Do(func() { close(resume) })
	waitFor(t, stopping)
	contend() // shutdown still owns the stores
	finishOnce.Do(func() { close(finish) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if opened.Load() != 0 {
		t.Fatalf("losers opened %d stores", opened.Load())
	}
	lock, err := store.LockData(alias)
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
}

func TestSimultaneousStartsOnlyOneConstructsStores(t *testing.T) {
	paths := &store.Paths{Data: t.TempDir()}
	gate, release := make(chan struct{}), make(chan struct{})
	results := make(chan error, 16)
	var opened atomic.Int32
	for i := 0; i < cap(results); i++ {
		go func() {
			<-gate
			results <- runLocked(context.Background(), zerolog.Nop(), paths, "unused.sock", func() helper {
				opened.Add(1)
				return &fakeHelper{start: func() error { <-release; return nil }, serve: func(string) error { return nil }, stop: func() {}}
			})
		}()
	}
	close(gate)
	var once sync.Once
	defer once.Do(func() { close(release) })
	for i := 0; i < cap(results)-1; i++ {
		select {
		case err := <-results:
			if !errors.Is(err, store.ErrDataLocked) {
				t.Errorf("loser: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("contenders did not fail promptly")
		}
	}
	if opened.Load() != 1 {
		t.Fatalf("opened %d stores", opened.Load())
	}
	once.Do(func() { close(release) })
	if err := <-results; err != nil {
		t.Fatal(err)
	}
}

// Real socket integration, fake stores. Relay runs this outside the socket-
// restricted sandbox. No skip on EPERM: a blocked run is not socket proof.
func TestStaleSocketDoesNotOwnDataAndIsRecovered(t *testing.T) {
	paths := &store.Paths{Data: t.TempDir(), Runtime: t.TempDir()}
	socket := paths.SocketPath()
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	opened := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = runLocked(ctx, zerolog.Nop(), paths, socket, func() helper {
		opened++
		return &fakeHelper{
			start: func() error { return nil },
			serve: func(path string) error {
				cancel()
				return (&daemon.Daemon{}).Serve(ctx, path)
			},
			stop: func() {},
		}
	})
	if err != nil || opened != 1 {
		t.Fatalf("opened=%d err=%v", opened, err)
	}
	if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket remains: %v", err)
	}
	lock, err := store.LockData(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
}

func TestStartOrServeFailureStopsBeforeUnlock(t *testing.T) {
	for _, failure := range []string{"start", "serve"} {
		t.Run(failure, func(t *testing.T) {
			paths := &store.Paths{Data: t.TempDir()}
			want := errors.New("synthetic startup failure")
			stopped := false
			err := runLocked(context.Background(), zerolog.Nop(), paths, "unused.sock", func() helper {
				return &fakeHelper{
					start: func() error {
						if failure == "start" {
							return want
						}
						return nil
					},
					serve: func(string) error { return want },
					stop: func() {
						stopped = true
						lock, err := store.LockData(paths.Data)
						if lock != nil {
							lock.Close()
						}
						if !errors.Is(err, store.ErrDataLocked) {
							t.Errorf("unlocked before Stop: %v", err)
						}
					},
				}
			})
			if !errors.Is(err, want) || !stopped {
				t.Fatalf("error=%v stopped=%v", err, stopped)
			}
			lock, err := store.LockData(paths.Data)
			if err != nil {
				t.Fatal(err)
			}
			lock.Close()
		})
	}
}
