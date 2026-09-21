package signal

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/onelegdave/omachat/internal/wire"
)

type signalContact struct {
	Number, UUID, Username, Name, GivenName, FamilyName string
	Unregistered                                        bool `json:"unregistered"`
}

func (b *Backend) ConversationTargets(ctx context.Context) ([]wire.ConversationTarget, error) {
	b.mu.RLock()
	account, client := b.account, b.client
	b.mu.RUnlock()
	if account == "" {
		return nil, errors.New("Signal is not linked")
	}
	var contacts []signalContact
	if err := client.Call(ctx, "listContacts", map[string]any{"account": account, "allRecipients": false}, &contacts); err != nil {
		return nil, err
	}
	targets := make([]wire.ConversationTarget, 0, len(contacts))
	for _, contact := range contacts {
		if contact.Unregistered || contact.Number == account {
			continue
		}
		id := first(contact.Number, contact.UUID, contact.Username)
		if id == "" {
			continue
		}
		name := first(contact.Name, strings.TrimSpace(contact.GivenName+" "+contact.FamilyName), contact.Username, contact.Number, contact.UUID)
		targets = append(targets, wire.ConversationTarget{ID: id, Name: name, Detail: contact.Number})
	}
	sort.Slice(targets, func(i, j int) bool { return strings.ToLower(targets[i].Name) < strings.ToLower(targets[j].Name) })
	return targets, nil
}

func (b *Backend) CreateConversation(ctx context.Context, p wire.CreateConversationParams) (wire.Conversation, error) {
	if len(p.TargetIDs) == 0 {
		return wire.Conversation{}, errors.New("select at least one contact")
	}
	b.mu.RLock()
	account, client := b.account, b.client
	b.mu.RUnlock()
	if len(p.TargetIDs) == 1 {
		id := directPrefix + p.TargetIDs[0]
		name := p.TargetIDs[0]
		if targets, err := b.ConversationTargets(ctx); err == nil {
			for _, target := range targets {
				if target.ID == p.TargetIDs[0] {
					name = target.Name
					break
				}
			}
		}
		b.ensureConversation(id, name, false)
		return b.conversation(id)
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return wire.Conversation{}, errors.New("group name is required")
	}
	var result struct {
		GroupID string `json:"groupId"`
	}
	params := map[string]any{"account": account, "name": name, "members": p.TargetIDs}
	if err := client.Call(ctx, "updateGroup", params, &result); err != nil {
		return wire.Conversation{}, err
	}
	if result.GroupID == "" {
		return wire.Conversation{}, errors.New("signal-cli did not return the new group ID")
	}
	id := groupPrefix + result.GroupID
	b.ensureConversation(id, name, true)
	return b.conversation(id)
}

func (b *Backend) conversation(id string) (wire.Conversation, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	conversation, ok := b.convs[id]
	if !ok {
		return wire.Conversation{}, errors.New("conversation was not created")
	}
	return conversation, nil
}
