package messenger

import (
	"github.com/onelegdave/omachat/internal/wire"
	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	"testing"
)

func TestMessengerHistoryAndUpdatesDoNotRequestNotifications(t *testing.T) {
	var flags []bool
	b := New(zerolog.Nop(), nil, func(event wire.Event) {
		if event.Event == wire.EventMessage {
			flags = append(flags, event.Notify)
		}
	})
	b.selfID = 99
	page := func(id string, sender int64) *table.LSTable {
		return &table.LSTable{LSUpsertMessage: []*table.LSUpsertMessage{{ThreadKey: 7, MessageId: id, Text: "Fixture", SenderId: sender}}}
	}
	b.handleTable(page("history", 42))
	b.handleTableUpdates(page("incoming", 42), true)
	b.handleTableUpdates(page("incoming", 42), true)
	b.handleTableUpdates(page("outgoing", 99), true)
	if len(flags) != 4 || flags[0] || !flags[1] || flags[2] || flags[3] {
		t.Fatalf("incorrect notification markers: %v", flags)
	}
}
