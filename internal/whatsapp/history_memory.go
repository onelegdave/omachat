package whatsapp

import (
	"sort"
	"strings"
	"unicode/utf8"
	"unsafe"

	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

const maxHistoryPreviewBytes = 4 * 1024

func truncateHistoryText(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	const suffix = "…"
	if maxBytes < len(suffix) {
		return strings.Repeat(".", maxBytes)
	}
	end := maxBytes - len(suffix)
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + suffix
}

func retainedConversationBytes(conv wire.Conversation) int64 {
	bytes := int64(unsafe.Sizeof(conv)) + int64(cap(conv.Participants))*int64(unsafe.Sizeof(wire.Participant{}))
	bytes += int64(len(conv.ID) + len(conv.Name) + len(conv.Preview) + len(conv.AvatarColor) + len(conv.AvatarPath) + len(conv.Initials) + len(conv.OutgoingID))
	for _, participant := range conv.Participants {
		bytes += int64(128 + len(participant.ID) + len(participant.Name) + len(participant.Number) + len(participant.AvatarColor) + len(participant.Initials))
	}
	return bytes
}

func retainedMessageBytes(message wire.Message) int64 {
	bytes := int64(cap(message.Attachments))*int64(unsafe.Sizeof(wire.Attachment{})) + int64(cap(message.Reactions))*int64(unsafe.Sizeof(wire.Reaction{}))
	bytes += int64(len(message.TmpID) + len(message.ID) + len(message.ConversationID) + len(message.Text) + len(message.SenderID) + len(message.SenderName) + len(message.Status) + len(message.Delivery) + len(message.ReplyToID))
	for _, attachment := range message.Attachments {
		bytes += int64(128 + len(attachment.Key) + len(attachment.MediaID) + len(attachment.Name) + len(attachment.MimeType) + len(attachment.Path))
	}
	for _, reaction := range message.Reactions {
		bytes += int64(96 + len(reaction.Emoji))
	}
	return bytes
}

func retainedReactionActorBytes(key string, actors map[string]string) int64 {
	bytes := int64(128 + len(key))
	for actor, emoji := range actors {
		bytes += int64(96 + len(actor) + len(emoji))
	}
	return bytes
}

func storedHotMemoryBytes(stored *StoredChatData) int64 {
	if stored == nil {
		return 0
	}
	bytes := int64(unsafe.Sizeof(*stored)) + int64(cap(stored.Order))*int64(unsafe.Sizeof(""))
	for _, chatID := range stored.Order {
		bytes += int64(64 + len(chatID))
	}
	for chatID, conversation := range stored.Conversations {
		bytes += int64(128+len(chatID)) + retainedConversationBytes(conversation)
	}
	for chatID, messages := range stored.Messages {
		bytes += int64(128+len(chatID)) + int64(cap(messages))*int64(unsafe.Sizeof(wire.Message{}))
		for _, message := range messages {
			bytes += retainedMessageBytes(message)
		}
	}
	for key, raw := range stored.RawMedia {
		bytes += int64(128 + len(key) + len(raw))
	}
	for key, actors := range stored.ReactionActors {
		bytes += retainedReactionActorBytes(key, actors)
	}
	return bytes
}

func trimStoredHotMessagesToByteBudget(stored *StoredChatData, byteBudget int64) {
	if stored == nil || byteBudget <= 0 {
		return
	}
	trimStoredHotMessages(stored)
	type candidate struct {
		chatID string
		id     string
		ts     int64
	}
	var candidates []candidate
	for chatID, messages := range stored.Messages {
		for _, message := range messages {
			candidates = append(candidates, candidate{chatID: chatID, id: message.ID, ts: message.Timestamp})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ts != candidates[j].ts {
			return candidates[i].ts < candidates[j].ts
		}
		if candidates[i].chatID != candidates[j].chatID {
			return candidates[i].chatID < candidates[j].chatID
		}
		return candidates[i].id < candidates[j].id
	})
	for _, candidate := range candidates {
		if storedHotMemoryBytes(stored) <= byteBudget {
			break
		}
		messages := stored.Messages[candidate.chatID]
		for i, message := range messages {
			if message.ID != candidate.id {
				continue
			}
			copy(messages[i:], messages[i+1:])
			messages[len(messages)-1] = wire.Message{}
			messages = messages[:len(messages)-1]
			if len(messages) == 0 {
				delete(stored.Messages, candidate.chatID)
			} else {
				stored.Messages[candidate.chatID] = messages
			}
			break
		}
		key := rawMediaKey(candidate.chatID, candidate.id)
		delete(stored.RawMedia, key)
		delete(stored.RawMedia, candidate.id)
		delete(stored.ReactionActors, key)
		delete(stored.ReactionActors, candidate.id)
	}
	for chatID, messages := range stored.Messages {
		stored.Messages[chatID] = append([]wire.Message(nil), messages...)
	}
	if storedHotMemoryBytes(stored) > byteBudget {
		for chatID, conversation := range stored.Conversations {
			conversation.Preview = truncateHistoryText(conversation.Preview, maxHistoryPreviewBytes)
			conversation.Name = truncateHistoryText(conversation.Name, maxHistoryPreviewBytes)
			stored.Conversations[chatID] = conversation
		}
	}
	for storedHotMemoryBytes(stored) > byteBudget && len(stored.Order) > 0 {
		last := len(stored.Order) - 1
		chatID := stored.Order[last]
		stored.Order[last] = ""
		stored.Order = stored.Order[:last]
		delete(stored.Conversations, chatID)
		delete(stored.Messages, chatID)
		for key := range stored.RawMedia {
			if strings.HasPrefix(key, chatID+"\x1f") {
				delete(stored.RawMedia, key)
			}
		}
		for key := range stored.ReactionActors {
			if strings.HasPrefix(key, chatID+"\x1f") {
				delete(stored.ReactionActors, key)
			}
		}
	}
	if storedHotMemoryBytes(stored) > byteBudget {
		stored.Conversations = make(map[string]wire.Conversation)
		stored.Order = nil
		stored.Messages = make(map[string][]wire.Message)
		stored.RawMedia = make(map[string][]byte)
		stored.ReactionActors = make(map[string]map[string]string)
	}
}

func (b *Backend) hotHistoryMemoryBytesLocked() int64 {
	bytes := int64(unsafe.Sizeof(*b)) + int64(cap(b.order))*int64(unsafe.Sizeof(""))
	bytes += int64(cap(b.historyAnchorOrder)) * int64(unsafe.Sizeof(hotHistoryAnchorKey{}))
	for _, chatID := range b.order {
		bytes += int64(64 + len(chatID))
	}
	for _, key := range b.historyAnchorOrder {
		bytes += int64(32 + len(key.chatID) + len(key.messageID))
	}
	for key := range b.historyAnchors {
		bytes += int64(128 + len(key.chatID) + len(key.messageID) + 32)
	}
	for chatID := range b.historyUnavailable {
		bytes += int64(96 + len(chatID))
	}
	for chatID := range b.historyCooldown {
		bytes += int64(96 + len(chatID))
	}
	if pending := b.historyPending; pending != nil {
		bytes += int64(160 + len(pending.chatID) + len(pending.anchor))
	}
	for chatID, conv := range b.convs {
		bytes += int64(128+len(chatID)) + retainedConversationBytes(conv)
	}
	for chatID, messages := range b.messages {
		bytes += int64(128+len(chatID)) + int64(cap(messages))*int64(unsafe.Sizeof(wire.Message{}))
		for _, message := range messages {
			bytes += retainedMessageBytes(message)
		}
	}
	for key, raw := range b.rawMsgs {
		if raw != nil {
			bytes += int64(128 + len(key) + proto.Size(raw))
		}
	}
	for key, actors := range b.reactionActors {
		bytes += retainedReactionActorBytes(key, actors)
	}
	return bytes
}

func (b *Backend) trimHotMemoryLocked() {
	if b.hotHistoryMemoryBytesLocked() <= maxHotHistoryMemoryBytes {
		return
	}
	type candidate struct {
		chatID string
		id     string
		ts     int64
	}
	var candidates []candidate
	for chatID, messages := range b.messages {
		for _, message := range messages {
			candidates = append(candidates, candidate{chatID: chatID, id: message.ID, ts: message.Timestamp})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ts != candidates[j].ts {
			return candidates[i].ts < candidates[j].ts
		}
		if candidates[i].chatID != candidates[j].chatID {
			return candidates[i].chatID < candidates[j].chatID
		}
		return candidates[i].id < candidates[j].id
	})
	for _, candidate := range candidates {
		if b.hotHistoryMemoryBytesLocked() <= maxHotHistoryMemoryBytes {
			break
		}
		messages := b.messages[candidate.chatID]
		for i, message := range messages {
			if message.ID != candidate.id {
				continue
			}
			copy(messages[i:], messages[i+1:])
			messages[len(messages)-1] = wire.Message{}
			messages = messages[:len(messages)-1]
			if len(messages) == 0 {
				delete(b.messages, candidate.chatID)
			} else {
				b.messages[candidate.chatID] = messages
			}
			break
		}
		key := rawMediaKey(candidate.chatID, candidate.id)
		delete(b.rawMsgs, key)
		delete(b.reactionActors, key)
	}
	for chatID, messages := range b.messages {
		b.messages[chatID] = append([]wire.Message(nil), messages...)
	}
	if b.hotHistoryMemoryBytesLocked() > maxHotHistoryMemoryBytes {
		for chatID, conversation := range b.convs {
			conversation.Preview = truncateHistoryText(conversation.Preview, maxHistoryPreviewBytes)
			conversation.Name = truncateHistoryText(conversation.Name, maxHistoryPreviewBytes)
			b.convs[chatID] = conversation
		}
	}
	b.trimHistoryAnchorsLocked()
	b.trimHistoryRequestStateLocked("")
	for b.hotHistoryMemoryBytesLocked() > maxHotHistoryMemoryBytes && len(b.order) > 0 {
		last := len(b.order) - 1
		chatID := b.order[last]
		b.order[last] = ""
		b.order = b.order[:last]
		delete(b.convs, chatID)
		delete(b.messages, chatID)
		for key := range b.rawMsgs {
			if strings.HasPrefix(key, chatID+"\x1f") {
				delete(b.rawMsgs, key)
			}
		}
		for key := range b.reactionActors {
			if strings.HasPrefix(key, chatID+"\x1f") {
				delete(b.reactionActors, key)
			}
		}
	}
	if b.hotHistoryMemoryBytesLocked() > maxHotHistoryMemoryBytes {
		b.convs = make(map[string]wire.Conversation)
		b.order = nil
		b.messages = make(map[string][]wire.Message)
		b.rawMsgs = make(map[string]*waE2E.Message)
		b.reactionActors = make(map[string]map[string]string)
	}
}
