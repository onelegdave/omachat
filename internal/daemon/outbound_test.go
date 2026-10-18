package daemon

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/onelegdave/omachat/internal/wire"
)

func TestOutboundTextAndListsAreBounded(t *testing.T) {
	long := strings.Repeat("é", maxMessageText)
	msg := wire.Message{ID: "m", Text: long, SenderName: long, Attachments: make([]wire.Attachment, maxAttachments+5), Reactions: make([]wire.Reaction, maxReactions+5)}
	original := msg
	got := limitOutbound(wire.Event{Event: wire.EventMessage, Data: msg}).(wire.Event).Data.(wire.Message)
	if len(got.Text) > maxMessageText+len("…") || !utf8.ValidString(got.Text) {
		t.Fatalf("text not bounded on a character boundary: %d bytes", len(got.Text))
	}
	if len(got.SenderName) > maxDisplayText+len("…") || len(got.Attachments) != maxAttachments || len(got.Reactions) != maxReactions {
		t.Fatalf("limits not applied: %d %d %d", len(got.SenderName), len(got.Attachments), len(got.Reactions))
	}
	if msg.Text != original.Text || len(msg.Attachments) != maxAttachments+5 {
		t.Fatal("the backend's value was modified")
	}

	convs := make([]wire.Conversation, maxListItems+10)
	convs[0] = wire.Conversation{Name: long, Preview: long, Participants: make([]wire.Participant, maxParticipants+1)}
	list := limitOutbound(wire.Response{OK: true, Result: convs}).(wire.Response).Result.([]wire.Conversation)
	if len(list) != maxListItems || len(list[0].Name) > maxDisplayText+len("…") || len(list[0].Preview) > maxDisplayText+len("…") || len(list[0].Participants) != maxParticipants {
		t.Fatal("conversation reply not bounded")
	}

	page := &wire.MessagesResult{Messages: []wire.Message{{Text: long}}}
	if out := limitOutbound(wire.Response{Result: page}).(wire.Response).Result.(*wire.MessagesResult); len(out.Messages[0].Text) > maxMessageText+len("…") || page.Messages[0].Text != long {
		t.Fatal("message page not bounded, or bounded in place")
	}
}

func TestOversizedFramesNeverReachThePanel(t *testing.T) {
	var out bytes.Buffer
	w := &connWriter{w: &out}
	huge := strings.Repeat("x", maxOutboundFrame)
	if err := w.send(wire.Event{Event: wire.EventMessage, Data: huge}); err != nil || out.Len() != 0 {
		t.Fatalf("oversized event was written (%d bytes, %v)", out.Len(), err)
	}
	if err := w.send(wire.Response{ID: "7", OK: true, Result: huge}); err != nil {
		t.Fatal(err)
	}
	var resp wire.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil || resp.ID != "7" || resp.OK || resp.Error == "" || out.Len() > 200 {
		t.Fatalf("oversized reply was not replaced by an error: %q", out.String())
	}
	out.Reset()
	if err := w.send(wire.Response{ID: "8", OK: true, Result: "small"}); err != nil || !strings.HasSuffix(out.String(), "}\n") {
		t.Fatalf("normal reply framing changed: %q", out.String())
	}
}

func TestOutboundReactionsAndIdentifiersAreBounded(t *testing.T) {
	huge := strings.Repeat("x", 2<<20)
	msg := wire.Message{ID: huge, ConversationID: huge, Reactions: []wire.Reaction{{Emoji: huge, Count: 1}, {Emoji: "👍", Count: 2}}, Attachments: []wire.Attachment{{Key: huge, MimeType: huge}}}
	got := limitOutbound(msg).(wire.Message)
	if len(got.Reactions) != 1 || got.Reactions[0].Emoji != "👍" {
		t.Fatalf("reactions: %+v", got.Reactions)
	}
	if len(got.ID) > maxIDText || len(got.ConversationID) > maxIDText || len(got.Attachments[0].Key) > maxIDText || len(got.Attachments[0].MimeType) > maxIDText {
		t.Fatal("identifiers not bounded")
	}
	frame, err := json.Marshal(got)
	if err != nil || len(frame) > 16<<10 {
		t.Fatalf("bounded message still marshals to %d bytes", len(frame))
	}
	conv := limitOutbound(wire.Conversation{ID: huge, OutgoingID: huge, Participants: []wire.Participant{{ID: huge}}}).(wire.Conversation)
	if len(conv.ID) > maxIDText || len(conv.OutgoingID) > maxIDText || len(conv.Participants[0].ID) > maxIDText {
		t.Fatal("conversation identifiers not bounded")
	}
}

func TestOutboundContactTargetsAreBounded(t *testing.T) {
	long := strings.Repeat("é", maxMessageText)
	targets := make([]wire.ConversationTarget, maxContactTargets+10)
	targets[0] = wire.ConversationTarget{ID: long, Name: long, Detail: long}
	got := limitOutbound(wire.Response{OK: true, Result: targets}).(wire.Response).Result.([]wire.ConversationTarget)
	if len(got) != maxContactTargets {
		t.Fatalf("contact list has %d entries", len(got))
	}
	if len(got[0].ID) > maxIDText || len(got[0].Name) > maxDisplayText+len("…") || len(got[0].Detail) > maxDisplayText+len("…") {
		t.Fatalf("contact fields not bounded: %d %d %d", len(got[0].ID), len(got[0].Name), len(got[0].Detail))
	}
	if targets[0].Name != long || len(targets) != maxContactTargets+10 {
		t.Fatal("the backend's value was modified")
	}
}
