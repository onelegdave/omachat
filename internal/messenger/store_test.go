package messenger

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

func setupMessengerPaths(t *testing.T) *appStore.Paths {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmp, "run"))
	paths, err := appStore.NewPaths()
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

// TestMessengerRestartRoundTrip is the regression test for the Budget Nudes
// bug: a live encrypted reply received while connected must still be there,
// with historyNotice, after the helper process restarts and the in-memory
// maps are gone.
func TestMessengerRestartRoundTrip(t *testing.T) {
	paths := setupMessengerPaths(t)

	first := New(zerolog.Nop(), paths, nil)
	first.handleTable(&table.LSTable{
		LSVerifyContactRowExists: []*table.LSVerifyContactRowExists{{ContactId: 42, Name: "Ada Lovelace"}},
		LSVerifyHybridThreadExists: []*table.LSVerifyHybridThreadExists{{
			ThreadKey: 348838578573334, ThreadJID: 42, ThreadType: table.ENCRYPTED_OVER_WA_GROUP,
		}},
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{{
			ThreadKey: 348838578573334, ThreadType: table.ENCRYPTED_OVER_WA_GROUP,
		}},
		LSUpsertMessage: []*table.LSUpsertMessage{{
			ThreadKey: 348838578573334, MessageId: "live-1", Text: "live reply", TimestampMs: 1_700_000_000_123, SenderId: 42,
		}},
	})

	info, err := os.Stat(paths.MessengerStoreFile())
	if err != nil {
		t.Fatalf("store file was not written: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("store permissions = %o, want 600", info.Mode().Perm())
	}

	// Simulate a helper restart: a fresh Backend with empty in-memory maps,
	// loading and merging whatever connect() would load before network sync.
	restarted := New(zerolog.Nop(), paths, nil)
	restarted.mu.Lock()
	restarted.mergeStoredMessengerDataLocked(loadStoredMessengerData(paths.MessengerStoreFile()))
	restarted.mu.Unlock()

	result, err := restarted.Messages(context.Background(), wire.MessagesParams{ConversationID: "348838578573334", Count: 60})
	if err != nil {
		t.Fatal(err)
	}
	if result.HistoryNotice != encryptedHistoryNotice {
		t.Fatalf("history notice after restart = %q, want the encrypted notice", result.HistoryNotice)
	}
	if len(result.Messages) != 1 || result.Messages[0].ID != "live-1" || result.Messages[0].SenderName != "Ada Lovelace" {
		t.Fatalf("messages after restart = %#v", result.Messages)
	}
	if restarted.threadToJID[348838578573334].IsEmpty() {
		t.Fatal("encrypted routing map was not restored after restart")
	}
}

// TestMessengerMergeDoesNotEraseLiveState guards the "merge, not erase"
// requirement: the pre-network-sync load must fold into whatever the current
// process then receives live, not the other way around, so a subsequent save
// does not drop either side.
func TestMessengerMergeDoesNotEraseLiveState(t *testing.T) {
	paths := setupMessengerPaths(t)

	stale := storedMessengerData{Messages: map[string][]wire.Message{
		"7": {{ID: "stale-1", ConversationID: "7", Text: "old", Timestamp: 1_000}},
	}}
	if err := saveStoredMessengerData(paths.MessengerStoreFile(), stale); err != nil {
		t.Fatal(err)
	}

	live := New(zerolog.Nop(), paths, nil)
	// connect() merges the persisted cache before any network sync runs.
	live.mu.Lock()
	live.mergeStoredMessengerDataLocked(loadStoredMessengerData(paths.MessengerStoreFile()))
	live.mu.Unlock()

	live.handleTable(&table.LSTable{LSUpsertMessage: []*table.LSUpsertMessage{{
		ThreadKey: 7, MessageId: "fresh-1", Text: "new", TimestampMs: 2_000, SenderId: 9,
	}}})

	got := live.messages["7"]
	if len(got) != 2 {
		t.Fatalf("merged messages = %#v, want both stale and fresh", got)
	}

	onDisk := loadStoredMessengerData(paths.MessengerStoreFile())
	if len(onDisk.Messages["7"]) != 2 {
		t.Fatalf("saved messages = %#v, want the merge preserved on disk too", onDisk.Messages["7"])
	}
}

// TestMessengerThreadTypeDoesNotDowngrade covers the classification bug: once
// a thread is known to be an encrypted WhatsApp bridge, a later thread row
// without that information must not downgrade it and hide historyNotice.
func TestMessengerThreadTypeDoesNotDowngrade(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	b.handleTable(&table.LSTable{
		LSVerifyHybridThreadExists: []*table.LSVerifyHybridThreadExists{{
			ThreadKey: 900, ThreadJID: 42, ThreadType: table.ENCRYPTED_OVER_WA_ONE_TO_ONE,
		}},
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{{
			ThreadKey: 900, ThreadType: table.ENCRYPTED_OVER_WA_ONE_TO_ONE,
		}},
	})
	if !b.threadTypes[900].IsWhatsApp() {
		t.Fatalf("thread type after initial sync = %v, want encrypted WhatsApp", b.threadTypes[900])
	}

	// A later, non-hybrid-aware refresh reports a generic type for the same
	// thread key, as happens on a plain inbox listing refresh.
	b.handleTable(&table.LSTable{LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{{
		ThreadKey: 900, ThreadType: table.ONE_TO_ONE,
	}}})
	if !b.threadTypes[900].IsWhatsApp() {
		t.Fatalf("thread type after downgraded refresh = %v, want to stay encrypted WhatsApp", b.threadTypes[900])
	}

	result, err := b.Messages(context.Background(), wire.MessagesParams{ConversationID: "900", Count: 60})
	if err != nil {
		t.Fatal(err)
	}
	if result.HistoryNotice != encryptedHistoryNotice {
		t.Fatalf("history notice after downgraded refresh = %q, want the encrypted notice", result.HistoryNotice)
	}
}

// TestMessengerUnpairClearsStore ensures unpairing still wipes any persisted
// conversation and message cache, not just the browser session cookies.
func TestMessengerUnpairClearsStore(t *testing.T) {
	paths := setupMessengerPaths(t)

	b := New(zerolog.Nop(), paths, nil)
	b.handleTable(&table.LSTable{LSUpsertMessage: []*table.LSUpsertMessage{{
		ThreadKey: 7, MessageId: "m1", Text: "hi", TimestampMs: 1, SenderId: 9,
	}}})
	if _, err := os.Stat(paths.MessengerStoreFile()); err != nil {
		t.Fatalf("store file was not written before unpair: %v", err)
	}

	if err := b.Unpair(context.Background()); err != nil {
		t.Fatalf("unpair failed: %v", err)
	}
	if _, err := os.Stat(paths.MessengerStoreFile()); !os.IsNotExist(err) {
		t.Fatalf("Messenger store file survived unpair: %v", err)
	}
}

// TestLoadStoredMessengerDataRejectsUnknownVersion guards against loading a
// cache from an incompatible layout as if it were current.
func TestLoadStoredMessengerDataRejectsUnknownVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messenger_store.json")
	if err := os.WriteFile(path, []byte(`{"version":999,"messages":{"7":[{"id":"x"}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := loadStoredMessengerData(path)
	if len(got.Messages) != 0 {
		t.Fatalf("loaded data from unknown version = %#v, want empty", got)
	}
}
