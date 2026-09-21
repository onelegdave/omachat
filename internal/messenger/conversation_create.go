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
	if len(p.TargetIDs) == 0 {
		return wire.Conversation{}, errors.New("select at least one contact")
	}
	ids := make([]int64, 0, len(p.TargetIDs))
	for _, raw := range p.TargetIDs {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return wire.Conversation{}, errors.New("invalid Messenger contact")
		}
		ids = append(ids, id)
	}
	b.mu.RLock()
	client := b.client
	b.mu.RUnlock()
	if client == nil {
		return wire.Conversation{}, ErrNotConfigured
	}
	threadID := ids[0]
	group := len(ids) > 1
	name := ""
	if group {
		name = strings.TrimSpace(p.Name)
		if name == "" {
			return wire.Conversation{}, errors.New("group name is required")
		}
		threadID = methods.GenerateEpochID()
		otid := strconv.FormatInt(methods.GenerateEpochID(), 10)
		_, err := client.ExecuteTasks(ctx,
			&socket.CreateGroupTask{Participants: ids, SendPayload: socket.CreateGroupPayload{ThreadID: threadID, OTID: otid, Source: 0, SendType: 8}},
			&socket.RenameThreadTask{ThreadKey: threadID, ThreadName: name, SyncGroup: 1},
		)
		if err != nil {
			return wire.Conversation{}, err
		}
	} else {
		if _, err := client.ExecuteTasks(ctx, &socket.CreateThreadTask{ThreadFBID: threadID, ForceUpsert: 1, SyncGroup: 1}); err != nil {
			return wire.Conversation{}, err
		}
		b.mu.RLock()
		name = b.contactNames[threadID]
		b.mu.RUnlock()
	}
	b.mu.Lock()
	b.upsertThreadLocked(threadID, name, "", time.Now().UnixMilli(), 0, false, group, false, "", map[bool]table.ThreadType{true: table.GROUP_THREAD, false: table.ONE_TO_ONE}[group])
	for _, id := range ids {
		b.addParticipantLocked(threadID, id)
	}
	b.saveStoredMessengerDataLocked()
	conversation := b.convs[strconv.FormatInt(threadID, 10)]
	b.mu.Unlock()
	if b.publish != nil {
		b.publish(wire.Event{Event: wire.EventConversation, Network: wire.NetworkMessenger, Data: conversation})
	}
	return conversation, nil
}
