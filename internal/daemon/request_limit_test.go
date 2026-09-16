package daemon

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/store"
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
	for i := 0; i < 3; i++ {
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
