package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/whatsapp"
	"github.com/onelegdave/omachat/internal/wire"
)

func setupTestDaemonWithMockWhatsApp(t *testing.T) (*Daemon, *whatsapp.MockClient) {
	t.Helper()
	dataDir := t.TempDir()
	cacheDir := t.TempDir()
	runtimeDir := t.TempDir()

	paths := &store.Paths{
		Data:    dataDir,
		Cache:   cacheDir,
		Runtime: runtimeDir,
	}
	_ = os.MkdirAll(paths.MediaDir(), 0o700)
	_ = os.MkdirAll(paths.WhatsAppMediaDir(), 0o700)
	_ = os.MkdirAll(paths.TelegramMediaDir(), 0o700)

	log := zerolog.Nop()
	if err := store.NewConfigStore(paths.ConfigFile()).SetEnabledServices([]string{"gmessages", "whatsapp", "telegram"}); err != nil {
		t.Fatal(err)
	}
	d := New(log, paths)

	mockWA := whatsapp.NewMockClient()
	d.WhatsApp().SetClient(mockWA, true)
	return d, mockWA
}

func TestNetworkRoutingIsolation(t *testing.T) {
	d, mockWA := setupTestDaemonWithMockWhatsApp(t)
	ctx := context.Background()

	// 1. WhatsApp status request
	reqWAStatus := wire.Request{
		ID:      "req-1",
		Network: wire.NetworkWhatsApp,
		Method:  wire.MethodStatus,
	}
	respWA := d.dispatch(ctx, reqWAStatus)
	if !respWA.OK {
		t.Fatalf("dispatch WhatsApp status failed: %s", respWA.Error)
	}
	stWA, ok := respWA.Result.(wire.Status)
	if !ok || stWA.Network != wire.NetworkWhatsApp {
		t.Fatalf("expected WhatsApp status, got: %+v", respWA.Result)
	}

	// 2. Google Messages status request (omitted network defaults to Google Messages)
	reqGMStatus := wire.Request{
		ID:     "req-2",
		Method: wire.MethodStatus,
	}
	respGM := d.dispatch(ctx, reqGMStatus)
	if !respGM.OK {
		t.Fatalf("dispatch Google status failed: %s", respGM.Error)
	}
	stGM, ok := respGM.Result.(wire.Status)
	if !ok || stGM.Network != wire.NetworkGMessages {
		t.Fatalf("expected Google status, got: %+v", respGM.Result)
	}

	// 3. WhatsApp unsupported methods are explicitly rejected
	for _, method := range []string{wire.MethodGaiaPairing, wire.MethodPairFromBrowser, wire.MethodListProfiles, wire.MethodReact} {
		req := wire.Request{
			ID:      "req-unsupported",
			Network: wire.NetworkWhatsApp,
			Method:  method,
		}
		resp := d.dispatch(ctx, req)
		if resp.OK {
			t.Errorf("method %q on WhatsApp must be rejected, got OK", method)
		}
		if resp.Error == "" {
			t.Errorf("method %q on WhatsApp must return descriptive error", method)
		}
	}

	// 4. WhatsApp GIF search uses the shared, key-protected GIPHY service.
	respWAGif := d.dispatch(ctx, wire.Request{
		ID:      "req-wa-gif",
		Network: wire.NetworkWhatsApp,
		Method:  wire.MethodGifSearch,
		Params:  map[string]any{"query": "cat", "limit": float64(24)},
	})
	if !respWAGif.OK {
		t.Fatalf("WhatsApp GIF search failed: %s", respWAGif.Error)
	}
	gifResult, ok := respWAGif.Result.(*wire.GifSearchResult)
	if !ok || !gifResult.NeedsKey {
		t.Fatalf("expected WhatsApp GIF search to request a key, got %+v", respWAGif.Result)
	}

	// 5. WhatsApp send routing
	chatJID, _ := types.ParseJID("15550001111@s.whatsapp.net")
	mockWA.SendMessageFunc = func(ctx context.Context, to types.JID, message *waE2E.Message, extra ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error) {
		return whatsmeow.SendResponse{
			ID:        "server-wa-id-1",
			Timestamp: time.Now(),
		}, nil
	}
	d.WhatsApp().SetClient(mockWA, true)
	_ = mockWA.Connect()

	sendParams := wire.SendParams{
		ConversationID: chatJID.String(),
		Text:           "Isolated WhatsApp text",
		TmpID:          "tmp-wa-1",
	}
	paramsRaw, _ := json.Marshal(sendParams)
	var paramsAny any
	_ = json.Unmarshal(paramsRaw, &paramsAny)

	reqSend := wire.Request{
		ID:      "req-send",
		Network: wire.NetworkWhatsApp,
		Method:  wire.MethodSend,
		Params:  paramsAny,
	}

	respSend := d.dispatch(ctx, reqSend)
	if !respSend.OK {
		t.Fatalf("WhatsApp send failed: %s", respSend.Error)
	}

	// Verify message in WhatsApp conversation list
	reqConvs := wire.Request{
		ID:      "req-convs",
		Network: wire.NetworkWhatsApp,
		Method:  wire.MethodConversations,
	}
	respConvs := d.dispatch(ctx, reqConvs)
	if !respConvs.OK {
		t.Fatalf("WhatsApp convs failed: %s", respConvs.Error)
	}
	convsWA, ok := respConvs.Result.([]wire.Conversation)
	if !ok || len(convsWA) != 1 {
		t.Fatalf("expected 1 WhatsApp conversation, got %+v", respConvs.Result)
	}
	if convsWA[0].Preview != "Isolated WhatsApp text" {
		t.Errorf("expected WhatsApp preview 'Isolated WhatsApp text', got %q", convsWA[0].Preview)
	}

	// Verify Google Messages conversation list was NOT touched
	reqGMConvs := wire.Request{
		ID:     "req-gm-convs",
		Method: wire.MethodConversations,
	}
	respGMConvs := d.dispatch(ctx, reqGMConvs)
	if !respGMConvs.OK {
		t.Fatalf("Google convs failed: %s", respGMConvs.Error)
	}
	convsGM, ok := respGMConvs.Result.([]wire.Conversation)
	if !ok || len(convsGM) != 0 {
		t.Errorf("Google conversations should remain empty, got %+v", convsGM)
	}
}

func TestUnpairIsolation(t *testing.T) {
	d, _ := setupTestDaemonWithMockWhatsApp(t)
	ctx := context.Background()

	// Seed files in Google and WhatsApp stores
	if err := os.WriteFile(d.paths.SessionFile(), []byte("gm-session"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.paths.WhatsAppDBFile(), []byte("wa-db"), 0o600); err != nil {
		t.Fatal(err)
	}
	gmMedia := filepath.Join(d.paths.MediaDir(), "gm-image.jpg")
	_ = os.WriteFile(gmMedia, []byte("gm-media"), 0o600)
	waMedia := filepath.Join(d.paths.WhatsAppMediaDir(), "wa-image.jpg")
	_ = os.WriteFile(waMedia, []byte("wa-media"), 0o600)

	// 1. Unpair WhatsApp
	reqWAUnpair := wire.Request{
		ID:      "wa-unpair",
		Network: wire.NetworkWhatsApp,
		Method:  wire.MethodUnpair,
	}
	respWA := d.dispatch(ctx, reqWAUnpair)
	if !respWA.OK {
		t.Fatalf("WhatsApp unpair failed: %s", respWA.Error)
	}

	// WhatsApp files deleted
	if _, err := os.Stat(d.paths.WhatsAppDBFile()); !os.IsNotExist(err) {
		t.Errorf("expected WhatsApp db file to be removed, got %v", err)
	}
	if _, err := os.Stat(waMedia); !os.IsNotExist(err) {
		t.Errorf("expected WhatsApp media file to be removed, got %v", err)
	}

	// Google files remain completely intact!
	if b, err := os.ReadFile(d.paths.SessionFile()); err != nil || string(b) != "gm-session" {
		t.Errorf("Google session file was corrupted or removed by WhatsApp unpair: %v", err)
	}
	if b, err := os.ReadFile(gmMedia); err != nil || string(b) != "gm-media" {
		t.Errorf("Google media file was corrupted or removed by WhatsApp unpair: %v", err)
	}
}

func TestUnknownNetworkRejection(t *testing.T) {
	d, _ := setupTestDaemonWithMockWhatsApp(t)
	ctx := context.Background()

	for _, badNetwork := range []string{"signal", "discord", "unknown", "slack"} {
		req := wire.Request{
			ID:      "bad-net",
			Network: badNetwork,
			Method:  wire.MethodStatus,
		}
		resp := d.dispatch(ctx, req)
		if resp.OK {
			t.Errorf("expected error for network %q, got OK", badNetwork)
		}
		if !strings.Contains(resp.Error, "unknown network") {
			t.Errorf("expected unknown network error message, got %q", resp.Error)
		}
	}
}

func TestIndependentNetworkStartup(t *testing.T) {
	dataDir := t.TempDir()
	cacheDir := t.TempDir()
	runtimeDir := t.TempDir()

	paths := &store.Paths{
		Data:    dataDir,
		Cache:   cacheDir,
		Runtime: runtimeDir,
	}
	_ = os.MkdirAll(paths.MediaDir(), 0o700)
	_ = os.MkdirAll(paths.WhatsAppMediaDir(), 0o700)
	_ = os.MkdirAll(paths.TelegramMediaDir(), 0o700)

	// Create a corrupted/invalid Google session file to force LoadSession error
	_ = os.WriteFile(paths.SessionFile(), []byte("not valid json at all"), 0o600)

	log := zerolog.Nop()
	d := New(log, paths)

	mockWA := whatsapp.NewMockClient()
	d.WhatsApp().SetClient(mockWA, false)

	ctx := context.Background()
	// Start must NOT crash or return error even though Google session failed
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Daemon Start must not fail when Google session is corrupted: %v", err)
	}

	// WhatsApp must be initialized and accessible
	waStatus := d.WhatsApp().Status()
	if waStatus.State != wire.StateUnpaired {
		t.Errorf("expected WhatsApp state Unpaired, got %s", waStatus.State)
	}

	// Telegram must be initialized and accessible
	tgStatus := d.Telegram().Status()
	if tgStatus.State != wire.StateUnpaired {
		t.Errorf("expected Telegram state Unpaired, got %s", tgStatus.State)
	}
}

func TestTelegramRouting(t *testing.T) {
	d, _ := setupTestDaemonWithMockWhatsApp(t)
	ctx := context.Background()

	// 1. Status request
	reqStatus := wire.Request{
		ID:      "req-tg-status",
		Network: wire.NetworkTelegram,
		Method:  wire.MethodStatus,
	}
	respStatus := d.dispatch(ctx, reqStatus)
	if !respStatus.OK {
		t.Fatalf("dispatch Telegram status failed: %s", respStatus.Error)
	}
	st, ok := respStatus.Result.(wire.Status)
	if !ok || st.Network != wire.NetworkTelegram {
		t.Fatalf("expected Telegram status with network %q, got: %+v", wire.NetworkTelegram, respStatus.Result)
	}
	if st.State != wire.StateUnpaired {
		t.Errorf("expected initial state %q, got %q", wire.StateUnpaired, st.State)
	}

	// 2. Conversations request
	reqConvs := wire.Request{
		ID:      "req-tg-convs",
		Network: wire.NetworkTelegram,
		Method:  wire.MethodConversations,
	}
	respConvs := d.dispatch(ctx, reqConvs)
	if !respConvs.OK {
		t.Fatalf("dispatch Telegram conversations failed: %s", respConvs.Error)
	}
	convs, ok := respConvs.Result.([]wire.Conversation)
	if !ok || len(convs) != 0 {
		t.Errorf("expected empty conversation list, got %+v", respConvs.Result)
	}

	// Seed test conversation and verify
	d.Telegram().AddTestConversation(wire.Conversation{
		ID:      "tg-test-chat",
		Name:    "Test Chat",
		Preview: "Hello Telegram",
	})
	respConvs = d.dispatch(ctx, reqConvs)
	if !respConvs.OK {
		t.Fatalf("dispatch Telegram conversations failed after seed: %s", respConvs.Error)
	}
	convs, ok = respConvs.Result.([]wire.Conversation)
	if !ok || len(convs) != 1 || convs[0].ID != "tg-test-chat" {
		t.Errorf("expected 1 conversation tg-test-chat, got %+v", respConvs.Result)
	}

	// 3. Messages request
	reqMsgs := wire.Request{
		ID:      "req-tg-msgs",
		Network: wire.NetworkTelegram,
		Method:  wire.MethodMessages,
		Params:  wire.MessagesParams{ConversationID: "tg-test-chat"},
	}
	respMsgs := d.dispatch(ctx, reqMsgs)
	if !respMsgs.OK {
		t.Fatalf("dispatch Telegram messages failed: %s", respMsgs.Error)
	}
	msgsRes, ok := respMsgs.Result.(wire.MessagesResult)
	if !ok || msgsRes.ConversationID != "tg-test-chat" {
		t.Errorf("expected MessagesResult for tg-test-chat, got %+v", respMsgs.Result)
	}

	// 4. Operations requiring protocol library return clear unconfigured error
	for _, unconfiguredMethod := range []string{
		wire.MethodSend,
		wire.MethodSendMedia,
		wire.MethodMedia,
		wire.MethodMarkRead,
		wire.MethodStartPairing,
	} {
		req := wire.Request{
			ID:      "req-unconf",
			Network: wire.NetworkTelegram,
			Method:  unconfiguredMethod,
		}
		resp := d.dispatch(ctx, req)
		if resp.OK {
			t.Errorf("method %q should be rejected as unconfigured, got OK", unconfiguredMethod)
		}
		if !strings.Contains(resp.Error, "integration pending") {
			t.Errorf("method %q error should mention integration pending, got %q", unconfiguredMethod, resp.Error)
		}
	}

	// 5. Unsupported network methods explicitly rejected
	for _, unsupportedMethod := range []string{
		wire.MethodGaiaPairing,
		wire.MethodPairFromBrowser,
		wire.MethodListProfiles,
		wire.MethodSetProfile,
		wire.MethodReact,
		wire.MethodGifSearch,
	} {
		req := wire.Request{
			ID:      "req-unsup",
			Network: wire.NetworkTelegram,
			Method:  unsupportedMethod,
		}
		resp := d.dispatch(ctx, req)
		if resp.OK {
			t.Errorf("method %q should be rejected on Telegram, got OK", unsupportedMethod)
		}
		if resp.Error == "" {
			t.Errorf("method %q should return descriptive error", unsupportedMethod)
		}
	}

	// 6. Refresh (no-op)
	reqRefresh := wire.Request{
		ID:      "req-refresh",
		Network: wire.NetworkTelegram,
		Method:  wire.MethodRefresh,
	}
	respRefresh := d.dispatch(ctx, reqRefresh)
	if !respRefresh.OK {
		t.Errorf("refresh on Telegram should succeed as no-op, got error: %s", respRefresh.Error)
	}
}

func TestThreeWayStorageIsolation(t *testing.T) {
	d, _ := setupTestDaemonWithMockWhatsApp(t)
	ctx := context.Background()

	// Seed files across all three networks
	if err := os.WriteFile(d.paths.SessionFile(), []byte("gm-session"), 0o600); err != nil {
		t.Fatal(err)
	}
	gmMedia := filepath.Join(d.paths.MediaDir(), "gm-image.jpg")
	_ = os.WriteFile(gmMedia, []byte("gm-media"), 0o600)

	if err := os.WriteFile(d.paths.WhatsAppDBFile(), []byte("wa-db"), 0o600); err != nil {
		t.Fatal(err)
	}
	waMedia := filepath.Join(d.paths.WhatsAppMediaDir(), "wa-image.jpg")
	_ = os.WriteFile(waMedia, []byte("wa-media"), 0o600)

	if err := os.WriteFile(d.paths.TelegramSessionFile(), []byte("tg-session"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.paths.TelegramStoreFile(), []byte("tg-store"), 0o600); err != nil {
		t.Fatal(err)
	}
	tgMedia := filepath.Join(d.paths.TelegramMediaDir(), "tg-image.jpg")
	_ = os.WriteFile(tgMedia, []byte("tg-media"), 0o600)

	// 1. Unpair Telegram
	reqTGUnpair := wire.Request{
		ID:      "tg-unpair",
		Network: wire.NetworkTelegram,
		Method:  wire.MethodUnpair,
	}
	respTG := d.dispatch(ctx, reqTGUnpair)
	if !respTG.OK {
		t.Fatalf("Telegram unpair failed: %s", respTG.Error)
	}

	// Telegram files deleted
	if _, err := os.Stat(d.paths.TelegramSessionFile()); !os.IsNotExist(err) {
		t.Errorf("expected Telegram session file to be removed, got %v", err)
	}
	if _, err := os.Stat(d.paths.TelegramStoreFile()); !os.IsNotExist(err) {
		t.Errorf("expected Telegram store file to be removed, got %v", err)
	}
	if _, err := os.Stat(tgMedia); !os.IsNotExist(err) {
		t.Errorf("expected Telegram media file to be removed, got %v", err)
	}

	// Google and WhatsApp files remain completely intact!
	if b, err := os.ReadFile(d.paths.SessionFile()); err != nil || string(b) != "gm-session" {
		t.Errorf("Google session file was corrupted or removed by Telegram unpair: %v", err)
	}
	if b, err := os.ReadFile(gmMedia); err != nil || string(b) != "gm-media" {
		t.Errorf("Google media file was corrupted or removed by Telegram unpair: %v", err)
	}
	if b, err := os.ReadFile(d.paths.WhatsAppDBFile()); err != nil || string(b) != "wa-db" {
		t.Errorf("WhatsApp db file was corrupted or removed by Telegram unpair: %v", err)
	}
	if b, err := os.ReadFile(waMedia); err != nil || string(b) != "wa-media" {
		t.Errorf("WhatsApp media file was corrupted or removed by Telegram unpair: %v", err)
	}

	// 2. Reseed Telegram files and Unpair WhatsApp
	_ = os.WriteFile(d.paths.TelegramSessionFile(), []byte("tg-session-2"), 0o600)
	_ = os.WriteFile(tgMedia, []byte("tg-media-2"), 0o600)

	reqWAUnpair := wire.Request{
		ID:      "wa-unpair",
		Network: wire.NetworkWhatsApp,
		Method:  wire.MethodUnpair,
	}
	respWA := d.dispatch(ctx, reqWAUnpair)
	if !respWA.OK {
		t.Fatalf("WhatsApp unpair failed: %s", respWA.Error)
	}

	// WhatsApp files deleted
	if _, err := os.Stat(d.paths.WhatsAppDBFile()); !os.IsNotExist(err) {
		t.Errorf("expected WhatsApp db file to be removed, got %v", err)
	}

	// Google and Telegram remain intact
	if b, err := os.ReadFile(d.paths.SessionFile()); err != nil || string(b) != "gm-session" {
		t.Errorf("Google session was corrupted by WhatsApp unpair: %v", err)
	}
	if b, err := os.ReadFile(d.paths.TelegramSessionFile()); err != nil || string(b) != "tg-session-2" {
		t.Errorf("Telegram session was corrupted by WhatsApp unpair: %v", err)
	}
	if b, err := os.ReadFile(tgMedia); err != nil || string(b) != "tg-media-2" {
		t.Errorf("Telegram media was corrupted by WhatsApp unpair: %v", err)
	}
}
