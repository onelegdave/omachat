package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
)

func TestHistoryStorePagesOlderMessagesAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	ctx := context.Background()
	store, err := openHistoryStore(path, 64)
	if err != nil {
		t.Fatal(err)
	}
	messages := make([]wire.Message, 150)
	for i := range messages {
		messages[i] = wire.Message{ID: fmt.Sprintf("synthetic-%03d", i), ConversationID: "chat@s.whatsapp.net", Timestamp: int64(i + 1), Text: "synthetic history"}
	}
	if err := store.put(ctx, messages); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}

	store, err = openHistoryStore(path, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	page, err := store.page(ctx, "chat@s.whatsapp.net", 60, "synthetic-090", 91)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 60 || !page.HasMore || page.Messages[len(page.Messages)-1].ID != "synthetic-089" || page.Messages[0].ID != "synthetic-030" {
		t.Fatalf("older page did not survive reopen: count=%d hasMore=%t first=%v last=%v", len(page.Messages), page.HasMore, firstMessageID(page.Messages), lastMessageID(page.Messages))
	}
}

func TestHistoryStoreCreatesPrivateDatabaseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	store, err := openHistoryStoreWithBudget(path, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("history database permissions = %04o, want 0600", got)
	}
}

func TestChatIndexPersistsMetadataWithoutDuplicatingHistoryPayloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "whatsapp_store.json")
	chatID := "chat@s.whatsapp.net"
	message := wire.Message{ID: "m1", ConversationID: chatID, Timestamp: 10, Text: "synthetic"}
	stored := &StoredChatData{
		Conversations:  map[string]wire.Conversation{chatID: {ID: chatID, Name: "Chat", Timestamp: 10}},
		Order:          []string{chatID},
		Messages:       map[string][]wire.Message{chatID: {message}},
		RawMedia:       map[string][]byte{rawMediaKey(chatID, message.ID): []byte("raw-media")},
		ReactionActors: map[string]map[string]string{rawMediaKey(chatID, message.ID): {"actor": "👍"}},
	}
	if err := saveChatIndex(path, stored); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted StoredChatData
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted.Conversations) != 1 || persisted.Conversations[chatID].Name != "Chat" {
		t.Fatalf("conversation metadata was not retained: %+v", persisted.Conversations)
	}
	if len(persisted.Messages) != 0 || len(persisted.RawMedia) != 0 {
		t.Fatalf("history payload was duplicated in JSON: messages=%d raw=%d", len(persisted.Messages), len(persisted.RawMedia))
	}
	if len(persisted.ReactionActors[rawMediaKey(chatID, message.ID)]) != 1 {
		t.Fatalf("hot reaction actor state was not retained: %+v", persisted.ReactionActors)
	}
}

func TestHistoryStoreReadsRawMediaMetadataByMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	store, err := openHistoryStore(path, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	chatID := "media@s.whatsapp.net"
	message := wire.Message{ID: "media-1", ConversationID: chatID, Timestamp: 1, Text: "attachment"}
	want := []byte("synthetic raw attachment metadata")
	if err := store.put(context.Background(), []wire.Message{message}, map[string][]byte{rawMediaKey(chatID, message.ID): want}); err != nil {
		t.Fatal(err)
	}
	got, err := store.getRaw(context.Background(), chatID, message.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("raw media metadata = %q, want %q", got, want)
	}
}

func TestHistoryStorePhysicalBudgetDoesNotExceedLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	store, err := openHistoryStore(path, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	if got := store.maxBytesLimit(); got <= 0 || got > 64*1024*1024 {
		t.Fatalf("history store byte cap = %d, want positive and at most 64 MiB", got)
	}
	large := strings.Repeat("x", 1024*1024)
	for i := range 30 {
		message := wire.Message{ID: fmt.Sprintf("large-%02d", i), ConversationID: "large-chat@s.whatsapp.net", Timestamp: int64(i + 1), Text: large}
		if err := store.put(context.Background(), []wire.Message{message}); err != nil {
			break // Reaching SQLite's configured physical page ceiling is bounded failure.
		}
	}
	var diskBytes int64
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		diskBytes += info.Size()
	}
	if diskBytes > 64*1024*1024 {
		t.Fatalf("history database and SQLite sidecars use %d bytes, exceeding 64 MiB", diskBytes)
	}
}

func TestHistoryStoreEvictsOldUnviewedChatsWithinPhysicalBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	const physicalBudget = int64(4 * 1024 * 1024)
	store, err := openHistoryStoreWithBudget(path, physicalBudget)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	ctx := context.Background()
	payload := strings.Repeat("x", 20*1024)
	for chat := 0; chat < 5; chat++ {
		messages := make([]wire.Message, 20)
		for i := range messages {
			messages[i] = wire.Message{ID: fmt.Sprintf("m-%02d-%02d", chat, i), ConversationID: fmt.Sprintf("chat-%02d@s.whatsapp.net", chat), Timestamp: int64(i + 1), Text: payload}
		}
		if err := store.put(ctx, messages); err != nil {
			t.Fatalf("write synthetic chat %d: %v", chat, err)
		}
		if chat == 1 {
			if err := store.markViewed(ctx, "chat-01@s.whatsapp.net", time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
	}
	page, err := store.page(ctx, "chat-01@s.whatsapp.net", 20, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 20 {
		t.Fatalf("recently viewed chat lost messages during eviction: retained %d", len(page.Messages))
	}
	var evictedBefore int64
	if err := store.db.QueryRow(`SELECT evicted_before FROM history_chat_state WHERE chat_id=?`, "chat-00@s.whatsapp.net").Scan(&evictedBefore); err != nil {
		t.Fatal(err)
	}
	if evictedBefore == 0 {
		t.Fatal("least-recently-viewed chat has no recorded eviction boundary")
	}
	var diskBytes int64
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		diskBytes += info.Size()
	}
	if diskBytes > physicalBudget {
		t.Fatalf("history database and sidecars use %d bytes, over %d byte budget", diskBytes, physicalBudget)
	}
}

func TestHistoryStoreBudgetShrinkEvictsLocallyAndHonorsNewPageCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	const initialBudget = int64(4 * 1024 * 1024)
	store, err := openHistoryStoreWithBudget(path, initialBudget)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	text := strings.Repeat("z", 16*1024)
	for chat := 0; chat < 4; chat++ {
		messages := make([]wire.Message, 12)
		for i := range messages {
			messages[i] = wire.Message{ID: fmt.Sprintf("shrink-%d-%d", chat, i), ConversationID: fmt.Sprintf("shrink-chat-%d", chat), Timestamp: int64(i + 1), Text: text}
		}
		if err := store.put(context.Background(), messages); err != nil {
			t.Fatal(err)
		}
	}
	const smallerBudget = int64(2 * 1024 * 1024)
	if err := store.setBudgetBytes(context.Background(), smallerBudget); err != nil {
		t.Fatalf("shrink history cache: %v", err)
	}
	if got, want := store.maxBytesLimit(), smallerBudget/3; got != want {
		t.Fatalf("new database-page budget = %d, want %d", got, want)
	}
	var pages int64
	if err := store.db.QueryRow(`PRAGMA page_count`).Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if pages*sqlitePageBytes > smallerBudget/3 {
		t.Fatalf("database page count %d exceeds reduced cap %d", pages, smallerBudget/3)
	}
	var diskBytes int64
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		diskBytes += info.Size()
	}
	if diskBytes > smallerBudget {
		t.Fatalf("shrunk history database and sidecars use %d bytes, over %d byte budget", diskBytes, smallerBudget)
	}
}

func firstMessageID(messages []wire.Message) string {
	if len(messages) == 0 {
		return ""
	}
	return messages[0].ID
}

func lastMessageID(messages []wire.Message) string {
	if len(messages) == 0 {
		return ""
	}
	return messages[len(messages)-1].ID
}
