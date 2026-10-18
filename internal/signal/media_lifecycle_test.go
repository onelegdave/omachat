package signal

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

// signalCLICopy stands in for the file signal-cli saves for a received
// attachment.
func signalCLICopy(t *testing.T, paths *appStore.Paths, id string) string {
	t.Helper()
	dir := filepath.Join(paths.SignalDataDir(), "attachments")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id)
	if err := os.WriteFile(path, []byte("invented"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// helperPreview stands in for a preview the helper wrote of src.
func helperPreview(t *testing.T, src string) string {
	t.Helper()
	dir := filepath.Join(filepath.Dir(src), appStore.PreviewDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, appStore.PreviewPrefix(src)+"0011223344556677.png")
	if err := os.WriteFile(path, []byte("preview"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func gone(t *testing.T, label, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("%s remains at %s (err=%v)", label, path, err)
	}
}

func startedBackend(t *testing.T) (*Backend, *fakeCaller, *appStore.Paths) {
	t.Helper()
	paths := testPaths(t)
	fake := &fakeCaller{}
	b := New(zerolog.Nop(), paths, nil)
	t.Cleanup(b.Stop)
	b.SetClient(fake)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return b, fake, paths
}

// Expiry and remote deletion remove every local copy of the media: OmaChat's
// cache, the helper's re-encoded preview, and signal-cli's own download.
func TestRedactedSignalMediaLeavesNoCopies(t *testing.T) {
	for _, expiry := range []bool{false, true} {
		t.Run(fmt.Sprint("expiry=", expiry), func(t *testing.T) {
			b, fake, paths := startedBackend(t)
			// A current timestamp, so the hour-long timer has not yet run.
			sent := time.Now().UnixMilli()
			fake.notify(json.RawMessage(fmt.Sprintf(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"timestamp":%d,"expiresInSeconds":3600,"attachments":[{"id":"photo.png","contentType":"image/png","size":5}]}}}`, sent)))
			cliCopy := signalCLICopy(t, paths, "photo.png")
			result, err := b.Media(context.Background(), wire.MediaParams{Key: mediaPrefix + "photo.png"})
			if err != nil {
				t.Fatal(err)
			}
			preview := helperPreview(t, result.Path)
			if expiry {
				b.expireMessage(messageID(sent, "+15550002222"))
			} else {
				b.redactMessage(directPrefix+"+15550002222", sent, "+15550002222")
			}
			gone(t, "cached media", result.Path)
			gone(t, "helper preview", preview)
			gone(t, "signal-cli copy", cliCopy)
		})
	}
}

func TestViewOnceSignalMediaIsRemovedFromSignalCLIStore(t *testing.T) {
	_, fake, paths := startedBackend(t)
	cliCopy := signalCLICopy(t, paths, "once.jpg")
	fake.notify(json.RawMessage(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"timestamp":1700000000000,"viewOnce":true,"attachments":[{"id":"once.jpg","contentType":"image/jpeg"}]}}}`))
	gone(t, "view-once signal-cli copy", cliCopy)
}

// Messages trimmed from the bounded local inbox must not leave media behind.
func TestTrimmedSignalMessagesLeaveNoMedia(t *testing.T) {
	b, fake, paths := startedBackend(t)
	fake.notify(json.RawMessage(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"timestamp":1000,"attachments":[{"id":"old.png","contentType":"image/png","size":5}]}}}`))
	cliCopy := signalCLICopy(t, paths, "old.png")
	result, err := b.Media(context.Background(), wire.MediaParams{Key: mediaPrefix + "old.png"})
	if err != nil {
		t.Fatal(err)
	}
	preview := helperPreview(t, result.Path)
	for i := 0; i < maxMessagesPerConversation; i++ {
		fake.notify(json.RawMessage(fmt.Sprintf(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"timestamp":%d,"message":"invented %d"}}}`, 2000+i, i)))
	}
	page, _ := b.Messages(context.Background(), wire.MessagesParams{ConversationID: directPrefix + "+15550002222"})
	if len(page.Messages) != maxMessagesPerConversation || len(page.Messages[0].Attachments) != 0 {
		t.Fatalf("retained %d messages, oldest %+v", len(page.Messages), page.Messages[0])
	}
	gone(t, "cached media", result.Path)
	gone(t, "helper preview", preview)
	gone(t, "signal-cli copy", cliCopy)
}

func TestOversizedSignalAttachmentIsRefusedBeforeDownload(t *testing.T) {
	b, fake, _ := startedBackend(t)
	fake.notify(json.RawMessage(fmt.Sprintf(`{"envelope":{"sourceNumber":"+15550002222","dataMessage":{"timestamp":1700000000000,"attachments":[{"id":"big.mp4","contentType":"video/mp4","size":%d}]}}}`, maxIncomingMediaBytes+1)))
	if _, err := b.Media(context.Background(), wire.MediaParams{Key: mediaPrefix + "big.mp4"}); err == nil {
		t.Fatal("oversized attachment downloaded")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, method := range fake.calls {
		if method == "getAttachment" {
			t.Fatal("getAttachment called for an attachment over the limit")
		}
	}
}

// An attachment id names a file inside signal-cli's attachment directory and
// can never reach outside it.
func TestSignalCLIAttachmentRemovalStaysInsideItsDirectory(t *testing.T) {
	paths := testPaths(t)
	b := New(zerolog.Nop(), paths, nil)
	t.Cleanup(b.Stop)
	outside := filepath.Join(paths.SignalDataDir(), "account.db")
	if err := os.WriteFile(outside, []byte("keys"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../account.db", "..", ".", "", "a/../../account.db"} {
		b.removeSignalCLIAttachment(id)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("file outside the attachment directory removed: %v", err)
	}
}

func TestReadLineSkipsOversizedLinesAndContinues(t *testing.T) {
	input := "short\n" + strings.Repeat("x", 300) + "\nnext\n"
	reader := bufio.NewReaderSize(strings.NewReader(input), 16)
	want := []struct {
		line    string
		tooLong bool
	}{{"short", false}, {"", true}, {"next", false}}
	for i, w := range want {
		line, tooLong, err := readLine(reader, 64)
		if err != nil || string(line) != w.line || tooLong != w.tooLong {
			t.Fatalf("line %d = %q tooLong=%v err=%v", i, line, tooLong, err)
		}
	}
	if _, _, err := readLine(reader, 64); err == nil {
		t.Fatal("expected end of stream")
	}
}

func TestOversizedReplyFailsOnlyPendingDownloads(t *testing.T) {
	c := NewRPCClient()
	download := make(chan rpcReply, 1)
	other := make(chan rpcReply, 1)
	c.pending["1"] = pendingCall{method: "getAttachment", reply: download}
	c.pending["2"] = pendingCall{method: "send", reply: other}
	c.failPending("getAttachment", fmt.Errorf("too large"))
	if reply := <-download; reply.err == nil {
		t.Fatal("download not failed")
	}
	if _, ok := c.pending["2"]; !ok || len(other) != 0 {
		t.Fatal("unrelated call failed")
	}
}

func TestSignalCLIIgnoresStories(t *testing.T) {
	args := strings.Join(signalCLIArgs("/invented/data"), " ")
	for _, want := range []string{"--data-dir /invented/data", "jsonRpc", "--receive-mode=manual", "--ignore-stories"} {
		if !strings.Contains(args, want) {
			t.Fatalf("signal-cli args %q missing %q", args, want)
		}
	}
}
