package daemon

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

func (d *Daemon) ConversationTargets(ctx context.Context) ([]wire.ConversationTarget, error) {
	client, err := d.requireClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.ListContacts(ctx)
	if err != nil {
		return nil, err
	}
	targets := make([]wire.ConversationTarget, 0, len(resp.GetContacts()))
	for _, contact := range resp.GetContacts() {
		number := contact.GetNumber().GetNumber()
		if number == "" {
			continue
		}
		name := strings.TrimSpace(contact.GetName())
		if name == "" {
			name = contact.GetNumber().GetFormattedNumber()
		}
		targets = append(targets, wire.ConversationTarget{ID: number, Name: name, Detail: contact.GetNumber().GetFormattedNumber()})
	}
	sort.Slice(targets, func(i, j int) bool { return strings.ToLower(targets[i].Name) < strings.ToLower(targets[j].Name) })
	return targets, nil
}

func (d *Daemon) CreateConversation(ctx context.Context, p wire.CreateConversationParams) (wire.Conversation, error) {
	if len(p.TargetIDs) == 0 {
		return wire.Conversation{}, errors.New("select at least one contact")
	}
	client, err := d.requireClient()
	if err != nil {
		return wire.Conversation{}, err
	}
	numbers := make([]*gmproto.ContactNumber, 0, len(p.TargetIDs))
	for _, id := range p.TargetIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			numbers = append(numbers, &gmproto.ContactNumber{Number: id, Number2: id, MysteriousInt: 7})
		}
	}
	if len(numbers) == 0 {
		return wire.Conversation{}, errors.New("select at least one contact")
	}
	request := &gmproto.GetOrCreateConversationRequest{Numbers: numbers}
	if len(numbers) > 1 {
		name, create := strings.TrimSpace(p.Name), true
		request.RCSGroupName, request.CreateRCSGroup = &name, &create
	}
	resp, err := client.GetOrCreateConversation(ctx, request)
	if err != nil {
		return wire.Conversation{}, err
	}
	if resp.GetConversation() == nil {
		return wire.Conversation{}, errors.New("Google Messages did not return the conversation")
	}
	d.upsertConversation(resp.GetConversation())
	return convertConversation(resp.GetConversation()), nil
}
