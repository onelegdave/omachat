package daemon

import (
	"context"
	"testing"

	"github.com/onelegdave/omachat/internal/wire"
)

func TestWhatsAppHistoryCacheSettingPersistsWithoutRestart(t *testing.T) {
	d := freshSelectionDaemon(t)
	response := d.dispatch(context.Background(), wire.Request{
		Network: wire.NetworkGMessages,
		Method:  wire.MethodSetWhatsAppHistoryCache,
		Params:  map[string]any{"sizeMB": 256},
	})
	if !response.OK {
		t.Fatalf("setting WhatsApp history cache failed: %s", response.Error)
	}
	result, ok := response.Result.(wire.ConfigResult)
	if !ok || result.WhatsAppHistoryCacheMB != 256 || result.RestartRequired {
		t.Fatalf("unexpected setting result: %#v", response.Result)
	}
	if got := d.config.Get().WhatsAppHistoryCacheMB; got != 256 {
		t.Fatalf("persisted size = %d MB, want 256 MB", got)
	}
}
