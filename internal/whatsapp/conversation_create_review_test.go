package whatsapp

import (
	"context"
	"errors"
	"go.mau.fi/whatsmeow"

	"github.com/onelegdave/omachat/internal/wire"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"reflect"
	"testing"
)

type creationContacts struct{ waStore.ContactStore }

func (creationContacts) GetAllContacts(context.Context) (map[types.JID]types.ContactInfo, error) {
	return map[types.JID]types.ContactInfo{types.NewJID("15550002222", types.DefaultUserServer): {FullName: "Taylor"}}, nil
}
func TestCreateConversationReopensWithoutReplacingActivity(t *testing.T) {
	b, mock, _ := setupTestBackend(t)
	b.SetClient(mock, true)
	b.device = &waStore.Device{Contacts: creationContacts{}}
	want := wire.Conversation{ID: "15550002222@s.whatsapp.net", Name: "Taylor", Timestamp: 3000, Preview: "new message", Unread: true}
	b.convs[want.ID] = want
	b.order = []string{want.ID}
	got, err := b.CreateConversation(context.Background(), wire.CreateConversationParams{TargetIDs: []string{want.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("existing activity overwritten: %#v", got)
	}
}

type creationClient struct {
	*MockClient
	create func(context.Context, whatsmeow.ReqCreateGroup) (*types.GroupInfo, error)
}

func (c *creationClient) CreateGroup(ctx context.Context, p whatsmeow.ReqCreateGroup) (*types.GroupInfo, error) {
	return c.create(ctx, p)
}
func TestCreateGroupValidatesNativeResultAndRetiresLateCommit(t *testing.T) {
	b, mock, _ := setupTestBackend(t)
	cli := &creationClient{MockClient: mock}
	b.SetClient(cli, true)
	b.device = &waStore.Device{Contacts: creationContacts{}}
	p := wire.CreateConversationParams{TargetIDs: []string{"15550002222@s.whatsapp.net", "15550003333@s.whatsapp.net"}, Name: "Friends"}
	cli.create = func(context.Context, whatsmeow.ReqCreateGroup) (*types.GroupInfo, error) { return nil, nil }
	if _, err := b.CreateConversation(context.Background(), p); err == nil {
		t.Fatal("missing group result accepted")
	}
	cli.create = func(_ context.Context, got whatsmeow.ReqCreateGroup) (*types.GroupInfo, error) {
		if len(got.Participants) != 2 || got.Name != "Friends" {
			t.Fatalf("native parameters: %#v", got)
		}
		b.Stop()
		return &types.GroupInfo{JID: types.NewJID("synthetic-group", types.GroupServer)}, nil
	}
	if _, err := b.CreateConversation(context.Background(), p); !errors.Is(err, context.Canceled) {
		t.Fatalf("retired operation result: %v", err)
	}
	if len(b.Conversations(0)) != 0 {
		t.Fatal("retired group cached")
	}
}
func TestCreateConversationDuplicateRecipientIsDirect(t *testing.T) {
	b, mock, _ := setupTestBackend(t)
	b.SetClient(mock, true)
	b.device = &waStore.Device{Contacts: creationContacts{}}
	id := "15550002222@s.whatsapp.net"
	got, err := b.CreateConversation(context.Background(), wire.CreateConversationParams{TargetIDs: []string{id, " " + id + " "}})
	if err != nil || got.IsGroup || got.ID != id {
		t.Fatalf("duplicate recipient created a group: %#v %v", got, err)
	}
	if _, err := b.CreateConversation(context.Background(), wire.CreateConversationParams{TargetIDs: []string{"fake@g.us"}}); err == nil {
		t.Fatal("group JID accepted as a person")
	}
}
