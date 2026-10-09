package whatsapp

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
	"unsafe"

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

func TestBackendReorderBoundsHotHistoryByPayloadBytes(t *testing.T) {
	paths := &appStore.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	backend := New(zerolog.Nop(), paths, nil)
	chatID := "memory@s.whatsapp.net"
	backend.convs[chatID] = wire.Conversation{ID: chatID, Timestamp: 3}
	backend.messages[chatID] = []wire.Message{
		{ID: "old", ConversationID: chatID, Timestamp: 1, Text: strings.Repeat("o", 6*1024*1024)},
		{ID: "middle", ConversationID: chatID, Timestamp: 2, Text: strings.Repeat("m", 6*1024*1024)},
		{ID: "new", ConversationID: chatID, Timestamp: 3, Text: strings.Repeat("n", 6*1024*1024)},
	}
	oldKey := rawMediaKey(chatID, "old")
	backend.rawMsgs[oldKey] = &waE2E.Message{Conversation: proto.String("synthetic raw metadata")}
	backend.reactionActors[oldKey] = map[string]string{"actor": "👍"}

	backend.reorderLocked()
	backend.saveStoreLocked()
	if got := backend.hotHistoryMemoryBytesLocked(); got > maxHotHistoryMemoryBytes {
		t.Fatalf("backend retains %d history bytes, over %d-byte cap", got, maxHotHistoryMemoryBytes)
	}
	if got := backend.messages[chatID]; len(got) != 2 || got[0].ID != "middle" || got[1].ID != "new" {
		t.Fatalf("backend did not retain the newest complete messages: %+v", got)
	}
	if _, ok := backend.rawMsgs[oldKey]; ok {
		t.Fatal("raw protobuf for an evicted message remains in memory")
	}
	if _, ok := backend.reactionActors[oldKey]; ok {
		t.Fatal("reaction actors for an evicted message remain in memory")
	}
}

func TestHotHistoryMemoryAccountingIncludesMessageSliceCapacity(t *testing.T) {
	paths := &appStore.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	backend := New(zerolog.Nop(), paths, nil)
	chatID := "capacity@s.whatsapp.net"
	backend.mu.Lock()
	before := backend.hotHistoryMemoryBytesLocked()
	backing := make([]wire.Message, 0, 128)
	backend.messages[chatID] = backing
	after := backend.hotHistoryMemoryBytesLocked()
	backend.mu.Unlock()
	actualSliceBytes := int64(cap(backing)) * int64(unsafe.Sizeof(wire.Message{}))
	if after-before < actualSliceBytes {
		t.Fatalf("memory estimate increased by %d bytes for a %d-byte message slice backing array", after-before, actualSliceBytes)
	}
}

func TestTruncateHistoryTextRespectsByteLimitAndUTF8(t *testing.T) {
	value := strings.Repeat("界", 20)
	got := truncateHistoryText(value, 7)
	if len(got) > 7 {
		t.Fatalf("truncated preview uses %d bytes, over the 7-byte limit", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncated preview is not valid UTF-8: %q", got)
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

func TestLegacyMigrationResumesAfterInterruptedSQLiteBatch(t *testing.T) {
	paths := &appStore.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	chatID := "migration@s.whatsapp.net"
	messages := make([]wire.Message, 10)
	for i := range messages {
		messages[i] = wire.Message{
			ID:             fmt.Sprintf("legacy-%03d", i),
			ConversationID: chatID,
			Timestamp:      int64(i + 1),
			Text:           strings.Repeat("x", 1<<20),
		}
	}
	legacy := &StoredChatData{
		Conversations: map[string]wire.Conversation{chatID: {ID: chatID, Name: "Legacy"}},
		Order:         []string{chatID},
		Messages:      map[string][]wire.Message{chatID: messages},
	}
	if err := saveChatStore(paths.WhatsAppStoreFile(), legacy); err != nil {
		t.Fatal(err)
	}
	sourceBefore, err := os.ReadFile(paths.WhatsAppStoreFile())
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", paths.WhatsAppHistoryFile())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE history_messages (
		chat_id TEXT NOT NULL,
		message_id TEXT NOT NULL,
		ts INTEGER NOT NULL,
		payload BLOB NOT NULL,
		raw_payload BLOB,
		PRIMARY KEY(chat_id, message_id)
	)`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER interrupt_legacy_migration BEFORE INSERT ON history_messages
		WHEN NEW.message_id='legacy-005'
		BEGIN SELECT RAISE(ABORT, 'synthetic interrupted migration'); END`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	interrupted := New(zerolog.Nop(), paths, nil)
	t.Cleanup(interrupted.Stop)
	if err := interrupted.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	var partialCount int
	if err := interrupted.historyStore.db.QueryRow(`SELECT COUNT(*) FROM history_messages`).Scan(&partialCount); err != nil {
		t.Fatal(err)
	}
	if partialCount <= 0 || partialCount >= len(messages) {
		t.Fatalf("interrupted migration stored %d rows, want a non-empty partial batch", partialCount)
	}
	sourceAfter, err := os.ReadFile(paths.WhatsAppStoreFile())
	if err != nil {
		t.Fatal(err)
	}
	if string(sourceAfter) != string(sourceBefore) {
		t.Fatal("interrupted migration changed the legacy source before all rows were stored")
	}
	if _, err := interrupted.historyStore.db.Exec(`DROP TRIGGER interrupt_legacy_migration`); err != nil {
		t.Fatal(err)
	}
	interrupted.Stop()

	recovered := New(zerolog.Nop(), paths, nil)
	t.Cleanup(recovered.Stop)
	if err := recovered.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	var recoveredCount int
	if err := recovered.historyStore.db.QueryRow(`SELECT COUNT(*) FROM history_messages`).Scan(&recoveredCount); err != nil {
		t.Fatal(err)
	}
	if recoveredCount != len(messages) {
		t.Fatalf("recovery retained %d history rows, want %d", recoveredCount, len(messages))
	}
	indexed, err := loadChatStore(paths.WhatsAppStoreFile())
	if err != nil {
		t.Fatal(err)
	}
	if len(indexed.Messages[chatID]) != 0 || indexed.Conversations[chatID].Name != "Legacy" {
		t.Fatalf("successful recovery did not retire message payloads while preserving metadata: %+v", indexed)
	}
	recovered.Stop()

	reopened := New(zerolog.Nop(), paths, nil)
	t.Cleanup(reopened.Stop)
	if err := reopened.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := reopened.historyStore.db.QueryRow(`SELECT COUNT(*) FROM history_messages`).Scan(&recoveredCount); err != nil {
		t.Fatal(err)
	}
	if recoveredCount != len(messages) {
		t.Fatalf("reopen duplicated or lost migrated rows: got %d, want %d", recoveredCount, len(messages))
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
	if got := cap(backend.messages[chatID]); got > maxPersistedMessages {
		t.Fatalf("hot message window retains slice capacity %d, want at most %d", got, maxPersistedMessages)
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
