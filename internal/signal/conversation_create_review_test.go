package signal

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/onelegdave/omachat/internal/wire"
	"github.com/rs/zerolog"
	"testing"
)

func TestCreateConversationRequiresLinkedAccount(t *testing.T) {
	b := New(zerolog.Nop(), testPaths(t), nil)
	b.SetClient(&fakeCaller{})
	if _, err := b.CreateConversation(context.Background(), wire.CreateConversationParams{TargetIDs: []string{"+15550002222"}}); err == nil {
		t.Fatal("unlinked account created a conversation")
	}
}
func TestCreateConversationPersistsAndPublishes(t *testing.T) {
	paths := testPaths(t)
	events := make(chan wire.Event, 50)
	b := New(zerolog.Nop(), paths, func(e wire.Event) { events <- e })
	b.SetClient(&fakeCaller{responses: map[string]string{"listContacts": `[{"number":"+15550002222","name":"Taylor"}]`}})
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer b.Stop()
	for len(events) > 0 {
		<-events
	}
	got, err := b.CreateConversation(context.Background(), wire.CreateConversationParams{TargetIDs: []string{"+15550002222"}})
	if err != nil {
		t.Fatal(err)
	}
	restored := New(zerolog.Nop(), paths, nil)
	if list := restored.Conversations(0); len(list) != 1 || list[0].ID != got.ID {
		t.Errorf("new conversation lost after restart: %#v", list)
	}
	select {
	case e := <-events:
		if e.Event != wire.EventConversation {
			t.Errorf("event = %#v", e)
		}
	default:
		t.Error("no conversation event published")
	}
}

type creationCaller struct {
	*fakeCaller
	duringGroup func()
}

func (c *creationCaller) Call(ctx context.Context, method string, p any, out any) error {
	if method == "updateGroup" {
		c.duringGroup()
		return json.Unmarshal([]byte(`{"groupId":"synthetic-group"}`), out)
	}
	return c.fakeCaller.Call(ctx, method, p, out)
}
func TestCreateConversationCannotRestoreStoppedSignalAccount(t *testing.T) {
	b := New(zerolog.Nop(), testPaths(t), nil)
	cli := &creationCaller{fakeCaller: &fakeCaller{}}
	b.SetClient(cli)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cli.duringGroup = b.Stop
	_, err := b.CreateConversation(context.Background(), wire.CreateConversationParams{TargetIDs: []string{"+15550002222", "+15550003333"}, Name: "Friends"})
	if !errors.Is(err, context.Canceled) || len(b.Conversations(0)) != 0 {
		t.Fatalf("retired result cached: %v", err)
	}
}
