package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
)

type gateConn struct {
	t      *testing.T
	client net.Conn
	reader *bufio.Reader
}

func openGateConn(t *testing.T, d *Daemon) *gateConn {
	t.Helper()
	server, client := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); client.Close() })
	go d.handleConn(ctx, server)
	c := &gateConn{t: t, client: client, reader: bufio.NewReader(client)}
	for range wire.KnownNetworks { // initial status for each backend
		if evt := c.read(); evt["event"] != wire.EventStatus {
			t.Fatalf("initial frame was not status: %v", evt)
		}
	}
	return c
}

func (c *gateConn) read() map[string]any {
	c.t.Helper()
	_ = c.client.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		c.t.Fatal(err)
	}
	var frame map[string]any
	if err := json.Unmarshal(line, &frame); err != nil {
		c.t.Fatal(err)
	}
	return frame
}

func (c *gateConn) send(req wire.Request) {
	c.t.Helper()
	if err := json.NewEncoder(c.client).Encode(req); err != nil {
		c.t.Fatal(err)
	}
}

// Panel code from an earlier release keeps running after an update until the
// shell restarts. It decodes originals and renders names as rich text, so it
// must receive no remote content.
func TestPanelsThatDoNotIdentifyReceiveStatusOnly(t *testing.T) {
	d := freshSelectionDaemon(t)
	d.activeServices["gmessages"] = true
	d.convs["c"] = wire.Conversation{ID: "c", Name: `<img src="https://attacker.example/x">`}
	legacy := openGateConn(t, d)

	legacy.send(wire.Request{ID: "1", Method: wire.MethodConversations})
	if resp := legacy.read(); resp["id"] != "1" || resp["ok"] == true || resp["error"] != wire.RestartRequiredError {
		t.Fatalf("legacy conversations reply: %v", resp)
	}
	legacy.send(wire.Request{ID: "2", Method: wire.MethodMedia, Params: map[string]any{"key": "k"}})
	if resp := legacy.read(); resp["error"] != wire.RestartRequiredError {
		t.Fatalf("legacy media reply: %v", resp)
	}
	// Content events are withheld; status still flows.
	d.publish(wire.EventConversation, d.convs["c"])
	d.publish(wire.EventStatus, d.Status())
	if evt := legacy.read(); evt["event"] != wire.EventStatus {
		t.Fatalf("legacy panel received %v", evt)
	}
	legacy.send(wire.Request{ID: "3", Method: wire.MethodStatus})
	if resp := legacy.read(); resp["id"] != "3" || resp["ok"] != true {
		t.Fatalf("legacy status reply: %v", resp)
	}
	// Asking for build identity, as any panel does, reports the capability but
	// does not open remote content to a panel that never identified itself.
	legacy.send(wire.Request{ID: "3b", Method: "buildInfo"})
	if resp := legacy.read(); resp["id"] != "3b" || resp["ok"] != true || resp["result"].(map[string]any)["contentProtocol"] != wire.SafeClient {
		t.Fatalf("legacy buildInfo reply: %v", resp)
	}
	legacy.send(wire.Request{ID: "3c", Method: wire.MethodConversations})
	if resp := legacy.read(); resp["id"] != "3c" || resp["error"] != wire.RestartRequiredError {
		t.Fatalf("legacy conversations after buildInfo: %v", resp)
	}
	d.publish(wire.EventConversation, d.convs["c"])
	d.publish(wire.EventStatus, d.Status())
	if evt := legacy.read(); evt["event"] != wire.EventStatus {
		t.Fatalf("legacy panel received %v after buildInfo", evt)
	}

	current := openGateConn(t, d)
	current.send(wire.Request{ID: "4", Method: wire.MethodConversations, Client: wire.SafeClient})
	if resp := current.read(); resp["id"] != "4" || resp["ok"] != true {
		t.Fatalf("current panel conversations reply: %v", resp)
	}
	d.publish(wire.EventConversation, d.convs["c"])
	if evt := current.read(); evt["event"] != wire.EventConversation {
		t.Fatalf("current panel did not receive the conversation: %v", evt)
	}
}
