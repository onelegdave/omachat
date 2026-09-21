package signal

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

const cacheVersion = 2

type storedData struct {
	Version        int                          `json:"version"`
	Account        string                       `json:"account,omitempty"`
	Conversations  map[string]wire.Conversation `json:"conversations"`
	Order          []string                     `json:"order"`
	Messages       map[string][]wire.Message    `json:"messages"`
	ReactionActors map[string]map[string]string `json:"reactionActors,omitempty"`
	Expirations    map[string]int64             `json:"expirations,omitempty"`
}

func emptyStoredData() storedData {
	return storedData{
		Conversations: map[string]wire.Conversation{}, Order: []string{}, Messages: map[string][]wire.Message{},
		ReactionActors: map[string]map[string]string{}, Expirations: map[string]int64{},
	}
}

func loadStoredData(path string) storedData {
	out := emptyStoredData()
	b, err := os.ReadFile(path)
	if err != nil || errors.Is(err, os.ErrNotExist) {
		return out
	}
	if json.Unmarshal(b, &out) != nil || (out.Version != 1 && out.Version != cacheVersion) {
		return emptyStoredData()
	}
	if out.Conversations == nil {
		out.Conversations = map[string]wire.Conversation{}
	}
	if out.Messages == nil {
		out.Messages = map[string][]wire.Message{}
	}
	if out.ReactionActors == nil {
		out.ReactionActors = map[string]map[string]string{}
	}
	if out.Expirations == nil {
		out.Expirations = map[string]int64{}
	}
	// Older send paths assumed every target had already been discovered. Repair
	// successful sends to Note to Self or another not-yet-listed recipient.
	for conversationID, conversation := range out.Conversations {
		if conversation.ID == "" {
			conversation.ID = conversationID
			conversation.Name = first(conversation.Name, fallbackConversationName(conversationID, out.Account))
			conversation.IsGroup = strings.HasPrefix(conversationID, groupPrefix)
			if conversation.AvatarColor == "" {
				conversation.AvatarColor = "#3a76f0"
			}
			conversation.Initials = conversationInitials(conversation.Name)
			out.Conversations[conversationID] = conversation
		}
		if !containsString(out.Order, conversationID) {
			out.Order = append(out.Order, conversationID)
		}
	}
	// Version 1 accidentally persisted reaction, receipt, and other control
	// envelopes as empty chat bubbles. They contain no recoverable content.
	for conversationID, messages := range out.Messages {
		kept := messages[:0]
		removedControl := false
		for _, message := range messages {
			if message.Text == "" && len(message.Attachments) == 0 && !message.Deleted {
				delete(out.ReactionActors, message.ID)
				delete(out.Expirations, message.ID)
				removedControl = true
				continue
			}
			kept = append(kept, message)
		}
		out.Messages[conversationID] = kept
		if conversation, ok := out.Conversations[conversationID]; ok {
			if len(kept) == 0 {
				conversation.Preview, conversation.Timestamp, conversation.Unread = "", 0, false
			} else {
				last := kept[len(kept)-1]
				conversation.Preview, conversation.PreviewMine, conversation.Timestamp = messagePreview(last), last.FromMe, last.Timestamp
				if removedControl {
					// Version 1 had no per-message read marker, so a removed control
					// envelope's unread bit cannot be attributed to older content.
					conversation.Unread = false
				}
			}
			out.Conversations[conversationID] = conversation
		}
	}
	// Early Signal builds inserted every discovered contact and group into the
	// inbox. Remove only untouched discovery stubs; conversations with actual
	// activity retain a timestamp and/or messages and remain visible.
	order := out.Order[:0]
	for _, conversationID := range out.Order {
		conversation, ok := out.Conversations[conversationID]
		if !ok {
			continue
		}
		if len(out.Messages[conversationID]) == 0 && conversation.Timestamp == 0 && conversation.Preview == "" && !conversation.Unread {
			delete(out.Conversations, conversationID)
			continue
		}
		order = append(order, conversationID)
	}
	out.Order = order
	out.Version = cacheVersion
	return out
}

func saveStoredData(path string, data storedData) error {
	data.Version = cacheVersion
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return appStore.WritePrivateJSON(path, b)
}
