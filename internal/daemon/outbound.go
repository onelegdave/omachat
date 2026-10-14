package daemon

import (
	"encoding/json"
	"io"
	"sync"
	"unicode/utf8"

	"github.com/onelegdave/omachat/internal/wire"
)

// The panel parses and lays out everything the helper sends inside the shared
// shell process, and most of it is remote content. Requests into the helper
// are bounded by maxFrame; these bound what flows back out.
const (
	maxOutboundFrame = 8 << 20
	maxMessageText   = 64 << 10
	maxDisplayText   = 1 << 10
	maxAttachments   = 32
	maxReactions     = 64
	maxParticipants  = 256
	maxListItems     = 1000
)

// connWriter serialises writes from the request path and the event pump.
type connWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// send writes one JSON line. An oversized reply becomes an error reply and an
// oversized event is dropped, so the shell never parses an unbounded frame.
func (w *connWriter) send(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(data) > maxOutboundFrame {
		resp, isResponse := v.(wire.Response)
		if !isResponse {
			return nil
		}
		if data, err = json.Marshal(wire.Response{ID: resp.ID, Error: "This reply was too large to show."}); err != nil {
			return err
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.w.Write(append(data, '\n'))
	return err
}

// truncateText cuts s to at most n bytes without splitting a character.
func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func limitConversation(c wire.Conversation) wire.Conversation {
	c.Name = truncateText(c.Name, maxDisplayText)
	c.Preview = truncateText(c.Preview, maxDisplayText)
	c.Initials = truncateText(c.Initials, 16)
	if len(c.Participants) > maxParticipants {
		c.Participants = c.Participants[:maxParticipants]
	}
	if len(c.Participants) > 0 {
		participants := make([]wire.Participant, len(c.Participants))
		for i, p := range c.Participants {
			p.Name = truncateText(p.Name, maxDisplayText)
			p.Number = truncateText(p.Number, maxDisplayText)
			p.Initials = truncateText(p.Initials, 16)
			participants[i] = p
		}
		c.Participants = participants
	}
	return c
}

func limitMessage(m wire.Message) wire.Message {
	m.Text = truncateText(m.Text, maxMessageText)
	m.SenderName = truncateText(m.SenderName, maxDisplayText)
	if len(m.Attachments) > maxAttachments {
		m.Attachments = m.Attachments[:maxAttachments]
	}
	if len(m.Attachments) > 0 {
		attachments := make([]wire.Attachment, len(m.Attachments))
		for i, a := range m.Attachments {
			a.Name = truncateText(a.Name, maxDisplayText)
			attachments[i] = a
		}
		m.Attachments = attachments
	}
	if len(m.Reactions) > maxReactions {
		m.Reactions = m.Reactions[:maxReactions]
	}
	return m
}

func limitMessagePtr(m *wire.Message) *wire.Message {
	if m == nil {
		return nil
	}
	out := limitMessage(*m)
	return &out
}

// limitOutbound bounds remote text and list sizes in anything the panel will
// receive. Values are copied, never modified in place: backends share them.
func limitOutbound(v any) any {
	switch value := v.(type) {
	case wire.Event:
		value.Data = limitOutbound(value.Data)
		return value
	case wire.Response:
		value.Result = limitOutbound(value.Result)
		return value
	case wire.Conversation:
		return limitConversation(value)
	case *wire.Conversation:
		if value == nil {
			return value
		}
		out := limitConversation(*value)
		return &out
	case []wire.Conversation:
		value = value[:min(len(value), maxListItems)]
		out := make([]wire.Conversation, len(value))
		for i, c := range value {
			out[i] = limitConversation(c)
		}
		return out
	case wire.Message:
		return limitMessage(value)
	case *wire.Message:
		return limitMessagePtr(value)
	case []wire.Message:
		value = value[:min(len(value), maxListItems)]
		out := make([]wire.Message, len(value))
		for i, m := range value {
			out[i] = limitMessage(m)
		}
		return out
	case wire.MessagesResult:
		value.Messages = limitOutbound(value.Messages).([]wire.Message)
		return value
	case *wire.MessagesResult:
		if value == nil {
			return value
		}
		out := *value
		out.Messages = limitOutbound(out.Messages).([]wire.Message)
		return &out
	case wire.SendMediaResult:
		value.Message, value.CaptionMessage = limitMessagePtr(value.Message), limitMessagePtr(value.CaptionMessage)
		return value
	case *wire.SendMediaResult:
		if value == nil {
			return value
		}
		out := *value
		out.Message, out.CaptionMessage = limitMessagePtr(out.Message), limitMessagePtr(out.CaptionMessage)
		return &out
	}
	return v
}
