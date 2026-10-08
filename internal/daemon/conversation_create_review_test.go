package daemon

import (
	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"reflect"
	"testing"
)

func TestCreatedConversationKeepsLatestGoogleActivity(t *testing.T) {
	d, _ := newTestDaemon(t)
	want := wire.Conversation{ID: "synthetic-chat", Name: "Taylor", Timestamp: 4000, Preview: "New activity", Unread: true, AvatarPath: "synthetic-avatar"}
	d.mu.Lock()
	d.convs[want.ID] = want
	d.reorderLocked()
	d.mu.Unlock()
	got := d.commitCreatedConversation(&gmproto.Conversation{ConversationID: want.ID, Name: "Old snapshot", LastMessageTimestamp: 1000})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("activity lost: %#v", got)
	}
	if got := d.Conversations(0); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("cached state changed: %#v", got)
	}
}
