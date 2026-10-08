package telegram

import (
	"context"
	"errors"

	"github.com/onelegdave/omachat/internal/wire"
	"reflect"
	"testing"
)

func TestCreateConversationReopensWithoutReplacingActivity(t *testing.T) {
	b, _, _, mock := setupTestTelegramWithMock(t)
	mock.CreateConversationFunc = func(context.Context, wire.CreateConversationParams) (Dialog, error) {
		return Dialog{ID: 42, Name: "Taylor"}, nil
	}
	b.SetClient(mock)
	want := wire.Conversation{ID: "tg:42", Name: "Taylor", Timestamp: 3000, Preview: "new message", Unread: true}
	b.mu.Lock()
	b.convs[want.ID] = want
	b.order = []string{want.ID}
	b.mu.Unlock()
	for i := 0; i < 2; i++ {
		got, err := b.CreateConversation(context.Background(), wire.CreateConversationParams{TargetIDs: []string{"42"}})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("existing activity overwritten: %#v", got)
		}
	}
	if got := b.Conversations(0); len(got) != 1 {
		t.Fatalf("duplicate conversation rows: %#v", got)
	}
}

func TestCreateConversationKeepsActivityArrivingDuringRequest(t *testing.T) {
	b, _, _, mock := setupTestTelegramWithMock(t)
	b.SetClient(mock)
	want := wire.Conversation{ID: "tg:42", Name: "Taylor", Timestamp: 4000, Preview: "arrived during request", Unread: true}
	mock.CreateConversationFunc = func(context.Context, wire.CreateConversationParams) (Dialog, error) {
		b.mu.Lock()
		b.convs[want.ID] = want
		b.order = []string{want.ID}
		b.mu.Unlock()
		return Dialog{ID: 42, Name: "Taylor"}, nil
	}
	got, err := b.CreateConversation(context.Background(), wire.CreateConversationParams{TargetIDs: []string{"42"}})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("concurrent activity lost: %#v %v", got, err)
	}
}
func TestCreateConversationCannotRestoreRetiredTelegramAccount(t *testing.T) {
	b, _, _, mock := setupTestTelegramWithMock(t)
	b.SetClient(mock)
	mock.CreateConversationFunc = func(context.Context, wire.CreateConversationParams) (Dialog, error) {
		b.mu.Lock()
		b.retireAccountLocked()
		b.mu.Unlock()
		return Dialog{ID: 42, Name: "Taylor"}, nil
	}
	_, err := b.CreateConversation(context.Background(), wire.CreateConversationParams{TargetIDs: []string{"42"}})
	if !errors.Is(err, context.Canceled) || len(b.Conversations(0)) != 0 {
		t.Fatalf("retired result cached: %v", err)
	}
}
