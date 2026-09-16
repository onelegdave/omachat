package messenger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waConsumerApplication"
	"go.mau.fi/whatsmeow/proto/waMediaTransport"
	waStore "go.mau.fi/whatsmeow/store"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestBackendRequiresConnection(t *testing.T) {
	log := zerolog.Nop()

	// Create mock paths
	tmp, err := os.MkdirTemp("", "messenger-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	os.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	os.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	os.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmp, "run"))
	defer os.Unsetenv("XDG_DATA_HOME")
	defer os.Unsetenv("XDG_CACHE_HOME")
	defer os.Unsetenv("XDG_RUNTIME_DIR")

	paths, err := appStore.NewPaths()
	if err != nil {
		t.Fatal(err)
	}

	events := make(chan wire.Event, 10)
	publish := func(e wire.Event) {
		events <- e
	}

	b := New(log, paths, publish)
	if b.Status().State != wire.StateUnpaired {
		t.Errorf("expected unpaired state, got %v", b.Status().State)
	}

	ctx := context.Background()

	if err := b.Refresh(ctx); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("expected ErrNotConfigured for Refresh, got %v", err)
	}

	if _, err := b.Send(ctx, wire.SendParams{ConversationID: "1", Text: "hello"}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("expected ErrNotConfigured for Send, got %v", err)
	}
}

func TestBackend_Unpair(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "messenger-test")
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	os.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	paths, _ := appStore.NewPaths()

	b := New(zerolog.Nop(), paths, func(wire.Event) {})
	if err := os.WriteFile(paths.MessengerSessionFile(), []byte(`{"cookies":{"c_user":"1"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	b.setState(wire.StateConnected, "")

	if err := b.Unpair(context.Background()); err != nil {
		t.Errorf("unpair failed: %v", err)
	}
	if b.Status().State != wire.StateUnpaired {
		t.Errorf("expected unpaired after Unpair, got %v", b.Status().State)
	}
	if _, err := os.Stat(paths.MessengerSessionFile()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Messenger session was not removed: %v", err)
	}
}

func TestNilDisconnectEventsDoNotPanic(t *testing.T) {
	b := New(zerolog.Nop(), nil, func(wire.Event) {})

	b.handleMetaEvent(context.Background(), &messagix.TransientDisconnectEvent{})
	if got := b.Status(); got.State != wire.StateDisconnected || got.Error != "Messenger transport disconnected" {
		t.Fatalf("unexpected transient disconnect status: %#v", got)
	}

	b.handleMetaEvent(context.Background(), &messagix.PermanentErrorEvent{})
	if got := b.Status(); got.State != wire.StateError || got.Error != "Messenger transport stopped" {
		t.Fatalf("unexpected permanent disconnect status: %#v", got)
	}
}

func TestMessengerDeviceRegistrationDetection(t *testing.T) {
	if !needsMessengerRegistration(nil) {
		t.Fatal("nil device must be registered")
	}
	if !needsMessengerRegistration(&waStore.Device{}) {
		t.Fatal("new unsaved device without an ID must be registered")
	}
	if needsMessengerRegistration(&waStore.Device{ID: &waTypes.JID{User: "1", Device: 1, Server: waTypes.MessengerServer}}) {
		t.Fatal("saved device with an ID must be reused")
	}
}

func TestEnsureMessengerSQLiteFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messenger.db")
	if err := os.WriteFile(path, []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureMessengerSQLiteFile(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("Messenger database mode = %04o, want 0600", got)
	}

	target := filepath.Join(t.TempDir(), "target.db")
	if err := os.WriteFile(target, []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "messenger.db")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := ensureMessengerSQLiteFile(link); err == nil {
		t.Fatal("expected Messenger database symlink to be rejected")
	}
}

func TestHandleTableIncludesHistoricalUpserts(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	b.selfID = 42
	b.handleTable(&table.LSTable{LSUpsertMessage: []*table.LSUpsertMessage{
		{ThreadKey: 7, MessageId: "history-1", Text: "historical text", TimestampMs: 1_700_000_000_123, SenderId: 42},
	}})

	got := b.messages["7"]
	if len(got) != 1 {
		t.Fatalf("historical upsert count = %d, want 1", len(got))
	}
	if got[0].Text != "historical text" || !got[0].FromMe {
		t.Fatalf("historical upsert mapped incorrectly: %#v", got[0])
	}
	if got[0].Timestamp != 1_700_000_000_123_000 {
		t.Fatalf("timestamp = %d, want microseconds", got[0].Timestamp)
	}
}

func TestHandleTablePublishesMessagesWithSenderNames(t *testing.T) {
	var published []wire.Event
	b := New(zerolog.Nop(), nil, func(event wire.Event) { published = append(published, event) })
	b.handleTable(&table.LSTable{
		LSVerifyContactRowExists: []*table.LSVerifyContactRowExists{{ContactId: 42, Name: "Ada Lovelace"}},
		LSUpsertMessage: []*table.LSUpsertMessage{{
			ThreadKey: 7, MessageId: "live-1", Text: "I see it", TimestampMs: 1_700_000_000_123, SenderId: 42,
		}},
	})

	got := b.messages["7"]
	if len(got) != 1 || got[0].SenderName != "Ada Lovelace" {
		t.Fatalf("stored message sender = %#v", got)
	}
	found := false
	for _, event := range published {
		msg, ok := event.Data.(wire.Message)
		if event.Event == wire.EventMessage && event.Network == wire.NetworkMessenger && ok && msg.ID == "live-1" && msg.SenderName == "Ada Lovelace" {
			found = true
		}
	}
	if !found {
		t.Fatalf("published events did not contain named live message: %#v", published)
	}
}

func TestLateContactNameUpdatesStoredMessages(t *testing.T) {
	var published []wire.Event
	b := New(zerolog.Nop(), nil, func(event wire.Event) { published = append(published, event) })
	b.handleTable(&table.LSTable{LSUpsertMessage: []*table.LSUpsertMessage{{
		ThreadKey: 7, MessageId: "live-1", Text: "I see it", TimestampMs: 1_700_000_000_123, SenderId: 42,
	}}})
	published = nil
	b.handleTable(&table.LSTable{LSDeleteThenInsertContact: []*table.LSDeleteThenInsertContact{{Id: 42, Name: "Ada Lovelace"}}})

	if got := b.messages["7"][0].SenderName; got != "Ada Lovelace" {
		t.Fatalf("late sender name = %q", got)
	}
	found := false
	for _, event := range published {
		msg, ok := event.Data.(wire.Message)
		if event.Event == wire.EventMessage && ok && msg.ID == "live-1" && msg.SenderName == "Ada Lovelace" {
			found = true
		}
	}
	if !found {
		t.Fatalf("late contact update was not published: %#v", published)
	}
}

func TestConversationNamesUseTitlesContactsAndParticipants(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	b.selfID = 1
	b.handleTable(&table.LSTable{
		LSVerifyContactRowExists: []*table.LSVerifyContactRowExists{
			{ContactId: 2, Name: "Ada Lovelace"},
			{ContactId: 3, Name: "Grace Hopper"},
		},
		LSAddParticipantIdToGroupThread: []*table.LSAddParticipantIdToGroupThread{
			{ThreadKey: 70, ContactId: 1},
			{ThreadKey: 70, ContactId: 2},
			{ThreadKey: 70, ContactId: 3},
		},
		LSDeleteThenInsertThread: []*table.LSDeleteThenInsertThread{
			{ThreadKey: 2, ThreadType: table.ONE_TO_ONE},
			{ThreadKey: 70, ThreadType: table.GROUP_THREAD, MemberCount: 3},
			{ThreadKey: 80, ThreadType: table.GROUP_THREAD, ThreadName: "Named group", MemberCount: 3},
		},
	})

	if got := b.convs["2"].Name; got != "Ada Lovelace" {
		t.Fatalf("one-to-one name = %q", got)
	}
	if got := b.convs["70"].Name; got != "Ada Lovelace, Grace Hopper" {
		t.Fatalf("derived group name = %q", got)
	}
	if got := b.convs["80"].Name; got != "Named group" {
		t.Fatalf("explicit group name = %q", got)
	}

	b.handleTable(&table.LSTable{LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{{
		ThreadKey: 80, ThreadType: table.GROUP_THREAD,
	}}})
	if got := b.convs["80"].Name; got != "Named group" {
		t.Fatalf("blank partial update replaced explicit group name with %q", got)
	}
}

func TestEncryptedConversationNameUsesMappedContact(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	b.handleTable(&table.LSTable{
		LSVerifyContactRowExists: []*table.LSVerifyContactRowExists{{ContactId: 42, Name: "Ada Lovelace"}},
		LSVerifyHybridThreadExists: []*table.LSVerifyHybridThreadExists{{
			ThreadKey: 900, ThreadJID: 42, ThreadType: table.ENCRYPTED_OVER_WA_ONE_TO_ONE,
		}},
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{{
			ThreadKey: 900, ThreadType: table.ENCRYPTED_OVER_WA_ONE_TO_ONE,
		}},
	})
	if got := b.convs["900"].Name; got != "Ada Lovelace" {
		t.Fatalf("encrypted one-to-one name = %q", got)
	}
}

func TestMessengerTimeTimestampUsesWireMicroseconds(t *testing.T) {
	timestamp := time.Date(2026, time.September, 15, 22, 30, 45, 123456000, time.UTC)
	if got, want := messengerTimeTimestamp(timestamp), timestamp.UnixMicro(); got != want {
		t.Fatalf("timestamp = %d, want microseconds %d", got, want)
	}
	if got := messengerTimeTimestamp(timestamp); got < 1_000_000_000_000_000 {
		t.Fatalf("timestamp = %d, looks like milliseconds and would render near 1970", got)
	}
}

func TestHandleTableLabelsUnsupportedHistory(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	b.handleTable(&table.LSTable{LSUpsertMessage: []*table.LSUpsertMessage{
		{ThreadKey: 8, MessageId: "history-media", TimestampMs: 1_700_000_000_123, SenderId: 9},
	}})

	got := b.messages["8"]
	if len(got) != 1 || got[0].Text != unsupportedMessageText {
		t.Fatalf("unsupported historical message = %#v", got)
	}
}

func TestHandleTableMapsMessengerAttachments(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	b.handleTable(&table.LSTable{
		LSUpsertMessage: []*table.LSUpsertMessage{{ThreadKey: 8, MessageId: "photo-1", TimestampMs: 1_700_000_000_123, SenderId: 9}},
		LSInsertAttachment: []*table.LSInsertAttachment{{
			MessageId: "photo-1", AttachmentFbid: "77", AttachmentIndex: 0,
			AttachmentType: table.AttachmentTypeImage, AttachmentMimeType: "image/jpeg",
			ImageUrl: "https://example.com/photo.jpg", Filename: "photo.jpg", Filesize: 1234,
		}},
	})

	got := b.messages["8"]
	if len(got) != 1 || len(got[0].Attachments) != 1 {
		t.Fatalf("mapped message = %#v", got)
	}
	att := got[0].Attachments[0]
	if got[0].Text != "" || !att.IsImage || att.MimeType != "image/jpeg" || att.Key == "" {
		t.Fatalf("mapped attachment = %#v in %#v", att, got[0])
	}
	if b.media[att.Key] == nil || b.media[att.Key].remoteURL != "https://example.com/photo.jpg" {
		t.Fatalf("media key did not resolve to its private record")
	}
}

func TestLateMessengerAttachmentUpdatesExistingMessage(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	b.handleTable(&table.LSTable{LSUpsertMessage: []*table.LSUpsertMessage{{ThreadKey: 8, MessageId: "late-1", TimestampMs: 1, SenderId: 9}}})
	b.handleTable(&table.LSTable{LSInsertBlobAttachment: []*table.LSInsertBlobAttachment{{
		MessageId: "late-1", AttachmentFbid: "88", AttachmentType: table.AttachmentTypeAudio,
		PlayableUrl: "https://example.com/voice.mp4", PlayableUrlMimeType: "audio/mp4",
	}}})
	got := b.messages["8"][0]
	if got.Text != "" || len(got.Attachments) != 1 || !got.Attachments[0].IsAudio {
		t.Fatalf("late attachment message = %#v", got)
	}
}

func TestMessengerAvatarJobsPreferGroupThenContact(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	b.contactAvatars[2] = "https://example.com/contact.jpg"
	b.threadAvatars[70] = "https://example.com/group.jpg"
	b.convs["2"] = wire.Conversation{ID: "2"}
	b.convs["70"] = wire.Conversation{ID: "70", IsGroup: true}
	b.mu.Lock()
	jobs := b.avatarJobsLocked()
	b.mu.Unlock()
	if len(jobs) != 2 {
		t.Fatalf("avatar jobs = %#v", jobs)
	}
	urls := map[string]string{}
	for _, job := range jobs {
		urls[job.conversationID] = job.rawURL
	}
	if urls["2"] != "https://example.com/contact.jpg" || urls["70"] != "https://example.com/group.jpg" {
		t.Fatalf("avatar sources = %#v", urls)
	}
}

func TestMessengerUploadRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := openMessengerUpload(link); err == nil {
		t.Fatal("expected symlink upload to be rejected")
	}
}

func TestEncryptedMessengerImageMapsToAttachment(t *testing.T) {
	directPath := "/mms/image"
	mimeType := "image/jpeg"
	length := uint64(42)
	encoded := &waConsumerApplication.ConsumerApplication_ImageMessage{}
	err := encoded.Set(&waMediaTransport.ImageTransport{Integral: &waMediaTransport.ImageTransport_Integral{Transport: &waMediaTransport.WAMediaTransport{
		Integral:  &waMediaTransport.WAMediaTransport_Integral{DirectPath: &directPath, FileSHA256: []byte{1}, FileEncSHA256: []byte{2}, MediaKey: []byte{3}},
		Ancillary: &waMediaTransport.WAMediaTransport_Ancillary{Mimetype: &mimeType, FileLength: &length},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	evt := &events.FBMessage{Message: &waConsumerApplication.ConsumerApplication{Payload: &waConsumerApplication.ConsumerApplication_Payload{Payload: &waConsumerApplication.ConsumerApplication_Payload_Content{Content: &waConsumerApplication.ConsumerApplication_Content{Content: &waConsumerApplication.ConsumerApplication_Content_ImageMessage{ImageMessage: encoded}}}}}}
	text, media := fbContent(evt)
	if text != "" || media == nil || !media.isImage || media.mime != mimeType || media.size != 42 || media.fbType != whatsmeow.MediaImage {
		t.Fatalf("encrypted image mapping = text %q media %#v", text, media)
	}
}

func TestMessengerMediaURLRejectsLocalTargets(t *testing.T) {
	if err := downloadMessengerURL(context.Background(), "https://127.0.0.1/private", &strings.Builder{}, 1024); err == nil {
		t.Fatal("expected local media URL to be rejected")
	}
}

func TestMessengerAnimatedVideoContainerIsNotClassifiedAsImage(t *testing.T) {
	media := &messengerMedia{name: "gif-1777463936921843", mime: "video/mp4"}
	classifyMessengerMedia(media, table.AttachmentTypeAnimatedImage)
	if !media.isVideo || media.isImage || !media.isGIF {
		t.Fatalf("animated video classification = image:%v gif:%v video:%v", media.isImage, media.isGIF, media.isVideo)
	}

	videoTypedGIF := &messengerMedia{name: "gif-1777463936921843", mime: "video/mp4"}
	classifyMessengerMedia(videoTypedGIF, table.AttachmentTypeVideo)
	if !videoTypedGIF.isVideo || videoTypedGIF.isImage || !videoTypedGIF.isGIF {
		t.Fatalf("video-typed GIF classification = image:%v gif:%v video:%v", videoTypedGIF.isImage, videoTypedGIF.isGIF, videoTypedGIF.isVideo)
	}

	gif := &messengerMedia{name: "animation.gif", mime: "image/gif"}
	classifyMessengerMedia(gif, table.AttachmentTypeAnimatedImage)
	if !gif.isImage || !gif.isGIF || gif.isVideo {
		t.Fatalf("GIF classification = image:%v gif:%v video:%v", gif.isImage, gif.isGIF, gif.isVideo)
	}
}

func TestEncryptedHistoryReturnsAnHonestNotice(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	b.threadTypes[9] = table.ENCRYPTED_OVER_WA_ONE_TO_ONE
	b.messages["9"] = []wire.Message{{ID: "live", ConversationID: "9", Text: "received while connected"}}

	result, err := b.Messages(context.Background(), wire.MessagesParams{ConversationID: "9", Count: 60})
	if err != nil {
		t.Fatal(err)
	}
	if result.HistoryNotice != encryptedHistoryNotice {
		t.Fatalf("history notice = %q", result.HistoryNotice)
	}
	if len(result.Messages) != 1 || result.Messages[0].ID != "live" {
		t.Fatalf("live encrypted messages = %#v", result.Messages)
	}

	b.threadTypes[10] = table.ONE_TO_ONE
	result, err = b.Messages(context.Background(), wire.MessagesParams{ConversationID: "10", Count: 60})
	if err != nil {
		t.Fatal(err)
	}
	if result.HistoryNotice != "" {
		t.Fatalf("unencrypted history notice = %q, want empty", result.HistoryNotice)
	}
}
