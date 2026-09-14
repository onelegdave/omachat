package telegram

import (
	"testing"

	"github.com/gotd/td/tg"
	"github.com/rs/zerolog"
)

func TestMediaMessageSuppressesTTLPhotoAndDocument(t *testing.T) {
	g := NewGotdClient(1, "hash", t.TempDir()+"/session", zerolog.Nop())
	photo := &tg.Message{ID: 1, Media: &tg.MessageMediaPhoto{TTLSeconds: 10, Photo: &tg.Photo{ID: 3, AccessHash: 4, Sizes: []tg.PhotoSizeClass{&tg.PhotoSize{Type: "x"}}}}}
	if got := g.mediaMessage(photo, 7); got.MediaKey != "" {
		t.Fatalf("TTL photo exposed media key %q", got.MediaKey)
	}
	doc := &tg.Message{ID: 2, Media: &tg.MessageMediaDocument{TTLSeconds: 10, Document: &tg.Document{ID: 5, AccessHash: 6, MimeType: "audio/ogg", Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeAudio{Voice: true}}}}}
	if got := g.mediaMessage(doc, 7); got.MediaKey != "" {
		t.Fatalf("TTL document exposed media key %q", got.MediaKey)
	}
	if len(g.mediaRefs) != 0 || len(g.mediaExts) != 0 {
		t.Fatal("TTL media left downloadable references")
	}
}
