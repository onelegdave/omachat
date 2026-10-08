package whatsapp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

func TestHistoryStorePageCeilingReservesIndexAndSQLitePeakSpace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	const physicalBudget = int64(64 * 1024 * 1024)
	indexFileLimit := historyIndexBudgetBytes(physicalBudget)
	store, err := openHistoryStoreWithBudget(path, physicalBudget)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()

	// Reserve three database-sized footprints for the main DB plus SQLite's
	// bounded compaction/journal peak, and two index-sized footprints for an
	// atomic replacement while the old index still exists.
	peakBytes := 3*store.maxBytesLimit() + 2*indexFileLimit + historySQLiteSafetyReserveBytes
	if peakBytes > physicalBudget {
		t.Fatalf("reserved peak storage %d bytes exceeds the %d-byte cache budget", peakBytes, physicalBudget)
	}
}

func TestOpenHistoryStoreRejectsOversizedExistingDatabaseBeforeMigrationWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE padding(value BLOB)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO padding(value) VALUES(zeroblob(20971520))`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	budget := int64(64 * 1024 * 1024)
	if before.Size() <= historyDatabaseBudgetBytes(budget) {
		t.Fatalf("synthetic database size %d did not exceed its %d-byte limit", before.Size(), historyDatabaseBudgetBytes(budget))
	}
	if history, err := openHistoryStoreWithBudget(path, budget); err == nil {
		_ = history.close()
		t.Fatal("oversized existing database opened under a smaller budget")
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() > before.Size() {
		t.Fatalf("failed open grew oversized database from %d to %d bytes", before.Size(), after.Size())
	}
}

func TestHistoryStoreCreatesPrivateDatabaseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	store, err := openHistoryStoreWithBudget(path, 64*1024*1024)
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

func TestHistoryStoreKeepsTemporaryTablesInMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	store, err := openHistoryStoreWithBudget(path, 64*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	var tempStore int
	if err := store.db.QueryRow(`PRAGMA temp_store`).Scan(&tempStore); err != nil {
		t.Fatal(err)
	}
	if tempStore != 2 {
		t.Fatalf("SQLite temp_store = %d, want MEMORY (2)", tempStore)
	}
}

func TestHistoryStorePagesWithinByteCeilingWithoutSkippingCursorRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	store, err := openHistoryStore(path, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	chatID := "large@s.whatsapp.net"
	rawPayload := bytes.Repeat([]byte("r"), 1<<20)
	messages := make([]wire.Message, 3)
	rawByMessage := make(map[string][]byte)
	for i := range messages {
		id := fmt.Sprintf("large-%d", i+1)
		messages[i] = wire.Message{ID: id, ConversationID: chatID, Timestamp: int64(i + 1), Text: strings.Repeat("x", 1<<20)}
		rawByMessage[rawMediaKey(chatID, id)] = rawPayload
	}
	if err := store.put(context.Background(), messages, rawByMessage); err != nil {
		t.Fatal(err)
	}
	page, err := store.page(context.Background(), chatID, 60, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	retainedBytes := int64(historyPageContainerOverhead)
	for _, message := range page.Messages {
		retainedBytes += retainedMessageBytes(message) + int64(len(page.Raw[rawMediaKey(chatID, message.ID)])) + historyPageEntryOverhead
	}
	if retainedBytes > maxHistoryPageBytes {
		t.Fatalf("history page retains %d bytes, above %d-byte page limit", retainedBytes, maxHistoryPageBytes)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != "large-3" || !page.HasMore {
		t.Fatalf("first byte-bounded page = %+v, want latest row and HasMore", page)
	}
	next, err := store.page(context.Background(), chatID, 60, page.Messages[0].ID, page.Messages[0].Timestamp)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Messages) != 1 || next.Messages[0].ID != "large-2" {
		t.Fatalf("byte-bounded continuation skipped a row: %+v", next.Messages)
	}
}

func TestHistoryStorePageRejectsAttachmentAmplificationBeforeDecode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	store, err := openHistoryStore(path, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()

	attachments := strings.TrimSuffix(strings.Repeat("{},", 10_000), ",")
	payload := []byte(fmt.Sprintf("{%q:%q,%q:%q,%q:1,%q:[%s]}", "id", "oversized", "conversationID", "chat@s.whatsapp.net", "timestamp", "attachments", attachments))
	if _, err := store.db.Exec(`INSERT INTO history_messages(chat_id,message_id,ts,payload) VALUES(?,?,?,?)`, "chat@s.whatsapp.net", "oversized", 1, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := store.page(context.Background(), "chat@s.whatsapp.net", 60, "", 0); err == nil {
		t.Fatal("decoded a compact JSON row with unbounded attachment-array amplification")
	}
}

func TestHistoryStoreClampsRequestedPageCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	store, err := openHistoryStore(path, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	chatID := "count@s.whatsapp.net"
	messages := make([]wire.Message, maxHistoryPageMessages+5)
	for i := range messages {
		messages[i] = wire.Message{ID: fmt.Sprintf("count-%d", i), ConversationID: chatID, Timestamp: int64(i + 1), Text: "synthetic"}
	}
	if err := store.put(context.Background(), messages); err != nil {
		t.Fatal(err)
	}
	page, err := store.page(context.Background(), chatID, maxHistoryPageMessages+1000, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != maxHistoryPageMessages || !page.HasMore {
		t.Fatalf("oversized page request returned %d messages, hasMore=%t; want %d and true", len(page.Messages), page.HasMore, maxHistoryPageMessages)
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

func TestChatIndexLimitPreservesPreviousIndexOnOversize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "whatsapp_store.json")
	previous := []byte(`{"version":"previous"}`)
	if err := os.WriteFile(path, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	chatID := "large@s.whatsapp.net"
	stored := &StoredChatData{
		Conversations: map[string]wire.Conversation{chatID: {ID: chatID, Name: strings.Repeat("x", 1024)}},
		Order:         []string{chatID},
		Messages:      map[string][]wire.Message{},
	}
	if err := saveChatIndexWithLimit(path, stored, 256); err == nil {
		t.Fatal("oversized conversation index unexpectedly replaced the previous file")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(previous) {
		t.Fatalf("failed bounded write changed previous index: %q", got)
	}
}

func TestChatIndexWriteRemovesOrphanedAtomicTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "whatsapp_store.json")
	orphan := filepath.Join(dir, ".omachat-whatsapp-history-orphan.tmp")
	if err := os.WriteFile(orphan, []byte("partial synthetic index"), 0o600); err != nil {
		t.Fatal(err)
	}
	chatID := "chat@s.whatsapp.net"
	stored := &StoredChatData{
		Conversations: map[string]wire.Conversation{chatID: {ID: chatID, Name: "Chat"}},
		Order:         []string{chatID},
		Messages:      map[string][]wire.Message{},
	}
	if err := saveChatIndexWithLimit(path, stored, 1024); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphaned history temp remains after atomic save: %v", err)
	}
}

func TestOpenHistoryStoreRemovesOrphanedIndexTempBeforeInitialization(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "whatsapp_store.json")
	orphan := filepath.Join(dir, historyIndexTempPrefix+"crash.tmp")
	if err := os.WriteFile(orphan, []byte("synthetic interrupted index write"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := openHistoryStore(filepath.Join(dir, historyDatabaseFileName), 64)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	if _, err := os.Lstat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphaned index temp remains after history initialization: %v", err)
	}
	if total, err := historyArtifactBytes(indexPath); err != nil || total > 64*1024*1024 {
		t.Fatalf("initialized history artifacts use %d bytes (err=%v), want at most 64 MiB", total, err)
	}
}

func TestHistoryArtifactBytesTreatsMissingDirectoryAsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-created", "whatsapp_store.json")
	got, err := historyArtifactBytes(path)
	if err != nil || got != 0 {
		t.Fatalf("missing history directory measured as %d bytes with err=%v, want 0,nil", got, err)
	}
}

func TestHistoryArtifactBytesRejectsSymlinkedOwnedTemp(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "whatsapp_store.json")
	target := filepath.Join(dir, "ordinary-file")
	if err := os.WriteFile(target, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	temp := filepath.Join(dir, historyIndexTempPrefix+"link.tmp")
	if err := os.Symlink(target, temp); err != nil {
		t.Fatal(err)
	}
	if _, err := historyArtifactBytes(indexPath); err == nil {
		t.Fatal("counted an owned temporary symlink as a regular history artifact")
	}
}

func TestHistoryIndexAtomicWritePreservesSourceWhenTempSyncFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "whatsapp_store.json")
	original := []byte(`{"version":"legacy"}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	syncErr := errors.New("synthetic temp sync failure")
	err := writeHistoryIndexAtomicallyWithHooks(path, []byte(`{"version":"replacement"}`), historyIndexWriteHooks{
		syncFile: func(*os.File) error { return syncErr },
		rename:   os.Rename,
		syncDir:  func(string) error { t.Fatal("directory sync ran after temp sync failed"); return nil },
	})
	if !errors.Is(err, syncErr) {
		t.Fatalf("atomic write error = %v, want temp sync failure", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("failed temp sync changed legacy source to %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), historyIndexTempPrefix) {
			t.Fatalf("failed atomic write left temporary artifact %q", entry.Name())
		}
	}
}

func TestHistoryIndexAtomicWriteSyncsDirectoryAfterDurableRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "whatsapp_store.json")
	var order []string
	err := writeHistoryIndexAtomicallyWithHooks(path, []byte(`{"version":"durable"}`), historyIndexWriteHooks{
		syncFile: func(file *os.File) error {
			order = append(order, "file-sync")
			return file.Sync()
		},
		rename: func(oldPath, newPath string) error {
			order = append(order, "rename")
			return os.Rename(oldPath, newPath)
		},
		syncDir: func(path string) error {
			order = append(order, "directory-sync")
			got, err := os.ReadFile(filepath.Join(path, "whatsapp_store.json"))
			if err != nil {
				return err
			}
			if string(got) != `{"version":"durable"}` {
				return fmt.Errorf("directory sync observed unexpected replacement contents %q", got)
			}
			return syncHistoryDirectory(path)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"file-sync", "rename", "directory-sync"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("durability operation order = %v, want %v", order, want)
	}
}

func TestLegacyJSONWriteRejectsOverBudgetAtomicPeak(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "whatsapp_store.json")
	original := []byte("previous legacy history")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(dir, "whatsapp_history.sqlite")
	for name, size := range map[string]int{"": 50, "-journal": 20, "-wal": 10, "-shm": 8} {
		if err := os.WriteFile(database+name, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	orphan := filepath.Join(dir, historyIndexTempPrefix+"crash.tmp")
	if err := os.WriteFile(orphan, []byte("stale temp"), 0o600); err != nil {
		t.Fatal(err)
	}
	stored := newStoredChatData()
	chatID := "chat@s.whatsapp.net"
	stored.Conversations[chatID] = wire.Conversation{ID: chatID, Name: strings.Repeat("n", 100)}
	newJSON, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	currentBytes := int64(len(original) + 50 + 20 + 10 + 8)
	budget := int64(len(newJSON)) + currentBytes/2
	if err := saveChatStoreWithBudget(path, stored, budget); err == nil {
		t.Fatalf("atomic JSON replacement exceeded %d-byte history limit without error", budget)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("rejected bounded write replaced legacy cache: got %q, want %q", got, original)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan temp was not removed before the budget check: %v", err)
	}
}

func TestTrimStoredHotMessagesToByteBudgetEvictsOldestWithAuxiliaryPayloads(t *testing.T) {
	const byteBudget = int64(16 * 1024 * 1024)
	chatID := "chat@s.whatsapp.net"
	stored := &StoredChatData{
		Conversations: map[string]wire.Conversation{chatID: {ID: chatID, Name: "Chat"}},
		Order:         []string{chatID},
		Messages: map[string][]wire.Message{chatID: {
			{ID: "old", ConversationID: chatID, Timestamp: 1, Text: strings.Repeat("o", 6*1024*1024)},
			{ID: "middle", ConversationID: chatID, Timestamp: 2, Text: strings.Repeat("m", 6*1024*1024)},
			{ID: "new", ConversationID: chatID, Timestamp: 3, Text: strings.Repeat("n", 6*1024*1024)},
		}},
		RawMedia:       map[string][]byte{rawMediaKey(chatID, "old"): make([]byte, 1024*1024)},
		ReactionActors: map[string]map[string]string{rawMediaKey(chatID, "old"): {"actor": "👍"}},
	}
	trimStoredHotMessagesToByteBudget(stored, byteBudget)
	if got := storedHotMemoryBytes(stored); got > byteBudget {
		t.Fatalf("retained synthetic history uses %d bytes, over %d-byte limit", got, byteBudget)
	}
	if len(stored.Messages[chatID]) != 2 || stored.Messages[chatID][0].ID != "middle" || stored.Messages[chatID][1].ID != "new" {
		t.Fatalf("byte trimming did not retain the newest complete messages: %+v", stored.Messages[chatID])
	}
	key := rawMediaKey(chatID, "old")
	if _, ok := stored.RawMedia[key]; ok {
		t.Fatal("raw media for the evicted message remains retained")
	}
	if _, ok := stored.ReactionActors[key]; ok {
		t.Fatal("reaction actors for the evicted message remain retained")
	}
}

func TestBoundStoredChatCompactsRetainedMessageSlice(t *testing.T) {
	chatID := "chat@s.whatsapp.net"
	messages := make([]wire.Message, maxPersistedMessages+1, maxPersistedMessages*100)
	for i := range messages {
		messages[i] = wire.Message{ID: fmt.Sprintf("message-%d", i), ConversationID: chatID, Timestamp: int64(i)}
	}
	stored := &StoredChatData{
		Conversations: map[string]wire.Conversation{chatID: {ID: chatID}},
		Order:         []string{chatID},
		Messages:      map[string][]wire.Message{chatID: messages},
	}
	boundStoredChat(stored)
	if got := cap(stored.Messages[chatID]); got > maxPersistedMessages {
		t.Fatalf("trimmed message slice retains capacity %d, want at most %d", got, maxPersistedMessages)
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
	var autoVacuum int
	if err := store.db.QueryRow(`PRAGMA auto_vacuum`).Scan(&autoVacuum); err != nil {
		t.Fatal(err)
	}
	if autoVacuum != 1 {
		t.Fatalf("history store vacuum mode = %d, want FULL (1)", autoVacuum)
	}
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
	if got, want := store.maxBytesLimit(), historyDatabaseBudgetBytes(smallerBudget); got != want {
		t.Fatalf("new database-page budget = %d, want %d", got, want)
	}
	var pages int64
	if err := store.db.QueryRow(`PRAGMA page_count`).Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if pages*sqlitePageBytes > historyDatabaseBudgetBytes(smallerBudget) {
		t.Fatalf("database page count %d exceeds reduced cap %d", pages, historyDatabaseBudgetBytes(smallerBudget))
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
