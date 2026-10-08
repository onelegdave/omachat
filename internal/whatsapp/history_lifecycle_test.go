package whatsapp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

const syntheticHistoryChat = "123456789@g.us"
const syntheticDirectPNChat = "111222333@s.whatsapp.net"
const syntheticDirectLIDChat = "444555666@lid"

func TestSetHistoryCacheMBRejectsUnenforceableBudgetWithoutHistoryStore(t *testing.T) {
	paths := &appStore.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	f, err := os.Create(paths.WhatsAppStoreFile())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(64*1024*1024 + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	backend := New(zerolog.Nop(), paths, nil)
	if err := backend.SetHistoryCacheMB(64); err == nil {
		t.Fatal("accepted a 64 MiB budget while legacy cache artifacts exceed it and SQLite is unavailable")
	}
	backend.mu.RLock()
	got := backend.historyCacheMB
	backend.mu.RUnlock()
	if got != 128 {
		t.Fatalf("failed budget update changed active size to %d MiB, want 128", got)
	}
}

func TestSetHistoryCacheMBRejectsBudgetWhenLegacyIndexAloneExceedsIt(t *testing.T) {
	paths := &appStore.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	index, err := os.Create(paths.WhatsAppStoreFile())
	if err != nil {
		t.Fatal(err)
	}
	if err := index.Truncate(65 * 1024 * 1024); err != nil {
		_ = index.Close()
		t.Fatal(err)
	}
	if err := index.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := openHistoryStore(paths.WhatsAppHistoryFile(), 128)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	backend := New(zerolog.Nop(), paths, nil)
	backend.historyStore = store
	if err := backend.SetHistoryCacheMB(64); err == nil {
		t.Fatal("accepted a 64 MiB budget while the unshrinkable legacy index alone exceeds it")
	}
	if backend.historyCacheMB != 128 {
		t.Fatalf("failed budget update changed active size to %d MiB, want 128", backend.historyCacheMB)
	}
}

func TestRememberHistoryAnchorRejectsOversizedIdentifierBytes(t *testing.T) {
	paths := &appStore.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	backend := New(zerolog.Nop(), paths, nil)
	chatID := strings.Repeat("c", maxHotHistoryAnchorKeyBytes)
	backend.mu.Lock()
	backend.rememberHistoryAnchorLocked(chatID, wire.Message{ID: "message-id", Timestamp: 1})
	backend.mu.Unlock()
	if len(backend.historyAnchors) != 0 {
		t.Fatalf("retained anchor with %d identifier bytes, limit is %d", len(chatID)+len("message-id"), maxHotHistoryAnchorKeyBytes)
	}
}

func TestMessagesRejectsOversizedHistoryIdentifierBytes(t *testing.T) {
	paths := &appStore.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	backend := New(zerolog.Nop(), paths, nil)
	_, err := backend.Messages(context.Background(), wire.MessagesParams{ConversationID: strings.Repeat("c", maxHotHistoryAnchorKeyBytes+1)})
	if err == nil {
		t.Fatal("Messages accepted an oversized conversation identifier")
	}
}

func TestHistoryRequestStatusMapsRemainBounded(t *testing.T) {
	paths := &appStore.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	backend := New(zerolog.Nop(), paths, nil)
	for i := 0; i < maxHistoryRequestStateEntries+5; i++ {
		chatID := fmt.Sprintf("chat-%d@g.us", i)
		pending := &pendingHistoryRequest{chatID: chatID, gen: backend.gen}
		backend.mu.Lock()
		backend.historyPending = pending
		backend.mu.Unlock()
		backend.finishHistoryRequest(pending, "unavailable", "")
		pending = &pendingHistoryRequest{chatID: chatID, gen: backend.gen}
		backend.mu.Lock()
		backend.historyPending = pending
		backend.mu.Unlock()
		backend.finishHistoryRequest(pending, "failed", "")
	}
	if len(backend.historyUnavailable) > maxHistoryRequestStateEntries {
		t.Fatalf("retained %d unavailable states, limit is %d", len(backend.historyUnavailable), maxHistoryRequestStateEntries)
	}
	if len(backend.historyCooldown) > maxHistoryRequestStateEntries {
		t.Fatalf("retained %d cooldown states, limit is %d", len(backend.historyCooldown), maxHistoryRequestStateEntries)
	}
}

func historyBackendWithAnchor(t *testing.T, timeout time.Duration) (*Backend, *MockClient, chan wire.Event) {
	return historyBackendWithAnchorChat(t, timeout, syntheticHistoryChat)
}

func historyBackendWithAnchorChat(t *testing.T, timeout time.Duration, chatID string) (*Backend, *MockClient, chan wire.Event) {
	t.Helper()
	paths := &appStore.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	eventsCh := make(chan wire.Event, 32)
	backend := New(zerolog.Nop(), paths, func(event wire.Event) { eventsCh <- event })
	backend.historyTimeout = timeout
	store, err := openHistoryStore(paths.WhatsAppHistoryFile(), 64)
	if err != nil {
		t.Fatal(err)
	}
	anchor := wire.Message{ID: "anchor-1000", ConversationID: chatID, Timestamp: 1_700_000_000_000_000, FromMe: true}
	if err := store.put(context.Background(), []wire.Message{anchor}); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	mock := NewMockClient()
	backend.SetClient(mock, true)
	if err := backend.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	backend.SetState(wire.StateConnected, "")
	return backend, mock, eventsCh
}

func requestOlderForTest(t *testing.T, backend *Backend) wire.MessagesResult {
	return requestOlderForTestChat(t, backend, syntheticHistoryChat)
}

func requestOlderForTestChat(t *testing.T, backend *Backend, chatID string) wire.MessagesResult {
	t.Helper()
	result, err := backend.Messages(context.Background(), wire.MessagesParams{
		ConversationID: chatID,
		Count:          60,
		CursorID:       "anchor-1000",
		CursorTime:     1_700_000_000_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func waitHistoryUpdate(t *testing.T, eventsCh <-chan wire.Event) wire.HistoryUpdate {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-eventsCh:
			if event.Event != wire.EventHistory {
				continue
			}
			update, ok := event.Data.(wire.HistoryUpdate)
			if !ok {
				t.Fatalf("history event data has type %T", event.Data)
			}
			return update
		case <-timer.C:
			t.Fatal("timed out waiting for WhatsApp history update")
			return wire.HistoryUpdate{}
		}
	}
}

func TestMessagesRequestsOlderHistoryWithTheCachedAnchor(t *testing.T) {
	backend, mock, eventsCh := historyBackendWithAnchor(t, time.Second)
	var captured types.MessageInfo
	var count int
	calls := 0
	mock.RequestHistoryFunc = func(_ context.Context, anchor types.MessageInfo, requested int) error {
		calls++
		captured, count = anchor, requested
		return nil
	}
	first := requestOlderForTest(t, backend)
	second := requestOlderForTest(t, backend)
	if first.HistoryFetchState != "loading" || second.HistoryFetchState != "loading" || calls != 1 {
		t.Fatalf("pending request not coalesced: first=%+v second=%+v calls=%d", first, second, calls)
	}
	if captured.Chat.String() != syntheticHistoryChat || string(captured.ID) != "anchor-1000" || !captured.IsFromMe || captured.Timestamp.Unix() != 1_700_000_000 || count != 50 {
		t.Fatalf("request anchor changed: %+v count=%d", captured, count)
	}
	if update := waitHistoryUpdate(t, eventsCh); update.State != "loading" || update.ConversationID != syntheticHistoryChat {
		t.Fatalf("unexpected request state event: %+v", update)
	}
}

func TestOnDemandHistoryChunkPersistsAndCompletesOnlyTheRequestedChat(t *testing.T) {
	backend, mock, eventsCh := historyBackendWithAnchor(t, time.Second)
	mock.RequestHistoryFunc = func(context.Context, types.MessageInfo, int) error { return nil }
	_ = requestOlderForTest(t, backend)
	_ = waitHistoryUpdate(t, eventsCh)
	wrong := historySyncChunk("not-the-requested-chat@s.whatsapp.net", "wrong-1", 800, 100)
	mock.TriggerEvent(&events.HistorySync{Data: wrong})
	failed := waitHistoryUpdate(t, eventsCh)
	if failed.State != "failed" || failed.ConversationID != syntheticHistoryChat {
		t.Fatalf("wrong-chat response was not rejected: %+v", failed)
	}
	if result, err := backend.Messages(context.Background(), wire.MessagesParams{ConversationID: "not-the-requested-chat@s.whatsapp.net", Count: 10}); err != nil || len(result.Messages) != 0 {
		t.Fatalf("wrong-chat history leaked into cache: result=%+v err=%v", result, err)
	}
}

func TestOnDemandHistoryMatchesPNLIDAliasAndStoresUnderRequestedChat(t *testing.T) {
	backend, mock, eventsCh := historyBackendWithAnchorChat(t, time.Second, syntheticDirectPNChat)
	mock.RequestHistoryFunc = func(context.Context, types.MessageInfo, int) error { return nil }
	if got := requestOlderForTestChat(t, backend, syntheticDirectPNChat); got.HistoryFetchState != "loading" {
		t.Fatalf("initial fetch state=%q", got.HistoryFetchState)
	}
	_ = waitHistoryUpdate(t, eventsCh)

	chunk := historySyncChunk(syntheticDirectLIDChat, "older-direct", 800, 100)
	chunk.Conversations[0].PnJID = proto.String(syntheticDirectPNChat)
	chunk.Conversations[0].LidJID = proto.String(syntheticDirectLIDChat)
	mock.TriggerEvent(&events.HistorySync{Data: chunk})
	if update := waitHistoryUpdate(t, eventsCh); update.State != "complete" {
		t.Fatalf("PN/LID-alias response was not accepted: %+v", update)
	}

	page, err := backend.Messages(context.Background(), wire.MessagesParams{
		ConversationID: syntheticDirectPNChat,
		Count:          10,
		CursorID:       "anchor-1000",
		CursorTime:     1_700_000_000_000_000,
	})
	if err != nil || len(page.Messages) != 1 || page.Messages[0].ID != "older-direct" {
		t.Fatalf("alias response was not stored under requested chat: page=%+v err=%v", page, err)
	}
	if _, ok := backend.convs[syntheticDirectLIDChat]; ok {
		t.Fatal("alias response created a duplicate LID conversation row")
	}
}

func TestOnDemandHistoryResponseMakesOlderPageAvailableOffline(t *testing.T) {
	backend, mock, eventsCh := historyBackendWithAnchor(t, time.Second)
	mock.RequestHistoryFunc = func(context.Context, types.MessageInfo, int) error { return nil }
	_ = requestOlderForTest(t, backend)
	_ = waitHistoryUpdate(t, eventsCh)
	mock.TriggerEvent(&events.HistorySync{Data: historySyncChunk(syntheticHistoryChat, "older-1", 800, 100)})
	if update := waitHistoryUpdate(t, eventsCh); update.State != "complete" {
		t.Fatalf("completed on-demand sync update = %+v", update)
	}
	page, err := backend.Messages(context.Background(), wire.MessagesParams{ConversationID: syntheticHistoryChat, Count: 60, CursorID: "anchor-1000", CursorTime: 1_700_000_000_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != "older-1" || page.Messages[0].Text != "synthetic older page" {
		t.Fatalf("on-demand history not available from local store: %+v", page)
	}
}

func TestEmptyOnDemandHistoryIsUnavailableAndDoesNotLoop(t *testing.T) {
	backend, mock, eventsCh := historyBackendWithAnchor(t, time.Second)
	calls := 0
	mock.RequestHistoryFunc = func(context.Context, types.MessageInfo, int) error { calls++; return nil }
	if got := requestOlderForTest(t, backend); got.HistoryFetchState != "loading" {
		t.Fatalf("initial fetch state=%q", got.HistoryFetchState)
	}
	_ = waitHistoryUpdate(t, eventsCh)
	mock.TriggerEvent(&events.HistorySync{
		Data:         &waHistorySync.HistorySync{SyncType: waHistorySync.HistorySync_ON_DEMAND.Enum()},
		Notification: &waE2E.HistorySyncNotification{Progress: proto.Uint32(100)},
	})
	if update := waitHistoryUpdate(t, eventsCh); update.State != "unavailable" {
		t.Fatalf("empty response state=%+v", update)
	}
	got := requestOlderForTest(t, backend)
	if got.HistoryFetchState != "unavailable" || calls != 1 {
		t.Fatalf("empty response triggered a request loop: state=%q calls=%d", got.HistoryFetchState, calls)
	}
}

func TestFinalNotificationCompletesAfterAnEarlierHistoryChunk(t *testing.T) {
	backend, mock, eventsCh := historyBackendWithAnchor(t, time.Second)
	defer backend.Stop()
	mock.RequestHistoryFunc = func(context.Context, types.MessageInfo, int) error { return nil }
	if got := requestOlderForTest(t, backend); got.HistoryFetchState != "loading" {
		t.Fatalf("initial fetch state=%q", got.HistoryFetchState)
	}
	_ = waitHistoryUpdate(t, eventsCh)
	mock.TriggerEvent(&events.HistorySync{Data: historySyncChunk(syntheticHistoryChat, "older-chunk", 800, 0)})
	mock.TriggerEvent(&events.HistorySync{
		Data: &waHistorySync.HistorySync{
			SyncType: waHistorySync.HistorySync_ON_DEMAND.Enum(),
			Progress: proto.Uint32(100),
		},
	})
	if update := waitHistoryUpdate(t, eventsCh); update.State != "complete" {
		t.Fatalf("completion-only notification discarded prior chunk: %+v", update)
	}
}

func TestPartialHistoryChunkCanBeLoadedFromCacheAfterFailure(t *testing.T) {
	backend, mock, eventsCh := historyBackendWithAnchor(t, time.Hour)
	defer backend.Stop()
	calls := 0
	mock.RequestHistoryFunc = func(context.Context, types.MessageInfo, int) error { calls++; return nil }
	if got := requestOlderForTest(t, backend); got.HistoryFetchState != "loading" {
		t.Fatalf("initial fetch state=%q", got.HistoryFetchState)
	}
	_ = waitHistoryUpdate(t, eventsCh)
	mock.TriggerEvent(&events.HistorySync{Data: historySyncChunk(syntheticHistoryChat, "older-partial", 800, 0)})
	mock.TriggerEvent(&events.Disconnected{})
	if update := waitHistoryUpdate(t, eventsCh); update.State != "failed" {
		t.Fatalf("disconnect update=%+v", update)
	}
	page, err := backend.Messages(context.Background(), wire.MessagesParams{
		ConversationID: syntheticHistoryChat,
		Count:          10,
		CursorID:       "anchor-1000",
		CursorTime:     1_700_000_000_000_000,
	})
	if err != nil || len(page.Messages) != 1 || page.Messages[0].ID != "older-partial" || calls != 1 {
		t.Fatalf("partial chunk was not available from cache on retry: page=%+v calls=%d err=%v", page, calls, err)
	}
}

func TestOnDemandHistoryConversationEndMarkerCompletesWithoutProgress(t *testing.T) {
	backend, mock, eventsCh := historyBackendWithAnchor(t, time.Second)
	mock.RequestHistoryFunc = func(context.Context, types.MessageInfo, int) error { return nil }
	_ = requestOlderForTest(t, backend)
	_ = waitHistoryUpdate(t, eventsCh)
	chunk := historySyncChunk(syntheticHistoryChat, "older-ended", 800, 0)
	chunk.Progress = nil
	chunk.Conversations[0].EndOfHistoryTransferType = waHistorySync.Conversation_COMPLETE_ON_DEMAND_SYNC_BUT_MORE_MSG_REMAIN_ON_PRIMARY.Enum()
	mock.TriggerEvent(&events.HistorySync{Data: chunk})
	if update := waitHistoryUpdate(t, eventsCh); update.State != "complete" {
		t.Fatalf("conversation end marker did not complete request: %+v", update)
	}
	page, err := backend.Messages(context.Background(), wire.MessagesParams{
		ConversationID: syntheticHistoryChat,
		Count:          10,
		CursorID:       "anchor-1000",
		CursorTime:     1_700_000_000_000_000,
	})
	if err != nil || len(page.Messages) != 1 || page.Messages[0].ID != "older-ended" {
		t.Fatalf("conversation end-marker page not persisted: page=%+v err=%v", page, err)
	}
}

func TestUnknownConversationTransferTypeDoesNotCompleteHistorySync(t *testing.T) {
	unknown := waHistorySync.Conversation_EndOfHistoryTransferType(99)
	data := &waHistorySync.HistorySync{SyncType: waHistorySync.HistorySync_ON_DEMAND.Enum()}
	conversation := &waHistorySync.Conversation{EndOfHistoryTransferType: &unknown}
	if onDemandHistorySyncComplete(data, nil, conversation) {
		t.Fatal("unknown transfer type was treated as a completion marker")
	}
}

func TestHistoryRequestTimeoutIsRetryable(t *testing.T) {
	backend, mock, eventsCh := historyBackendWithAnchor(t, 10*time.Millisecond)
	backend.historyRetryCooldown = time.Hour
	calls := 0
	mock.RequestHistoryFunc = func(context.Context, types.MessageInfo, int) error { calls++; return nil }
	_ = requestOlderForTest(t, backend)
	_ = waitHistoryUpdate(t, eventsCh)
	if update := waitHistoryUpdate(t, eventsCh); update.State != "failed" {
		t.Fatalf("timeout state=%+v", update)
	}
	if got := requestOlderForTest(t, backend); got.HistoryFetchState != "failed" || calls != 1 {
		t.Fatalf("timeout retry bypassed its cooldown: result=%+v calls=%d", got, calls)
	}
	backend.mu.Lock()
	backend.historyCooldown[syntheticHistoryChat] = time.Now().Add(-time.Second)
	backend.mu.Unlock()
	if got := requestOlderForTest(t, backend); got.HistoryFetchState != "loading" || calls != 2 {
		t.Fatalf("request did not become retryable after cooldown: state=%q calls=%d", got.HistoryFetchState, calls)
	}
}

func TestHistoryRequestDisconnectIsRetryableAndCancelsTimer(t *testing.T) {
	backend, mock, eventsCh := historyBackendWithAnchor(t, time.Hour)
	mock.RequestHistoryFunc = func(context.Context, types.MessageInfo, int) error { return nil }
	if got := requestOlderForTest(t, backend); got.HistoryFetchState != "loading" {
		t.Fatalf("initial fetch state=%q", got.HistoryFetchState)
	}
	_ = waitHistoryUpdate(t, eventsCh)
	mock.TriggerEvent(&events.Disconnected{})
	if update := waitHistoryUpdate(t, eventsCh); update.State != "failed" || !strings.Contains(update.Notice, "disconnected") {
		t.Fatalf("disconnect update=%+v", update)
	}
	backend.mu.RLock()
	pending := backend.historyPending
	backend.mu.RUnlock()
	if pending != nil {
		t.Fatal("disconnect left the history request pending")
	}
}

func TestOnDemandHistoryCacheWriteFailureIsNotReportedAsPhoneUnavailable(t *testing.T) {
	backend, mock, eventsCh := historyBackendWithAnchor(t, time.Second)
	if err := backend.historyStore.setBudgetBytes(context.Background(), 4*1024*1024); err != nil {
		t.Fatal(err)
	}
	mock.RequestHistoryFunc = func(context.Context, types.MessageInfo, int) error { return nil }
	_ = requestOlderForTest(t, backend)
	_ = waitHistoryUpdate(t, eventsCh)
	message := &waWeb.WebMessageInfo{
		Key:              &waCommon.MessageKey{ID: proto.String("oversized-older")},
		MessageTimestamp: proto.Uint64(800),
		Message:          &waE2E.Message{Conversation: proto.String(strings.Repeat("x", 1024*1024))},
	}
	data := &waHistorySync.HistorySync{
		SyncType: waHistorySync.HistorySync_ON_DEMAND.Enum(),
		Progress: proto.Uint32(100),
		Conversations: []*waHistorySync.Conversation{{
			ID:       proto.String(syntheticHistoryChat),
			Messages: []*waHistorySync.HistorySyncMsg{{Message: message}},
		}},
	}
	mock.TriggerEvent(&events.HistorySync{Data: data})
	update := waitHistoryUpdate(t, eventsCh)
	if update.State != "failed" || !strings.Contains(update.Notice, "cache") {
		t.Fatalf("cache write failure was misreported: %+v", update)
	}
	stored, err := loadChatStore(backend.paths.WhatsAppStoreFile())
	if err != nil {
		t.Fatal(err)
	}
	for _, messages := range stored.Messages {
		for _, cached := range messages {
			if cached.ID == "oversized-older" {
				t.Fatal("oversized message bypassed SQLite budget through legacy JSON fallback")
			}
		}
	}
}

func historySyncChunk(chatID, messageID string, timestamp uint64, progress uint32) *waHistorySync.HistorySync {
	return &waHistorySync.HistorySync{
		SyncType: waHistorySync.HistorySync_ON_DEMAND.Enum(),
		Progress: proto.Uint32(progress),
		Conversations: []*waHistorySync.Conversation{{
			ID: proto.String(chatID),
			Messages: []*waHistorySync.HistorySyncMsg{{Message: &waWeb.WebMessageInfo{
				Key:              &waCommon.MessageKey{ID: proto.String(messageID)},
				MessageTimestamp: proto.Uint64(timestamp),
				Message:          &waE2E.Message{Conversation: proto.String("synthetic older page")},
			}}},
		}},
	}
}

func TestOlderHistoryFetchUsesHotAnchorWhenDiskRowWasEvicted(t *testing.T) {
	backend, mock, eventsCh := historyBackendWithAnchor(t, time.Second)
	defer backend.Stop()
	seedAnchor := wire.Message{
		ID:             "anchor-1000",
		ConversationID: syntheticHistoryChat,
		Timestamp:      1_700_000_000_000_000,
		FromMe:         true,
	}
	visibleMessage := wire.Message{
		ID:             "visible-900",
		ConversationID: syntheticHistoryChat,
		Timestamp:      1_699_999_999_000_000,
	}
	if err := backend.historyStore.put(context.Background(), []wire.Message{visibleMessage}); err != nil {
		t.Fatal(err)
	}
	visiblePage, err := backend.Messages(context.Background(), wire.MessagesParams{
		ConversationID: syntheticHistoryChat,
		Count:          10,
		CursorID:       seedAnchor.ID,
		CursorTime:     seedAnchor.Timestamp,
	})
	if err != nil || len(visiblePage.Messages) != 1 || visiblePage.Messages[0].ID != visibleMessage.ID {
		t.Fatalf("could not load the page that establishes the cursor: page=%+v err=%v", visiblePage, err)
	}

	evicted, err := backend.historyStore.evictOne(context.Background())
	if err != nil || !evicted {
		t.Fatalf("could not simulate history eviction: evicted=%t err=%v", evicted, err)
	}
	page, err := backend.historyStore.page(context.Background(), syntheticHistoryChat, 10, visibleMessage.ID, visibleMessage.Timestamp)
	if err != nil || len(page.Messages) != 0 || !page.Gap {
		t.Fatalf("expected an evicted page gap, got page=%+v err=%v", page, err)
	}

	calls := 0
	var captured types.MessageInfo
	mock.RequestHistoryFunc = func(_ context.Context, got types.MessageInfo, _ int) error {
		calls++
		captured = got
		return nil
	}
	result, err := backend.Messages(context.Background(), wire.MessagesParams{
		ConversationID: syntheticHistoryChat,
		Count:          10,
		CursorID:       visibleMessage.ID,
		CursorTime:     visibleMessage.Timestamp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.HistoryFetchState != "loading" || calls != 1 {
		t.Fatalf("evicted anchor did not trigger phone history: result=%+v calls=%d", result, calls)
	}
	if captured.ID != types.MessageID(visibleMessage.ID) || captured.IsFromMe || captured.Timestamp.UnixMicro() != visibleMessage.Timestamp {
		t.Fatalf("phone request did not preserve the visible-page anchor: %+v", captured)
	}
	if update := waitHistoryUpdate(t, eventsCh); update.State != "loading" {
		t.Fatalf("history request event=%+v", update)
	}
}
