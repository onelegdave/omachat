package daemon

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"
)

// waitReset waits up to the timeout for the context to be canceled,
// which indicates resetSessionLocked ran.
func waitReset(t *testing.T, ctx context.Context, timeout time.Duration) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(timeout):
		t.Fatal("timed out waiting for session reset (context cancellation)")
	}
}

// waitNoReset ensures the context is NOT canceled within the timeout.
func waitNoReset(t *testing.T, ctx context.Context, waitTime time.Duration) {
	t.Helper()
	select {
	case <-ctx.Done():
		t.Fatal("session was unexpectedly reset (context canceled)")
	case <-time.After(waitTime):
	}
}

// assertSessionCleared verifies that credentials are no longer persisted.
func assertSessionCleared(t *testing.T, d *Daemon) {
	t.Helper()
	if _, err := os.Stat(d.paths.SessionFile()); !os.IsNotExist(err) {
		t.Fatalf("expected session file to be absent, stat error: %v", err)
	}
}

// TestGaiaLogoutDuringPairingIgnored ensures an unexpected logout event
// during an active, unconfirmed pairing does not cancel the handshake.
func TestGaiaLogoutDuringPairingIgnored(t *testing.T) {
	d, paths := newTestDaemon(t)

	// Ensure no session exists
	if err := paths.ClearSession(); err != nil {
		t.Fatal(err)
	}

	d.sessionMu.Lock()
	d.gaiaActive = true
	d.sessionMu.Unlock()

	d.mu.Lock()
	d.paired = false
	d.status.State = wire.StateGaiaPairing
	ctx := d.sessionCtx
	client := d.client
	d.mu.Unlock()

	// Route through production event handler logic
	handler := d.clientEventHandler(client, ctx)
	handler(&events.GaiaLoggedOut{})

	// Since we expect it to be ignored synchronously, a small wait suffices.
	waitNoReset(t, ctx, 100*time.Millisecond)

	d.mu.RLock()
	state := d.status.State
	liveCtx := d.sessionCtx
	liveClient := d.client
	d.mu.RUnlock()

	if state != wire.StateGaiaPairing {
		t.Fatalf("expected exact StateGaiaPairing, got %s", state)
	}
	if liveCtx != ctx {
		t.Fatal("live context was replaced, expected same context")
	}
	if liveClient != client {
		t.Fatal("live client was replaced, expected same client")
	}
	assertSessionCleared(t, d)
}

// TestGaiaLogoutAfterPairingResetsSession ensures a genuine logout event
// AFTER confirmation (paired=true) properly resets the session, even if
// gaiaActive hasn't flipped to false yet.
func TestGaiaLogoutAfterPairingResetsSession(t *testing.T) {
	d, paths := newTestDaemon(t)

	// Seed session on disk
	if err := paths.SaveSession(d.auth); err != nil {
		t.Fatalf("failed to seed session: %v", err)
	}

	d.sessionMu.Lock()
	d.gaiaActive = true
	d.sessionMu.Unlock()

	d.mu.Lock()
	d.paired = true
	d.status.State = wire.StateConnecting
	ctx := d.sessionCtx
	client := d.client
	d.mu.Unlock()

	handler := d.clientEventHandler(client, ctx)
	handler(&events.GaiaLoggedOut{})

	// Wait for asynchronous reset
	waitReset(t, ctx, 2*time.Second)

	// waitReset returns as soon as the context is canceled, but resetLoggedOut
	// still holds sessionMu to finish updating state. Acquire sessionMu to sync.
	d.sessionMu.Lock()
	d.sessionMu.Unlock()

	d.mu.RLock()
	state := d.status.State
	paired := d.paired
	d.mu.RUnlock()

	if state != wire.StateUnpaired {
		t.Fatalf("expected state %s after genuine logout, got %s", wire.StateUnpaired, state)
	}
	if paired {
		t.Fatal("expected paired to be false")
	}

	assertSessionCleared(t, d)
}

// TestGaiaLogoutNormalResetsSession ensures a genuine logout event
// during normal operation (paired=true, gaiaActive=false) resets session.
func TestGaiaLogoutNormalResetsSession(t *testing.T) {
	d, paths := newTestDaemon(t)

	if err := paths.SaveSession(d.auth); err != nil {
		t.Fatalf("failed to seed session: %v", err)
	}

	d.sessionMu.Lock()
	d.gaiaActive = false
	d.sessionMu.Unlock()

	d.mu.Lock()
	d.paired = true
	d.status.State = wire.StateConnected
	ctx := d.sessionCtx
	client := d.client
	d.mu.Unlock()

	handler := d.clientEventHandler(client, ctx)
	handler(&events.GaiaLoggedOut{})

	waitReset(t, ctx, 2*time.Second)

	d.sessionMu.Lock()
	d.sessionMu.Unlock()

	d.mu.RLock()
	state := d.status.State
	paired := d.paired
	d.mu.RUnlock()

	if state != wire.StateUnpaired {
		t.Fatalf("expected state %s after normal logout, got %s", wire.StateUnpaired, state)
	}
	if paired {
		t.Fatal("expected paired to be false")
	}

	assertSessionCleared(t, d)
}

// TestGaiaLogoutObsoleteCallbackIgnored ensures a logout callback from an old
// client instance (e.g. replaced by a fresh pairing) does not reset the new pairing.
func TestGaiaLogoutObsoleteCallbackIgnored(t *testing.T) {
	d, paths := newTestDaemon(t)

	// Initial session state
	if err := paths.SaveSession(d.auth); err != nil {
		t.Fatalf("failed to seed session: %v", err)
	}

	d.mu.Lock()
	d.paired = true
	d.status.State = wire.StateConnected
	oldCtx := d.sessionCtx
	oldClient := d.client
	d.mu.Unlock()

	// Force a reset to create a new client/context, simulating a new pairing
	d.sessionMu.Lock()
	_, resetErr := d.resetSessionLocked()
	d.mu.Lock()
	d.auth = syntheticTestAuth()
	d.mu.Unlock()
	d.sessionMu.Unlock()
	if resetErr != nil {
		t.Fatal(resetErr)
	}

	d.mu.Lock()
	d.paired = true
	d.status.State = wire.StateConnected
	newCtx := d.sessionCtx
	newClient := d.client
	d.mu.Unlock()

	if err := paths.SaveSession(d.auth); err != nil {
		t.Fatalf("failed to seed new session: %v", err)
	}

	// Fire the logout event through the old client's handler
	oldHandler := d.clientEventHandler(oldClient, oldCtx)

	// Because the context is already canceled by resetSessionLocked,
	// the handler will exit early. But even if it didn't, the client check protects it.
	oldHandler(&events.GaiaLoggedOut{})

	// Give it a moment to ensure no async reset is fired on the new context
	waitNoReset(t, newCtx, 100*time.Millisecond)

	d.mu.RLock()
	state := d.status.State
	paired := d.paired
	liveCtx := d.sessionCtx
	liveClient := d.client
	d.mu.RUnlock()

	if state != wire.StateConnected {
		t.Fatalf("obsolete callback altered state, got %s", state)
	}
	if !paired {
		t.Fatal("obsolete callback set paired to false")
	}
	if liveCtx != newCtx {
		t.Fatal("obsolete callback altered live context")
	}
	if liveClient != newClient {
		t.Fatal("obsolete callback altered live client")
	}

	// The new session should still be intact on disk
	_, pairedDisk, err := paths.LoadSession()
	if err != nil || !pairedDisk {
		t.Fatalf("expected new session to remain intact, err=%v, paired=%v", err, pairedDisk)
	}
}
