package whatsapp

import (
	"github.com/onelegdave/omachat/internal/wire"
	"testing"
)

func TestOnlyNewIncomingWhatsAppMessagesRequestNotifications(t *testing.T) {
	b, _, events := setupTestBackend(t)
	msg := wire.Message{ID: "fixture-1", ConversationID: "fixture@s.whatsapp.net", Text: "Fixture"}
	b.appendMessage(msg, nil)
	b.appendMessage(msg, nil)
	msg.ID, msg.FromMe = "fixture-2", true
	b.appendMessage(msg, nil)
	var flags []bool
	for len(events) > 0 {
		event := <-events
		if event.Event == wire.EventMessage {
			flags = append(flags, event.Notify)
		}
	}
	if len(flags) != 3 || !flags[0] || flags[1] || flags[2] {
		t.Fatalf("incorrect notification markers: %v", flags)
	}
}
