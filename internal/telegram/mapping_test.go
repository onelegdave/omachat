package telegram

import "testing"

func TestMapDialogsOrdersAndLimits(t *testing.T) {
	got := mapDialogs([]Dialog{{ID: 1, Name: "Older", Timestamp: 10}, {ID: 2, Timestamp: 20}, {ID: 3, Name: "Newest", Timestamp: 30}}, 2)
	if len(got) != 2 || got[0].ID != "tg:3" || got[1].ID != "tg:2" {
		t.Fatalf("unexpected dialogs: %+v", got)
	}
	if got[1].Name != "Telegram chat 2" || got[1].ReadOnly {
		t.Fatalf("fallback mapping missing: %+v", got[1])
	}
}

func TestMapMessagesFiltersAndOrders(t *testing.T) {
	got := mapMessages([]Message{{ID: 2, ConversationID: 7, Timestamp: 20}, {ID: 1, ConversationID: 8, Timestamp: 1}, {ID: 3, ConversationID: 7, Timestamp: 10}}, 7)
	if len(got) != 2 || got[0].ID != "tg:3" || got[1].ID != "tg:2" {
		t.Fatalf("unexpected messages: %+v", got)
	}
	if got[0].ConversationID != "tg:7" {
		t.Fatal("conversation ID was not namespaced")
	}
}

func TestMapEmpty(t *testing.T) {
	if got := mapDialogs(nil, 50); len(got) != 0 {
		t.Fatalf("expected empty dialogs, got %v", got)
	}
	if got := mapMessages(nil, 1); len(got) != 0 {
		t.Fatalf("expected empty messages, got %v", got)
	}
}
