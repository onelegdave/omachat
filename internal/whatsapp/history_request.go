package whatsapp

import (
	"context"
	"errors"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type historyRequester interface {
	RequestHistory(context.Context, types.MessageInfo, int) error
}

func requestHistory(ctx context.Context, client Client, anchor types.MessageInfo, count int) error {
	if count <= 0 || count > 50 {
		return fmt.Errorf("invalid WhatsApp history page size %d", count)
	}
	if requester, ok := client.(historyRequester); ok {
		return requester.RequestHistory(ctx, anchor, count)
	}
	live, ok := client.(*whatsmeow.Client)
	if !ok || live == nil {
		return errors.New("WhatsApp client does not support on-demand history requests")
	}
	_, err := live.SendPeerMessage(ctx, live.BuildHistorySyncRequest(&anchor, count))
	if err != nil {
		return fmt.Errorf("request WhatsApp phone history: %w", err)
	}
	return nil
}
