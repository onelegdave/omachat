package telegram

import (
	"github.com/onelegdave/omachat/internal/wire"
	"testing"
)

func TestOnlyNewIncomingTelegramMessagesRequestNotifications(t *testing.T) {
	b, _, events := setupTestTelegram(t)
	msg := Message{ID: 1, ConversationID: 42, Text: "Fixture"}
	b.ingestMessage(msg)
	b.ingestMessage(msg)
	msg.ID, msg.FromMe = 2, true
	b.ingestMessage(msg)
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
