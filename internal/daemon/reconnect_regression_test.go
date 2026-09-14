package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

// 1. TestListenRecoveredRemainsConnectingBeforePhoneSync verifies that ListenRecovered
// leaves the connection in StateConnecting and does not falsely jump to StateConnected
// before authenticated conversation sync completes.
func TestListenRecoveredRemainsConnectingBeforePhoneSync(t *testing.T) {
	d, _ := newTestDaemon(t)
	d.mu.Lock()
	d.paired = true
	d.status.State = wire.StateDisconnected
	d.mu.Unlock()

	d.sessionMu.Lock()
	d.handleEvent(&events.ListenRecovered{})
	d.sessionMu.Unlock()

	d.mu.RLock()
	st := d.status.State
	paired := d.paired
	d.mu.RUnlock()

	if st != wire.StateConnecting {
		t.Fatalf("expected StateConnecting on ListenRecovered, got %q", st)
	}
	if !paired {
		t.Fatal("expected paired to remain true")
	}

	// Cancel session context to terminate spawned initialSync helper goroutine cleanly.
	d.sessionCancel()
}

// 2. TestAuthenticatedSyncMovesConnectingToConnected verifies that acceptConversationSync
// commits conversations, marks PhoneOK=true, and transitions state from connecting to connected.
func TestAuthenticatedSyncMovesConnectingToConnected(t *testing.T) {
	d, _ := newTestDaemon(t)
	d.mu.Lock()
	d.paired = true
	d.status.State = wire.StateConnecting
	d.status.PhoneOK = false
	d.mu.Unlock()

	convID := "sync-conv-1"
	resp := &gmproto.ListConversationsResponse{
		Conversations: []*gmproto.Conversation{
			{
				ConversationID: convID,
				Name:           "Alice",
			},
		},
	}

	err := d.acceptConversationSync(context.Background(), d.client, resp)
	if err != nil {
		t.Fatalf("acceptConversationSync failed: %v", err)
	}

	d.mu.RLock()
	st := d.status.State
	phoneOK := d.status.PhoneOK
	_, exists := d.convs[convID]
	d.mu.RUnlock()

	if st != wire.StateConnected {
		t.Errorf("expected StateConnected, got %q", st)
	}
	if !phoneOK {
		t.Error("expected PhoneOK to be true after successful authenticated sync")
	}
	if !exists {
		t.Errorf("expected conversation %s to be recorded in daemon store", convID)
	}
}

// 3. TestFailedSessionCannotBeResurrected verifies that once a session is marked invalid/unpaired
// via requirePairingLocked, neither ClientReady, ListenRecovered, PairSuccessful, nor late
// acceptConversationSync can resurrect the session into connected or connecting.
func TestFailedSessionCannotBeResurrected(t *testing.T) {
	d, _ := newTestDaemon(t)
	d.paired = true
	d.setState(wire.StateConnected, "")

	d.sessionMu.Lock()
	d.requirePairingLocked()
	d.sessionMu.Unlock()

	d.mu.RLock()
	st := d.status.State
	paired := d.paired
	d.mu.RUnlock()

	if st != wire.StateError {
		t.Fatalf("expected StateError after requirePairingLocked, got %q", st)
	}
	if paired {
		t.Fatal("expected paired to be false after requirePairingLocked")
	}

	// 3a. ClientReady cannot resurrect
	d.sessionMu.Lock()
	d.handleEvent(&events.ClientReady{
		Conversations: []*gmproto.Conversation{{ConversationID: "ghost-conv"}},
	})
	d.sessionMu.Unlock()

	d.mu.RLock()
	st = d.status.State
	d.mu.RUnlock()
	if st != wire.StateError {
		t.Fatalf("ClientReady resurrected state to %q; want StateError", st)
	}

	// 3b. ListenRecovered cannot resurrect
	d.sessionMu.Lock()
	d.handleEvent(&events.ListenRecovered{})
	d.sessionMu.Unlock()

	d.mu.RLock()
	st = d.status.State
	d.mu.RUnlock()
	if st != wire.StateError {
		t.Fatalf("ListenRecovered resurrected state to %q; want StateError", st)
	}

	// 3c. Unsolicited PairSuccessful cannot resurrect
	d.sessionMu.Lock()
	d.handleEvent(&events.PairSuccessful{})
	d.sessionMu.Unlock()

	d.mu.RLock()
	st = d.status.State
	d.mu.RUnlock()
	if st != wire.StateError {
		t.Fatalf("PairSuccessful resurrected state to %q; want StateError", st)
	}

	// 3d. Late acceptConversationSync cannot resurrect
	resp := &gmproto.ListConversationsResponse{
		Conversations: []*gmproto.Conversation{{ConversationID: "late-conv"}},
	}
	err := d.acceptConversationSync(context.Background(), d.client, resp)
	if !errors.Is(err, errNotConnected) {
		t.Fatalf("expected errNotConnected from late sync, got %v", err)
	}

	d.mu.RLock()
	st = d.status.State
	_, exists := d.convs["late-conv"]
	d.mu.RUnlock()
	if st != wire.StateError {
		t.Fatalf("late acceptConversationSync resurrected state to %q; want StateError", st)
	}
	if exists {
		t.Fatal("late acceptConversationSync recorded conversations on invalid session")
	}
}

// 4. TestExplicitPairSuccessfulBecomesConnectingNotConnected verifies that a legitimate
// PairSuccessful event moves state to StateConnecting (not StateConnected) pending phone sync.
func TestExplicitPairSuccessfulBecomesConnectingNotConnected(t *testing.T) {
	d, _ := newTestDaemon(t)
	d.mu.Lock()
	d.status.State = wire.StatePairing
	d.paired = false
	d.mu.Unlock()

	d.sessionMu.Lock()
	d.handleEvent(&events.PairSuccessful{})
	d.sessionMu.Unlock()

	d.mu.RLock()
	st := d.status.State
	paired := d.paired
	d.mu.RUnlock()

	if !paired {
		t.Fatal("expected paired to be true after PairSuccessful")
	}
	if st != wire.StateConnecting {
		t.Fatalf("expected StateConnecting after PairSuccessful, got %q", st)
	}

	// Cancel spawned initialSync
	d.sessionCancel()
}

// 5. TestAuthRetrySessionCookieInvalidNeverReadsBrowserOrPairs verifies that when
// SESSION_COOKIE_INVALID occurs, withAuthRetry immediately calls requirePairing,
// never accesses browser profile cookies, and never initiates Gaia pairing.
func TestAuthRetrySessionCookieInvalidNeverReadsBrowserOrPairs(t *testing.T) {
	d, _ := newTestDaemon(t)
	d.mu.Lock()
	d.paired = true
	d.status.State = wire.StateConnecting
	d.mu.Unlock()

	d.cookies.mu.Lock()
	d.cookies.last = time.Time{}
	d.cookies.mu.Unlock()

	deadErr := errors.New(`HTTP 401: [["type.googleapis.com/google.rpc.ErrorInfo",["SESSION_COOKIE_INVALID","googleapis.com"]]]`)
	opCount := 0
	_, err := withAuthRetry(context.Background(), d, func() (string, error) {
		opCount++
		return "", deadErr
	})

	if !errors.Is(err, deadErr) {
		t.Fatalf("expected deadErr returned, got %v", err)
	}
	if opCount != 1 {
		t.Fatalf("expected op to be called exactly once without retry, called %d times", opCount)
	}

	d.cookies.mu.Lock()
	cookieCheckTime := d.cookies.last
	d.cookies.mu.Unlock()
	if !cookieCheckTime.IsZero() {
		t.Fatal("withAuthRetry touched browser cookies on SESSION_COOKIE_INVALID")
	}

	d.mu.RLock()
	active := d.gaiaActive
	paired := d.paired
	st := d.status.State
	errMsg := d.status.Error
	d.mu.RUnlock()

	if active {
		t.Fatal("withAuthRetry started Gaia pairing automatically")
	}
	if paired {
		t.Fatal("expected paired=false after SESSION_COOKIE_INVALID")
	}
	if st != wire.StateError {
		t.Fatalf("expected StateError, got %q", st)
	}
	if !strings.Contains(errMsg, "Pair with Google") {
		t.Errorf("expected explicit pair prompt in error, got %q", errMsg)
	}
}

// 6. TestFatalAuthErrorsRequireExplicitPair verifies that 401 and 403 fatal errors
// (both via ListenFatalError and withAuthRetry) latch paired=false and require explicit pairing.
func TestFatalAuthErrorsRequireExplicitPair(t *testing.T) {
	// 6a. ListenFatalError with 401
	d1, _ := newTestDaemon(t)
	d1.mu.Lock()
	d1.paired = true
	d1.status.State = wire.StateConnecting
	d1.mu.Unlock()

	d1.sessionMu.Lock()
	d1.handleEvent(&events.ListenFatalError{
		Error: errors.New("HTTP 401: unauthorized"),
	})
	d1.sessionMu.Unlock()

	d1.mu.RLock()
	st1 := d1.status.State
	paired1 := d1.paired
	d1.mu.RUnlock()

	if paired1 || st1 != wire.StateError {
		t.Fatalf("401 ListenFatalError: paired=%v, state=%q; want paired=false, StateError", paired1, st1)
	}

	// 6b. ListenFatalError with 403
	d2, _ := newTestDaemon(t)
	d2.mu.Lock()
	d2.paired = true
	d2.status.State = wire.StateConnecting
	d2.mu.Unlock()

	d2.sessionMu.Lock()
	d2.handleEvent(&events.ListenFatalError{
		Error: errors.New("HTTP 403: forbidden access"),
	})
	d2.sessionMu.Unlock()

	d2.mu.RLock()
	st2 := d2.status.State
	paired2 := d2.paired
	d2.mu.RUnlock()

	if paired2 || st2 != wire.StateError {
		t.Fatalf("403 ListenFatalError: paired=%v, state=%q; want paired=false, StateError", paired2, st2)
	}

	// 6c. withAuthRetry unrecoverable 401
	d3, _ := newTestDaemon(t)
	d3.mu.Lock()
	d3.paired = true
	d3.status.State = wire.StateConnecting
	d3.mu.Unlock()

	// Do not read the real browser or keyring during this regression.
	d3.cookies.last = time.Now()
	_, _ = withAuthRetry(context.Background(), d3, func() (string, error) {
		return "", errors.New("HTTP 401: invalid authentication credentials")
	})

	d3.mu.RLock()
	st3 := d3.status.State
	paired3 := d3.paired
	d3.mu.RUnlock()

	if paired3 || st3 != wire.StateError {
		t.Fatalf("401 withAuthRetry: paired=%v, state=%q; want paired=false, StateError", paired3, st3)
	}
}

// 7. TestCanceledSyncCannotOverrideNewerSession verifies that a canceled sync context or a
// stale client instance cannot overwrite conversations or connected status on a newer session.
func TestCanceledSyncCannotOverrideNewerSession(t *testing.T) {
	// 7a. Canceled sync context
	d, _ := newTestDaemon(t)
	d.mu.Lock()
	d.paired = true
	d.status.State = wire.StateConnecting
	d.mu.Unlock()

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	resp := &gmproto.ListConversationsResponse{
		Conversations: []*gmproto.Conversation{{ConversationID: "canceled-conv"}},
	}
	err := d.acceptConversationSync(canceledCtx, d.client, resp)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	d.mu.RLock()
	st := d.status.State
	_, exists := d.convs["canceled-conv"]
	d.mu.RUnlock()

	if st == wire.StateConnected {
		t.Error("canceled sync set StateConnected")
	}
	if exists {
		t.Error("canceled sync recorded conversation in store")
	}

	// 7b. Superseded client instance
	d2, _ := newTestDaemon(t)
	d2.mu.Lock()
	d2.paired = true
	d2.status.State = wire.StateConnecting
	d2.mu.Unlock()
	oldClient := d2.client

	// Reset session creates new client and cancels old session context
	d2.sessionMu.Lock()
	oldC, err := d2.resetSessionLocked()
	d2.sessionMu.Unlock()
	if oldC != nil {
		oldC.Disconnect()
	}
	if err != nil {
		t.Fatalf("resetSessionLocked failed: %v", err)
	}

	// Make the replacement session eligible so this specifically checks
	// client identity, independently of the unpaired-state guard.
	d2.paired = true
	d2.setState(wire.StateConnecting, "")
	resp2 := &gmproto.ListConversationsResponse{
		Conversations: []*gmproto.Conversation{{ConversationID: "stale-client-conv"}},
	}
	err = d2.acceptConversationSync(context.Background(), oldClient, resp2)
	if !errors.Is(err, errNotConnected) {
		t.Fatalf("expected errNotConnected for old client, got %v", err)
	}

	d2.mu.RLock()
	st2 := d2.status.State
	_, exists2 := d2.convs["stale-client-conv"]
	d2.mu.RUnlock()

	if st2 == wire.StateConnected {
		t.Error("stale client sync set StateConnected")
	}
	if exists2 {
		t.Error("stale client sync recorded conversation in store")
	}
}
