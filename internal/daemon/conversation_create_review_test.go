package daemon

import (
	"context"
	"reflect"
	"testing"

	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
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

func TestGoogleGroupCreationAsksForRCSOnlyWhenThePhoneRequiresIt(t *testing.T) {
	numbers := []*gmproto.ContactNumber{{Number: "+15550001"}, {Number: "+15550002"}}
	for _, tc := range []struct {
		name       string
		first      gmproto.GetOrCreateConversationResponse_Status
		wantCalls  int
		wantRCSSet bool
	}{
		{"existing or MMS group", gmproto.GetOrCreateConversationResponse_SUCCESS, 1, false},
		{"new RCS group", gmproto.GetOrCreateConversationResponse_CREATE_RCS, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests []*gmproto.GetOrCreateConversationRequest
			call := func(_ context.Context, req *gmproto.GetOrCreateConversationRequest) (*gmproto.GetOrCreateConversationResponse, error) {
				copied := &gmproto.GetOrCreateConversationRequest{Numbers: req.Numbers, RCSGroupName: req.RCSGroupName, CreateRCSGroup: req.CreateRCSGroup}
				requests = append(requests, copied)
				if len(requests) == 1 && tc.first == gmproto.GetOrCreateConversationResponse_CREATE_RCS {
					return &gmproto.GetOrCreateConversationResponse{Status: tc.first}, nil
				}
				return &gmproto.GetOrCreateConversationResponse{Status: gmproto.GetOrCreateConversationResponse_SUCCESS, Conversation: &gmproto.Conversation{ConversationID: "synthetic-group"}}, nil
			}
			conv, err := getOrCreateGoogleConversation(context.Background(), numbers, "Invented group", call)
			if err != nil || conv.GetConversationID() != "synthetic-group" {
				t.Fatalf("conversation = %v, err = %v", conv, err)
			}
			if len(requests) != tc.wantCalls || requests[0].GetCreateRCSGroup() {
				t.Fatalf("requests = %+v", requests)
			}
			last := requests[len(requests)-1]
			if last.GetCreateRCSGroup() != tc.wantRCSSet || (tc.wantRCSSet && last.GetRCSGroupName() != "Invented group") {
				t.Fatalf("final request = %+v", last)
			}
		})
	}
}

func TestGoogleConversationCreationRequiresAReturnedConversation(t *testing.T) {
	call := func(context.Context, *gmproto.GetOrCreateConversationRequest) (*gmproto.GetOrCreateConversationResponse, error) {
		return &gmproto.GetOrCreateConversationResponse{Status: gmproto.GetOrCreateConversationResponse_CREATE_RCS}, nil
	}
	if _, err := getOrCreateGoogleConversation(context.Background(), []*gmproto.ContactNumber{{Number: "+15550001"}}, "", call); err == nil {
		t.Fatal("direct chat reported created without a conversation")
	}
	empty := func(context.Context, *gmproto.GetOrCreateConversationRequest) (*gmproto.GetOrCreateConversationResponse, error) {
		return &gmproto.GetOrCreateConversationResponse{Status: gmproto.GetOrCreateConversationResponse_SUCCESS}, nil
	}
	if _, err := getOrCreateGoogleConversation(context.Background(), []*gmproto.ContactNumber{{Number: "+15550001"}}, "", empty); err == nil {
		t.Fatal("success without a conversation was accepted")
	}
}
