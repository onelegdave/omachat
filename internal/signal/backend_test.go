package signal

import (
	"context"
	"encoding/json"
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
	mu      sync.Mutex
	notify  func(json.RawMessage)
	started bool
	closed  bool
	calls   []string
	params  map[string]any
}

func (f *fakeCaller) Start(_ context.Context, _ string, notify func(json.RawMessage)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started, f.notify = true, notify
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
	default:
		raw = `{}`
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
	if _, err := b.Send(context.Background(), wire.SendParams{ConversationID: groupPrefix + "base64-group", Text: "hello group"}); err != nil {
		t.Fatal(err)
	}
	group := fake.params["send"].(map[string]any)
	if _, ok := group["groupId"]; !ok {
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
