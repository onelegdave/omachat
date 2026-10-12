package telegram

import (
	"context"
	"github.com/gotd/td/tg"
	"github.com/rs/zerolog"
	"os"
	"path/filepath"
	"testing"
)

func TestAvatarCacheRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	key := "tg-avatar:42:7"
	outside, err := writeAvatarFixture(t.TempDir(), key)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, safeMediaName(key)+".jpg")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if cachedAvatar(dir, key) != "" {
		t.Fatal("avatar cache followed a symlink")
	}
	g := NewGotdClient(1, "hash", t.TempDir()+"/session", zerolog.Nop())
	g.mediaRefs[key] = &tg.InputPeerPhotoFileLocation{Peer: &tg.InputPeerUser{UserID: 42, AccessHash: 123}, PhotoID: 7}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.DownloadMedia(ctx, key, dir); err == nil {
		t.Fatal("cancelled downloader reused an unsafe cached symlink")
	}
}
