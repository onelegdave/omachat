package whatsapp

import (
	"path/filepath"
	"testing"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/onelegdave/omachat/internal/wire"
)

// The status filter must drop only status broadcasts. Ordinary chats arriving
// in the same live stream or history batch keep working.
func TestStatusFilterKeepsOrdinaryChatsOnBothRoutes(t *testing.T) {
	backend, _, _ := setupTestBackend(t)
	friend := types.NewJID("15550001111", types.DefaultUserServer)
	statusText := "invented story text"

	backend.handleEvent(&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: types.StatusBroadcastJID, Sender: friend},
			ID:            "status-live",
		},
		Message: &waE2E.Message{Conversation: proto.String(statusText)},
	})
	backend.handleEvent(&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: types.StatusBroadcastJID, Sender: friend},
			ID:            "status-reaction",
		},
		Message: &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{
			Key:  &waCommon.MessageKey{ID: proto.String("status-live"), RemoteJID: proto.String(types.StatusBroadcastJID.String())},
			Text: proto.String("👍"),
		}},
	})
	backend.handleEvent(&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: friend, Sender: friend},
			ID:            "chat-live",
		},
		Message: &waE2E.Message{Conversation: proto.String("ordinary live text")},
	})

	history := &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{
		{
			ID: proto.String(types.StatusBroadcastJID.String()),
			Messages: []*waHistorySync.HistorySyncMsg{{Message: &waWeb.WebMessageInfo{
				Key:     &waCommon.MessageKey{ID: proto.String("status-history"), RemoteJID: proto.String(types.StatusBroadcastJID.String())},
				Message: &waE2E.Message{Conversation: proto.String(statusText)},
			}}},
		},
		{
			ID: proto.String("15550002222@s.whatsapp.net"),
			Messages: []*waHistorySync.HistorySyncMsg{{Message: &waWeb.WebMessageInfo{
				Key:              &waCommon.MessageKey{ID: proto.String("chat-history"), RemoteJID: proto.String("15550002222@s.whatsapp.net")},
				Message:          &waE2E.Message{Conversation: proto.String("ordinary history text")},
				MessageTimestamp: proto.Uint64(1_700_000_000),
			}}},
		},
	}}
	if err := backend.ingestHistorySyncMode(backend.gen, history, false); err != nil {
		t.Fatal(err)
	}

	got := map[string]wire.Conversation{}
	for _, conv := range backend.Conversations(10) {
		got[conv.ID] = conv
	}
	if _, ok := got[types.StatusBroadcastJID.String()]; ok || len(got) != 2 {
		t.Fatalf("conversations = %+v, want only the two ordinary chats", got)
	}
	if got[friend.String()].Preview != "ordinary live text" || got["15550002222@s.whatsapp.net"].Preview != "ordinary history text" {
		t.Fatalf("ordinary chats lost their messages: %+v", got)
	}
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	if len(backend.messages[types.StatusBroadcastJID.String()]) != 0 || len(backend.reactionActors) != 0 {
		t.Fatalf("status content retained: messages=%v reactions=%v", backend.messages[types.StatusBroadcastJID.String()], backend.reactionActors)
	}
}

// A store written by an earlier release can already hold the status
// broadcast as a conversation. Loading it must drop that conversation with
// its messages and media keys and keep the rest.
func TestStoredStatusConversationIsDroppedOnLoad(t *testing.T) {
	status := types.StatusBroadcastJID.String()
	friend := "15550001111@s.whatsapp.net"
	stored := newStoredChatData()
	stored.Conversations[status] = wire.Conversation{ID: status, Name: "status", Timestamp: 20}
	stored.Conversations[friend] = wire.Conversation{ID: friend, Name: "Invented Friend", Timestamp: 10}
	stored.Order = []string{status, friend}
	stored.Messages[status] = []wire.Message{{ID: "s1", ConversationID: status, Text: "invented story", Timestamp: 20}}
	stored.Messages[friend] = []wire.Message{{ID: "f1", ConversationID: friend, Text: "hello", Timestamp: 10}}
	stored.RawMedia[rawMediaKey(status, "s1")] = []byte{1}
	stored.RawMedia[rawMediaKey(friend, "f1")] = []byte{2}
	stored.ReactionActors[rawMediaKey(status, "s1")] = map[string]string{friend: "👍"}

	path := filepath.Join(t.TempDir(), "whatsapp.json")
	if err := saveChatStore(path, stored); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadChatStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Conversations[status]; ok || len(loaded.Conversations) != 1 {
		t.Fatalf("conversations = %+v", loaded.Conversations)
	}
	if len(loaded.Order) != 1 || loaded.Order[0] != friend {
		t.Fatalf("order = %v", loaded.Order)
	}
	if _, ok := loaded.Messages[status]; ok {
		t.Fatalf("status messages kept: %+v", loaded.Messages[status])
	}
	if _, ok := loaded.RawMedia[rawMediaKey(status, "s1")]; ok {
		t.Fatal("status media key kept")
	}
	if _, ok := loaded.ReactionActors[rawMediaKey(status, "s1")]; ok {
		t.Fatal("status reactions kept")
	}
	if len(loaded.Messages[friend]) != 1 || loaded.RawMedia[rawMediaKey(friend, "f1")] == nil {
		t.Fatalf("ordinary chat lost data: %+v", loaded)
	}
}
