package messenger

import (
	"testing"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	waTypes "go.mau.fi/whatsmeow/types"

	"github.com/onelegdave/omachat/internal/wire"
)

// An encrypted message that arrived before its thread was known left an empty stand-in
// conversation holding an unread flag nothing could clear (2026-09-24).
func TestStandInFoldsIntoTheRealThread(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	const real, user int64 = 61572712178626, 1104550122108922
	jid := waTypes.NewJID("1104550122108922", waTypes.MessengerServer)
	b.mu.Lock()
	defer b.mu.Unlock()
	// The stand-in, as the early encrypted path files it: keyed by the JID user, unread.
	b.setMappingLocked(user, user, table.ENCRYPTED_OVER_WA_ONE_TO_ONE)
	b.convs["1104550122108922"] = wire.Conversation{ID: "1104550122108922", Name: "Invented Person", Unread: true}
	// The real thread already holds the message the owner has read.
	b.convs["61572712178626"] = wire.Conversation{ID: "61572712178626", Name: "Invented Person", Timestamp: 5}
	b.messages["61572712178626"] = []wire.Message{{ID: "m1", ConversationID: "61572712178626", Timestamp: 5}}
	b.recountUnreadLocked()
	if b.status.Unread != 1 {
		t.Fatalf("setup: unread = %d, want 1", b.status.Unread)
	}

	b.setMappingLocked(real, user, table.ENCRYPTED_OVER_WA_ONE_TO_ONE)

	if _, ok := b.convs["1104550122108922"]; ok {
		t.Fatal("the stand-in conversation is still listed")
	}
	if b.status.Unread != 0 {
		t.Fatalf("unread = %d, want 0: an empty stand-in must not keep the badge", b.status.Unread)
	}
	if got := b.jidToThread[jid.ToNonAD().String()]; got != real {
		t.Fatalf("the JID points at %d, want the real thread", got)
	}
}

func TestStandInMessagesMoveAndStayUnread(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	const real, user int64 = 700, 900
	b.mu.Lock()
	defer b.mu.Unlock()
	b.setMappingLocked(user, user, table.ENCRYPTED_OVER_WA_ONE_TO_ONE)
	b.convs["900"] = wire.Conversation{ID: "900", Preview: "new", Timestamp: 9, Unread: true}
	b.messages["900"] = []wire.Message{{ID: "m2", ConversationID: "900", Timestamp: 9}, {ID: "m1", ConversationID: "900", Timestamp: 5}}
	b.convs["700"] = wire.Conversation{ID: "700", Preview: "old", Timestamp: 5}
	b.messages["700"] = []wire.Message{{ID: "m1", ConversationID: "700", Timestamp: 5}}

	b.setMappingLocked(real, user, table.ENCRYPTED_OVER_WA_ONE_TO_ONE)

	msgs := b.messages["700"]
	if len(msgs) != 2 || msgs[1].ID != "m2" || msgs[1].ConversationID != "700" {
		t.Fatalf("messages not merged in order without duplicates: %+v", msgs)
	}
	if c := b.convs["700"]; !c.Unread || c.Preview != "new" || c.Timestamp != 9 {
		t.Fatalf("a newer unread message must carry its flag and preview: %+v", c)
	}
}

func TestAGenuineThreadSharingTheNumberIsNotFolded(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.convs["900"] = wire.Conversation{ID: "900", Unread: true} // an ordinary thread, no JID mapping of its own
	b.setMappingLocked(700, 900, table.ENCRYPTED_OVER_WA_ONE_TO_ONE)
	if _, ok := b.convs["900"]; !ok {
		t.Fatal("a genuine thread was folded away")
	}
}
