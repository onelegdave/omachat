package whatsapp

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

type mediaFlood struct{}

func (mediaFlood) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestMediaCopyLimitRemovesPartialFile(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	backend.SetClient(mock, true)
	backend.rawMsgs["fixture"] = &waE2E.Message{ImageMessage: &waE2E.ImageMessage{FileLength: proto.Uint64(1)}}
	called := false
	mock.DownloadToFileFunc = func(ctx context.Context, msg whatsmeow.DownloadableMessage, file whatsmeow.File) error {
		called = true
		// Match the vendored downloader's io.Copy destination path, not a mock
		// that calls Write directly. The body deliberately lies about its size.
		_, err := io.Copy(file, io.TeeReader(io.LimitReader(mediaFlood{}, maxInboundMediaBytes+1), io.Discard))
		stat, statErr := file.Stat()
		if statErr != nil || stat.Size() > maxInboundMediaBytes {
			t.Fatalf("in-flight file exceeded cap: stat=%v err=%v", stat, statErr)
		}
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := backend.Media(ctx, wire.MediaParams{Key: "fixture"})
	if !called || !errors.Is(err, errInboundMediaTooLarge) || result.Path != "" {
		t.Fatalf("oversized media was accepted: called=%v result=%+v err=%v", called, result, err)
	}
	files, err := os.ReadDir(backend.paths.WhatsAppMediaDir())
	if err != nil || len(files) != 0 {
		t.Fatalf("partial file survived failure: %v %v", files, err)
	}
}

func TestMediaCachedOversizeIsRefusedWithoutDeletion(t *testing.T) {
	backend, _, _ := setupTestBackend(t)
	path := filepath.Join(backend.paths.WhatsAppMediaDir(), mediaKeyToOpaque("fixture")+".bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = file.Truncate(maxInboundMediaBytes + 1)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Media(context.Background(), wire.MediaParams{Key: "fixture"}); !errors.Is(err, errInboundMediaTooLarge) {
		t.Fatalf("oversized old cache was reused: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("review must not delete existing cache data: %v", err)
	}
}
