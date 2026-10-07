package whatsapp

import (
	"context"
	"fmt"
	"testing"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

func TestWhatsAppMessagesReadsPersistedSQLitePages(t *testing.T) {
	paths := &appStore.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	backend := New(zerolog.Nop(), paths, nil)
	mock := NewMockClient()
	store, err := openHistoryStore(paths.WhatsAppHistoryFile(), 64)
	if err != nil {
		t.Fatal(err)
	}
	messages := make([]wire.Message, 150)
	for i := range messages {
		messages[i] = wire.Message{ID: fmt.Sprintf("offline-%03d", i), ConversationID: "123@g.us", Timestamp: int64(i + 1), Text: "synthetic offline history"}
	}
	if err := store.put(context.Background(), messages); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	backend.SetClient(mock, false)
	if err := backend.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := backend.Messages(context.Background(), wire.MessagesParams{ConversationID: "123@g.us", Count: 60, CursorID: "offline-120", CursorTime: 121})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 60 || !result.HasMore || result.Messages[0].ID != "offline-060" || result.Messages[59].ID != "offline-119" {
		t.Fatalf("persisted older page = count %d, hasMore=%t, first=%s, last=%s", len(result.Messages), result.HasMore, result.Messages[0].ID, result.Messages[len(result.Messages)-1].ID)
	}
}

func TestLegacyChatStoreMigratesThenRetiresDuplicatedPayloads(t *testing.T) {
	paths := &appStore.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	chatID := "legacy@s.whatsapp.net"
	legacy := newStoredChatData()
	legacy.Conversations[chatID] = wire.Conversation{ID: chatID, Name: "Legacy", Timestamp: 20}
	legacy.Order = []string{chatID}
	legacy.Messages[chatID] = []wire.Message{{ID: "legacy-1", ConversationID: chatID, Timestamp: 20, Text: "synthetic migrated message"}}
	if err := saveChatStore(paths.WhatsAppStoreFile(), legacy); err != nil {
		t.Fatal(err)
	}
	backend := New(zerolog.Nop(), paths, nil)
	backend.SetClient(NewMockClient(), false)
	if err := backend.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer backend.Stop()
	indexed, err := loadChatStore(paths.WhatsAppStoreFile())
	if err != nil {
		t.Fatal(err)
	}
	messageCount := 0
	for _, messages := range indexed.Messages {
		messageCount += len(messages)
	}
	if messageCount != 0 || len(indexed.RawMedia) != 0 {
		t.Fatalf("legacy message payloads remain duplicated after migration: messages=%d raw=%d", messageCount, len(indexed.RawMedia))
	}
	if indexed.Conversations[chatID].Name != "Legacy" {
		t.Fatalf("conversation index lost during migration: %+v", indexed.Conversations)
	}
	page, err := backend.Messages(context.Background(), wire.MessagesParams{ConversationID: chatID, Count: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != "legacy-1" || page.Messages[0].Text != "synthetic migrated message" {
		t.Fatalf("migrated message not served from SQLite: %+v", page.Messages)
	}
	backend.Stop()
	if err := backend.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	page, err = backend.Messages(context.Background(), wire.MessagesParams{ConversationID: chatID, Count: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != "legacy-1" {
		t.Fatalf("reopened migrated history changed: %+v", page.Messages)
	}
	var count int
	if err := backend.historyStore.db.QueryRow(`SELECT COUNT(*) FROM history_messages WHERE chat_id=?`, chatID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("migration duplicated history rows after reopen: %d", count)
	}
}

func TestWhatsAppHistorySyncPersistsPagesBeyondHotMemoryWindow(t *testing.T) {
	paths := &appStore.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	backend := New(zerolog.Nop(), paths, nil)
	mock := NewMockClient()
	backend.SetClient(mock, false)
	if err := backend.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	chatID := "history@s.whatsapp.net"
	entries := make([]*waHistorySync.HistorySyncMsg, 130)
	for i := range entries {
		entries[i] = &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
			Key:              &waCommon.MessageKey{ID: proto.String(fmt.Sprintf("history-%03d", i+1))},
			MessageTimestamp: proto.Uint64(uint64(i + 1)),
			Message:          &waE2E.Message{Conversation: proto.String(fmt.Sprintf("message %03d", i+1))},
		}}
	}
	mock.TriggerEvent(&events.HistorySync{Data: &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{{ID: proto.String(chatID), Messages: entries}}}})
	if got := len(backend.messages[chatID]); got != maxPersistedMessages {
		t.Fatalf("hot message window = %d, want %d", got, maxPersistedMessages)
	}
	page, err := backend.Messages(context.Background(), wire.MessagesParams{ConversationID: chatID, Count: 60, CursorID: "history-121", CursorTime: 121_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 60 || page.Messages[0].ID != "history-061" || page.Messages[59].ID != "history-120" || !page.HasMore {
		t.Fatalf("SQLite page lost history outside hot window: count=%d first=%s last=%s hasMore=%t", len(page.Messages), firstMessageID(page.Messages), lastMessageID(page.Messages), page.HasMore)
	}
}

func TestHistorySyncKeepsConversationIndexBeyondHotMessageWindow(t *testing.T) {
	paths := &appStore.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	backend := New(zerolog.Nop(), paths, nil)
	mock := NewMockClient()
	backend.SetClient(mock, false)
	if err := backend.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer backend.Stop()
	conversations := make([]*waHistorySync.Conversation, 60)
	for i := range conversations {
		chatID := fmt.Sprintf("chat-%02d@s.whatsapp.net", i)
		message := &waWeb.WebMessageInfo{
			Key:              &waCommon.MessageKey{ID: proto.String(fmt.Sprintf("id-%02d", i))},
			MessageTimestamp: proto.Uint64(uint64(i + 1)),
			Message:          &waE2E.Message{Conversation: proto.String("synthetic history")},
		}
		conversations[i] = &waHistorySync.Conversation{ID: proto.String(chatID), Messages: []*waHistorySync.HistorySyncMsg{{Message: message}}}
	}
	mock.TriggerEvent(&events.HistorySync{Data: &waHistorySync.HistorySync{Conversations: conversations}})
	if got := len(backend.Conversations(100)); got != 60 {
		t.Fatalf("conversation list retained %d chats, want 60", got)
	}
	if got := len(backend.messages); got != maxHotMessageConversations {
		t.Fatalf("hot in-memory chat windows = %d, want %d", got, maxHotMessageConversations)
	}
	page, err := backend.Messages(context.Background(), wire.MessagesParams{ConversationID: "chat-00@s.whatsapp.net", Count: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != "id-00" {
		t.Fatalf("history outside hot memory window was not served: %+v", page.Messages)
	}
}
