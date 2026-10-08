package messenger

import (
	"context"
	"errors"
	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	waTypes "go.mau.fi/whatsmeow/types"
	"reflect"
	"strings"
	"testing"
)

func TestCreateConversationReopensKnownEncryptedMessengerThread(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	b.client = &messagix.Client{}
	b.selfID = 1
	want := wire.Conversation{ID: "70", Name: "Taylor", Timestamp: 4000, Preview: "New activity", Unread: true}
	b.convs[want.ID] = want
	b.participants[70] = []int64{1, 7}
	b.threadTypes[70] = table.ENCRYPTED_OVER_WA_ONE_TO_ONE
	b.threadToJID[70] = waTypes.NewJID("7", waTypes.MessengerServer)
	got, err := b.CreateConversation(context.Background(), wire.CreateConversationParams{TargetIDs: []string{"7"}})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("encrypted conversation not reused: %#v %v", got, err)
	}
	b.mu.Lock()
	b.commitCreatedConversationLocked(70, []int64{7}, "Old name", false)
	b.mu.Unlock()
	if !reflect.DeepEqual(b.convs[want.ID], want) || b.threadTypes[70] != table.ENCRYPTED_OVER_WA_ONE_TO_ONE {
		t.Fatal("creation result overwrote activity or encrypted classification")
	}
}

func TestCreateConversationDoesNotInventMessengerSuccess(t *testing.T) {
	for _, response := range []*table.LSTable{nil, {}, {LSHandleFailedTask: []*table.LSHandleFailedTask{{Message: "synthetic private provider detail"}}}, {LSIssueNewError: []*table.LSIssueNewError{{ErrorMessage: "synthetic private provider detail"}}}} {
		b := New(zerolog.Nop(), nil, nil)
		_, err := b.createConversationThrough(context.Background(), wire.CreateConversationParams{TargetIDs: []string{"7"}}, func(context.Context, ...socket.Task) (*table.LSTable, error) { return response, nil }, func() bool { return true })
		if err == nil || strings.Contains(err.Error(), "private provider detail") || len(b.convs) > 0 {
			t.Fatalf("unconfirmed result created a phantom chat or exposed raw error: %v %#v", err, b.convs)
		}
	}
}
func TestCreateMessengerGroupUsesConfirmedServerID(t *testing.T) {
	paths := &appStore.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	b := New(zerolog.Nop(), paths, nil)
	calls := 0
	execute := func(_ context.Context, tasks ...socket.Task) (*table.LSTable, error) {
		calls++
		if calls == 1 {
			task := tasks[0].(*socket.CreateGroupTask)
			if len(tasks) != 1 || len(task.Participants) != 2 {
				t.Fatalf("group task: %#v", tasks)
			}
			return &table.LSTable{LSApplyNewGroupThread: []*table.LSApplyNewGroupThread{{OtidOfFirstMessage: task.SendPayload.OTID, ThreadKey: 900, ThreadType: table.GROUP_THREAD}}}, nil
		}
		if rename := tasks[0].(*socket.RenameThreadTask); rename.ThreadKey != 900 || rename.ThreadName != "Friends" {
			t.Fatalf("rename uses provisional group ID: %#v", rename)
		}
		return &table.LSTable{}, nil
	}
	got, err := b.createConversationThrough(context.Background(), wire.CreateConversationParams{TargetIDs: []string{"7", "8"}, Name: "Friends"}, execute, func() bool { return true })
	if err != nil || got.ID != "900" || !got.IsGroup || len(b.convs) != 1 || calls != 2 {
		t.Fatalf("canonical group result: %#v %v", got, err)
	}
}

func TestCreateMessengerCannotRenameAfterRequestRetirement(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	execute := func(_ context.Context, tasks ...socket.Task) (*table.LSTable, error) {
		calls++
		task := tasks[0].(*socket.CreateGroupTask)
		cancel()
		return &table.LSTable{LSApplyNewGroupThread: []*table.LSApplyNewGroupThread{{OtidOfFirstMessage: task.SendPayload.OTID, ThreadKey: 900, ThreadType: table.GROUP_THREAD}}}, nil
	}
	_, err := b.createConversationThrough(ctx, wire.CreateConversationParams{TargetIDs: []string{"7", "8"}, Name: "Friends"}, execute, func() bool { return true })
	if !errors.Is(err, context.Canceled) || calls != 1 || len(b.convs) != 0 {
		t.Fatalf("retired request continued: %v calls=%d", err, calls)
	}
}
