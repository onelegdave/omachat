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

	log := zerolog.Nop()
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
	for _, method := range []string{wire.MethodGaiaPairing, wire.MethodPairFromBrowser, wire.MethodListProfiles, wire.MethodReact, wire.MethodSetTyping, wire.MethodGifSearch} {
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

	// 4. WhatsApp send routing
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

	for _, badNetwork := range []string{"telegram", "signal", "unknown"} {
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
}
