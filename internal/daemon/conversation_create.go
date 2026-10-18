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
	ctx = sessionBoundContext{Context: ctx, session: d.sessionContext()}
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
	normalized, normalizeErr := wire.NormalizeCreateConversation(p)
	if normalizeErr != nil {
		return wire.Conversation{}, normalizeErr
	}
	p = normalized
	ctx = sessionBoundContext{Context: ctx, session: d.sessionContext()}
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
	created, err := getOrCreateGoogleConversation(ctx, numbers, strings.TrimSpace(p.Name), client.GetOrCreateConversation)
	if err != nil {
		return wire.Conversation{}, err
	}
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	if ctx.Err() != nil {
		return wire.Conversation{}, ctx.Err()
	}
	current, currentErr := d.requireClient()
	if currentErr != nil || current != client {
		return wire.Conversation{}, errNotConnected
	}
	return d.commitCreatedConversation(created), nil
}

type getOrCreateFunc func(context.Context, *gmproto.GetOrCreateConversationRequest) (*gmproto.GetOrCreateConversationResponse, error)

// getOrCreateGoogleConversation first asks for the recipients' existing or
// new SMS/MMS conversation. When the phone answers that they need a new RCS
// group instead, it asks again to create one with the chosen name, as the
// upstream bridge does. Only a returned conversation counts as success.
func getOrCreateGoogleConversation(ctx context.Context, numbers []*gmproto.ContactNumber, name string, call getOrCreateFunc) (*gmproto.Conversation, error) {
	request := &gmproto.GetOrCreateConversationRequest{Numbers: numbers}
	resp, err := call(ctx, request)
	if err != nil {
		return nil, err
	}
	if resp.GetStatus() == gmproto.GetOrCreateConversationResponse_CREATE_RCS {
		if len(numbers) < 2 || name == "" {
			return nil, errors.New("Google Messages needs a group name to create this RCS group")
		}
		create := true
		request.RCSGroupName, request.CreateRCSGroup = &name, &create
		if resp, err = call(ctx, request); err != nil {
			return nil, err
		}
	}
	if resp.GetConversation() == nil || resp.GetConversation().GetConversationID() == "" {
		return nil, errors.New("Google Messages did not return the conversation")
	}
	return resp.GetConversation(), nil
}

func (d *Daemon) commitCreatedConversation(c *gmproto.Conversation) wire.Conversation {
	w := convertConversation(c)
	d.mu.Lock()
	if existing, ok := d.convs[w.ID]; ok {
		w = existing
	} else {
		if p, ok := d.avatars.cached(w.ID); ok {
			w.AvatarPath = p
		}
		d.convs[w.ID] = w
		d.reorderLocked()
	}
	d.mu.Unlock()
	d.publish(wire.EventConversation, w)
	d.publishStatus()
	return w
}
