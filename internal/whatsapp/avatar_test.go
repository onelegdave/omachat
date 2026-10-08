package whatsapp

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/onelegdave/omachat/internal/wire"
)

func TestWhatsAppAvatarTargetKeepsDirectAndGroupJIDs(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{name: "direct contact", id: "15551234567@s.whatsapp.net", want: "15551234567@s.whatsapp.net"},
		{name: "group", id: "1234567890-123456@g.us", want: "1234567890-123456@g.us"},
		{name: "LID contact", id: "123456789@lid", want: "123456789@lid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := whatsappAvatarTarget(tt.id)
			if !ok {
				t.Fatalf("whatsappAvatarTarget(%q) unexpectedly rejected", tt.id)
			}
			if got.String() != tt.want {
				t.Fatalf("whatsappAvatarTarget(%q) = %q, want %q", tt.id, got.String(), tt.want)
			}
		})
	}
}

func TestWhatsAppAvatarTargetRejectsUnsupportedOrInvalidIDs(t *testing.T) {
	for _, id := range []string{"", "not a jid", "12345@newsletter", "12345@broadcast"} {
		if got, ok := whatsappAvatarTarget(id); ok {
			t.Errorf("whatsappAvatarTarget(%q) = %q, want rejected", id, got.String())
		}
	}
}

func TestFetchWhatsAppAvatarImageRejectsNonHTTPSAndUntrustedHosts(t *testing.T) {
	for _, raw := range []string{"http://mmg.whatsapp.net/avatar", "https://example.invalid/avatar", "https://whatsapp.net.evil.invalid/avatar", "https://mmg.whatsapp.net:444/avatar", "https://cdn.fbcdn.net/avatar"} {
		if _, err := fetchWhatsAppAvatarImage(context.Background(), raw); err == nil {
			t.Errorf("fetchWhatsAppAvatarImage(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestFetchWhatsAppAvatarImageBoundsResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxWhatsAppAvatarBytes+1)))
	}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "https://mmg.whatsapp.net/avatar", nil))

	if _, err := readWhatsAppAvatarResponse(recorder.Result()); err == nil {
		t.Fatal("readWhatsAppAvatarResponse accepted an oversized body")
	}
}

func TestAvatarWorkerFetchesDirectAndGroupPicturesInBackground(t *testing.T) {
	backend, mock, eventsCh := setupTestBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend.SetClient(mock, true)
	backend.ctx = ctx
	gen := backend.gen
	directID := "15551234567@s.whatsapp.net"
	groupID := "1234567890-123456@g.us"
	backend.convs[directID] = wire.Conversation{ID: directID, Name: "Direct"}
	backend.convs[groupID] = wire.Conversation{ID: groupID, Name: "Group", IsGroup: true}

	var pngBytes bytes.Buffer
	avatarImage := image.NewRGBA(image.Rect(0, 0, 1, 1))
	avatarImage.Set(0, 0, color.NRGBA{R: 1, G: 2, B: 3, A: 255})
	if err := png.Encode(&pngBytes, avatarImage); err != nil {
		t.Fatal(err)
	}
	requested := make(chan string, 3)
	mock.GetProfilePictureInfoFunc = func(_ context.Context, jid types.JID, params *whatsmeow.GetProfilePictureParams) (*types.ProfilePictureInfo, error) {
		if params == nil || !params.Preview {
			t.Errorf("profile picture lookup params = %+v, want preview image", params)
		}
		requested <- jid.String()
		return &types.ProfilePictureInfo{ID: "synthetic-picture", URL: "https://mmg.whatsapp.net/synthetic"}, nil
	}
	backend.downloadAvatarURL = func(context.Context, string) ([]byte, error) { return pngBytes.Bytes(), nil }

	started := time.Now()
	backend.queueAvatarFetches(gen, []string{directID, groupID})
	if time.Since(started) > 100*time.Millisecond {
		t.Fatal("queueAvatarFetches blocked on avatar retrieval")
	}

	seen := map[string]bool{}
	deadline := time.After(3 * time.Second)
	for len(seen) < 2 {
		select {
		case event := <-eventsCh:
			if event.Event != wire.EventConversation {
				continue
			}
			conv := event.Data.(wire.Conversation)
			if conv.AvatarPath != "" {
				info, err := os.Stat(conv.AvatarPath)
				if err != nil {
					t.Fatalf("avatar path %q is missing: %v", conv.AvatarPath, err)
				}
				if info.Mode().Perm() != 0o600 {
					t.Fatalf("avatar mode = %o, want 600", info.Mode().Perm())
				}
				seen[conv.ID] = true
			}
		case <-deadline:
			t.Fatalf("avatar updates did not arrive for both direct and group chats: %v", seen)
		}
	}
	got := map[string]bool{<-requested: true, <-requested: true}
	if !got[directID] || !got[groupID] {
		t.Fatalf("requested profile pictures for %v, want direct %q and group %q", got, directID, groupID)
	}

	jid, _ := types.ParseJID(directID)
	backend.mu.RLock()
	oldPath := backend.convs[directID].AvatarPath
	backend.mu.RUnlock()
	pngBytes.Reset()
	avatarImage.Set(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	if err := png.Encode(&pngBytes, avatarImage); err != nil {
		t.Fatal(err)
	}
	backend.handlePictureEvent(gen, &events.Picture{JID: jid})
	select {
	case gotID := <-requested:
		if gotID != directID {
			t.Fatalf("picture-change lookup used JID %q, want %q", gotID, directID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("picture-change event did not trigger a refresh")
	}
	deadline = time.After(2 * time.Second)
	for {
		backend.mu.RLock()
		newPath := backend.convs[directID].AvatarPath
		backend.mu.RUnlock()
		if newPath != "" && newPath != oldPath {
			if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
				t.Fatalf("replaced avatar file remains (stat err %v)", err)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("changed picture did not publish a new cache path")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	backend.mu.RLock()
	updatedPath := backend.convs[directID].AvatarPath
	backend.mu.RUnlock()
	backend.handlePictureEvent(gen, &events.Picture{JID: jid, Remove: true})
	backend.mu.RLock()
	removed := backend.convs[directID]
	backend.mu.RUnlock()
	if removed.AvatarPath != "" {
		t.Fatalf("removed profile photo left avatar path %q", removed.AvatarPath)
	}
	if _, err := os.Stat(updatedPath); !os.IsNotExist(err) {
		t.Fatalf("removed profile photo file still exists (stat err %v)", err)
	}
}

func TestAvatarWorkerKeepsInitialsWhenNoPhotoIsAvailable(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend.SetClient(mock, true)
	backend.ctx = ctx
	conversationID := "15550001111@s.whatsapp.net"
	backend.convs[conversationID] = wire.Conversation{ID: conversationID, Name: "No photo", Initials: "NP"}
	mock.GetProfilePictureInfoFunc = func(context.Context, types.JID, *whatsmeow.GetProfilePictureParams) (*types.ProfilePictureInfo, error) {
		return nil, whatsmeow.ErrProfilePictureNotSet
	}
	backend.queueAvatarFetches(backend.gen, []string{conversationID})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		backend.mu.RLock()
		attempted := backend.avatarAttempted[conversationID]
		conversation := backend.convs[conversationID]
		backend.mu.RUnlock()
		if attempted {
			if conversation.AvatarPath != "" || conversation.Initials != "NP" {
				t.Fatalf("no-photo fallback changed unexpectedly: %+v", conversation)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("avatar lookup did not complete")
}

func TestAvatarWorkerDoesNotCommitAfterSessionGenerationChanges(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend.SetClient(mock, true)
	backend.ctx = ctx
	conversationID := "15550002222@s.whatsapp.net"
	backend.convs[conversationID] = wire.Conversation{ID: conversationID, Name: "Stale result"}
	started := make(chan struct{})
	release := make(chan struct{})
	mock.GetProfilePictureInfoFunc = func(context.Context, types.JID, *whatsmeow.GetProfilePictureParams) (*types.ProfilePictureInfo, error) {
		return &types.ProfilePictureInfo{ID: "stale", URL: "https://mmg.whatsapp.net/synthetic"}, nil
	}
	backend.downloadAvatarURL = func(context.Context, string) ([]byte, error) {
		close(started)
		<-release
		return []byte("synthetic stale result"), nil
	}
	backend.queueAvatarFetches(backend.gen, []string{conversationID})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("avatar worker did not reach its synthetic download seam")
	}
	backend.SetClient(NewMockClient(), false)
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		backend.mu.RLock()
		pending := backend.avatarPending[conversationID]
		conversation := backend.convs[conversationID]
		backend.mu.RUnlock()
		if pending == 0 {
			if conversation.AvatarPath != "" {
				t.Fatalf("stale avatar result was committed after session generation changed: %q", conversation.AvatarPath)
			}
			entries, err := os.ReadDir(backend.paths.WhatsAppMediaDir())
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "avatar-") {
					t.Fatalf("stale avatar file %q was written", entry.Name())
				}
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("stale avatar worker did not finish")
}
