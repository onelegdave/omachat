package whatsapp

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type groupCreator interface {
	CreateGroup(context.Context, whatsmeow.ReqCreateGroup) (*types.GroupInfo, error)
}

func (b *Backend) ConversationTargets(ctx context.Context) ([]wire.ConversationTarget, error) {
	b.mu.RLock()
	device := b.device
	b.mu.RUnlock()
	if device == nil || device.Contacts == nil {
		return nil, errors.New("WhatsApp contacts are unavailable")
	}
	contacts, err := device.Contacts.GetAllContacts(ctx)
	if err != nil {
		return nil, err
	}
	targets := make([]wire.ConversationTarget, 0, len(contacts))
	for jid, info := range contacts {
		if jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer {
			continue
		}
		name := contactDisplayName(info)
		if name == "" {
			continue
		}
		targets = append(targets, wire.ConversationTarget{ID: jid.String(), Name: name, Detail: jid.User})
	}
	sort.Slice(targets, func(i, j int) bool { return strings.ToLower(targets[i].Name) < strings.ToLower(targets[j].Name) })
	return targets, nil
}

func (b *Backend) CreateConversation(ctx context.Context, p wire.CreateConversationParams) (wire.Conversation, error) {
	if len(p.TargetIDs) == 0 {
		return wire.Conversation{}, errors.New("select at least one contact")
	}
	targets, err := b.ConversationTargets(ctx)
	if err != nil {
		return wire.Conversation{}, err
	}
	names := make(map[string]string, len(targets))
	for _, target := range targets {
		names[target.ID] = target.Name
	}
	var jid types.JID
	name := strings.TrimSpace(p.Name)
	group := len(p.TargetIDs) > 1
	if group {
		if name == "" {
			return wire.Conversation{}, errors.New("group name is required")
		}
		members := make([]types.JID, 0, len(p.TargetIDs))
		for _, id := range p.TargetIDs {
			member, parseErr := types.ParseJID(id)
			if parseErr != nil {
				return wire.Conversation{}, parseErr
			}
			members = append(members, member)
		}
		b.mu.RLock()
		creator, ok := b.client.(groupCreator)
		b.mu.RUnlock()
		if !ok {
			return wire.Conversation{}, errors.New("WhatsApp group creation is unavailable")
		}
		info, createErr := creator.CreateGroup(ctx, whatsmeow.ReqCreateGroup{Name: name, Participants: members})
		if createErr != nil {
			return wire.Conversation{}, createErr
		}
		jid = info.JID
	} else {
		jid, err = types.ParseJID(p.TargetIDs[0])
		if err != nil {
			return wire.Conversation{}, err
		}
		name = names[p.TargetIDs[0]]
	}
	conversation := wire.Conversation{ID: jid.String(), Name: name, Timestamp: time.Now().UnixMicro(), IsGroup: group, AvatarColor: avatarColor(jid.String()), Initials: initials(name)}
	b.mu.Lock()
	b.convs[conversation.ID] = conversation
	gen := b.gen
	b.reorderLocked()
	b.saveStoreLocked()
	b.emitLocked(wire.EventConversation, conversation)
	b.mu.Unlock()
	b.queueAvatarFetches(gen, []string{conversation.ID})
	return conversation, nil
}
