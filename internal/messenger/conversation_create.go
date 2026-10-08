package messenger

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

func (b *Backend) ConversationTargets(context.Context) ([]wire.ConversationTarget, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	targets := make([]wire.ConversationTarget, 0, len(b.contactNames))
	for id, name := range b.contactNames {
		if id == b.selfID || strings.TrimSpace(name) == "" {
			continue
		}
		targets = append(targets, wire.ConversationTarget{ID: strconv.FormatInt(id, 10), Name: name})
	}
	sort.Slice(targets, func(i, j int) bool { return strings.ToLower(targets[i].Name) < strings.ToLower(targets[j].Name) })
	return targets, nil
}

func (b *Backend) CreateConversation(ctx context.Context, p wire.CreateConversationParams) (wire.Conversation, error) {
	b.mu.RLock()
	client, lifetime := b.client, b.runCtx
	b.mu.RUnlock()
	if client == nil {
		return wire.Conversation{}, ErrNotConfigured
	}
	if lifetime != nil {
		bound, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(lifetime, cancel)
		defer func() { stop(); cancel() }()
		if lifetime.Err() != nil {
			cancel()
		}
		ctx = bound
	}
	return b.createConversationThrough(ctx, p, client.ExecuteTasks, func() bool { return b.client == client })
}

func (b *Backend) createConversationThrough(ctx context.Context, p wire.CreateConversationParams, execute func(context.Context, ...socket.Task) (*table.LSTable, error), current func() bool) (wire.Conversation, error) {
	normalized, err := wire.NormalizeCreateConversation(p)
	if err != nil {
		return wire.Conversation{}, err
	}
	p = normalized
	ids := make([]int64, 0, len(p.TargetIDs))
	seen := make(map[int64]bool)
	for _, raw := range p.TargetIDs {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return wire.Conversation{}, errors.New("invalid Messenger contact")
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	if ctx.Err() != nil {
		return wire.Conversation{}, ctx.Err()
	}
	group := len(ids) > 1
	threadID := ids[0]
	b.mu.RLock()
	valid := current()
	existing, exists := b.existingDirectConversationLocked(threadID)
	b.mu.RUnlock()
	if !valid {
		return wire.Conversation{}, context.Canceled
	}
	if !group && exists {
		return existing, nil
	}
	var task socket.Task
	otid, name := "", ""
	if group {
		name = p.Name
		threadID = methods.GenerateEpochID()
		otid = strconv.FormatInt(methods.GenerateEpochID(), 10)
		task = &socket.CreateGroupTask{Participants: ids, SendPayload: socket.CreateGroupPayload{ThreadID: threadID, OTID: otid, Source: 0, SendType: 8}}
	} else {
		task = &socket.CreateThreadTask{ThreadFBID: threadID, ForceUpsert: 1, SyncGroup: 1}
		b.mu.RLock()
		name = b.contactNames[threadID]
		b.mu.RUnlock()
	}
	response, err := execute(ctx, task)
	if err != nil {
		return wire.Conversation{}, errors.New("Could not confirm Messenger chat creation; refresh before retrying")
	}
	if err := messengerCreationTaskError(response); err != nil {
		return wire.Conversation{}, err
	}
	confirmedID, typ := confirmedCreatedMessengerThread(response, threadID, otid)
	if confirmedID <= 0 {
		return wire.Conversation{}, errors.New("Messenger did not confirm the new chat; refresh before retrying")
	}
	if typ.IsWhatsApp() {
		return wire.Conversation{}, errors.New("Start new encrypted chats in Messenger, then refresh OmaChat")
	}
	threadID = confirmedID
	if group {
		b.mu.RLock()
		valid = current()
		b.mu.RUnlock()
		if !valid || ctx.Err() != nil {
			return wire.Conversation{}, context.Canceled
		}
		response, err = execute(ctx, &socket.RenameThreadTask{ThreadKey: threadID, ThreadName: name, SyncGroup: 1})
		if err != nil || messengerCreationTaskError(response) != nil {
			return wire.Conversation{}, errors.New("Messenger group was created but its name was not confirmed; refresh before retrying")
		}
	}
	b.mu.Lock()
	if !current() || ctx.Err() != nil {
		b.mu.Unlock()
		return wire.Conversation{}, context.Canceled
	}
	b.commitCreatedConversationLocked(threadID, ids, name, group)
	b.saveStoredMessengerDataLocked()
	conversation := b.convs[strconv.FormatInt(threadID, 10)]
	b.mu.Unlock()
	if b.publish != nil {
		b.publish(wire.Event{Event: wire.EventConversation, Network: wire.NetworkMessenger, Data: conversation})
	}
	return conversation, nil
}

func messengerCreationTaskError(response *table.LSTable) error {
	if response == nil || len(response.LSHandleFailedTask) > 0 || len(response.LSIssueNewError) > 0 {
		return errors.New("Messenger could not confirm the request; refresh before retrying")
	}
	return nil
}

// Require a server thread row or the group result tied to this request's OTID.
// Transport success alone is not evidence that the provider created the chat.
func confirmedCreatedMessengerThread(response *table.LSTable, requested int64, otid string) (int64, table.ThreadType) {
	if otid != "" {
		for _, row := range response.LSApplyNewGroupThread {
			if row.OtidOfFirstMessage == otid && row.ThreadKey > 0 {
				return row.ThreadKey, row.ThreadType
			}
		}
	}
	for _, row := range response.LSDeleteThenInsertThread {
		if row.ThreadKey == requested {
			return requested, row.ThreadType
		}
	}
	for _, row := range response.LSUpdateOrInsertThread {
		if row.ThreadKey == requested {
			return requested, row.ThreadType
		}
	}
	for _, row := range response.LSVerifyThreadExists {
		if row.ThreadKey == requested {
			return requested, row.ThreadType
		}
	}
	return 0, 0
}

// A contact picker is not new activity and must not clear an existing unread
// thread or replace encrypted routing learned from the provider.
func (b *Backend) commitCreatedConversationLocked(threadID int64, ids []int64, name string, group bool) {
	key := strconv.FormatInt(threadID, 10)
	if _, exists := b.convs[key]; exists {
		return
	}
	b.upsertThreadLocked(threadID, name, "", time.Now().UnixMilli(), 0, false, group, false, "", map[bool]table.ThreadType{true: table.GROUP_THREAD, false: table.ONE_TO_ONE}[group])
	for _, id := range ids {
		b.addParticipantLocked(threadID, id)
	}
}

func (b *Backend) existingDirectConversationLocked(contact int64) (wire.Conversation, bool) {
	key := strconv.FormatInt(contact, 10)
	best, found := b.convs[key]
	if found && best.IsGroup {
		found = false
	}
	for thread, participants := range b.participants {
		c, ok := b.convs[strconv.FormatInt(thread, 10)]
		if !ok || c.IsGroup {
			continue
		}
		matches, others := false, 0
		for _, id := range participants {
			if id != b.selfID {
				others++
				matches = matches || id == contact
			}
		}
		if matches && others == 1 && (!found || c.Timestamp > best.Timestamp || (c.Timestamp == best.Timestamp && c.ID < best.ID)) {
			best, found = c, true
		}
	}
	for thread, jid := range b.threadToJID {
		c, ok := b.convs[strconv.FormatInt(thread, 10)]
		if ok && !c.IsGroup && jid.User == key && (!found || c.Timestamp > best.Timestamp || (c.Timestamp == best.Timestamp && c.ID < best.ID)) {
			best, found = c, true
		}
	}
	return best, found
}
