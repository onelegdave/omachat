package signal

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
)

type signalContact struct {
	Number, UUID, Username, Name, GivenName, FamilyName string
	Unregistered                                        bool `json:"unregistered"`
}

func (b *Backend) creationSession(ctx context.Context) (context.Context, func(), Caller, string, uint64, error) {
	b.mu.RLock()
	account, client, lifetime, gen, state := b.account, b.client, b.runCtx, b.generation, b.status.State
	b.mu.RUnlock()
	if account == "" || client == nil || lifetime == nil || state != wire.StateConnected {
		return ctx, func() {}, nil, "", 0, errors.New("Signal is not linked and connected")
	}
	bound, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(lifetime, cancel)
	if lifetime.Err() != nil {
		cancel()
	}
	return bound, func() { stop(); cancel() }, client, account, gen, nil
}

func signalConversationTargets(ctx context.Context, client Caller, account string) ([]wire.ConversationTarget, error) {
	var contacts []signalContact
	if err := client.Call(ctx, "listContacts", map[string]any{"account": account, "allRecipients": false}, &contacts); err != nil {
		return nil, errors.New("Could not load Signal contacts")
	}
	targets := make([]wire.ConversationTarget, 0, len(contacts))
	seen := make(map[string]bool)
	for _, contact := range contacts {
		if contact.Unregistered || contact.Number == account {
			continue
		}
		id := first(contact.Number, contact.UUID, contact.Username)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		name := first(contact.Name, strings.TrimSpace(contact.GivenName+" "+contact.FamilyName), contact.Username, contact.Number, contact.UUID)
		targets = append(targets, wire.ConversationTarget{ID: id, Name: name, Detail: contact.Number})
	}
	sort.Slice(targets, func(i, j int) bool { return strings.ToLower(targets[i].Name) < strings.ToLower(targets[j].Name) })
	return targets, nil
}

func (b *Backend) ConversationTargets(ctx context.Context) ([]wire.ConversationTarget, error) {
	ctx, finish, client, account, gen, err := b.creationSession(ctx)
	defer finish()
	if err != nil {
		return nil, err
	}
	targets, err := signalConversationTargets(ctx, client, account)
	b.mu.RLock()
	retired := b.generation != gen || b.account != account || ctx.Err() != nil
	b.mu.RUnlock()
	if retired {
		return nil, context.Canceled
	}
	return targets, err
}

func (b *Backend) CreateConversation(ctx context.Context, p wire.CreateConversationParams) (wire.Conversation, error) {
	p, err := wire.NormalizeCreateConversation(p)
	if err != nil {
		return wire.Conversation{}, err
	}
	ctx, finish, client, account, gen, err := b.creationSession(ctx)
	defer finish()
	if err != nil {
		return wire.Conversation{}, err
	}
	group := len(p.TargetIDs) > 1
	id, name := directPrefix+p.TargetIDs[0], p.TargetIDs[0]
	if !group {
		targets, err := signalConversationTargets(ctx, client, account)
		if err != nil {
			return wire.Conversation{}, err
		}
		for _, target := range targets {
			if target.ID == p.TargetIDs[0] {
				name = target.Name
				break
			}
		}
	} else {
		var result struct {
			GroupID string `json:"groupId"`
		}
		params := map[string]any{"account": account, "name": p.Name, "members": p.TargetIDs}
		if err := client.Call(ctx, "updateGroup", params, &result); err != nil {
			return wire.Conversation{}, errors.New("Could not create Signal group; refresh before retrying")
		}
		if result.GroupID == "" {
			return wire.Conversation{}, errors.New("signal-cli did not return the new group ID; refresh before retrying")
		}
		id, name = groupPrefix+result.GroupID, p.Name
	}
	b.mu.Lock()
	if b.generation != gen || b.account != account || ctx.Err() != nil {
		b.mu.Unlock()
		return wire.Conversation{}, context.Canceled
	}
	conversation, exists := b.convs[id]
	if !exists {
		conversation = newConversation(id, name, group)
		// Explicitly opened chats are not untouched discovery stubs.
		conversation.Timestamp = time.Now().UnixMicro()
		b.convs[id] = conversation
		if !containsString(b.order, id) {
			b.order = append([]string{id}, b.order...)
		}
	}
	err = b.saveLocked()
	b.mu.Unlock()
	if b.publish != nil {
		b.publish(wire.Event{Event: wire.EventConversation, Network: wire.NetworkSignal, Data: conversation})
	}
	if err != nil {
		return conversation, errors.New("Chat opened, but its local cache could not be saved; refresh before retrying")
	}
	return conversation, nil
}
