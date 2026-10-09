package whatsapp

import (
	"context"
	"sort"
	"strings"

	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"

	"github.com/onelegdave/omachat/internal/wire"
)

// reconcileConversationAliases coalesces direct-chat rows when WhatsApp has
// both the phone-number JID and its linked-device LID in the local cache.
func (b *Backend) reconcileConversationAliases(gen uint64, device *waStore.Device) {
	if device == nil || device.LIDs == nil {
		return
	}
	b.mu.RLock()
	if b.gen != gen {
		b.mu.RUnlock()
		return
	}
	ctx := b.ctx
	ids := append([]string(nil), b.order...)
	history := b.historyStore
	conversationIDs := make(map[string]struct{}, len(b.convs))
	for id := range b.convs {
		conversationIDs[id] = struct{}{}
	}
	b.mu.RUnlock()
	if ctx == nil {
		ctx = context.Background()
	}

	// The phone-number JID is the stable user-facing chat identity when both
	// forms exist, and also gives contacts without a saved name a useful label.
	pairs := make(map[string]string)
	for _, id := range ids {
		jid, err := types.ParseJID(id)
		if err != nil || jid.Server != types.HiddenUserServer {
			continue
		}
		pn, err := device.GetAltJID(ctx, jid)
		if err != nil || pn.IsEmpty() || pn.Server != types.DefaultUserServer {
			continue
		}
		pnID := pn.ToNonAD().String()
		_, hasLID := conversationIDs[id]
		_, hasPN := conversationIDs[pnID]
		if id != pnID && hasLID && hasPN {
			pairs[id] = pnID
		}
	}

	for oldID, newID := range pairs {
		if history != nil {
			if err := history.rekeyChat(ctx, oldID, newID); err != nil {
				b.log.Warn().Err(err).Msg("Could not merge WhatsApp chat history aliases")
				continue
			}
		}
		b.mu.Lock()
		if b.gen != gen {
			b.mu.Unlock()
			return
		}
		oldConv, oldOK := b.convs[oldID]
		newConv, newOK := b.convs[newID]
		if !oldOK || !newOK {
			b.mu.Unlock()
			continue
		}
		if oldConv.Timestamp > newConv.Timestamp {
			newConv.Preview = oldConv.Preview
			newConv.PreviewMine = oldConv.PreviewMine
			newConv.Timestamp = oldConv.Timestamp
		}
		newConv.Unread = newConv.Unread || oldConv.Unread
		newConv.Pinned = newConv.Pinned || oldConv.Pinned
		newConv.IsGroup = false
		if newConv.AvatarPath == "" {
			newConv.AvatarPath = oldConv.AvatarPath
		}
		if newConv.Name == "" || isFallbackConversationName(types.JID{User: strings.TrimSuffix(newID, "@s.whatsapp.net"), Server: types.DefaultUserServer}, newConv.Name) {
			newConv.Name = types.JID{User: strings.TrimSuffix(newID, "@s.whatsapp.net"), Server: types.DefaultUserServer}.User
			newConv.Initials = initials(newConv.Name)
		}
		newConv.ID = newID
		b.convs[newID] = newConv
		delete(b.convs, oldID)

		merged := make(map[string]wire.Message)
		for _, list := range [][]wire.Message{b.messages[newID], b.messages[oldID]} {
			for _, message := range list {
				message.ConversationID = newID
				if _, exists := merged[message.ID]; !exists {
					merged[message.ID] = message
				}
			}
		}
		messages := make([]wire.Message, 0, len(merged))
		for _, message := range merged {
			messages = append(messages, message)
		}
		sort.Slice(messages, func(i, j int) bool {
			if messages[i].Timestamp != messages[j].Timestamp {
				return messages[i].Timestamp < messages[j].Timestamp
			}
			return messages[i].ID < messages[j].ID
		})
		b.messages[newID] = messages
		delete(b.messages, oldID)

		for key, payload := range b.rawMsgs {
			chatID, messageID, composite := strings.Cut(key, "\x1f")
			if composite && chatID == oldID {
				b.rawMsgs[rawMediaKey(newID, messageID)] = payload
				delete(b.rawMsgs, key)
			}
		}
		for key, actors := range b.reactionActors {
			chatID, messageID, composite := strings.Cut(key, "\x1f")
			if !composite || chatID != oldID {
				continue
			}
			target := rawMediaKey(newID, messageID)
			if b.reactionActors[target] == nil {
				b.reactionActors[target] = make(map[string]string)
			}
			for actor, emoji := range actors {
				b.reactionActors[target][actor] = emoji
			}
			delete(b.reactionActors, key)
		}
		for i, id := range b.order {
			if id == oldID {
				b.order = append(b.order[:i], b.order[i+1:]...)
				break
			}
		}
		b.reorderLocked()
		b.saveStoreLocked()
		b.emitLocked(wire.EventConversation, b.convs[newID])
		b.mu.Unlock()
	}
}
