package whatsapp

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

const (
	maxPersistedConversations  = 1000
	maxPersistedMessages       = 100
	maxHotMessageConversations = 50
)

// StoredChatData represents the on-disk state of WhatsApp conversations and messages.
type StoredChatData struct {
	Conversations  map[string]wire.Conversation `json:"conversations"`
	Order          []string                     `json:"order"`
	Messages       map[string][]wire.Message    `json:"messages"`
	ReactionActors map[string]map[string]string `json:"reactionActors,omitempty"`
	// RawMedia holds protobuf payloads needed to download attachments after
	// restart. View-once messages are never stored here.
	RawMedia map[string][]byte `json:"rawMedia,omitempty"`
}

func newStoredChatData() *StoredChatData {
	return &StoredChatData{
		Conversations:  make(map[string]wire.Conversation),
		Order:          make([]string, 0),
		Messages:       make(map[string][]wire.Message),
		ReactionActors: make(map[string]map[string]string),
		RawMedia:       make(map[string][]byte),
	}
}

// loadChatStore loads persisted WhatsApp messages and conversations.
// Corrupt or missing files return empty state without crashing.
func loadChatStore(path string) (*StoredChatData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return newStoredChatData(), nil
		}
		return newStoredChatData(), err
	}

	var stored StoredChatData
	if err := json.Unmarshal(data, &stored); err != nil {
		return newStoredChatData(), err
	}

	if stored.Conversations == nil {
		stored.Conversations = make(map[string]wire.Conversation)
	}
	if stored.Messages == nil {
		stored.Messages = make(map[string][]wire.Message)
	}
	if stored.RawMedia == nil {
		stored.RawMedia = make(map[string][]byte)
	}
	if stored.ReactionActors == nil {
		stored.ReactionActors = make(map[string]map[string]string)
	}

	boundStoredChat(&stored)
	return &stored, nil
}

// saveChatStore atomically persists conversation and message data to disk (0600).
func saveChatStore(path string, stored *StoredChatData) error {
	if stored == nil {
		return nil
	}

	boundStoredChat(stored)
	toSave := *stored

	raw, err := json.Marshal(toSave)
	if err != nil {
		return err
	}

	return store.WritePrivateJSON(path, raw)
}

// saveChatIndex stores only bounded conversation metadata and hot reaction
// actors. Message bodies and attachment protobufs live in the budgeted SQLite
// history store after successful migration/persistence.
func saveChatIndex(path string, stored *StoredChatData) error {
	if stored == nil {
		return nil
	}
	keep := make(map[string]struct{})
	hotChats := make(map[string]struct{}, maxHotMessageConversations)
	for i, chatID := range stored.Order {
		if i >= maxHotMessageConversations {
			break
		}
		hotChats[chatID] = struct{}{}
	}
	for chatID, messages := range stored.Messages {
		if _, hot := hotChats[chatID]; !hot {
			continue
		}
		for _, message := range messages {
			if message.ID == "" {
				continue
			}
			keep[rawMediaKey(chatID, message.ID)] = struct{}{}
		}
	}
	actors := make(map[string]map[string]string)
	for key, values := range stored.ReactionActors {
		if _, ok := keep[key]; !ok || len(values) == 0 {
			continue
		}
		copyValues := make(map[string]string, len(values))
		for actor, emoji := range values {
			copyValues[actor] = emoji
		}
		actors[key] = copyValues
	}
	index := &StoredChatData{
		Conversations:  stored.Conversations,
		Order:          stored.Order,
		Messages:       make(map[string][]wire.Message),
		ReactionActors: actors,
	}
	raw, err := json.Marshal(index)
	if err != nil {
		return err
	}
	return store.WritePrivateJSON(path, raw)
}

func trimStoredHotMessages(stored *StoredChatData) {
	if stored == nil {
		return
	}
	hotChats := make(map[string]struct{}, maxHotMessageConversations)
	hotIDs := make(map[string]struct{})
	for i, chatID := range stored.Order {
		if i >= maxHotMessageConversations {
			break
		}
		hotChats[chatID] = struct{}{}
		for _, message := range stored.Messages[chatID] {
			if message.ID != "" {
				hotIDs[message.ID] = struct{}{}
			}
		}
	}
	for chatID := range stored.Messages {
		if _, ok := hotChats[chatID]; !ok {
			delete(stored.Messages, chatID)
		}
	}
	for key := range stored.RawMedia {
		chatID, _, composite := strings.Cut(key, "\x1f")
		if composite {
			if _, ok := hotChats[chatID]; !ok {
				delete(stored.RawMedia, key)
			}
		} else if _, ok := hotIDs[key]; !ok {
			delete(stored.RawMedia, key)
		}
	}
	for key := range stored.ReactionActors {
		chatID, _, composite := strings.Cut(key, "\x1f")
		if composite {
			if _, ok := hotChats[chatID]; !ok {
				delete(stored.ReactionActors, key)
			}
		} else if _, ok := hotIDs[key]; !ok {
			delete(stored.ReactionActors, key)
		}
	}
}

func boundStoredChat(stored *StoredChatData) {
	if stored == nil {
		return
	}
	if stored.Conversations == nil {
		stored.Conversations = make(map[string]wire.Conversation)
	}
	if stored.Messages == nil {
		stored.Messages = make(map[string][]wire.Message)
	}
	if stored.RawMedia == nil {
		stored.RawMedia = make(map[string][]byte)
	}
	if stored.ReactionActors == nil {
		stored.ReactionActors = make(map[string]map[string]string)
	}

	type entry struct {
		id string
		ts int64
	}
	entries := make([]entry, 0, len(stored.Conversations))
	seen := make(map[string]struct{}, len(stored.Order))
	for _, id := range stored.Order {
		if conv, ok := stored.Conversations[id]; ok {
			entries = append(entries, entry{id: id, ts: conv.Timestamp})
			seen[id] = struct{}{}
		}
	}
	for id, conv := range stored.Conversations {
		if _, ok := seen[id]; ok {
			continue
		}
		entries = append(entries, entry{id: id, ts: conv.Timestamp})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ts != entries[j].ts {
			return entries[i].ts > entries[j].ts
		}
		return entries[i].id < entries[j].id
	})
	if len(entries) > maxPersistedConversations {
		entries = entries[:maxPersistedConversations]
	}

	order := make([]string, len(entries))
	convs := make(map[string]wire.Conversation, len(entries))
	msgs := make(map[string][]wire.Message, len(entries))
	keepIDs := make(map[string]struct{}, len(entries)*maxPersistedMessages)
	for i, e := range entries {
		order[i] = e.id
		convs[e.id] = stored.Conversations[e.id]
		list := stored.Messages[e.id]
		sort.Slice(list, func(a, b int) bool {
			if list[a].Timestamp != list[b].Timestamp {
				return list[a].Timestamp < list[b].Timestamp
			}
			return list[a].ID < list[b].ID
		})
		if len(list) > maxPersistedMessages {
			list = list[len(list)-maxPersistedMessages:]
		}
		msgs[e.id] = list
		for _, m := range list {
			keepIDs[m.ID] = struct{}{}
			keepIDs[rawMediaKey(e.id, m.ID)] = struct{}{}
		}
	}

	raw := make(map[string][]byte, len(keepIDs))
	for key, data := range stored.RawMedia {
		if _, ok := keepIDs[key]; ok {
			raw[key] = data
		}
	}

	stored.Order = order
	stored.Conversations = convs
	stored.Messages = msgs
	stored.RawMedia = raw
	for key := range stored.ReactionActors {
		if _, ok := keepIDs[key]; !ok {
			delete(stored.ReactionActors, key)
		}
	}
}

func snapshotRawMedia(raw map[string]*waE2E.Message) map[string][]byte {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string][]byte, len(raw))
	for id, msg := range raw {
		if msg == nil || isViewOnce(msg) || isEphemeralWrapped(msg) || !hasDownloadableMedia(msg) {
			continue
		}
		data, err := proto.Marshal(msg)
		if err != nil {
			continue
		}
		out[id] = data
	}
	return out
}

func restoreRawMedia(stored map[string][]byte) map[string]*waE2E.Message {
	out := make(map[string]*waE2E.Message)
	for id, data := range stored {
		if len(data) == 0 {
			continue
		}
		var msg waE2E.Message
		if err := proto.Unmarshal(data, &msg); err != nil {
			continue
		}
		if isViewOnce(&msg) {
			continue
		}
		cloned, ok := proto.Clone(&msg).(*waE2E.Message)
		if !ok || cloned == nil {
			continue
		}
		out[id] = cloned
	}
	return out
}
