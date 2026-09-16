package telegram

import (
	"context"
	"fmt"
	"sort"

	"github.com/onelegdave/omachat/internal/wire"
)

// Dialog is the small, protocol-neutral subset needed to render a Telegram chat.
// Keeping this type independent of gotd makes mapping tests entirely offline.
type Dialog struct {
	ID        int64
	Name      string
	Preview   string
	Timestamp int64
	Unread    bool
	IsGroup   bool
}

// Message is the protocol-neutral subset needed for a Telegram message bubble.
type Message struct {
	ID             int64
	ConversationID int64
	Text           string
	Timestamp      int64
	FromMe         bool
	SenderID       int64
	SenderName     string
	MediaKey       string
	MediaMime      string
	MediaAudio     bool
	MediaSticker   bool
	Reactions      []wire.Reaction
}

// ReadClient is the future read-only synchronization seam. Implementations may
// use gotd or an offline fake; callers never need to know the transport type.
type ReadClient interface {
	Dialogs(ctx context.Context, limit int) ([]Dialog, error)
	Messages(ctx context.Context, conversationID int64, limit int) ([]Message, error)
}

// HistoryClient is an optional capability for fetching older pages of a
// conversation's history. beforeID is exclusive: passing the oldest message
// ID already held returns the page immediately preceding it. A zero beforeID
// returns the most recent page. Implementations that don't support paging
// need not satisfy this interface; callers should type-assert for it.
type HistoryClient interface {
	MessagesPage(ctx context.Context, conversationID int64, beforeID int64, limit int) (HistoryPage, error)
}

var _ HistoryClient = (*GotdClient)(nil)

// SyncClient adds the operation needed to acknowledge a conversation after it
// has been opened. Keeping it separate preserves the read-only seam used by
// offline clients and tests.
type SyncClient interface {
	ReadClient
	MarkRead(ctx context.Context, conversationID int64, messageID int64) error
}

type SendClient interface {
	SyncClient
	SendText(ctx context.Context, conversationID int64, text string) (Message, error)
}

type MediaClient interface {
	SendImage(ctx context.Context, conversationID int64, path, caption string) (Message, error)
	SendVoice(ctx context.Context, conversationID int64, path, caption string) (Message, error)
	DownloadMedia(ctx context.Context, key, dir string) (string, error)
}

// ReactionClient is the optional capability used to add or remove a standard
// emoji reaction. An empty emoji removes the current user's reaction.
type ReactionClient interface {
	React(ctx context.Context, conversationID, messageID int64, emoji string) error
}

func mapDialog(d Dialog) wire.Conversation {
	name := d.Name
	if name == "" {
		name = fmt.Sprintf("Telegram chat %d", d.ID)
	}
	return wire.Conversation{
		ID: d.IDString(), Name: name, Preview: d.Preview,
		Timestamp: d.Timestamp, Unread: d.Unread, IsGroup: d.IsGroup,
		ReadOnly: false, Initials: initials(name),
	}
}

func (d Dialog) IDString() string { return fmt.Sprintf("tg:%d", d.ID) }

func mapMessage(m Message) wire.Message {
	out := wire.Message{
		ID: fmt.Sprintf("tg:%d", m.ID), ConversationID: fmt.Sprintf("tg:%d", m.ConversationID),
		Text: m.Text, Timestamp: m.Timestamp, FromMe: m.FromMe,
		SenderID: fmt.Sprintf("tg:%d", m.SenderID), SenderName: m.SenderName,
		Reactions: append([]wire.Reaction(nil), m.Reactions...),
	}
	if m.MediaKey != "" {
		mimeType := m.MediaMime
		if mimeType == "" && !m.MediaAudio {
			mimeType = "image/jpeg"
		}
		out.Attachments = []wire.Attachment{{Key: m.MediaKey, MimeType: mimeType, IsImage: !m.MediaAudio, IsAudio: m.MediaAudio}}
	}
	return out
}

func mapDialogs(in []Dialog, limit int) []wire.Conversation {
	out := make([]wire.Conversation, 0, len(in))
	for _, d := range in {
		out = append(out, mapDialog(d))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func mapMessages(in []Message, conversationID int64) []wire.Message {
	out := make([]wire.Message, 0, len(in))
	for _, m := range in {
		if m.ConversationID == conversationID {
			out = append(out, mapMessage(m))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp < out[j].Timestamp })
	return out
}

func initials(name string) string {
	for _, r := range name {
		return string(r)
	}
	return "?"
}

// HistoryPage preserves the raw cursor even when a page contains only service events.
type HistoryPage struct {
	Messages []Message
	CursorID int64
	HasMore  bool
}
