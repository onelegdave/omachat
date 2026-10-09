package whatsapp

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/onelegdave/omachat/internal/wire"
)

func TestOnDemandHistoryUsesStoredPNLIDMappingWhenHistoryAliasesAreAbsent(t *testing.T) {
	backend, mock, eventsCh := historyBackendWithAnchorChat(t, time.Second, syntheticDirectPNChat)
	backend.device = syntheticHistoryDevice()
	mock.RequestHistoryFunc = func(context.Context, types.MessageInfo, int) error { return nil }
	if got := requestOlderForTestChat(t, backend, syntheticDirectPNChat); got.HistoryFetchState != "loading" {
		t.Fatalf("initial fetch state=%q", got.HistoryFetchState)
	}
	_ = waitHistoryUpdate(t, eventsCh)

	mock.TriggerEvent(&events.HistorySync{Data: historySyncChunk(syntheticDirectLIDChat, "older-mapped", 800, 100)})
	if update := waitHistoryUpdate(t, eventsCh); update.State != "complete" {
		t.Fatalf("stored PN/LID mapping did not match response: %+v", update)
	}
	page, err := backend.Messages(context.Background(), wire.MessagesParams{
		ConversationID: syntheticDirectPNChat,
		Count:          10,
		CursorID:       "anchor-1000",
		CursorTime:     1_700_000_000_000_000,
	})
	if err != nil || len(page.Messages) != 1 || page.Messages[0].ID != "older-mapped" {
		t.Fatalf("mapped response was not stored under requested chat: page=%+v err=%v", page, err)
	}
}

func TestOnDemandHistoryRejectsUnrelatedDirectChatDespiteMatchingName(t *testing.T) {
	backend, mock, eventsCh := historyBackendWithAnchorChat(t, time.Second, syntheticDirectPNChat)
	mock.RequestHistoryFunc = func(context.Context, types.MessageInfo, int) error { return nil }
	if got := requestOlderForTestChat(t, backend, syntheticDirectPNChat); got.HistoryFetchState != "loading" {
		t.Fatalf("initial fetch state=%q", got.HistoryFetchState)
	}
	_ = waitHistoryUpdate(t, eventsCh)

	wrong := historySyncChunk("999888777@s.whatsapp.net", "unrelated-message", 800, 100)
	wrong.Conversations[0].Name = proto.String(syntheticWhatsAppPushName)
	mock.TriggerEvent(&events.HistorySync{Data: wrong})
	if update := waitHistoryUpdate(t, eventsCh); update.State != "failed" {
		t.Fatalf("unrelated direct chat was incorrectly accepted: %+v", update)
	}
	if _, ok := backend.convs["999888777@s.whatsapp.net"]; ok {
		t.Fatal("unrelated direct chat was persisted")
	}
}
