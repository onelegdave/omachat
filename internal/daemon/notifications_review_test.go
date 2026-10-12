package daemon

import (
	"context"
	"github.com/godbus/dbus/v5"
	"github.com/onelegdave/omachat/internal/wire"
	"os"
	"testing"
)

func TestFormerNotificationOwnerCannotOpenOrCloseReplacementIDs(t *testing.T) {
	var events []wire.Event
	n := newDesktopNotifications(func(e wire.Event) { events = append(events, e) })
	transport := &fakeNotifications{server: ":1.99"}
	n.transport = transport
	target := wire.NotificationTarget{Network: "telegram", ConversationID: "demo"}
	if err := n.send(context.Background(), target, "mail-unread", "OmaChat", "New message"); err != nil {
		t.Fatal(err)
	}
	transport.server = ":1.100"
	n.handleSignal(&dbus.Signal{Sender: ":1.99", Name: notificationInterface + ".ActionInvoked", Body: []any{uint32(1), "default"}})
	if len(events) != 0 {
		t.Fatal("former owner opened a conversation after losing its name")
	}
	n.dismiss(context.Background(), "")
	if len(transport.closed) != 0 || len(n.targets) != 0 {
		t.Fatal("old IDs were closed against the replacement server or retained")
	}
}

func TestSuccessfulUnpairReportsStaticPreferenceCleanupWarning(t *testing.T) {
	d := freshSelectionDaemon(t)
	d.activeServices[wire.NetworkGMessages] = true
	// Corrupt synthetic config makes preference cleanup fail; unpair itself succeeds.
	if err := os.WriteFile(d.paths.ConfigFile(), []byte("not JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	response := d.dispatch(context.Background(), wire.Request{Method: wire.MethodUnpair})
	if !response.OK || d.Status().State != wire.StateUnpaired {
		t.Fatalf("successful unpair was represented as failed: %+v", response)
	}
	warning, ok := response.Result.(map[string]string)
	if !ok || warning["warning"] != "Unpaired. The remembered conversation could not be cleared. Check Settings before pairing again." {
		t.Fatal("cleanup did not return the static warning")
	}
	data, err := os.ReadFile(d.paths.ConfigFile())
	if err != nil || string(data) != "not JSON" {
		t.Fatal("cleanup replaced the corrupt config")
	}
}
