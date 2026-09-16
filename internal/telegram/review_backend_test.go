package telegram

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/onelegdave/omachat/internal/wire"
)

func TestConcurrentTelegramPersistence(t *testing.T) {
	b, paths, _ := setupTestTelegram(t)
	var wg sync.WaitGroup
	for worker := 0; worker < 2; worker++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 1; i <= 30; i++ {
				b.ingestMessage(Message{ID: int64(w*100 + i), ConversationID: 42, Text: "fixture"})
			}
		}(worker)
	}
	wg.Wait()
	stored := loadStoredData(paths.TelegramStoreFile())
	if len(stored.Messages["tg:42"]) != 60 {
		t.Fatal("concurrent saves lost messages")
	}
}

func TestTelegramOldSendCannotRestoreAfterUnpair(t *testing.T) {
	b, paths, _ := setupTestTelegram(t)
	entered, release := make(chan struct{}), make(chan struct{})
	mock := NewMockClient()
	mock.SendTextFunc = func(ctx context.Context, id int64, text string) (Message, error) {
		close(entered)
		<-release
		return Message{ID: 3, ConversationID: id, Text: text, FromMe: true}, nil
	}
	b.SetClient(mock)
	result := make(chan error, 1)
	go func() {
		_, err := b.Send(context.Background(), wire.SendParams{ConversationID: "tg:42", Text: "fixture"})
		result <- err
	}()
	<-entered
	if err := b.Unpair(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("stale send: %v", err)
	}
	if len(b.Conversations(50)) != 0 {
		t.Fatal("old send restored conversation")
	}
	if _, err := os.Stat(paths.TelegramStoreFile()); !os.IsNotExist(err) {
		t.Fatalf("old send restored file: %v", err)
	}
}

type historyMock struct {
	*MockClient
	page func(context.Context, int64, int64, int) ([]Message, error)
}

func (m *historyMock) MessagesPage(ctx context.Context, id, before int64, count int) (HistoryPage, error) {
	rows, err := m.page(ctx, id, before, count)
	out := HistoryPage{Messages: rows, HasMore: len(rows) >= count}
	for _, row := range rows {
		if out.CursorID == 0 || row.ID < out.CursorID {
			out.CursorID = row.ID
		}
	}
	return out, err
}

func TestTelegramHistoryPagesAndScopesSignedPeer(t *testing.T) {
	b, _, _ := setupTestTelegram(t)
	m := &historyMock{MockClient: NewMockClient()}
	m.page = func(_ context.Context, id, before int64, count int) ([]Message, error) {
		if id != -1000000000042 || count != 2 {
			t.Fatalf("wrong page args %d %d", id, count)
		}
		if before == 0 {
			return []Message{{ID: 4, ConversationID: id, Timestamp: 4}, {ID: 3, ConversationID: id, Timestamp: 3}}, nil
		}
		if before != 3 {
			t.Fatalf("wrong offset %d", before)
		}
		return []Message{{ID: 2, ConversationID: id, Timestamp: 2}}, nil
	}
	b.SetClient(m)
	first, err := b.Messages(context.Background(), wire.MessagesParams{ConversationID: "tg:-1000000000042", Count: 2})
	if err != nil || !first.HasMore || first.CursorID != "tg:3" {
		t.Fatalf("first %+v %v", first, err)
	}
	second, err := b.Messages(context.Background(), wire.MessagesParams{ConversationID: first.ConversationID, CursorID: first.CursorID, Count: 2})
	if err != nil || second.HasMore || len(second.Messages) != 1 || second.Messages[0].ID != "tg:2" {
		t.Fatalf("second %+v %v", second, err)
	}
}

func TestTelegramRefreshAndHistoryCannotRestoreAfterUnpair(t *testing.T) {
	for _, kind := range []string{"refresh", "history", "read"} {
		t.Run(kind, func(t *testing.T) {
			b, paths, _ := setupTestTelegram(t)
			entered, release := make(chan struct{}), make(chan struct{})
			m := &historyMock{MockClient: NewMockClient()}
			wait := func() { close(entered); <-release }
			m.DialogsFunc = func(context.Context, int) ([]Dialog, error) { wait(); return []Dialog{{ID: 7, Name: "old"}}, nil }
			m.MessagesFunc = func(context.Context, int64, int) ([]Message, error) {
				return []Message{{ID: 1, ConversationID: 7}}, nil
			}
			m.page = func(context.Context, int64, int64, int) ([]Message, error) {
				wait()
				return []Message{{ID: 1, ConversationID: 7}}, nil
			}
			m.MarkReadFunc = func(context.Context, int64, int64) error { wait(); return nil }
			b.SetClient(m)
			done := make(chan error, 1)
			go func() {
				var err error
				switch kind {
				case "refresh":
					err = b.Refresh(context.Background())
				case "history":
					_, err = b.Messages(context.Background(), wire.MessagesParams{ConversationID: "tg:7"})
				case "read":
					err = b.MarkRead(context.Background(), wire.MarkReadParams{ConversationID: "tg:7"})
				}
				done <- err
			}()
			<-entered
			if err := b.Unpair(context.Background()); err != nil {
				t.Fatal(err)
			}
			close(release)
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("stale operation: %v", err)
			}
			if _, err := os.Stat(paths.TelegramStoreFile()); !os.IsNotExist(err) {
				t.Fatalf("restored file: %v", err)
			}
		})
	}
}

func TestTelegramOldCacheIsIgnoredWithoutRemovingSession(t *testing.T) {
	_, paths, _ := setupTestTelegram(t)
	if err := os.WriteFile(paths.TelegramStoreFile(), []byte(`{"conversations":{"tg:42":{"id":"tg:42"}},"order":["tg:42"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := loadStoredData(paths.TelegramStoreFile()); len(got.Order) != 0 {
		t.Fatal("ambiguous peer cache accepted")
	}
}

func TestTelegramMessageIDsRejectPeerIDs(t *testing.T) {
	for _, id := range []int64{-42, -1000000000042, 0, 2147483648} {
		if _, err := parseTelegramMessageID(fmt.Sprintf("tg:%d", id)); err == nil {
			t.Fatalf("accepted message ID %d", id)
		}
	}
}
