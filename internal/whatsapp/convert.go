package whatsapp

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/onelegdave/omachat/internal/wire"
)

// initials derives up to two uppercase letters for the avatar fallback circle.
func initials(name string) string {
	var out []rune
	for _, f := range strings.Fields(name) {
		for _, r := range f {
			if unicode.IsLetter(r) {
				out = append(out, unicode.ToUpper(r))
				break
			}
		}
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "#"
	}
	return string(out)
}

// avatarColor generates a deterministic hex color from a JID.
func avatarColor(jid string) string {
	h := sha256.Sum256([]byte(jid))
	colors := []string{
		"#1f77b4", "#ff7f0e", "#2ca02c", "#d62728", "#9467bd",
		"#8c564b", "#e377c2", "#7f7f7f", "#bcbd22", "#17becf",
		"#00a884", "#25d366", "#34b7f1", "#128c7e", "#075e54",
	}
	idx := int(h[0]) % len(colors)
	return colors[idx]
}

// unwrapMessage peels away Ephemeral and ViewOnce outer envelopes.
func unwrapMessage(msg *waE2E.Message) (*waE2E.Message, bool) {
	if msg == nil {
		return nil, false
	}
	viewOnce := false
	curr := msg

	for {
		if eph := curr.GetEphemeralMessage(); eph != nil && eph.GetMessage() != nil {
			curr = eph.GetMessage()
			continue
		}
		if vo := curr.GetViewOnceMessage(); vo != nil && vo.GetMessage() != nil {
			viewOnce = true
			curr = vo.GetMessage()
			continue
		}
		if vo2 := curr.GetViewOnceMessageV2(); vo2 != nil && vo2.GetMessage() != nil {
			viewOnce = true
			curr = vo2.GetMessage()
			continue
		}
		if vo2Ext := curr.GetViewOnceMessageV2Extension(); vo2Ext != nil && vo2Ext.GetMessage() != nil {
			viewOnce = true
			curr = vo2Ext.GetMessage()
			continue
		}
		break
	}

	if img := curr.GetImageMessage(); img != nil && img.GetViewOnce() {
		viewOnce = true
	}
	if vid := curr.GetVideoMessage(); vid != nil && vid.GetViewOnce() {
		viewOnce = true
	}
	if aud := curr.GetAudioMessage(); aud != nil && aud.GetViewOnce() {
		viewOnce = true
	}

	return curr, viewOnce
}

func rawMediaKey(chatID, messageID string) string {
	return chatID + "\x1f" + messageID
}

func isEphemeralWrapped(msg *waE2E.Message) bool {
	return msg != nil && msg.GetEphemeralMessage() != nil
}

func restrictedLifetime(evt *events.Message, raw *waE2E.Message) bool {
	if evt != nil && (evt.IsViewOnce || evt.IsEphemeral) {
		return true
	}
	return isViewOnce(raw) || isEphemeralWrapped(raw)
}

func lifetimePlaceholder() string {
	return "Disappearing or view-once messages are not kept on this desktop."
}

// isViewOnce reports whether a message is designated as view-once.
func isViewOnce(msg *waE2E.Message) bool {
	_, vo := unwrapMessage(msg)
	return vo
}

// isDisplayableMessage filters out unsupported control/protocol messages so empty bubbles
// are not shown to the user.
func isDisplayableMessage(raw *waE2E.Message, text string, atts []wire.Attachment) bool {
	if raw == nil {
		return false
	}
	// Pure protocol or control envelopes
	if raw.ProtocolMessage != nil ||
		raw.SenderKeyDistributionMessage != nil ||
		raw.FastRatchetKeySenderKeyDistributionMessage != nil {
		return false
	}
	// Must have text, attachments, or reaction to be displayable
	if text == "" && len(atts) == 0 && raw.ReactionMessage == nil {
		return false
	}
	return true
}

// extractText pulls the primary human-readable text from a WhatsApp message payload.
func extractText(msg *waE2E.Message) string {
	msg, _ = unwrapMessage(msg)
	if msg == nil {
		return ""
	}
	if c := msg.GetConversation(); c != "" {
		return c
	}
	if ext := msg.GetExtendedTextMessage(); ext != nil && ext.GetText() != "" {
		return ext.GetText()
	}
	if img := msg.GetImageMessage(); img != nil && img.GetCaption() != "" {
		return img.GetCaption()
	}
	if doc := msg.GetDocumentMessage(); doc != nil && doc.GetCaption() != "" {
		return doc.GetCaption()
	}
	if vid := msg.GetVideoMessage(); vid != nil && vid.GetCaption() != "" {
		return vid.GetCaption()
	}
	return ""
}

func hasDownloadableMedia(msg *waE2E.Message) bool {
	if msg == nil || isViewOnce(msg) {
		return false
	}
	inner, _ := unwrapMessage(msg)
	if inner == nil {
		return false
	}
	return inner.GetImageMessage() != nil ||
		inner.GetDocumentMessage() != nil ||
		inner.GetVideoMessage() != nil ||
		inner.GetAudioMessage() != nil ||
		inner.GetStickerMessage() != nil
}

// extractAttachments extracts attachment metadata from a WhatsApp message.
// View-once media is never exposed as a downloadable attachment.
func extractAttachments(msg *waE2E.Message, chatID, messageID string) []wire.Attachment {
	msg, isVO := unwrapMessage(msg)
	if msg == nil || isVO {
		return nil
	}
	key := rawMediaKey(chatID, messageID)
	var atts []wire.Attachment
	if img := msg.GetImageMessage(); img != nil {
		mime := img.GetMimetype()
		if mime == "" {
			mime = "image/jpeg"
		}
		atts = append(atts, wire.Attachment{
			Key:      key,
			MediaID:  messageID,
			MimeType: mime,
			Size:     int64(img.GetFileLength()),
			Width:    int64(img.GetWidth()),
			Height:   int64(img.GetHeight()),
			IsImage:  true,
		})
	} else if doc := msg.GetDocumentMessage(); doc != nil {
		mime := doc.GetMimetype()
		atts = append(atts, wire.Attachment{
			Key:      key,
			MediaID:  messageID,
			Name:     doc.GetFileName(),
			MimeType: mime,
			Size:     int64(doc.GetFileLength()),
			IsImage:  strings.HasPrefix(mime, "image/"),
		})
	} else if vid := msg.GetVideoMessage(); vid != nil {
		mime := vid.GetMimetype()
		if mime == "" {
			mime = "video/mp4"
		}
		atts = append(atts, wire.Attachment{
			Key:      key,
			MediaID:  messageID,
			MimeType: mime,
			Size:     int64(vid.GetFileLength()),
			IsVideo:  true,
		})
	} else if aud := msg.GetAudioMessage(); aud != nil {
		mime := aud.GetMimetype()
		if mime == "" {
			mime = "audio/ogg"
		}
		atts = append(atts, wire.Attachment{
			Key:      key,
			MediaID:  messageID,
			MimeType: mime,
			Size:     int64(aud.GetFileLength()),
			IsAudio:  true,
		})
	} else if stk := msg.GetStickerMessage(); stk != nil {
		mime := stk.GetMimetype()
		if mime == "" {
			mime = "image/webp"
		}
		atts = append(atts, wire.Attachment{
			Key:      key,
			MediaID:  messageID,
			MimeType: mime,
			Size:     int64(stk.GetFileLength()),
			Width:    int64(stk.GetWidth()),
			Height:   int64(stk.GetHeight()),
			IsImage:  true,
		})
	}
	return atts
}

// convertEventMessage maps an incoming WhatsApp event message to wire.Message.
func convertEventMessage(evt *events.Message) (wire.Message, bool) {
	chatJID := evt.Info.Chat.String()
	senderJID := evt.Info.Sender.String()
	unwrapped, _ := unwrapMessage(evt.Message)
	restricted := restrictedLifetime(evt, evt.Message)
	var text string
	var atts []wire.Attachment
	if restricted {
		text = lifetimePlaceholder()
	} else {
		text = extractText(unwrapped)
		atts = extractAttachments(evt.Message, evt.Info.Chat.String(), evt.Info.ID)
	}

	if !isDisplayableMessage(unwrapped, text, atts) {
		return wire.Message{}, false
	}

	senderName := evt.Info.PushName
	if senderName == "" {
		senderName = evt.Info.Sender.User
	}

	timestamp := evt.Info.Timestamp.UnixMicro()
	if timestamp == 0 {
		timestamp = 1
	}

	out := wire.Message{
		ID:             evt.Info.ID,
		ConversationID: chatJID,
		Text:           text,
		Timestamp:      timestamp,
		FromMe:         evt.Info.IsFromMe,
		SenderID:       senderJID,
		SenderName:     senderName,
		Attachments:    atts,
	}
	if out.FromMe {
		out.Delivery = wire.DeliverySent
	}
	return out, true
}

// formatConversationName resolves a friendly chat name from a JID and available contact/push name.
func formatConversationName(jid types.JID, pushName string) string {
	if pushName != "" {
		return pushName
	}
	if jid.Server == types.GroupServer {
		return fmt.Sprintf("Group (%s)", jid.User)
	}
	if jid.Server == "lid" {
		return fmt.Sprintf("WhatsApp User (%s)", jid.User)
	}
	if jid.User != "" {
		return jid.User
	}
	return jid.String()
}
