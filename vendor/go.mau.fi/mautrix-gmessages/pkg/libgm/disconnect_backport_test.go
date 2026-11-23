package libgm

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

// OmaChat: tests for the Client.stopped patch (see NOTICE).

func stoppedTestClient(t *testing.T) (*Client, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "no network in tests", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	auth := NewAuthData()
	auth.TachyonAuthToken = []byte("synthetic-token")
	auth.Browser = &gmproto.Device{SourceID: "browser"}
	c := NewClient(auth, nil, zerolog.Nop())
	if err := c.SetProxy(srv.URL); err != nil {
		t.Fatal(err)
	}
	return c, &requests
}

func TestDisconnectStopsLongPollBeforeItConnects(t *testing.T) {
	c, requests := stoppedTestClient(t)
	// Nothing is connected yet, so closeLongPolling alone has nothing to close.
	c.Disconnect()
	if !c.stopped.Load() {
		t.Fatal("Disconnect did not set stopped")
	}
	if clean := c.doLongPoll(false, false, nil); !clean {
		t.Fatal("a stopped long poll did not return cleanly")
	}
	if n := requests.Load(); n != 0 {
		t.Fatalf("a stopped long poll sent %d requests", n)
	}
}

func TestConnectClearsStopped(t *testing.T) {
	c, _ := stoppedTestClient(t)
	c.Disconnect()
	// Connect fails here (no network), but clears the flag before trying.
	_ = c.Connect()
	if c.stopped.Load() {
		t.Fatal("Connect left the client stopped")
	}
	c.Disconnect()
}
