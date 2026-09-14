package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"
)

func TestInitialSyncFetchesWhileConnectingWithoutPublishingEarlyReadiness(t *testing.T) {
	d, _ := newTestDaemon(t)
	d.paired = true
	d.setState(wire.StateConnecting, "")
	ctx, cancel := context.WithTimeout(d.sessionContext(), 10*time.Second)
	defer cancel()
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "synthetic connection failure", http.StatusBadGateway)
		// Cancel only AFTER startup actually attempts a fetch. The old
		// circular connected-state dependency never reaches this proxy.
		cancel()
	}))
	defer proxy.Close()
	if err := d.client.SetProxy(proxy.URL); err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := d.Subscribe()
	defer unsubscribe()
	d.initialSync(ctx)
	if requests.Load() == 0 {
		t.Fatal("startup never attempted a conversation fetch while connecting")
	}
	if got := d.Status().State; got != wire.StateConnecting {
		t.Fatalf("failed fetch changed startup state to %s", got)
	}
	for {
		select {
		case event := <-events:
			if event.Event == wire.EventStatus {
				status, ok := event.Data.(wire.Status)
				if !ok {
					t.Fatalf("unexpected status type %T", event.Data)
				}
				if status.State == wire.StateConnected {
					t.Fatal("announced connected without a successful fetch")
				}
			}
		default:
			return
		}
	}
}

func TestNonAuthFailureDoesNotRequirePairing(t *testing.T) {
	d, _ := newTestDaemon(t)
	d.paired = true
	d.setState(wire.StateConnected, "")
	expected := errors.New("HTTP 503 service unavailable")
	calls := 0
	_, err := withAuthRetry(d.sessionContext(), d, func() (string, error) {
		calls++
		return "", expected
	})
	if !errors.Is(err, expected) || calls != 1 || !d.paired {
		t.Fatalf("temporary error changed pairing or retried: err=%v calls=%d paired=%v", err, calls, d.paired)
	}
	if !d.cookies.last.IsZero() {
		t.Fatal("temporary error attempted browser-cookie recovery")
	}
}

func TestActiveGaiaPairingKeepsChallengeThroughTransportEvents(t *testing.T) {
	d, _ := newTestDaemon(t)
	d.gaiaActive = true
	d.status.State = wire.StateGaiaPairing
	d.status.Emoji = "synthetic-challenge"
	for _, event := range []any{
		&events.ClientReady{}, &events.ListenRecovered{},
		&events.ListenTemporaryError{Error: errors.New("offline")},
		&events.ListenFatalError{Error: errors.New("HTTP 401")},
	} {
		d.sessionMu.Lock()
		d.handleEvent(event)
		d.sessionMu.Unlock()
		got := d.Status()
		if got.State != wire.StateGaiaPairing || got.Emoji != "synthetic-challenge" {
			t.Fatalf("%T overwrote explicit pairing: %+v", event, got)
		}
	}
}

func TestExplicitGaiaConfirmationWaitsForPhoneSync(t *testing.T) {
	d, _ := newTestDaemon(t)
	d.gaiaActive = true
	d.gaiaCtx = d.sessionContext()
	d.status.State = wire.StateGaiaPairing
	d.status.Emoji = "synthetic-challenge"
	d.sessionMu.Lock()
	d.handleEvent(&events.PairSuccessful{})
	d.sessionMu.Unlock()
	if got := d.Status(); got.State != wire.StateConnecting || got.Emoji != "" || !d.paired {
		t.Fatalf("confirmation did not wait for sync and clear challenge: %+v", got)
	}
}
