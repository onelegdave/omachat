package whatsapp

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/onelegdave/omachat/internal/wire"
)

const whatsappHistoryPageSize = 50

type pendingHistoryRequest struct {
	chatID     string
	gen        uint64
	anchor     string
	anchorTime int64
	timer      *time.Timer
}

const (
	maxHotHistoryAnchors          = 256
	maxHotHistoryAnchorKeyBytes   = 2 * 1024
	maxHistoryRequestStateEntries = 256
)

type hotHistoryAnchorKey struct {
	chatID    string
	messageID string
}

type hotHistoryAnchor struct {
	timestamp int64
	fromMe    bool
}

func (b *Backend) publishHistory(update wire.HistoryUpdate) {
	if b.publish != nil {
		b.publish(wire.Event{Event: wire.EventHistory, Network: wire.NetworkWhatsApp, Data: update})
	}
}

func jidIdentityAliases(ctx context.Context, device *waStore.Device, jid types.JID) []types.JID {
	jid = jid.ToNonAD()
	aliases := []types.JID{jid}
	if device == nil || device.LIDs == nil || (jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer) {
		return aliases
	}
	if alt, err := device.GetAltJID(ctx, jid); err == nil && !alt.IsEmpty() {
		alt = alt.ToNonAD()
		if alt != jid {
			aliases = append(aliases, alt)
		}
	}
	return aliases
}

func historyConversationMatches(ctx context.Context, device *waStore.Device, chatID string, conversation *waHistorySync.Conversation) bool {
	if conversation == nil {
		return false
	}
	requested, err := types.ParseJID(chatID)
	if err != nil {
		return false
	}
	requestedAliases := make(map[types.JID]struct{})
	for _, alias := range jidIdentityAliases(ctx, device, requested) {
		requestedAliases[alias] = struct{}{}
	}
	for _, raw := range []string{conversation.GetID(), conversation.GetPnJID(), conversation.GetLidJID()} {
		candidate, err := types.ParseJID(raw)
		if err != nil {
			continue
		}
		for _, alias := range jidIdentityAliases(ctx, device, candidate) {
			if _, ok := requestedAliases[alias]; ok {
				return true
			}
		}
	}
	return false
}

func (b *Backend) canFetchHistoryLocked(chatID string) bool {
	return b.paired && b.historyUnavailable[chatID] != true
}

func (b *Backend) trimHistoryRequestStateLocked(keepChatID string) {
	for len(b.historyUnavailable) > maxHistoryRequestStateEntries {
		for chatID := range b.historyUnavailable {
			if chatID != keepChatID {
				delete(b.historyUnavailable, chatID)
				break
			}
		}
	}
	for len(b.historyCooldown) > maxHistoryRequestStateEntries {
		for chatID := range b.historyCooldown {
			if chatID != keepChatID {
				delete(b.historyCooldown, chatID)
				break
			}
		}
	}
}

func (b *Backend) rememberHistoryAnchorLocked(chatID string, message wire.Message) {
	if chatID == "" || message.ID == "" || len(chatID)+len(message.ID) > maxHotHistoryAnchorKeyBytes {
		return
	}
	key := hotHistoryAnchorKey{chatID: chatID, messageID: message.ID}
	for i, existing := range b.historyAnchorOrder {
		if existing == key {
			copy(b.historyAnchorOrder[i:], b.historyAnchorOrder[i+1:])
			last := len(b.historyAnchorOrder) - 1
			b.historyAnchorOrder[last] = hotHistoryAnchorKey{}
			b.historyAnchorOrder = b.historyAnchorOrder[:last]
			break
		}
	}
	b.historyAnchors[key] = hotHistoryAnchor{timestamp: message.Timestamp, fromMe: message.FromMe}
	b.historyAnchorOrder = append(b.historyAnchorOrder, key)
	if len(b.historyAnchorOrder) > maxHotHistoryAnchors {
		oldest := b.historyAnchorOrder[0]
		delete(b.historyAnchors, oldest)
		copy(b.historyAnchorOrder, b.historyAnchorOrder[1:])
		last := len(b.historyAnchorOrder) - 1
		b.historyAnchorOrder[last] = hotHistoryAnchorKey{}
		b.historyAnchorOrder = b.historyAnchorOrder[:last]
	}
	b.trimHistoryAnchorsLocked()
}

func (b *Backend) trimHistoryAnchorsLocked() {
	for b.hotHistoryMemoryBytesLocked() > maxHotHistoryMemoryBytes && len(b.historyAnchorOrder) > 0 {
		oldest := b.historyAnchorOrder[0]
		delete(b.historyAnchors, oldest)
		copy(b.historyAnchorOrder, b.historyAnchorOrder[1:])
		last := len(b.historyAnchorOrder) - 1
		b.historyAnchorOrder[last] = hotHistoryAnchorKey{}
		b.historyAnchorOrder = b.historyAnchorOrder[:last]
	}
}

func (b *Backend) hotHistoryAnchor(chatID, messageID string) *wire.Message {
	b.mu.RLock()
	anchor, ok := b.historyAnchors[hotHistoryAnchorKey{chatID: chatID, messageID: messageID}]
	b.mu.RUnlock()
	if !ok {
		return nil
	}
	return &wire.Message{
		ID:             messageID,
		ConversationID: chatID,
		Timestamp:      anchor.timestamp,
		FromMe:         anchor.fromMe,
	}
}

func (b *Backend) requestOlderHistory(ctx context.Context, chatID, anchorID string, hotAnchor *wire.Message) (string, string) {
	if chatID == "" || anchorID == "" || len(chatID)+len(anchorID) > maxHotHistoryAnchorKeyBytes {
		return "failed", "The WhatsApp conversation or history cursor is invalid."
	}
	b.mu.RLock()
	store := b.historyStore
	client := b.client
	paired := b.paired
	connected := b.status.State == wire.StateConnected
	gen := b.gen
	timeout := b.historyTimeout
	b.mu.RUnlock()
	if !paired || client == nil {
		return "failed", "Pair WhatsApp and connect your phone to request older history."
	}
	if !connected {
		return "failed", "Reconnect WhatsApp, then retry loading older messages."
	}
	if store == nil {
		return "failed", "The local WhatsApp history cache is unavailable."
	}
	anchorMessage, ok, err := store.get(ctx, chatID, anchorID)
	if err != nil {
		return "failed", "Could not read the WhatsApp history anchor."
	}
	if !ok && hotAnchor != nil && hotAnchor.ID == anchorID && hotAnchor.ConversationID == chatID {
		anchorMessage, ok = *hotAnchor, true
	}
	if !ok {
		return "failed", "The history anchor is no longer cached. Refresh the conversation and retry."
	}
	chat, err := types.ParseJID(chatID)
	if err != nil {
		return "failed", "The WhatsApp conversation cannot request older history."
	}
	anchor := types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chat, IsFromMe: anchorMessage.FromMe, IsGroup: chat.Server == types.GroupServer},
		ID:            types.MessageID(anchorMessage.ID),
		Timestamp:     time.UnixMicro(anchorMessage.Timestamp),
	}
	b.mu.Lock()
	if b.historyPending != nil {
		if b.historyPending.chatID == chatID && b.historyPending.anchor == anchorID && b.historyPending.gen == gen {
			b.mu.Unlock()
			return "loading", ""
		}
		b.mu.Unlock()
		return "failed", "Another WhatsApp history request is still in progress."
	}
	if b.historyUnavailable[chatID] {
		b.mu.Unlock()
		return "unavailable", "Your phone did not provide an older history page."
	}
	if time.Now().Before(b.historyCooldown[chatID]) {
		b.mu.Unlock()
		return "failed", "Wait briefly before retrying older history."
	}
	delete(b.historyCooldown, chatID)
	pending := &pendingHistoryRequest{chatID: chatID, gen: gen, anchor: anchorID, anchorTime: anchorMessage.Timestamp}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	pending.timer = time.AfterFunc(timeout, func() {
		b.finishHistoryRequest(pending, "failed", "The phone did not respond in time. Check the connection and retry.")
	})
	b.historyPending = pending
	b.mu.Unlock()

	b.publishHistory(wire.HistoryUpdate{ConversationID: chatID, State: "loading"})
	requestCtx := ctx
	if requestCtx == nil {
		requestCtx = context.Background()
	}
	if err := requestHistory(requestCtx, client, anchor, whatsappHistoryPageSize); err != nil {
		b.finishHistoryRequest(pending, "failed", "Could not request older history from the phone. Retry when WhatsApp is connected.")
		return "failed", "Could not request older history from the phone. Retry when WhatsApp is connected."
	}
	return "loading", ""
}

func (b *Backend) finishHistoryRequest(pending *pendingHistoryRequest, state, notice string) {
	if pending == nil {
		return
	}
	b.mu.Lock()
	if b.historyPending != pending || b.gen != pending.gen {
		b.mu.Unlock()
		return
	}
	if pending.timer != nil {
		pending.timer.Stop()
	}
	b.historyPending = nil
	switch state {
	case "unavailable":
		b.historyUnavailable[pending.chatID] = true
		delete(b.historyCooldown, pending.chatID)
	case "complete":
		delete(b.historyUnavailable, pending.chatID)
		delete(b.historyCooldown, pending.chatID)
	case "failed":
		cooldown := b.historyRetryCooldown
		if cooldown <= 0 {
			cooldown = 2 * time.Second
		}
		b.historyCooldown[pending.chatID] = time.Now().Add(cooldown)
	}
	b.trimHistoryRequestStateLocked(pending.chatID)
	b.mu.Unlock()
	b.publishHistory(wire.HistoryUpdate{ConversationID: pending.chatID, State: state, Notice: notice})
}

// handleOnDemandHistorySync consumes ON_DEMAND chunks only when they match the
// single in-flight chat request. The pinned builder provides no documented
// mapping from the notification's session/original-message fields to the
// outbound request, so correlation remains serialized and chat-scoped. A chat
// matches only by exact JID, explicit PN/LID fields, or the local PN/LID map;
// display names are never used as identity evidence.
func (b *Backend) handleOnDemandHistorySync(gen uint64, data *waHistorySync.HistorySync, notification *waE2E.HistorySyncNotification) bool {
	if data == nil || data.GetSyncType() != waHistorySync.HistorySync_ON_DEMAND {
		return false
	}
	b.mu.RLock()
	pending := b.historyPending
	current := pending != nil && pending.gen == gen && b.gen == gen
	device := b.device
	ctx := b.ctx
	b.mu.RUnlock()
	if !current {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var matching *waHistorySync.Conversation
	for _, conversation := range data.GetConversations() {
		if historyConversationMatches(ctx, device, pending.chatID, conversation) {
			matching = conversation
			break
		}
	}
	complete := onDemandHistorySyncComplete(data, notification, matching)
	if matching == nil {
		if len(data.GetConversations()) > 0 {
			b.finishHistoryRequest(pending, "failed", "The phone returned history for a different conversation. Retry this chat.")
		} else if complete {
			b.finishCompletedHistoryFromCache(pending)
		}
		return true
	}
	filtered := proto.Clone(data).(*waHistorySync.HistorySync)
	matchedCopy := proto.Clone(matching).(*waHistorySync.Conversation)
	matchedCopy.ID = proto.String(pending.chatID)
	filtered.Conversations = []*waHistorySync.Conversation{matchedCopy}
	b.mu.Lock()
	if b.historyPending != pending || b.gen != gen {
		b.mu.Unlock()
		return true
	}
	b.mu.Unlock()
	if err := b.ingestHistorySyncMode(gen, filtered, true); err != nil {
		b.finishHistoryRequest(pending, "failed", "The local WhatsApp history cache could not retain the phone's response. Increase the cache budget or free local space, then retry.")
		return true
	}
	if complete {
		b.finishCompletedHistoryFromCache(pending)
	}
	return true
}

func (b *Backend) finishCompletedHistoryFromCache(pending *pendingHistoryRequest) {
	b.mu.RLock()
	store := b.historyStore
	b.mu.RUnlock()
	if store == nil {
		b.finishHistoryRequest(pending, "failed", "The local WhatsApp history cache closed before the response was stored.")
		return
	}
	page, err := store.page(context.Background(), pending.chatID, 1, pending.anchor, pending.anchorTime)
	if err != nil {
		b.finishHistoryRequest(pending, "failed", "Could not verify the cached WhatsApp history response.")
	} else if len(page.Messages) == 0 {
		b.finishHistoryRequest(pending, "unavailable", "Your phone did not provide an older history page.")
	} else {
		b.finishHistoryRequest(pending, "complete", "")
	}
}

func onDemandHistorySyncComplete(data *waHistorySync.HistorySync, notification *waE2E.HistorySyncNotification, conversation *waHistorySync.Conversation) bool {
	if data != nil && data.GetProgress() >= 100 {
		return true
	}
	if notification != nil && notification.GetProgress() >= 100 {
		return true
	}
	if conversation == nil {
		return false
	}
	if conversation.GetEndOfHistoryTransfer() {
		return true
	}
	if conversation.EndOfHistoryTransferType == nil {
		return false
	}
	switch conversation.GetEndOfHistoryTransferType() {
	case waHistorySync.Conversation_COMPLETE_BUT_MORE_MESSAGES_REMAIN_ON_PRIMARY,
		waHistorySync.Conversation_COMPLETE_AND_NO_MORE_MESSAGE_REMAIN_ON_PRIMARY,
		waHistorySync.Conversation_COMPLETE_ON_DEMAND_SYNC_BUT_MORE_MSG_REMAIN_ON_PRIMARY,
		waHistorySync.Conversation_COMPLETE_ON_DEMAND_SYNC_WITH_MORE_MSG_ON_PRIMARY_BUT_NO_ACCESS:
		return true
	default:
		return false
	}
}
