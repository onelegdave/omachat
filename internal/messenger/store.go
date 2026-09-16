package messenger

import (
	"encoding/json"
	"errors"
	"os"
	"sort"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	waTypes "go.mau.fi/whatsmeow/types"
)

// messengerCacheVersion guards against loading a cache written by an
// incompatible earlier layout; a mismatch is treated as no cache at all.
const messengerCacheVersion = 1

// storedMessengerData is the private on-disk cache of Messenger conversation
// and message state. It never carries session cookies or media decryption
// secrets (those live in MessengerSessionFile and only in memory,
// respectively): everything here is safe to keep around so live messages and
// thread classification survive a helper restart.
type storedMessengerData struct {
	Version       int                          `json:"version"`
	SelfID        int64                        `json:"selfID,omitempty"`
	Conversations map[string]wire.Conversation `json:"conversations"`
	Messages      map[string][]wire.Message    `json:"messages"`
	ContactNames  map[int64]string             `json:"contactNames"`
	ThreadNames   map[int64]string             `json:"threadNames"`
	Participants  map[int64][]int64            `json:"participants"`
	ThreadTypes   map[int64]table.ThreadType   `json:"threadTypes"`
	ThreadToJID   map[int64]waTypes.JID        `json:"threadToJID"`
}

func emptyStoredMessengerData() storedMessengerData {
	return storedMessengerData{
		Conversations: map[string]wire.Conversation{},
		Messages:      map[string][]wire.Message{},
		ContactNames:  map[int64]string{},
		ThreadNames:   map[int64]string{},
		Participants:  map[int64][]int64{},
		ThreadTypes:   map[int64]table.ThreadType{},
		ThreadToJID:   map[int64]waTypes.JID{},
	}
}

func loadStoredMessengerData(path string) storedMessengerData {
	out := emptyStoredMessengerData()
	data, err := os.ReadFile(path)
	if err != nil || errors.Is(err, os.ErrNotExist) {
		return out
	}
	if json.Unmarshal(data, &out) != nil || out.Version != messengerCacheVersion {
		return emptyStoredMessengerData()
	}
	if out.Conversations == nil {
		out.Conversations = map[string]wire.Conversation{}
	}
	if out.Messages == nil {
		out.Messages = map[string][]wire.Message{}
	}
	if out.ContactNames == nil {
		out.ContactNames = map[int64]string{}
	}
	if out.ThreadNames == nil {
		out.ThreadNames = map[int64]string{}
	}
	if out.Participants == nil {
		out.Participants = map[int64][]int64{}
	}
	if out.ThreadTypes == nil {
		out.ThreadTypes = map[int64]table.ThreadType{}
	}
	if out.ThreadToJID == nil {
		out.ThreadToJID = map[int64]waTypes.JID{}
	}
	return out
}

func saveStoredMessengerData(path string, data storedMessengerData) error {
	data.Version = messengerCacheVersion
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return appStore.WritePrivateJSON(path, encoded)
}

// mergeStoredMessengerDataLocked folds persisted state into the in-memory
// maps without discarding anything already present. It must run before the
// network sync populates those maps from the server (which itself only ever
// merges): the loaded copy is the sole source of the live E2EE messages a
// restart would otherwise lose, since the server does not replay them.
// Caller must hold b.mu.
func (b *Backend) mergeStoredMessengerDataLocked(data storedMessengerData) {
	if b.selfID == 0 {
		b.selfID = data.SelfID
	}
	for id, conv := range data.Conversations {
		if _, ok := b.convs[id]; !ok {
			b.convs[id] = conv
		}
	}
	for key, stored := range data.Messages {
		existing := b.messages[key]
		seen := make(map[string]bool, len(existing))
		for _, m := range existing {
			seen[m.ID] = true
		}
		for _, m := range stored {
			if m.ID == "" || seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			existing = append(existing, m)
		}
		sort.Slice(existing, func(i, j int) bool { return existing[i].Timestamp < existing[j].Timestamp })
		b.messages[key] = existing
	}
	for id, name := range data.ContactNames {
		if _, ok := b.contactNames[id]; !ok {
			b.contactNames[id] = name
		}
	}
	for id, name := range data.ThreadNames {
		if _, ok := b.threadNames[id]; !ok {
			b.threadNames[id] = name
		}
	}
	for id, list := range data.Participants {
		if _, ok := b.participants[id]; !ok {
			b.participants[id] = list
		}
	}
	for id, typ := range data.ThreadTypes {
		if current, ok := b.threadTypes[id]; !ok || (!current.IsWhatsApp() && typ.IsWhatsApp()) {
			b.threadTypes[id] = typ
		}
	}
	for id, jid := range data.ThreadToJID {
		if jid.IsEmpty() {
			continue
		}
		if _, ok := b.threadToJID[id]; ok {
			continue
		}
		b.threadToJID[id] = jid
		b.jidToThread[jid.ToNonAD().String()] = id
	}
}

// snapshotStoredMessengerDataLocked captures the fields safe to persist.
// Caller must hold b.mu.
func (b *Backend) snapshotStoredMessengerDataLocked() storedMessengerData {
	return storedMessengerData{
		SelfID:        b.selfID,
		Conversations: b.convs,
		Messages:      b.messages,
		ContactNames:  b.contactNames,
		ThreadNames:   b.threadNames,
		Participants:  b.participants,
		ThreadTypes:   b.threadTypes,
		ThreadToJID:   b.threadToJID,
	}
}

// saveStoredMessengerDataLocked persists current state. Caller must hold b.mu.
func (b *Backend) saveStoredMessengerDataLocked() {
	if b.paths == nil {
		return
	}
	if err := saveStoredMessengerData(b.paths.MessengerStoreFile(), b.snapshotStoredMessengerDataLocked()); err != nil {
		b.log.Warn().Err(err).Msg("Could not save Messenger chat cache")
	}
}
