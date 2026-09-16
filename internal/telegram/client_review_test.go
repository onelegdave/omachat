package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	"github.com/rs/zerolog"
)

// TestPeerIDNamespacesByType verifies the Bot API signed-ID convention: a
// user, basic group, and channel that happen to share the same raw numeric
// ID must not collide in the typed peer identity (review finding 1).
func TestPeerIDNamespacesByType(t *testing.T) {
	const raw = 42
	user := peerID(&tg.PeerUser{UserID: raw})
	chat := peerID(&tg.PeerChat{ChatID: raw})
	channel := peerID(&tg.PeerChannel{ChannelID: raw})

	if user != raw {
		t.Fatalf("user peer ID = %d, want %d", user, raw)
	}
	if chat != -raw {
		t.Fatalf("chat peer ID = %d, want %d", chat, -raw)
	}
	if want := int64(-(channelIDOffset + raw)); channel != want {
		t.Fatalf("channel peer ID = %d, want %d", channel, want)
	}
	if user == chat || user == channel || chat == channel {
		t.Fatalf("peer IDs collided across types: user=%d chat=%d channel=%d", user, chat, channel)
	}
}

// TestMediaMessageKeysNamespaceByConversation verifies that a photo message
// with the same raw message ID in two different conversations gets distinct
// media keys and distinct cache entries (review finding 2).
func TestMediaMessageKeysNamespaceByConversation(t *testing.T) {
	g := NewGotdClient(1, "hash", t.TempDir()+"/session", zerolog.Nop())
	photo := func() *tg.Message {
		return &tg.Message{ID: 7, Media: &tg.MessageMediaPhoto{Photo: &tg.Photo{ID: 3, AccessHash: 4, Sizes: []tg.PhotoSizeClass{&tg.PhotoSize{Type: "x"}}}}}
	}

	first := g.mediaMessage(photo(), 100)
	second := g.mediaMessage(photo(), -1000000000200)

	if first.MediaKey == "" || second.MediaKey == "" {
		t.Fatalf("expected media keys, got %q and %q", first.MediaKey, second.MediaKey)
	}
	if first.MediaKey == second.MediaKey {
		t.Fatalf("same message ID in different conversations produced colliding media key %q", first.MediaKey)
	}
	if len(g.mediaRefs) != 2 {
		t.Fatalf("expected two distinct cached media refs, got %d", len(g.mediaRefs))
	}
	if safeMediaName(first.MediaKey) == safeMediaName(second.MediaKey) {
		t.Fatal("distinct media keys hashed to the same cache filename")
	}
}

// TestSafeMediaNameUnambiguous checks that the filename hash is deterministic
// and doesn't collapse keys that a naive character filter would have merged
// (e.g. colons stripped leaving "tg7123" ambiguous between "tg:7:123" and
// "tg:71:23").
func TestSafeMediaNameUnambiguous(t *testing.T) {
	a := safeMediaName("tg:7:123")
	b := safeMediaName("tg:71:23")
	if a == b {
		t.Fatal("distinct keys hashed to the same filename")
	}
	if safeMediaName("tg:7:123") != a {
		t.Fatal("safeMediaName is not deterministic")
	}
}

// TestCappedWriterRejectsDuringStream ensures the size limit is enforced as
// bytes arrive rather than after a full oversized body has already been
// written to disk (review finding 5).
func TestCappedWriterRejectsDuringStream(t *testing.T) {
	buf := make([]byte, 0, 16)
	sink := &sliceWriter{buf: &buf}
	c := &cappedWriter{w: sink, limit: 8}

	if _, err := c.Write(make([]byte, 5)); err != nil {
		t.Fatalf("unexpected error under limit: %v", err)
	}
	if _, err := c.Write(make([]byte, 5)); err == nil {
		t.Fatal("expected error when write would exceed the cap")
	}
	if len(buf) != 5 {
		t.Fatalf("rejected write should not reach the underlying writer, got %d bytes", len(buf))
	}
}

type sliceWriter struct{ buf *[]byte }

func (s *sliceWriter) Write(p []byte) (int, error) {
	*s.buf = append(*s.buf, p...)
	return len(p), nil
}

// TestDownloadMediaRejectsWhenStopped confirms DownloadMedia refuses to start
// (and therefore cannot commit a file into the cache) once the client has
// been marked stopped, without requiring a live MTProto connection.
func TestDownloadMediaRejectsWhenStopped(t *testing.T) {
	g := NewGotdClient(1, "hash", t.TempDir()+"/session", zerolog.Nop())
	g.mu.Lock()
	g.stopped = true
	g.mu.Unlock()

	if _, err := g.DownloadMedia(context.Background(), "tg:1:1", t.TempDir()); err == nil {
		t.Fatal("expected DownloadMedia to reject work after Stop")
	}
}

// TestStopWaitsForInFlightDownloadCommit exercises the synchronization
// contract directly: Stop must not return while a DownloadMedia commit is
// still in flight, so a caller can rely on Stop() returning meaning no more
// cache writes will land (review finding 4 applied to media).
func TestStopWaitsForInFlightDownloadCommit(t *testing.T) {
	g := NewGotdClient(1, "hash", t.TempDir()+"/session", zerolog.Nop())
	g.downloadWG.Add(1)
	released := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		g.downloadWG.Done()
		close(released)
	}()

	start := time.Now()
	if err := g.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Fatal("Stop returned before the in-flight download commit finished")
	}
	<-released
}

// TestGotdClientImplementsHistoryClient locks in the optional paging seam
// used for older-history support (review finding 7).
func TestGotdClientImplementsHistoryClient(t *testing.T) {
	var c Client = NewGotdClient(1, "hash", t.TempDir()+"/session", zerolog.Nop())
	if _, ok := c.(HistoryClient); !ok {
		t.Fatal("GotdClient does not implement HistoryClient")
	}
}

func TestTelegramReactionsMapsEmojiCountsAndMine(t *testing.T) {
	mine := tg.ReactionCount{Reaction: &tg.ReactionEmoji{Emoticon: "👍"}, Count: 2}
	mine.SetChosenOrder(0)
	got := telegramReactions(tg.MessageReactions{Results: []tg.ReactionCount{
		mine,
		{Reaction: &tg.ReactionCustomEmoji{DocumentID: 7}, Count: 1},
		{Reaction: &tg.ReactionEmoji{Emoticon: "❤️"}, Count: 3},
	}})
	if len(got) != 2 || got[0].Emoji != "👍" || got[0].Count != 2 || !got[0].Mine || got[1].Emoji != "❤️" || got[1].Mine {
		t.Fatalf("unexpected reactions: %+v", got)
	}
}

func TestChannelHistoryKeepsCursorThroughServiceEvents(t *testing.T) {
	g := NewGotdClient(1, "hash", t.TempDir()+"/session", zerolog.Nop())
	page := g.historyPage(&tg.MessagesChannelMessages{Messages: []tg.MessageClass{
		&tg.Message{ID: 8, Date: 1, Message: "visible"}, &tg.MessageService{ID: 7},
	}}, channelPeerID(42), 2)
	if len(page.Messages) != 1 || page.CursorID != 7 || !page.HasMore {
		t.Fatalf("lost channel history/cursor: %+v", page)
	}
	page = g.historyPage(&tg.MessagesChannelMessages{Messages: []tg.MessageClass{&tg.MessageService{ID: 6}}}, channelPeerID(42), 1)
	if len(page.Messages) != 0 || page.CursorID != 6 || !page.HasMore {
		t.Fatalf("service-only page stops pagination: %+v", page)
	}
}
