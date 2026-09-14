package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"

	"github.com/onelegdave/omachat/internal/wire"
)

// TestUnpairRemoteFailure verifies that when remote revocation fails, d.unpair
// returns an error preserving the remote error, wipes local credentials and cache,
// ensures saveSession() and Stop() cannot recreate credentials, and updates status
// to StateUnpaired advising that local cleanup succeeded and OmaChat must be
// removed in Google Messages Device pairing.
func TestUnpairRemoteFailure(t *testing.T) {
	d, paths := newTestDaemon(t)

	auth := syntheticTestAuth()
	if err := paths.SaveSession(auth); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	d.mu.Lock()
	d.paired = true
	d.auth = auth
	d.mu.Unlock()

	dummyMedia := filepath.Join(paths.MediaDir(), "test_attachment.png")
	if err := os.WriteFile(dummyMedia, []byte("test attachment content"), 0o600); err != nil {
		t.Fatalf("WriteFile dummy media: %v", err)
	}

	if _, err := os.Stat(paths.SessionFile()); err != nil {
		t.Fatalf("session file must exist before unpair: %v", err)
	}

	oldClient := d.client
	remoteErr := errors.New("simulated remote network revocation failure")
	unpairCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := d.unpair(unpairCtx, func(ctx context.Context, c *libgm.Client) error {
		if c != oldClient || ctx.Err() != nil {
			t.Error("revocation did not receive the old client and usable request context")
		}
		if _, err := os.Stat(paths.SessionFile()); !os.IsNotExist(err) {
			t.Error("local credentials must be cleared before remote revocation")
		}
		return remoteErr
	})
	if err == nil {
		t.Fatalf("expected error from unpair on remote failure, got nil")
	}
	if !errors.Is(err, remoteErr) {
		t.Fatalf("expected errors.Is(err, remoteErr) to be true, got: %v", err)
	}

	// Local session and media cache must be cleared
	if _, err := os.Stat(paths.SessionFile()); !os.IsNotExist(err) {
		t.Fatalf("session file must be removed after unpair even on remote error")
	}
	if _, err := os.Stat(dummyMedia); !os.IsNotExist(err) {
		t.Fatalf("cached media must be removed after unpair: %v", err)
	}

	d.mu.RLock()
	st := d.status
	paired := d.paired
	d.mu.RUnlock()

	if paired {
		t.Fatalf("daemon must not be paired after unpair")
	}
	if st.State != wire.StateUnpaired {
		t.Fatalf("expected state %q, got %q", wire.StateUnpaired, st.State)
	}
	if !strings.Contains(strings.ToLower(st.Error), "local credentials") ||
		!strings.Contains(strings.ToLower(st.Error), "cleared") {
		t.Fatalf("expected status.Error to indicate local credentials/cache cleared, got: %q", st.Error)
	}
	if !strings.Contains(st.Error, "Device pairing") {
		t.Fatalf("expected status.Error to advise removing in Device pairing, got: %q", st.Error)
	}

	// saveSession() or Stop() must not resurrect session file
	d.saveSession()
	if _, err := os.Stat(paths.SessionFile()); !os.IsNotExist(err) {
		t.Fatalf("saveSession resurrected session file after remote failure")
	}
	d.Stop()
	if _, err := os.Stat(paths.SessionFile()); !os.IsNotExist(err) {
		t.Fatalf("Stop resurrected session file after remote failure")
	}
}

// TestUnpairLocalCleanupFailure verifies that when local cleanup fails (induced
// by a non-empty directory at SessionFile without permissions tricks/root),
// remote revocation is still attempted, an error is returned, and status.Error
// does NOT claim cleanup success.
func TestUnpairLocalCleanupFailure(t *testing.T) {
	d, paths := newTestDaemon(t)

	// Induce local cleanup failure at SessionFile without permission tricks:
	// Replace SessionFile with a non-empty directory so os.Remove fails with ENOTEMPTY.
	if err := os.Remove(paths.SessionFile()); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove initial session file: %v", err)
	}
	if err := os.Mkdir(paths.SessionFile(), 0o700); err != nil {
		t.Fatalf("mkdir SessionFile: %v", err)
	}
	blocker := filepath.Join(paths.SessionFile(), "blocker.txt")
	if err := os.WriteFile(blocker, []byte("blocker"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(paths.SessionFile())
	})

	var remoteAttempted atomic.Bool
	unpairCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := d.unpair(unpairCtx, func(ctx context.Context, c *libgm.Client) error {
		remoteAttempted.Store(true)
		return nil
	})

	if !remoteAttempted.Load() {
		t.Fatalf("remote revocation must still be attempted when local cleanup fails")
	}
	if err == nil {
		t.Fatalf("expected error from unpair when local cleanup fails, got nil")
	}

	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("expected errors.As(err, &pathErr) for local cleanup failure, got: %v", err)
	}

	d.mu.RLock()
	st := d.status
	d.mu.RUnlock()

	if st.State != wire.StateUnpaired {
		t.Fatalf("expected StateUnpaired, got %q", st.State)
	}
	if strings.Contains(st.Error, "were cleared") || strings.Contains(st.Error, "was cleared") {
		t.Fatalf("status error must not claim cleanup success when local cleanup failed: %q", st.Error)
	}
	if !strings.Contains(st.Error, "incomplete") {
		t.Fatalf("status error should indicate local cleanup was incomplete: %q", st.Error)
	}
}

// TestUnpairBothFailuresPreserved verifies that when both local cleanup and
// remote revocation fail, both errors are composed and preserved via errors.Is.
func TestUnpairBothFailuresPreserved(t *testing.T) {
	d, paths := newTestDaemon(t)

	// Induce local cleanup failure with non-empty directory at SessionFile
	if err := os.Remove(paths.SessionFile()); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove initial session file: %v", err)
	}
	if err := os.Mkdir(paths.SessionFile(), 0o700); err != nil {
		t.Fatalf("mkdir SessionFile: %v", err)
	}
	blocker := filepath.Join(paths.SessionFile(), "blocker.txt")
	if err := os.WriteFile(blocker, []byte("blocker"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(paths.SessionFile())
	})

	remoteErr := errors.New("simulated remote revoke failure")
	var remoteAttempted atomic.Bool

	unpairCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := d.unpair(unpairCtx, func(ctx context.Context, c *libgm.Client) error {
		remoteAttempted.Store(true)
		return remoteErr
	})

	if !remoteAttempted.Load() {
		t.Fatalf("remote revocation must be attempted even on local error")
	}
	if err == nil {
		t.Fatalf("expected error from unpair, got nil")
	}

	if !errors.Is(err, remoteErr) {
		t.Fatalf("errors.Is(err, remoteErr) failed, got: %v", err)
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("expected errors.As(err, &pathErr) for local failure, got: %v", err)
	}

	d.mu.RLock()
	st := d.status
	d.mu.RUnlock()

	if strings.Contains(st.Error, "were cleared") || strings.Contains(st.Error, "was cleared") {
		t.Fatalf("status error must not claim cleanup success: %q", st.Error)
	}
	if !strings.Contains(st.Error, "Device pairing") {
		t.Fatalf("status error must advise removing in Device pairing: %q", st.Error)
	}
}

// TestUnpairBothSuccess verifies that when local cleanup and remote revocation
// both succeed, d.unpair returns nil, clears credentials/cache, and leaves
// StateUnpaired with empty error status.
func TestUnpairBothSuccess(t *testing.T) {
	d, paths := newTestDaemon(t)

	auth := syntheticTestAuth()
	if err := paths.SaveSession(auth); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	d.mu.Lock()
	d.paired = true
	d.auth = auth
	d.mu.Unlock()

	dummyMedia := filepath.Join(paths.MediaDir(), "cached_file.bin")
	if err := os.WriteFile(dummyMedia, []byte("bytes"), 0o600); err != nil {
		t.Fatalf("WriteFile dummy media: %v", err)
	}

	if _, err := os.Stat(paths.SessionFile()); err != nil {
		t.Fatalf("session file must exist before unpair: %v", err)
	}

	var remoteCalled atomic.Bool
	unpairCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := d.unpair(unpairCtx, func(ctx context.Context, c *libgm.Client) error {
		remoteCalled.Store(true)
		return nil
	})

	if !remoteCalled.Load() {
		t.Fatalf("remote revoke was not called")
	}
	if err != nil {
		t.Fatalf("expected nil error on success, got: %v", err)
	}

	if _, err := os.Stat(paths.SessionFile()); !os.IsNotExist(err) {
		t.Fatalf("session file still exists after successful unpair")
	}
	if _, err := os.Stat(dummyMedia); !os.IsNotExist(err) {
		t.Fatalf("cached media still exists after successful unpair")
	}

	d.mu.RLock()
	st := d.status
	paired := d.paired
	d.mu.RUnlock()

	if paired {
		t.Fatalf("daemon must not be paired after successful unpair")
	}
	if st.State != wire.StateUnpaired {
		t.Fatalf("expected state %q, got %q", wire.StateUnpaired, st.State)
	}
	if st.Error != "" {
		t.Fatalf("expected empty status error on success, got: %q", st.Error)
	}
}

// TestUnpairDelayedRevocationCannotOverwriteReplacedSession verifies that if
// remote revocation takes time and a new replacement session begins in the
// interim, the completion of the delayed revocation cannot overwrite the newly
// replaced session's state or status.
func TestUnpairDelayedRevocationCannotOverwriteReplacedSession(t *testing.T) {
	d, _ := newTestDaemon(t)

	startRevoke := make(chan struct{})
	finishRevoke := make(chan struct{})
	done := make(chan error, 1)

	remoteErr := errors.New("delayed revocation failure")

	unpairCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		err := d.unpair(unpairCtx, func(ctx context.Context, c *libgm.Client) error {
			close(startRevoke)
			select {
			case <-finishRevoke:
				return remoteErr
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		done <- err
	}()

	// Wait until unpair has called resetSessionLocked and entered the injected revoke
	select {
	case <-startRevoke:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for revoke to start")
	}

	// While revocation is pending outside sessionMu, replace the session.
	// resetSessionLocked cancels the previous replacement session context.
	d.sessionMu.Lock()
	_, _ = d.resetSessionLocked()
	d.mu.Lock()
	d.paired = true
	d.status = wire.Status{State: wire.StateConnected, PhoneOK: true, Error: ""}
	d.mu.Unlock()
	d.sessionMu.Unlock()

	// Now unblock the delayed revoke
	close(finishRevoke)

	select {
	case err := <-done:
		if !errors.Is(err, remoteErr) {
			t.Fatalf("expected unpair to return remoteErr, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for unpair to finish")
	}

	// Verify that the delayed unpair did NOT overwrite the replacement session
	d.mu.RLock()
	st := d.status
	paired := d.paired
	d.mu.RUnlock()

	if !paired {
		t.Fatalf("new session should remain paired; delayed revocation must not clear paired flag")
	}
	if st.State != wire.StateConnected {
		t.Fatalf("expected state to remain %q, got %q", wire.StateConnected, st.State)
	}
	if st.Error != "" {
		t.Fatalf("status.Error was overwritten by delayed revocation: %q", st.Error)
	}
}
