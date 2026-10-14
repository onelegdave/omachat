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

// Reproductions from review: a same-moment thread snippet, an update that
// would inherit earlier media, and media arriving after the message.
func TestRestrictedMessengerMessagesStayWithheld(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	b.handleTable(&table.LSTable{
		LSUpsertMessage:    []*table.LSUpsertMessage{{ThreadKey: 5, MessageId: "m1", Text: "visible first", TimestampMs: 1000, SenderId: 9}},
		LSInsertAttachment: []*table.LSInsertAttachment{{MessageId: "m1", AttachmentFbid: "1", AttachmentType: table.AttachmentTypeImage, AttachmentMimeType: "image/jpeg", ImageUrl: "https://example.com/first.jpg"}},
	})
	if got := b.messages["5"]; len(got) != 1 || len(got[0].Attachments) != 1 {
		t.Fatalf("setup: %#v", got)
	}
	b.handleTable(&table.LSTable{
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{{ThreadKey: 5, ThreadName: "Fixture", Snippet: "secret snippet", LastActivityTimestampMs: 2000}},
		LSUpsertMessage:        []*table.LSUpsertMessage{{ThreadKey: 5, MessageId: "m1", Text: "secret", TimestampMs: 2000, SenderId: 9, EphemeralExpirationTs: 9999}},
	})
	got := b.messages["5"]
	if len(got) != 1 || got[0].Text != lifetimeRestrictedText || len(got[0].Attachments) != 0 {
		t.Fatalf("restricted update kept content: %#v", got)
	}
	if len(b.media) != 0 {
		t.Fatalf("earlier media still downloadable: %d records", len(b.media))
	}
	if conv := b.convs["5"]; strings.Contains(conv.Preview, "secret") {
		t.Fatalf("snippet kept: %q", conv.Preview)
	}
	// A repeated thread snapshot with the same snippet is redacted again.
	b.handleTable(&table.LSTable{LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{{ThreadKey: 5, ThreadName: "Fixture", Snippet: "secret snippet", LastActivityTimestampMs: 2000}}})
	if conv := b.convs["5"]; strings.Contains(conv.Preview, "secret") {
		t.Fatalf("snippet returned: %q", conv.Preview)
	}
	// Late media and a later update without lifetime fields change nothing.
	b.handleTable(&table.LSTable{
		LSInsertAttachment: []*table.LSInsertAttachment{{MessageId: "m1", AttachmentFbid: "2", AttachmentType: table.AttachmentTypeImage, AttachmentMimeType: "image/jpeg", ImageUrl: "https://example.com/late.jpg"}},
		LSUpsertMessage:    []*table.LSUpsertMessage{{ThreadKey: 5, MessageId: "m1", Text: "secret again", TimestampMs: 2000, SenderId: 9}},
	})
	got = b.messages["5"]
	if len(got) != 1 || got[0].Text != lifetimeRestrictedText || len(got[0].Attachments) != 0 || len(b.media) != 0 {
		t.Fatalf("withheld message came back: %#v, %d media", got, len(b.media))
	}
}
