package messenger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waConsumerApplication"
	"go.mau.fi/whatsmeow/proto/waMediaTransport"
	"go.mau.fi/whatsmeow/proto/waMsgApplication"
	waTypes "go.mau.fi/whatsmeow/types"
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

func encryptedText(id, text string, timestamp int64, restricted bool) *events.FBMessage {
	evt := &events.FBMessage{Message: &waConsumerApplication.ConsumerApplication{Payload: &waConsumerApplication.ConsumerApplication_Payload{Payload: &waConsumerApplication.ConsumerApplication_Payload_Content{Content: &waConsumerApplication.ConsumerApplication_Content{Content: &waConsumerApplication.ConsumerApplication_Content_MessageText{MessageText: &waCommon.MessageText{Text: proto.String(text)}}}}}}}
	evt.Info.Chat = waTypes.NewJID("7", waTypes.MessengerServer)
	evt.Info.Sender = waTypes.NewJID("42", waTypes.MessengerServer)
	evt.Info.ID = id
	evt.Info.Timestamp = time.UnixMilli(timestamp)
	if restricted {
		evt.FBApplication = &waMsgApplication.MessageApplication{Metadata: &waMsgApplication.MessageApplication_Metadata{Ephemeral: &waMsgApplication.MessageApplication_Metadata_ChatEphemeralSetting{ChatEphemeralSetting: &waMsgApplication.MessageApplication_EphemeralSetting{EphemeralExpiration: proto.Uint32(60)}}}}
	}
	return evt
}

func TestEncryptedReplayOfWithheldMessageStaysWithheld(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	b.handleE2EEEvent(encryptedText("e1", "secret", 2000, true))
	b.handleE2EEEvent(encryptedText("e1", "secret", 2000, false))
	for _, list := range b.messages {
		for _, msg := range list {
			if strings.Contains(msg.Text, "secret") {
				t.Fatalf("replay restored the text: %#v", msg)
			}
		}
	}
	for _, conv := range b.Conversations(0) {
		if strings.Contains(conv.Preview, "secret") {
			t.Fatalf("replay restored the snippet: %q", conv.Preview)
		}
	}
}

func TestWithholdingNeverDeletesTheUsersOwnFiles(t *testing.T) {
	paths := setupMessengerPaths(t)
	b := New(zerolog.Nop(), paths, nil)
	own := filepath.Join(t.TempDir(), "my-photo.jpg")
	cached := filepath.Join(paths.MessengerMediaDir(), "fb-cached.jpg")
	for _, file := range []string{own, cached} {
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	b.media["sent"] = &messengerMedia{key: "sent", path: own}
	b.media["got"] = &messengerMedia{key: "got", path: cached}
	b.forgetMediaLocked([]wire.Attachment{{Key: "sent"}, {Key: "got"}})
	if _, err := os.Stat(own); err != nil {
		t.Fatal("the user's own file was deleted")
	}
	if _, err := os.Stat(cached); !os.IsNotExist(err) {
		t.Fatal("the downloaded copy was kept")
	}
}

func TestRepeatedMediaRecordKeepsItsDownload(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	first := b.rememberRemoteLocked("m", "a", 0, &messengerMedia{remoteURL: "https://example.com/a.jpg", mime: "image/jpeg"}, table.AttachmentTypeImage)
	b.media[first.Key].path = "/cache/fb-a.jpg"
	b.rememberRemoteLocked("m", "a", 0, &messengerMedia{remoteURL: "https://example.com/a.jpg", mime: "image/jpeg"}, table.AttachmentTypeImage)
	if b.media[first.Key].path != "/cache/fb-a.jpg" {
		t.Fatal("repeated record lost its downloaded path")
	}
}

func TestWithholdingRemovesOnlyThatFilesPreviews(t *testing.T) {
	paths := setupMessengerPaths(t)
	b := New(zerolog.Nop(), paths, nil)
	dir := paths.MessengerMediaDir()
	revoked, kept := filepath.Join(dir, "fb-revoked.jpg"), filepath.Join(dir, "fb-kept.jpg")
	previews := filepath.Join(dir, appStore.PreviewDirName)
	if err := os.MkdirAll(previews, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{revoked, kept,
		filepath.Join(previews, appStore.PreviewPrefix(revoked)+"a.jpg"),
		filepath.Join(previews, appStore.PreviewPrefix(kept)+"b.jpg")} {
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	b.media["r"] = &messengerMedia{key: "r", path: revoked}
	b.forgetMediaLocked([]wire.Attachment{{Key: "r"}})
	entries, _ := os.ReadDir(previews)
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), appStore.PreviewPrefix(kept)) {
		t.Fatalf("previews after withholding: %v", entries)
	}
}

func TestEncryptedReplayKeepsDownloadForLaterWithholding(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	key := messengerMediaKey("e2", "e2ee", 0)
	b.media[key] = &messengerMedia{key: key, path: "/cache/fb-e2.jpg"}
	b.handleE2EEEvent(encryptedImage(t, "e2", 3000))
	if b.media[key] == nil || b.media[key].path != "/cache/fb-e2.jpg" {
		t.Fatalf("replay lost the downloaded path: %#v", b.media[key])
	}
}

func encryptedImage(t *testing.T, id string, timestamp int64) *events.FBMessage {
	t.Helper()
	directPath, mimeType, length := "/mms/image", "image/jpeg", uint64(42)
	encoded := &waConsumerApplication.ConsumerApplication_ImageMessage{}
	if err := encoded.Set(&waMediaTransport.ImageTransport{Integral: &waMediaTransport.ImageTransport_Integral{Transport: &waMediaTransport.WAMediaTransport{
		Integral:  &waMediaTransport.WAMediaTransport_Integral{DirectPath: &directPath, FileSHA256: []byte{1}, FileEncSHA256: []byte{2}, MediaKey: []byte{3}},
		Ancillary: &waMediaTransport.WAMediaTransport_Ancillary{Mimetype: &mimeType, FileLength: &length},
	}}}); err != nil {
		t.Fatal(err)
	}
	evt := &events.FBMessage{Message: &waConsumerApplication.ConsumerApplication{Payload: &waConsumerApplication.ConsumerApplication_Payload{Payload: &waConsumerApplication.ConsumerApplication_Payload_Content{Content: &waConsumerApplication.ConsumerApplication_Content{Content: &waConsumerApplication.ConsumerApplication_Content_ImageMessage{ImageMessage: encoded}}}}}}
	evt.Info.Chat = waTypes.NewJID("7", waTypes.MessengerServer)
	evt.Info.Sender = waTypes.NewJID("42", waTypes.MessengerServer)
	evt.Info.ID = id
	evt.Info.Timestamp = time.UnixMilli(timestamp)
	return evt
}
