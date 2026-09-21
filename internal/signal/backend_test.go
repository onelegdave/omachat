package signal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

type fakeCaller struct {
	mu        sync.Mutex
	notify    func(json.RawMessage)
	started   bool
	closed    bool
	calls     []string
	params    map[string]any
	responses map[string]string
	onExit    func(error)
}

func (f *fakeCaller) Start(_ context.Context, _ string, notify func(json.RawMessage), onExit func(error)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started, f.notify, f.onExit = true, notify, onExit
	return nil
}
func (f *fakeCaller) Close() error { f.mu.Lock(); defer f.mu.Unlock(); f.closed = true; return nil }
func (f *fakeCaller) Call(_ context.Context, method string, params any, out any) error {
	f.mu.Lock()
	f.calls = append(f.calls, method)
	if f.params == nil {
		f.params = make(map[string]any)
	}
	f.params[method] = params
	f.mu.Unlock()
	var raw string
	switch method {
	case "listAccounts":
		raw = `[{"number":"+15550001111"}]`
	case "listContacts":
		raw = `[]`
	case "listGroups":
		raw = `[]`
	case "startLink":
		raw = `{"deviceLinkUri":"sgnl://linkdevice?uuid=test"}`
	case "finishLink":
		raw = `{"number":"+15550001111"}`
	case "send":
		raw = `{"timestamp":1700000000000}`
	case "getAttachment":
		raw = `{"data":"aGVsbG8="}`
	default:
		raw = `{}`
	}
	if response, ok := f.responses[method]; ok {
		raw = response
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal([]byte(raw), out)
}

func TestSendUsesSignalCLIParameterNames(t *testing.T) {
	fake := &fakeCaller{}
	b := New(zerolog.Nop(), testPaths(t), nil)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Send(context.Background(), wire.SendParams{ConversationID: directPrefix + "+15550002222", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	direct := fake.params["send"].(map[string]any)
	if _, ok := direct["recipient"]; !ok {
		t.Fatalf("direct send params = %#v", direct)
	}
	if _, ok := direct["recipients"]; ok {
		t.Fatalf("deprecated plural recipient param present: %#v", direct)
	}
	conversations := b.Conversations(0)
	if len(conversations) != 1 || conversations[0].ID != directPrefix+"+15550002222" {
		t.Fatalf("direct conversation was not created: %#v", conversations)
	}
	if _, err := b.Send(context.Background(), wire.SendParams{ConversationID: groupPrefix + "base64-group", Text: "hello group"}); err != nil {
		t.Fatal(err)
	}
	group := fake.params["send"].(map[string]any)
	if got, ok := group["groupId"].(string); !ok || got != "base64-group" {
		t.Fatalf("group send params = %#v", group)
	}
}

func testPaths(t *testing.T) *appStore.Paths {
	t.Helper()
	root := t.TempDir()
	p := &appStore.Paths{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Runtime: filepath.Join(root, "run")}
	for _, dir := range []string{p.Data, p.Cache, p.Runtime, p.SignalDataDir(), p.SignalMediaDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestBackendStartsAndStopsOptionalClient(t *testing.T) {
	fake := &fakeCaller{}
	b := New(zerolog.Nop(), testPaths(t), nil)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := b.Status().State; got != wire.StateConnected {
		t.Fatalf("state = %q", got)
	}
	b.Stop()
	if !fake.started || !fake.closed {
		t.Fatalf("lifecycle: started=%v closed=%v", fake.started, fake.closed)
	}
}

func TestUnexpectedSignalCLIExitUpdatesConnectionState(t *testing.T) {
	fake := &fakeCaller{}
	b := New(zerolog.Nop(), testPaths(t), nil)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	fake.onExit(errors.New("process failed"))
	if status := b.Status(); status.State != wire.StateDisconnected || status.Error == "" {
		t.Fatalf("status = %#v", status)
	}
}

func TestIncomingAndSyncMessagesUseSeparateConversationDirections(t *testing.T) {
	fake := &fakeCaller{}
	b := New(zerolog.Nop(), testPaths(t), nil)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	fake.notify(json.RawMessage(`{"account":"+15550001111","envelope":{"sourceNumber":"+15550002222","sourceName":"Taylor","timestamp":1700000000000,"dataMessage":{"timestamp":1700000000000,"message":"hello"}}}`))
	fake.notify(json.RawMessage(`{"account":"+15550001111","envelope":{"timestamp":1700000001000,"syncMessage":{"sentMessage":{"destinationNumber":"+15550002222","timestamp":1700000001000,"message":"hi back"}}}}`))
	convID := directPrefix + "+15550002222"
	result, err := b.Messages(context.Background(), wire.MessagesParams{ConversationID: convID, Count: 60})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 || result.Messages[0].FromMe || !result.Messages[1].FromMe {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if got := b.Conversations(10); len(got) != 1 || got[0].Preview != "hi back" {
		t.Fatalf("conversations = %#v", got)
	}
}

func TestRefreshDoesNotExposeEmptyContactsOrGroups(t *testing.T) {
	fake := &fakeCaller{responses: map[string]string{
		"listContacts": `[{"number":"+15550002222","name":"Taylor"}]`,
		"listGroups":   `[{"id":"group-id","name":"Book Club","isMember":true}]`,
	}}
	b := New(zerolog.Nop(), testPaths(t), nil)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if conversations := b.Conversations(0); len(conversations) != 0 {
		t.Fatalf("refresh exposed empty conversations: %#v", conversations)
	}
	fake.notify(json.RawMessage(`{"envelope":{"sourceNumber":"+15550002222","sourceName":"Taylor","dataMessage":{"timestamp":1700000000000,"message":"hello","groupInfo":{"groupId":"group-id","groupName":"Book Club"}}}}`))
	conversations := b.Conversations(0)
	if len(conversations) != 1 || conversations[0].ID != groupPrefix+"group-id" || !conversations[0].IsGroup {
		t.Fatalf("active group was not exposed: %#v", conversations)
	}
}

func TestIncomingAndSyncedGroupMessagesPreserveDirectionAndSender(t *testing.T) {
	fake := &fakeCaller{}
	b := New(zerolog.Nop(), testPaths(t), nil)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	fake.notify(json.RawMessage(`{"envelope":{"sourceUuid":"sender-uuid","sourceNumber":"+15550002222","sourceName":"Taylor","dataMessage":{"timestamp":1700000000000,"message":"hello group","groupInfo":{"groupId":"group-id","groupName":"Book Club"}}}}`))
	fake.notify(json.RawMessage(`{"envelope":{"syncMessage":{"sentMessage":{"timestamp":1700000001000,"message":"hi back","groupInfo":{"groupId":"group-id","groupName":"Book Club"}}}}}`))
	result, err := b.Messages(context.Background(), wire.MessagesParams{ConversationID: groupPrefix + "group-id"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("group messages = %#v", result.Messages)
	}
	incoming, outgoing := result.Messages[0], result.Messages[1]
	if incoming.FromMe || incoming.SenderID != "sender-uuid" || incoming.SenderName != "Taylor" {
		t.Fatalf("incoming sender = %#v", incoming)
	}
	if !outgoing.FromMe || outgoing.SenderID != "+15550001111" {
		t.Fatalf("synced outgoing message = %#v", outgoing)
	}
}

func TestPairingPublishesQRAndCompletes(t *testing.T) {
	fake := &fakeCaller{}
	events := make(chan wire.Event, 8)
	b := New(zerolog.Nop(), testPaths(t), func(event wire.Event) { events <- event })
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	uri, err := b.StartPairing(context.Background())
	if err != nil || uri == "" {
		t.Fatalf("pair: %q %v", uri, err)
	}
	if got := b.Status(); got.State != wire.StatePairing || got.QRURL != uri {
		t.Fatalf("pairing status = %#v", got)
	}
	deadline := time.After(time.Second)
	for b.Status().State != wire.StateConnected {
		select {
		case <-events:
		case <-deadline:
			t.Fatal("pairing did not complete")
		}
	}
}

func TestReactionsUpdateTargetWithoutBlankMessages(t *testing.T) {
	fake := &fakeCaller{}
	b := New(zerolog.Nop(), testPaths(t), nil)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	convID := directPrefix + "+15550002222"
	fake.notify(json.RawMessage(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"timestamp":1700000000000,"message":"hello"}}}`))
	fake.notify(json.RawMessage(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"reaction":{"emoji":"👍","targetSentTimestamp":1700000000000}}}}`))
	result, _ := b.Messages(context.Background(), wire.MessagesParams{ConversationID: convID})
	if len(result.Messages) != 1 || len(result.Messages[0].Reactions) != 1 || result.Messages[0].Reactions[0].Emoji != "👍" {
		t.Fatalf("reaction messages = %#v", result.Messages)
	}
	fake.notify(json.RawMessage(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"reaction":{"emoji":"❤️","targetSentTimestamp":1700000000000}}}}`))
	result, _ = b.Messages(context.Background(), wire.MessagesParams{ConversationID: convID})
	if got := result.Messages[0].Reactions; len(got) != 1 || got[0].Emoji != "❤️" {
		t.Fatalf("replacement = %#v", got)
	}
	fake.notify(json.RawMessage(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"reaction":{"emoji":"❤️","targetSentTimestamp":1700000000000,"isRemove":true}}}}`))
	result, _ = b.Messages(context.Background(), wire.MessagesParams{ConversationID: convID})
	if len(result.Messages[0].Reactions) != 0 {
		t.Fatalf("removal = %#v", result.Messages[0].Reactions)
	}
}

func TestOutgoingReactionTogglesAndUsesSignalCLIParameters(t *testing.T) {
	fake := &fakeCaller{}
	b := New(zerolog.Nop(), testPaths(t), nil)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	fake.notify(json.RawMessage(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"timestamp":1700000000000,"message":"hello"}}}`))
	p := wire.ReactParams{ConversationID: directPrefix + "+15550002222", MessageID: messageID(1700000000000, "+15550002222"), Emoji: "👍"}
	if err := b.React(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	params := fake.params["sendReaction"].(map[string]any)
	if params["targetAuthor"] != "+15550002222" || params["targetTimestamp"] != int64(1700000000000) || params["remove"] != false {
		t.Fatalf("params = %#v", params)
	}
	if err := b.React(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if params = fake.params["sendReaction"].(map[string]any); params["remove"] != true {
		t.Fatalf("toggle params = %#v", params)
	}
}

func TestGroupReactionUsesGroupTarget(t *testing.T) {
	fake := &fakeCaller{}
	b := New(zerolog.Nop(), testPaths(t), nil)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	fake.notify(json.RawMessage(`{"envelope":{"sourceUuid":"sender-uuid","sourceName":"Taylor","dataMessage":{"timestamp":1700000000000,"message":"hello","groupInfo":{"groupId":"group-id","groupName":"Book Club"}}}}`))
	p := wire.ReactParams{ConversationID: groupPrefix + "group-id", MessageID: messageID(1700000000000, "sender-uuid"), Emoji: "👍"}
	if err := b.React(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	params := fake.params["sendReaction"].(map[string]any)
	if params["groupId"] != "group-id" || params["targetAuthor"] != "sender-uuid" || params["targetTimestamp"] != int64(1700000000000) {
		t.Fatalf("group reaction params = %#v", params)
	}
}

func TestPhoneSyncedReactionIsMarkedMine(t *testing.T) {
	fake := &fakeCaller{}
	b := New(zerolog.Nop(), testPaths(t), nil)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	convID := directPrefix + "+15550002222"
	fake.notify(json.RawMessage(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"timestamp":1700000000000,"message":"hello"}}}`))
	fake.notify(json.RawMessage(`{"envelope":{"syncMessage":{"sentMessage":{"destinationNumber":"+15550002222","dataMessage":{"reaction":{"emoji":"👍","targetSentTimestamp":1700000000000}}}}}}`))
	result, _ := b.Messages(context.Background(), wire.MessagesParams{ConversationID: convID})
	if got := result.Messages[0].Reactions; len(got) != 1 || !got[0].Mine || got[0].Emoji != "👍" {
		t.Fatalf("synced reaction = %#v", got)
	}
}

func TestIncomingAttachmentDownloadsLazily(t *testing.T) {
	fake := &fakeCaller{}
	paths := testPaths(t)
	b := New(zerolog.Nop(), paths, nil)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	convID := directPrefix + "+15550002222"
	fake.notify(json.RawMessage(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"timestamp":1700000000000,"attachments":[{"id":"attachment-1","contentType":"image/png","filename":"photo.png","size":5,"width":10,"height":20}]}}}`))
	result, _ := b.Messages(context.Background(), wire.MessagesParams{ConversationID: convID})
	if len(result.Messages) != 1 || len(result.Messages[0].Attachments) != 1 || result.Messages[0].Attachments[0].Path != "" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	media, err := b.Media(context.Background(), wire.MediaParams{Key: mediaPrefix + "attachment-1"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(media.Path)
	if err != nil || string(data) != "hello" {
		t.Fatalf("media = %q, %v", data, err)
	}
	params := fake.params["getAttachment"].(map[string]any)
	if params["id"] != "attachment-1" {
		t.Fatalf("params = %#v", params)
	}
}

func TestSendMediaUsesAttachmentAndVoiceNoteParameters(t *testing.T) {
	fake := &fakeCaller{}
	b := New(zerolog.Nop(), testPaths(t), nil)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "voice.ogg")
	if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := b.SendMedia(context.Background(), wire.SendMediaParams{ConversationID: groupPrefix + "group-id", Path: path, Caption: "listen"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Message == nil || len(result.Message.Attachments) != 1 || !result.Message.Attachments[0].IsAudio {
		t.Fatalf("result = %#v", result)
	}
	params := fake.params["send"].(map[string]any)
	if params["groupId"] != "group-id" || params["voiceNote"] != true || params["message"] != "listen" {
		t.Fatalf("params = %#v", params)
	}
}

func TestGroupImageUsesAttachmentAndGroupParameters(t *testing.T) {
	fake := &fakeCaller{}
	b := New(zerolog.Nop(), testPaths(t), nil)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.SendMedia(context.Background(), wire.SendMediaParams{ConversationID: groupPrefix + "group-id", Path: path, Caption: "look"}); err != nil {
		t.Fatal(err)
	}
	params := fake.params["send"].(map[string]any)
	attachments, ok := params["attachments"].([]string)
	if params["groupId"] != "group-id" || params["message"] != "look" || !ok || len(attachments) != 1 || attachments[0] != path {
		t.Fatalf("group image params = %#v", params)
	}
	if _, exists := params["voiceNote"]; exists {
		t.Fatalf("image marked as voice note: %#v", params)
	}
}

func TestControlMessagesAreSkippedAndRemoteDeleteRedacts(t *testing.T) {
	fake := &fakeCaller{}
	b := New(zerolog.Nop(), testPaths(t), nil)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	convID := directPrefix + "+15550002222"
	fake.notify(json.RawMessage(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"timestamp":1700000000000}}}`))
	result, _ := b.Messages(context.Background(), wire.MessagesParams{ConversationID: convID})
	if len(result.Messages) != 0 {
		t.Fatalf("control envelope created %#v", result.Messages)
	}
	fake.notify(json.RawMessage(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"timestamp":1700000000000,"message":"secret"}}}`))
	fake.notify(json.RawMessage(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"remoteDelete":{"timestamp":1700000000000}}}}`))
	result, _ = b.Messages(context.Background(), wire.MessagesParams{ConversationID: convID})
	if len(result.Messages) != 1 || !result.Messages[0].Deleted || result.Messages[0].Text != "" {
		t.Fatalf("deleted = %#v", result.Messages)
	}
}

func TestViewOnceAttachmentIsNotPersisted(t *testing.T) {
	fake := &fakeCaller{}
	b := New(zerolog.Nop(), testPaths(t), nil)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	convID := directPrefix + "+15550002222"
	fake.notify(json.RawMessage(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"timestamp":1700000000000,"viewOnce":true,"attachments":[{"id":"secret","contentType":"image/jpeg"}]}}}`))
	result, _ := b.Messages(context.Background(), wire.MessagesParams{ConversationID: convID})
	if len(result.Messages) != 0 {
		t.Fatalf("view-once persisted = %#v", result.Messages)
	}
}

func TestVersionOneStoreDropsControlBubblesAndTheirUnreadFlag(t *testing.T) {
	paths := testPaths(t)
	conversationID := directPrefix + "+15550002222"
	legacy := `{"version":1,"conversations":{"` + conversationID + `":{"id":"` + conversationID + `","name":"Taylor","preview":"","timestamp":1700000001000000,"unread":true}},"order":["` + conversationID + `"],"messages":{"` + conversationID + `":[{"id":"signal:1700000000000:+15550002222","conversationID":"` + conversationID + `","text":"hello","timestamp":1700000000000000},{"id":"signal:1700000001000:+15550002222","conversationID":"` + conversationID + `","text":"","timestamp":1700000001000000}]}}`
	if err := os.WriteFile(paths.SignalStoreFile(), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	b := New(zerolog.Nop(), paths, nil)
	result, _ := b.Messages(context.Background(), wire.MessagesParams{ConversationID: conversationID})
	if len(result.Messages) != 1 || result.Messages[0].Text != "hello" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	conversations := b.Conversations(1)
	if len(conversations) != 1 || conversations[0].Unread || conversations[0].Preview != "hello" {
		t.Fatalf("conversations = %#v", conversations)
	}
}

func TestStoreRepairsSuccessfulSendToUndiscoveredSelfConversation(t *testing.T) {
	paths := testPaths(t)
	account := "+15550001111"
	conversationID := directPrefix + account
	legacy := `{"version":2,"account":"` + account + `","conversations":{"` + conversationID + `":{"id":"","preview":"test","timestamp":1700000000000000}},"order":[],"messages":{"` + conversationID + `":[{"id":"signal:1700000000000:` + account + `","conversationID":"` + conversationID + `","text":"test","timestamp":1700000000000000,"fromMe":true}]}}`
	if err := os.WriteFile(paths.SignalStoreFile(), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	b := New(zerolog.Nop(), paths, nil)
	conversations := b.Conversations(0)
	if len(conversations) != 1 || conversations[0].ID != conversationID || conversations[0].Name != "Note to Self" {
		t.Fatalf("repaired conversations = %#v", conversations)
	}
	result, _ := b.Messages(context.Background(), wire.MessagesParams{ConversationID: conversationID})
	if len(result.Messages) != 1 || result.Messages[0].Text != "test" {
		t.Fatalf("repaired messages = %#v", result.Messages)
	}
}

func TestStorePrunesLegacyDiscoveryStubs(t *testing.T) {
	paths := testPaths(t)
	emptyID := directPrefix + "+15550002222"
	activeID := groupPrefix + "group-id"
	legacy := `{"version":2,"conversations":{"` + emptyID + `":{"id":"` + emptyID + `","name":"Taylor"},"` + activeID + `":{"id":"` + activeID + `","name":"Book Club","isGroup":true,"preview":"hello","timestamp":1700000000000000}},"order":["` + emptyID + `","` + activeID + `"],"messages":{"` + activeID + `":[{"id":"signal:1700000000000:sender","conversationID":"` + activeID + `","text":"hello","timestamp":1700000000000000}]}}`
	if err := os.WriteFile(paths.SignalStoreFile(), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	b := New(zerolog.Nop(), paths, nil)
	conversations := b.Conversations(0)
	if len(conversations) != 1 || conversations[0].ID != activeID {
		t.Fatalf("pruned conversations = %#v", conversations)
	}
}
