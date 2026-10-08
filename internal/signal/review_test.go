package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
	"github.com/rs/zerolog"
)

type delayedCaller struct {
	fakeCaller
	method  string
	entered chan context.Context
	release chan struct{}
}

func (f *delayedCaller) Call(ctx context.Context, method string, params any, out any) error {
	if method == f.method {
		f.entered <- ctx
		<-f.release
	}
	return f.fakeCaller.Call(ctx, method, params, out)
}

func TestCachedSignalHistoryPagesWithoutGaps(t *testing.T) {
	b := New(zerolog.Nop(), testPaths(t), nil)
	t.Cleanup(b.Stop)
	id := groupPrefix + "synthetic-group"
	b.ensureConversation(id, "Synthetic group", true)
	// Equal timestamps and out-of-order reception must not repeat or skip a page.
	for i := 149; i >= 0; i-- {
		b.ingest(wire.Message{ID: fmt.Sprintf("message-%03d", i), ConversationID: id,
			Text: "fixture", Timestamp: int64(i/3+1) * 1000}, "")
	}
	params := wire.MessagesParams{ConversationID: id, Count: 60}
	seen := map[string]bool{}
	for page := 0; page < 3; page++ {
		result, err := b.Messages(context.Background(), params)
		if err != nil {
			t.Fatal(err)
		}
		want := 60
		if page == 2 {
			want = 30
		}
		if len(result.Messages) != want || result.HasMore != (page < 2) {
			t.Fatalf("page %d: count=%d hasMore=%v", page, len(result.Messages), result.HasMore)
		}
		for _, message := range result.Messages {
			if seen[message.ID] {
				t.Fatalf("repeated %s", message.ID)
			}
			seen[message.ID] = true
		}
		params.CursorID, params.CursorTime = result.CursorID, result.CursorTime
	}
	if len(seen) != 150 {
		t.Fatalf("history count = %d", len(seen))
	}
}

func TestLateSignalDownloadCannotRestoreRedactedMedia(t *testing.T) {
	for _, expiry := range []bool{false, true} {
		t.Run(fmt.Sprint("expiry=", expiry), func(t *testing.T) {
			paths := testPaths(t)
			fake := &delayedCaller{method: "getAttachment", entered: make(chan context.Context, 1), release: make(chan struct{})}
			b := New(zerolog.Nop(), paths, nil)
			t.Cleanup(b.Stop)
			b.SetClient(fake)
			if err := b.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			id := directPrefix + "+15550002222"
			fake.notify(json.RawMessage(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"timestamp":1700000000000,"attachments":[{"id":"late-photo","contentType":"image/png"}]}}}`))
			completed := make(chan error, 1)
			go func() {
				_, err := b.Media(context.Background(), wire.MediaParams{Key: mediaPrefix + "late-photo"})
				completed <- err
			}()
			select {
			case <-fake.entered:
			case <-time.After(time.Second):
				t.Fatal("download did not start")
			}
			if expiry {
				b.expireMessage(messageID(1700000000000, "+15550002222"))
			} else {
				b.redactMessage(id, 1700000000000, "+15550002222")
			}
			close(fake.release)
			select {
			case err := <-completed:
				if err == nil {
					t.Fatal("redacted attachment returned to UI")
				}
			case <-time.After(time.Second):
				t.Fatal("download did not complete")
			}
			files, err := os.ReadDir(paths.SignalMediaDir())
			if err != nil || len(files) != 0 {
				t.Fatalf("redacted media remains: count=%d err=%v", len(files), err)
			}
		})
	}
}

func TestSignalStartupPreservesUnreadableOrUnsupportedCache(t *testing.T) {
	for _, raw := range []string{`{invalid`, `{"version":99,"messages":{"fixture":[]}}`} {
		t.Run(raw[:8], func(t *testing.T) {
			paths := testPaths(t)
			if err := os.WriteFile(paths.SignalStoreFile(), []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			b := New(zerolog.Nop(), paths, nil)
			t.Cleanup(b.Stop)
			fake := &fakeCaller{}
			b.SetClient(fake)
			if err := b.Start(context.Background()); err == nil {
				t.Fatal("invalid cache accepted")
			}
			data, err := os.ReadFile(paths.SignalStoreFile())
			if err != nil || string(data) != raw {
				t.Fatal("original cache was overwritten")
			}
			if fake.started {
				t.Fatal("client started against invalid cache")
			}
		})
	}
}

func TestSignalStartupRetainsCacheOnReadError(t *testing.T) {
	paths := testPaths(t)
	// A directory at the cache filename produces a real read error without
	// depending on the test runner's UID or permission-bypass capabilities.
	if err := os.Mkdir(paths.SignalStoreFile(), 0700); err != nil {
		t.Fatal(err)
	}
	marker := paths.SignalStoreFile() + "/retained-fixture"
	if err := os.WriteFile(marker, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	b := New(zerolog.Nop(), paths, nil)
	t.Cleanup(b.Stop)
	fake := &fakeCaller{}
	b.SetClient(fake)
	if err := b.Start(context.Background()); err == nil {
		t.Fatal("cache read error accepted")
	}
	if fake.started {
		t.Fatal("client started despite cache read failure")
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "retained" {
		t.Fatal("unreadable cache was changed")
	}
}

func TestSignalPairingDoesNotCompleteAfterStop(t *testing.T) {
	fake := &delayedCaller{fakeCaller: fakeCaller{responses: map[string]string{"listAccounts": `[]`}},
		method: "finishLink", entered: make(chan context.Context, 1), release: make(chan struct{})}
	var mu sync.Mutex
	paired := false
	b := New(zerolog.Nop(), testPaths(t), func(event wire.Event) {
		if event.Event == wire.EventPaired {
			mu.Lock()
			paired = true
			mu.Unlock()
		}
	})
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.StartPairing(context.Background()); err != nil {
		t.Fatal(err)
	}
	var ctx context.Context
	select {
	case ctx = <-fake.entered:
	case <-time.After(time.Second):
		t.Fatal("pairing did not start")
	}
	if _, err := b.StartPairing(context.Background()); err == nil {
		t.Fatal("overlapping pairing accepted")
	}
	stopped := make(chan struct{})
	go func() { b.Stop(); close(stopped) }()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		close(fake.release)
		t.Fatal("pairing context survived service stop")
	}
	close(fake.release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("service did not stop")
	}
	mu.Lock()
	got := paired
	mu.Unlock()
	if got || b.Status().State == wire.StateConnected {
		t.Fatal("stopped pairing completed")
	}
	if b.account != "" {
		t.Fatal("stopped pairing committed an account")
	}
}

func TestSignalAccountSelectionAndSubscribedReceive(t *testing.T) {
	paths := testPaths(t)
	if err := saveStoredData(paths.SignalStoreFile(), storedData{Account: "+15550001111"}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCaller{responses: map[string]string{"listAccounts": `[{"number":"+15550009999"},{"number":"+15550001111"}]`}}
	b := New(zerolog.Nop(), paths, nil)
	t.Cleanup(b.Stop)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.account != "+15550001111" {
		t.Fatal("saved inbox switched accounts")
	}
	if len(fake.calls) < 2 || fake.calls[0] != "listAccounts" || fake.calls[1] != "subscribeReceive" {
		t.Fatal("receive subscription did not follow account selection")
	}
	fake.notify(json.RawMessage(`{"subscription":0,"result":{"account":"+15550009999","envelope":{"sourceNumber":"+15550002222","dataMessage":{"timestamp":100,"message":"other-account fixture"}}}}`))
	if len(b.Conversations(0)) != 0 {
		t.Fatal("foreign account message entered inbox")
	}
	fake.notify(json.RawMessage(`{"subscription":0,"result":{"account":"+15550001111","envelope":{"sourceNumber":"+15550002222","dataMessage":{"timestamp":101,"message":"selected-account fixture"}}}}`))
	if len(b.Conversations(0)) != 1 {
		t.Fatal("subscribed receive envelope was not ingested")
	}
}

func TestSignalControlsTargetAuthorAtSharedTimestamp(t *testing.T) {
	for _, control := range []string{"reaction", "remote-delete", "expiry"} {
		t.Run(control, func(t *testing.T) {
			b := New(zerolog.Nop(), testPaths(t), nil)
			t.Cleanup(b.Stop)
			id := groupPrefix + "synthetic-group"
			for _, author := range []string{"alice-uuid", "bob-uuid"} {
				b.handleDataMessage(id, author, "Fixture", "Synthetic group", false,
					&dataMessage{Timestamp: 1700000000000, Message: author})
			}
			switch control {
			case "reaction":
				b.handleDataMessage(id, "carol-uuid", "Fixture", "Synthetic group", false,
					&dataMessage{Reaction: &signalReaction{Emoji: "👍", TargetSentTimestamp: 1700000000000, TargetAuthorUUID: "bob-uuid"}})
			case "remote-delete":
				b.handleDataMessage(id, "bob-uuid", "Fixture", "Synthetic group", false,
					&dataMessage{RemoteDelete: &remoteDelete{Timestamp: 1700000000000}})
			case "expiry":
				b.expireMessage(messageID(1700000000000, "bob-uuid"))
			}
			b.mu.RLock()
			alice, _ := b.findMessageLocked(id, messageID(1700000000000, "alice-uuid"))
			bob, _ := b.findMessageLocked(id, messageID(1700000000000, "bob-uuid"))
			b.mu.RUnlock()
			if alice.Deleted || len(alice.Reactions) > 0 {
				t.Fatal("control changed another author's message")
			}
			if control == "reaction" && len(bob.Reactions) != 1 {
				t.Fatal("target reaction missing")
			}
			if control != "reaction" && !bob.Deleted {
				t.Fatal("target content retained")
			}
		})
	}
}
