package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/onelegdave/omachat/internal/wire"
)

func inboxBoundaryFixture(t *testing.T) (*Backend, *MockClient) {
	t.Helper()
	b, _, _, mock := setupTestTelegramWithMock(t)
	b.SetClient(mock)
	b.mu.Lock()
	b.convs["tg:7"] = wire.Conversation{ID: "tg:7", Timestamp: 42, Preview: "invented", Unread: true}
	b.order = []string{"tg:7"}
	b.messages["tg:7"] = []wire.Message{{ID: "tg:42", ConversationID: "tg:7", Timestamp: 42}}
	b.mu.Unlock()
	return b, mock
}

func TestInboxBoundedReadAdvertisedAndAcknowledged(t *testing.T) {
	b, mock := inboxBoundaryFixture(t)
	if !b.Status().BoundedRead {
		t.Fatal("bounded protocol capability missing")
	}
	called := 0
	mock.MarkReadFunc = func(_ context.Context, conversation, message int64) error {
		called++
		if conversation != 7 || message != 42 {
			t.Fatal("provider boundary changed")
		}
		return nil
	}
	if err := b.MarkRead(context.Background(), wire.MarkReadParams{ConversationID: "tg:7", MessageID: "tg:42", ThroughTimestamp: 42}); err != nil {
		t.Fatal(err)
	}
	if called != 1 || b.Conversations(1)[0].Unread {
		t.Fatal("acknowledgement was not applied")
	}
}

func TestInboxBoundedReadKeepsArrivalDuringProviderCallUnread(t *testing.T) {
	for _, timestamp := range []int64{42, 43} {
		b, mock := inboxBoundaryFixture(t)
		mock.MarkReadFunc = func(_ context.Context, conversation, message int64) error {
			b.mu.Lock()
			defer b.mu.Unlock()
			c := b.convs["tg:7"]
			c.Timestamp = timestamp
			// Same timestamp and text still needs a new message ID check.
			b.convs["tg:7"] = c
			b.messages["tg:7"] = append(b.messages["tg:7"], wire.Message{ID: "tg:43", ConversationID: "tg:7", Timestamp: timestamp})
			return nil
		}
		if err := b.MarkRead(context.Background(), wire.MarkReadParams{ConversationID: "tg:7", MessageID: "tg:42", ThroughTimestamp: 42}); err != nil {
			t.Fatal(err)
		}
		if !b.Conversations(1)[0].Unread {
			t.Fatal("new arrival was incorrectly cleared")
		}
	}
}

func TestInboxBoundedReadRejectsUnknownStaleOrMalformedBoundary(t *testing.T) {
	for _, p := range []wire.MarkReadParams{
		{ConversationID: "tg:7", MessageID: "tg:41", ThroughTimestamp: 42},
		{ConversationID: "tg:7", MessageID: "tg:42", ThroughTimestamp: 41},
		{ConversationID: "tg:7", MessageID: "", ThroughTimestamp: 42},
		{ConversationID: "tg:7", MessageID: "tg:42", ThroughTimestamp: -1},
	} {
		b, mock := inboxBoundaryFixture(t)
		mock.MarkReadFunc = func(context.Context, int64, int64) error { t.Fatal("provider called for stale boundary"); return nil }
		if err := b.MarkRead(context.Background(), p); err == nil {
			t.Fatal("invalid boundary accepted")
		}
		if !b.Conversations(1)[0].Unread {
			t.Fatal("rejected boundary cleared unread")
		}
	}
}

func TestInboxBoundedReadProviderFailureKeepsUnread(t *testing.T) {
	b, mock := inboxBoundaryFixture(t)
	mock.MarkReadFunc = func(context.Context, int64, int64) error { return errors.New("invented provider refusal") }
	if err := b.MarkRead(context.Background(), wire.MarkReadParams{ConversationID: "tg:7", MessageID: "tg:42", ThroughTimestamp: 42}); err == nil {
		t.Fatal("provider error ignored")
	}
	if !b.Conversations(1)[0].Unread {
		t.Fatal("failed acknowledgement cleared unread")
	}
}

func TestInboxBoundedReadKeepsLaterKnownMessageAtSameTimestampUnread(t *testing.T) {
	b, mock := inboxBoundaryFixture(t)
	b.mu.Lock()
	b.messages["tg:7"] = append(b.messages["tg:7"], wire.Message{ID: "tg:43", ConversationID: "tg:7", Timestamp: 42})
	b.mu.Unlock()
	mock.MarkReadFunc = func(context.Context, int64, int64) error { return nil }
	if err := b.MarkRead(context.Background(), wire.MarkReadParams{ConversationID: "tg:7", MessageID: "tg:42", ThroughTimestamp: 42}); err != nil {
		t.Fatal(err)
	}
	if !b.Conversations(1)[0].Unread {
		t.Fatal("same-time later message was cleared")
	}
}

func TestInboxAccountIdentityAndAdditiveWire(t *testing.T) {
	g := &GotdClient{}
	if g.AccountIdentity() != "" {
		t.Fatal("guessed account identity")
	}
	g.setAccountIdentity(314159)
	if g.AccountIdentity() != "tg:314159" {
		t.Fatal("wrong account identity")
	}
	raw, err := json.Marshal(wire.Status{AccountIdentity: g.AccountIdentity(), BoundedRead: true})
	if err != nil {
		t.Fatal(err)
	}
	var status map[string]any
	if err := json.Unmarshal(raw, &status); err != nil {
		t.Fatal(err)
	}
	if status["accountIdentity"] != "tg:314159" || status["boundedRead"] != true {
		t.Fatal("wire lost additive fields")
	}
	var p wire.MarkReadParams
	if err := json.Unmarshal([]byte(`{"conversationID":"tg:7","messageID":"tg:42","throughTimestamp":42}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.ThroughTimestamp != 42 {
		t.Fatal("wire lost boundary")
	}
}
