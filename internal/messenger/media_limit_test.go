package messenger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/onelegdave/omachat/internal/boundedfile"
	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waMediaTransport"
)

func TestMediaFailureRemovesTempAndPreservesCachedFiles(t *testing.T) {
	paths := &appStore.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	backend := New(zerolog.Nop(), paths, func(wire.Event) {})
	dir := paths.MessengerMediaDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(dir, "existing.img")
	if err := os.WriteFile(kept, []byte("existing fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend.media["fixture"] = &messengerMedia{key: "fixture", size: 1, fbTransport: &waMediaTransport.WAMediaTransport_Integral{}}
	if result, err := backend.Media(context.Background(), wire.MediaParams{Key: "fixture"}); err == nil || result.Path != "" {
		t.Fatalf("failed encrypted download was accepted: %+v %v", result, err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 || files[0].Name() != "existing.img" {
		t.Fatalf("failure changed cache or left temp files: %v %v", files, err)
	}
	file, err := os.OpenFile(kept, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	err = file.Truncate(maxMessengerInboundBytes + 1)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	backend.media["cached"] = &messengerMedia{key: "cached", path: kept}
	if _, err := backend.Media(context.Background(), wire.MediaParams{Key: "cached"}); !errors.Is(err, boundedfile.ErrLimit) {
		t.Fatalf("oversized old cache was reused: %v", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("existing cache file was deleted: %v", err)
	}
}
