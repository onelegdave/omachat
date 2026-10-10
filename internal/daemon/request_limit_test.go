package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
	"github.com/rs/zerolog"
)

func TestConnectionBackpressureAndCancellation(t *testing.T) {
	paths := &store.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	d := New(zerolog.Nop(), paths)
	server, client := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { d.handleConn(ctx, server); close(done) }()
	reader := bufio.NewReader(client)
	// Initial status for each backend; then deliberately stop reading responses.
	for i := 0; i < 4; i++ {
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
	}
	var written atomic.Int32
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for i := 0; i < 1000; i++ {
			if _, err := fmt.Fprintf(client, "{\"id\":\"%d\",\"method\":\"status\"}\n", i); err != nil {
				return
			}
			written.Add(1)
		}
	}()
	deadline := time.Now().Add(time.Second)
	for written.Load() < maxRequestsPerConnection && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if written.Load() < maxRequestsPerConnection {
		t.Fatal("requests did not start")
	}
	time.Sleep(20 * time.Millisecond)
	if n := written.Load(); n > maxRequestsPerConnection+1 {
		t.Fatalf("unbounded requests: %d", n)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not cancel")
	}
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("writer did not unblock")
	}
}

type scanCheckpointConn struct {
	net.Conn
	reads    int
	nextScan chan struct{}
}

func (c *scanCheckpointConn) Read(b []byte) (int, error) {
	c.reads++
	if c.reads == 2 {
		close(c.nextScan)
	}
	return c.Conn.Read(b)
}

func TestConnectionDrainsStoreWritersBeforeReturning(t *testing.T) {
	d := New(zerolog.Nop(), &store.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()})
	server, client := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn := &scanCheckpointConn{Conn: server, nextScan: make(chan struct{})}
	done := make(chan struct{})
	go func() { d.handleConn(ctx, conn); close(done) }()
	reader := bufio.NewReader(client)
	for i := 0; i < 4; i++ {
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
	}
	// Block a real synthetic config writer after it has been scheduled, then
	// disconnect. Returning early would let runLocked release ownership while
	// this writer could still change the store underneath the next daemon.
	d.configResponseMu.Lock()
	locked := true
	defer func() {
		if locked {
			d.configResponseMu.Unlock()
		}
	}()
	req := wire.Request{ID: "slow-writer", Method: wire.MethodSetUiScale, Params: wire.SetUiScaleParams{Scale: 1.4}}
	if err := json.NewEncoder(client).Encode(req); err != nil {
		t.Fatal(err)
	}
	select {
	case <-conn.nextScan: // scanner scheduled the request and reached its next Read
	case <-time.After(time.Second):
		t.Fatal("request was not scheduled")
	}
	cancel()
	returnedEarly := false
	select {
	case <-done:
		returnedEarly = true
	case <-time.After(30 * time.Millisecond):
	}
	d.configResponseMu.Unlock()
	locked = false
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler failed to drain")
	}
	// Also let the broken implementation's straggler finish during the red
	// regression run, before its temporary directory is removed.
	deadline := time.Now().Add(time.Second)
	for d.PluginConfig().UiScale != 1.4 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if returnedEarly {
		t.Fatal("connection returned with a store writer still running")
	}
	if d.PluginConfig().UiScale != 1.4 {
		t.Fatal("synthetic writer did not finish")
	}
}
