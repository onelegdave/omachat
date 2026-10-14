package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestIgnoreStatusUpdatesFromLiveAndHistory(t *testing.T) {
	backend, _, _ := setupTestBackend(t)
	statusJID := types.StatusBroadcastJID
	story := &waE2E.Message{Conversation: proto.String("a story update")}

	live := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: statusJID, Sender: types.NewJID("15551234567", types.DefaultUserServer)},
			ID:            "status-live", IsNewsletterStatus: true,
		},
		Message: story,
	}
	backend.handleEvent(live)
	if got := backend.Conversations(10); len(got) != 0 {
		t.Fatalf("live status created conversations: %+v", got)
	}

	history := &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{{
		ID: proto.String(statusJID.String()),
		Messages: []*waHistorySync.HistorySyncMsg{{Message: &waWeb.WebMessageInfo{
			Key:     &waCommon.MessageKey{ID: proto.String("status-history"), RemoteJID: proto.String(statusJID.String())},
			Message: story,
		}}},
	}}}
	if err := backend.ingestHistorySyncMode(backend.gen, history, false); err != nil {
		t.Fatal(err)
	}
	if got := backend.Conversations(10); len(got) != 0 {
		t.Fatalf("historical status created conversations: %+v", got)
	}
}

func TestStatusUpdateDetection(t *testing.T) {
	cases := []struct {
		name string
		info types.MessageInfo
		want bool
	}{
		{"status JID", types.MessageInfo{MessageSource: types.MessageSource{Chat: types.StatusBroadcastJID}}, true},
		{"status metadata", types.MessageInfo{IsNewsletterStatus: true}, true},
		{"regular chat", types.MessageInfo{MessageSource: types.MessageSource{Chat: types.NewJID("15551234567", types.DefaultUserServer)}}, false},
		{"ordinary broadcast list", types.MessageInfo{MessageSource: types.MessageSource{Chat: types.NewJID("123456789", types.BroadcastServer)}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isStatusUpdate(tc.info); got != tc.want {
				t.Fatalf("isStatusUpdate() = %v, want %v", got, tc.want)
			}
		})
	}
}
