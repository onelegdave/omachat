package messenger

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	"go.mau.fi/whatsmeow/proto/waMsgApplication"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestDisappearingMessengerMessagesAreNotKept(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	b.handleTable(&table.LSTable{
		LSUpsertMessage: []*table.LSUpsertMessage{{ThreadKey: 8, MessageId: "secret-1", Text: "private disappearing text", TimestampMs: 2000, SenderId: 9, EphemeralDurationInSec: 300}},
		LSInsertAttachment: []*table.LSInsertAttachment{{
			MessageId: "secret-1", AttachmentFbid: "77", AttachmentType: table.AttachmentTypeImage,
			AttachmentMimeType: "image/jpeg", ImageUrl: "https://example.com/secret.jpg", Filename: "secret.jpg",
		}},
	})
	got := b.messages["8"]
	if len(got) != 1 || got[0].Text != lifetimeRestrictedText || len(got[0].Attachments) != 0 {
		t.Fatalf("restricted message kept content: %#v", got)
	}
	for key, media := range b.media {
		if strings.Contains(media.remoteURL, "secret") {
			t.Fatalf("restricted media remains downloadable as %q", key)
		}
	}
	for _, conv := range b.Conversations(0) {
		if strings.Contains(conv.Preview, "private") {
			t.Fatalf("restricted text reached the preview: %q", conv.Preview)
		}
	}

	// An ordinary message in the same thread is unaffected.
	b.handleTable(&table.LSTable{LSInsertMessage: []*table.LSInsertMessage{{ThreadKey: 8, MessageId: "plain-1", Text: "hello", TimestampMs: 3000, SenderId: 9}}})
	if got := b.messages["8"]; len(got) != 2 || got[1].Text != "hello" {
		t.Fatalf("ordinary message changed: %#v", got)
	}
}

func TestEncryptedMessengerLifetimeSettings(t *testing.T) {
	timer := &waMsgApplication.MessageApplication_EphemeralSetting{EphemeralExpiration: proto.Uint32(86400)}
	seenOnce := &waMsgApplication.MessageApplication_EphemeralSetting{EphemeralityType: waMsgApplication.MessageApplication_EphemeralSetting_SEEN_ONCE.Enum()}
	for name, tc := range map[string]struct {
		metadata *waMsgApplication.MessageApplication_Metadata
		want     bool
	}{
		"none":      {nil, false},
		"off":       {&waMsgApplication.MessageApplication_Metadata{Ephemeral: &waMsgApplication.MessageApplication_Metadata_ChatEphemeralSetting{ChatEphemeralSetting: &waMsgApplication.MessageApplication_EphemeralSetting{}}}, false},
		"timer":     {&waMsgApplication.MessageApplication_Metadata{Ephemeral: &waMsgApplication.MessageApplication_Metadata_ChatEphemeralSetting{ChatEphemeralSetting: timer}}, true},
		"seen once": {&waMsgApplication.MessageApplication_Metadata{Ephemeral: &waMsgApplication.MessageApplication_Metadata_ChatEphemeralSetting{ChatEphemeralSetting: seenOnce}}, true},
		"list":      {&waMsgApplication.MessageApplication_Metadata{Ephemeral: &waMsgApplication.MessageApplication_Metadata_EphemeralSettingList{EphemeralSettingList: &waMsgApplication.MessageApplication_Metadata_EphemeralSettingMap{EphemeralSetting: timer}}}, true},
	} {
		evt := &events.FBMessage{FBApplication: &waMsgApplication.MessageApplication{Metadata: tc.metadata}}
		if got := fbMessageRestricted(evt); got != tc.want {
			t.Errorf("%s: restricted = %v, want %v", name, got, tc.want)
		}
	}
	if fbMessageRestricted(&events.FBMessage{}) {
		t.Error("a message without application metadata was restricted")
	}
}
