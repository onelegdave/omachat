package telegram

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/gotd/td/tg"
	"github.com/rs/zerolog"

	"github.com/onelegdave/omachat/internal/wire"
)

func writeAvatarFixture(dir, key string) (string, error) {
	var data bytes.Buffer
	if err := jpeg.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		return "", err
	}
	path := filepath.Join(dir, safeMediaName(key)+".jpg")
	return path, os.WriteFile(path, data.Bytes(), 0600)
}

func TestDialogAvatarReferencesSeparatePeerTypes(t *testing.T) {
	g := NewGotdClient(1, "hash", t.TempDir()+"/session", zerolog.Nop())
	dialogs := g.dialogs(&tg.MessagesDialogs{
		Dialogs: []tg.DialogClass{
			&tg.Dialog{Peer: &tg.PeerUser{UserID: 42}},
			&tg.Dialog{Peer: &tg.PeerChat{ChatID: 42}},
			&tg.Dialog{Peer: &tg.PeerChannel{ChannelID: 42}},
			&tg.Dialog{Peer: &tg.PeerUser{UserID: 79}},
			&tg.Dialog{Peer: &tg.PeerUser{UserID: 80}},
		},
		Users: []tg.UserClass{
			&tg.User{ID: 42, FirstName: "Demo User", AccessHash: 123, Photo: &tg.UserProfilePhoto{PhotoID: 7}},
			&tg.User{ID: 79, Self: true, Photo: &tg.UserProfilePhoto{PhotoID: 7}},
			&tg.User{ID: 80, FirstName: "Inaccessible", Photo: &tg.UserProfilePhoto{PhotoID: 7}},
		},
		Chats: []tg.ChatClass{
			&tg.Chat{ID: 42, Title: "Demo Group", Photo: &tg.ChatPhoto{PhotoID: 7}},
			&tg.Channel{ID: 42, Title: "Demo Channel", AccessHash: 456, Photo: &tg.ChatPhoto{PhotoID: 7}},
		},
	})
	if len(dialogs) != 5 || len(g.mediaRefs) != 4 {
		t.Fatalf("unexpected avatar mapping: %d dialogs, %d references", len(dialogs), len(g.mediaRefs))
	}
	for i, dialog := range dialogs[:4] {
		location, ok := g.mediaRefs[dialog.AvatarKey].(*tg.InputPeerPhotoFileLocation)
		if !ok || location.PhotoID != 7 || location.Big {
			t.Fatalf("wrong thumbnail location: %+v", location)
		}
		switch i {
		case 0:
			peer, ok := location.Peer.(*tg.InputPeerUser)
			if !ok || peer.UserID != 42 || peer.AccessHash != 123 {
				t.Fatalf("wrong user photo peer: %+v", location.Peer)
			}
		case 1:
			peer, ok := location.Peer.(*tg.InputPeerChat)
			if !ok || peer.ChatID != 42 {
				t.Fatalf("wrong group photo peer: %+v", location.Peer)
			}
		case 2:
			peer, ok := location.Peer.(*tg.InputPeerChannel)
			if !ok || peer.ChannelID != 42 || peer.AccessHash != 456 {
				t.Fatalf("wrong channel photo peer: %+v", location.Peer)
			}
		case 3:
			if _, ok := location.Peer.(*tg.InputPeerSelf); !ok {
				t.Fatalf("self photo requires InputPeerSelf: %+v", location.Peer)
			}
		}
		for _, other := range dialogs[:i] {
			if safeMediaName(other.AvatarKey) == safeMediaName(dialog.AvatarKey) {
				t.Fatal("photo cache collides across peer types")
			}
		}
	}
	if dialogs[4].AvatarKey != "" {
		t.Fatal("peer without an access hash exposed a photo download")
	}
	// Refreshing removed photos must retire their references, without losing message media.
	g.mediaRefs["tg:42:1"] = &tg.InputPhotoFileLocation{ID: 9}
	g.dialogs(&tg.MessagesDialogsSlice{Dialogs: []tg.DialogClass{&tg.Dialog{Peer: &tg.PeerUser{UserID: 42}}}, Users: []tg.UserClass{&tg.User{ID: 42, AccessHash: 123, Photo: &tg.UserProfilePhotoEmpty{}}}})
	if len(g.mediaRefs) != 1 || g.mediaRefs["tg:42:1"] == nil {
		t.Fatal("removed avatar reference persisted or message media was lost")
	}
}

func TestTelegramAvatarCacheReuseAndRefresh(t *testing.T) {
	b, paths, events := setupTestTelegram(t)
	t.Cleanup(b.Stop)
	mock := NewMockClient()
	photo := "tg-avatar:42:7"
	mock.DialogsFunc = func(context.Context, int) ([]Dialog, error) {
		return []Dialog{{ID: 42, Name: "Demo User", AvatarKey: photo}, {ID: 80, Name: "No photo"}}, nil
	}
	var downloads atomic.Int32
	mock.DownloadMediaFunc = func(_ context.Context, key, dir string) (string, error) {
		downloads.Add(1)
		return writeAvatarFixture(dir, key)
	}
	b.SetClient(mock)
	refresh := func() {
		t.Helper()
		if err := b.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		b.wg.Wait()
	}
	refresh()
	path := cachedAvatar(paths.TelegramMediaDir(), photo)
	if path == "" || b.convs["tg:42"].AvatarPath != path || b.convs["tg:80"].AvatarPath != "" {
		t.Fatal("real photo and initials fallback were not mapped correctly")
	}
	if loadStoredData(paths.TelegramStoreFile()).Conversations["tg:42"].AvatarPath != path {
		t.Fatal("avatar path was not persisted")
	}
	found := false
	for len(events) > 0 {
		event := <-events
		if conv, ok := event.Data.(wire.Conversation); ok && event.Network == wire.NetworkTelegram && conv.AvatarPath == path {
			found = true
		}
	}
	if !found {
		t.Fatal("completed avatar did not update the UI")
	}
	refresh()
	if downloads.Load() != 1 {
		t.Fatal("unchanged photo was downloaded again")
	}
	// A restored client can reuse the same cached file without a network connection.
	g := NewGotdClient(1, "hash", t.TempDir()+"/session", zerolog.Nop())
	g.mediaRefs[photo] = &tg.InputPeerPhotoFileLocation{Peer: &tg.InputPeerUser{UserID: 42, AccessHash: 123}, PhotoID: 7}
	if got, err := g.DownloadMedia(context.Background(), photo, paths.TelegramMediaDir()); err != nil || got != path {
		t.Fatalf("cached download was not reused: %s %v", got, err)
	}
	photo = "tg-avatar:42:8"
	refresh()
	if downloads.Load() != 2 || b.convs["tg:42"].AvatarPath == path {
		t.Fatal("changed photo reused the old image")
	}
	photo = ""
	refresh()
	if b.convs["tg:42"].AvatarPath != "" {
		t.Fatal("removed photo did not restore initials")
	}
	if err := b.Unpair(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("unpairing left a cached Telegram profile photo")
	}
}

func TestTelegramAvatarFailureRetriesOnRefresh(t *testing.T) {
	b, _, _ := setupTestTelegram(t)
	t.Cleanup(b.Stop)
	mock := NewMockClient()
	mock.DialogsFunc = func(context.Context, int) ([]Dialog, error) {
		return []Dialog{{ID: 42, Name: "Demo", AvatarKey: "tg-avatar:42:7"}}, nil
	}
	var attempts atomic.Int32
	mock.DownloadMediaFunc = func(_ context.Context, key, dir string) (string, error) {
		if attempts.Add(1) == 1 {
			return "", errors.New("photo temporarily unavailable")
		}
		return writeAvatarFixture(dir, key)
	}
	b.SetClient(mock)
	for i := 0; i < 2; i++ {
		if err := b.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		b.wg.Wait()
		if (b.convs["tg:42"].AvatarPath != "") != (i == 1) {
			t.Fatal("failed avatar should fall back to initials and retry on refresh")
		}
	}
}

func TestTelegramStaleAvatarCannotReplaceNewPhotoOrRestoreUnpairedAccount(t *testing.T) {
	for _, unpair := range []bool{false, true} {
		t.Run(fmt.Sprintf("unpair=%t", unpair), func(t *testing.T) {
			b, paths, _ := setupTestTelegram(t)
			t.Cleanup(b.Stop)
			mock := NewMockClient()
			photo := "tg-avatar:42:7"
			entered, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			var oldDownloads atomic.Int32
			mock.DialogsFunc = func(context.Context, int) ([]Dialog, error) {
				return []Dialog{{ID: 42, Name: "Demo", AvatarKey: photo}}, nil
			}
			mock.DownloadMediaFunc = func(_ context.Context, key, dir string) (string, error) {
				if key == "tg-avatar:42:7" {
					if oldDownloads.Add(1) == 1 {
						close(entered)
					}
					<-release
					// Deliberately ignore cancellation to exercise the backend's stale-result guard.
					return filepath.Join(dir, "old-photo.jpg"), nil
				}
				return writeAvatarFixture(dir, key)
			}
			b.SetClient(mock)
			if err := b.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			<-entered
			// A repeated refresh must not start another download of the same photo.
			if err := b.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			if unpair {
				if err := b.Unpair(context.Background()); err != nil {
					t.Fatal(err)
				}
			} else {
				photo = "tg-avatar:42:8"
				if err := b.Refresh(context.Background()); err != nil {
					t.Fatal(err)
				}
				b.ingestMessage(Message{ID: 1, ConversationID: 42, Text: "New preview"})
			}
			close(release)
			b.wg.Wait()
			if oldDownloads.Load() != 1 {
				t.Fatal("duplicate refresh started duplicate photo downloads")
			}
			if unpair {
				if len(b.Conversations(50)) != 0 {
					t.Fatal("old avatar restored an unpaired conversation")
				}
				if _, err := os.Stat(paths.TelegramStoreFile()); !os.IsNotExist(err) {
					t.Fatal("old avatar restored the removed cache")
				}
			} else {
				conv := b.convs["tg:42"]
				if conv.AvatarPath != cachedAvatar(paths.TelegramMediaDir(), photo) || conv.Preview != "New preview" || !conv.Unread {
					t.Fatalf("avatar download overwrote the new photo or live conversation: %+v", conv)
				}
			}
		})
	}
}
