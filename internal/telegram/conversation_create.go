package telegram

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
)

func (b *Backend) ConversationTargets(ctx context.Context) ([]wire.ConversationTarget, error) {
	ctx, finish, cli, _ := b.operation(ctx)
	defer finish()
	creator, ok := cli.(ConversationClient)
	if !ok {
		return nil, ErrNotConfigured
	}
	return creator.ConversationTargets(ctx)
}

func (b *Backend) CreateConversation(ctx context.Context, p wire.CreateConversationParams) (wire.Conversation, error) {
	ctx, finish, cli, epoch := b.operation(ctx)
	defer finish()
	creator, ok := cli.(ConversationClient)
	if !ok {
		return wire.Conversation{}, ErrNotConfigured
	}
	dialog, err := creator.CreateConversation(ctx, p)
	if err != nil {
		return wire.Conversation{}, err
	}
	if dialog.ID == 0 {
		return wire.Conversation{}, errors.New("Telegram did not return the conversation")
	}
	if dialog.Name == "" {
		targets, _ := creator.ConversationTargets(ctx)
		if len(p.TargetIDs) == 1 {
			for _, target := range targets {
				if target.ID == p.TargetIDs[0] {
					dialog.Name = target.Name
					break
				}
			}
		}
	}
	if dialog.Name == "" {
		dialog.Name = fmt.Sprintf("Telegram chat %d", dialog.ID)
	}
	dialog.Timestamp = time.Now().UnixMicro()
	conversation := mapDialog(dialog)
	b.mu.Lock()
	if b.epoch != epoch {
		b.mu.Unlock()
		return wire.Conversation{}, context.Canceled
	}
	b.convs[conversation.ID] = conversation
	b.order = append([]string{conversation.ID}, b.order...)
	_ = b.saveLocked()
	b.mu.Unlock()
	if b.publish != nil {
		b.publish(wire.Event{Event: wire.EventConversation, Network: wire.NetworkTelegram, Data: conversation})
	}
	return conversation, nil
}
