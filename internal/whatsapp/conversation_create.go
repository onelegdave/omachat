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
	normalized, normalizeErr := wire.NormalizeCreateConversation(p)
	if normalizeErr != nil {
		return wire.Conversation{}, normalizeErr
	}
	p = normalized
	b.mu.RLock()
	gen, cli, paired := b.gen, b.client, b.paired
	b.mu.RUnlock()
	if cli == nil || !paired {
		return wire.Conversation{}, errors.New("WhatsApp is not linked")
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
			if parseErr != nil || member.User == "" || (member.Server != types.DefaultUserServer && member.Server != types.HiddenUserServer) {
				return wire.Conversation{}, errors.New("invalid WhatsApp contact")
			}
			members = append(members, member)
		}
		creator, ok := cli.(groupCreator)
		if !ok {
			return wire.Conversation{}, errors.New("WhatsApp group creation is unavailable")
		}
		info, createErr := creator.CreateGroup(ctx, whatsmeow.ReqCreateGroup{Name: name, Participants: members})
		if createErr != nil {
			return wire.Conversation{}, createErr
		}
		if info == nil || info.JID.User == "" || info.JID.Server != types.GroupServer {
			return wire.Conversation{}, errors.New("WhatsApp did not return the new group")
		}
		jid = info.JID
	} else {
		jid, err = types.ParseJID(p.TargetIDs[0])
		if err != nil || jid.User == "" || (jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer) {
			return wire.Conversation{}, errors.New("invalid WhatsApp contact")
		}
		name = names[p.TargetIDs[0]]
		if name == "" {
			name = jid.User
		}
	}
	conversation := wire.Conversation{ID: jid.String(), Name: name, Timestamp: time.Now().UnixMicro(), IsGroup: group, AvatarColor: avatarColor(jid.String()), Initials: initials(name)}
	b.mu.Lock()
	if b.gen != gen || b.client != cli || ctx.Err() != nil {
		b.mu.Unlock()
		return wire.Conversation{}, context.Canceled
	}
	if existing, ok := b.convs[conversation.ID]; ok {
		conversation = existing
	} else {
		b.convs[conversation.ID] = conversation
		b.reorderLocked()
	}
	b.saveStoreLocked()
	b.emitLocked(wire.EventConversation, conversation)
	b.mu.Unlock()
	return conversation, nil
}
