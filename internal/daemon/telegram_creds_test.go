package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

func TestDaemonSetTelegramCredentials(t *testing.T) {
	t.Setenv("OMACHAT_TELEGRAM_API_ID", "")
	t.Setenv("OMACHAT_TELEGRAM_API_HASH", "")

	paths := &store.Paths{
		Data:    t.TempDir(),
		Cache:   t.TempDir(),
		Runtime: t.TempDir(),
	}

	d := New(zerolog.Nop(), paths)
	ctx := context.Background()

	// Initial state: not configured
	cfg := d.PluginConfig()
	if cfg.TelegramConfigured {
		t.Errorf("expected TelegramConfigured false initially, got true")
	}
	if cfg.TelegramAPIID != 0 {
		t.Errorf("expected TelegramAPIID 0 initially, got %d", cfg.TelegramAPIID)
	}

	// 1. Set valid credentials via RPC dispatch
	const validID = 123456
	const validHash = "0123456789abcdef0123456789abcdef"
	resp := d.dispatch(ctx, wire.Request{
		ID:      "req-1",
		Network: wire.NetworkTelegram,
		Method:  wire.MethodSetTelegramCredentials,
		Params:  map[string]any{"apiId": validID, "apiHash": validHash},
	})
	if !resp.OK {
		t.Fatalf("dispatch setTelegramCredentials failed: %s", resp.Error)
	}

	cfgRes, ok := resp.Result.(wire.ConfigResult)
	if !ok {
		t.Fatalf("expected ConfigResult in response, got %T", resp.Result)
	}
	if !cfgRes.TelegramConfigured {
		t.Errorf("expected TelegramConfigured true, got false")
	}
	if cfgRes.TelegramAPIID != validID {
		t.Errorf("expected TelegramAPIID %d, got %d", validID, cfgRes.TelegramAPIID)
	}

	// Verify persistence in config store
	saved := d.config.Get()
	if saved.TelegramAPIID != validID || saved.TelegramAPIHash != validHash {
		t.Errorf("saved credentials mismatch: %+v", saved)
	}

	// 2. Reject invalid credentials without leaking secret in error
	const invalidSecret = "secret-bad-hash-xyz-9999"
	respInvalid := d.dispatch(ctx, wire.Request{
		ID:      "req-2",
		Network: wire.NetworkTelegram,
		Method:  wire.MethodSetTelegramCredentials,
		Params:  map[string]any{"apiId": validID, "apiHash": invalidSecret},
	})
	if respInvalid.OK {
		t.Fatalf("expected error for invalid apiHash, got OK")
	}
	if strings.Contains(respInvalid.Error, invalidSecret) {
		t.Errorf("SECURITY LEAK: error message leaked secret hash: %s", respInvalid.Error)
	}

	// Verify config was preserved after invalid attempt
	savedAfterInvalid := d.config.Get()
	if savedAfterInvalid.TelegramAPIHash != validHash {
		t.Errorf("config was corrupted by invalid request: %+v", savedAfterInvalid)
	}

	// 3. Partial update: change only apiID while keeping existing hash
	const newID = 654321
	respPartialID := d.dispatch(ctx, wire.Request{
		ID:      "req-3",
		Network: wire.NetworkTelegram,
		Method:  wire.MethodSetTelegramCredentials,
		Params:  map[string]any{"apiId": newID, "apiHash": ""},
	})
	if !respPartialID.OK {
		t.Fatalf("partial ID update failed: %s", respPartialID.Error)
	}
	if savedPartial := d.config.Get(); savedPartial.TelegramAPIID != newID || savedPartial.TelegramAPIHash != validHash {
		t.Errorf("partial ID update mismatch: %+v", savedPartial)
	}

	// 4. Partial update: change only apiHash while keeping existing ID
	const newHash = "fedcba9876543210fedcba9876543210"
	respPartialHash := d.dispatch(ctx, wire.Request{
		ID:      "req-4",
		Network: wire.NetworkTelegram,
		Method:  wire.MethodSetTelegramCredentials,
		Params:  map[string]any{"apiId": 0, "apiHash": newHash},
	})
	if !respPartialHash.OK {
		t.Fatalf("partial Hash update failed: %s", respPartialHash.Error)
	}
	if savedPartial := d.config.Get(); savedPartial.TelegramAPIID != newID || savedPartial.TelegramAPIHash != newHash {
		t.Errorf("partial Hash update mismatch: %+v", savedPartial)
	}

	// 5. Deletion / Removal of credentials
	respDelete := d.dispatch(ctx, wire.Request{
		ID:      "req-5",
		Network: wire.NetworkTelegram,
		Method:  wire.MethodSetTelegramCredentials,
		Params:  map[string]any{"apiId": 0, "apiHash": ""},
	})
	if !respDelete.OK {
		t.Fatalf("deletion failed: %s", respDelete.Error)
	}
	cfgDeleted, ok := respDelete.Result.(wire.ConfigResult)
	if !ok || cfgDeleted.TelegramConfigured {
		t.Errorf("expected TelegramConfigured false after deletion, got: %+v", respDelete.Result)
	}
	if savedDeleted := d.config.Get(); savedDeleted.TelegramAPIID != 0 || savedDeleted.TelegramAPIHash != "" {
		t.Errorf("credentials not cleared from config: %+v", savedDeleted)
	}
}
