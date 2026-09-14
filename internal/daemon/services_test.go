package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
	"github.com/rs/zerolog"
)

func freshSelectionDaemon(t *testing.T) *Daemon {
	t.Helper()
	return New(zerolog.Nop(), &store.Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()})
}

func TestDisabledStartupAndRouting(t *testing.T) {
	d := freshSelectionDaemon(t)
	if err := d.config.SetEnabledServices([]string{}); err != nil {
		t.Fatal(err)
	}
	// Deliberately invalid retained credentials prove disabled startup never restores them.
	for _, file := range []string{d.paths.SessionFile(), d.paths.TelegramSessionFile()} {
		if err := os.WriteFile(file, []byte("retained account"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer d.Stop()
	if d.client != nil || d.tg.Client() != nil {
		t.Fatal("disabled clients initialized")
	}
	if _, err := os.Stat(d.paths.WhatsAppDBFile()); !os.IsNotExist(err) {
		t.Fatal("disabled WhatsApp initialized its store")
	}
	for _, service := range []string{"gmessages", "whatsapp", "telegram"} {
		resp := d.dispatch(context.Background(), wire.Request{Network: service, Method: wire.MethodStatus})
		if !resp.OK {
			t.Fatal(resp.Error)
		}
		st := resp.Result.(wire.Status)
		if st.State != wire.StateDisabled || st.Unread != 0 {
			t.Fatalf("%s: %+v", service, st)
		}
		for _, method := range []string{wire.MethodSend, wire.MethodRefresh, wire.MethodStartPairing, wire.MethodMedia, wire.MethodUnpair} {
			resp := d.dispatch(context.Background(), wire.Request{Network: service, Method: method})
			if resp.OK || !strings.Contains(resp.Error, "service disabled") {
				t.Fatalf("%s %s: %+v", service, method, resp)
			}
		}
		if !d.dispatch(context.Background(), wire.Request{Network: service, Method: wire.MethodConfig}).OK {
			t.Fatal("config unavailable")
		}
	}
	resp := d.dispatch(context.Background(), wire.Request{Method: wire.MethodSetUiScale, Params: map[string]any{"scale": 1.2}})
	if !resp.OK {
		t.Fatal(resp.Error)
	}
	for _, file := range []string{d.paths.SessionFile(), d.paths.TelegramSessionFile()} {
		data, err := os.ReadFile(file)
		if err != nil || string(data) != "retained account" {
			t.Fatalf("credential changed: %s %v", file, err)
		}
	}
}

func TestServiceSetterValidationAndBoundary(t *testing.T) {
	d := freshSelectionDaemon(t)
	for _, params := range []any{nil, map[string]any{}, map[string]any{"enabledServices": nil}, map[string]any{"enabledServices": "telegram"}, map[string]any{"enabledServices": []string{"bad"}}, map[string]any{"enabledServices": []string{"telegram", "telegram"}}, map[string]any{"enabledServices": []string{}, "surprise": true}} {
		resp := d.dispatch(context.Background(), wire.Request{Method: wire.MethodSetEnabledServices, Params: params})
		if resp.OK {
			t.Fatalf("accepted %#v", params)
		}
	}
	empty := d.dispatch(context.Background(), wire.Request{Method: wire.MethodSetEnabledServices, Params: wire.SetEnabledServicesParams{EnabledServices: []string{}}})
	if !empty.OK || empty.Result.(wire.ConfigResult).RestartRequired || empty.Result.(wire.ConfigResult).ServiceSelectionRequired {
		t.Fatalf("empty selection: %+v", empty)
	}
	d.acknowledgeServiceChange(empty)
	select {
	case <-d.RestartRequested():
		t.Fatal("unchanged restarted")
	default:
	}
	changed := d.dispatch(context.Background(), wire.Request{Method: wire.MethodSetEnabledServices, Params: wire.SetEnabledServicesParams{EnabledServices: []string{"telegram"}}})
	if !changed.OK || !changed.Result.(wire.ConfigResult).RestartRequired {
		t.Fatalf("change: %+v", changed)
	}
	if !d.PluginConfig().RestartRequired || d.serviceEnabled("telegram") {
		t.Fatal("old helper claimed new active state")
	}
	select {
	case <-d.RestartRequested():
		t.Fatal("restart before acknowledgement")
	default:
	}
	resp := d.dispatch(context.Background(), wire.Request{Network: "telegram", Method: wire.MethodSend})
	if resp.OK || !strings.Contains(resp.Error, "not started") {
		t.Fatalf("pending send: %+v", resp)
	}
	d.acknowledgeServiceChange(changed)
	select {
	case <-d.RestartRequested():
	default:
		t.Fatal("acknowledgement did not request restart")
	}
	newDaemon := New(zerolog.Nop(), d.paths)
	cfg := newDaemon.PluginConfig()
	if cfg.RestartRequired || !reflect.DeepEqual(cfg.EnabledServices, []string{"telegram"}) {
		t.Fatalf("reload: %+v", cfg)
	}
}

func TestServiceRestartOnlyAfterSocketWrite(t *testing.T) {
	d := freshSelectionDaemon(t)
	server, client := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.handleConn(ctx, server)
	reader := bufio.NewReader(client)
	for i := 0; i < 3; i++ {
		if _, err := reader.ReadBytes('\n'); err != nil {
			t.Fatal(err)
		}
	}
	req := wire.Request{ID: "choose", Method: wire.MethodSetEnabledServices, Params: wire.SetEnabledServicesParams{EnabledServices: []string{"telegram"}}}
	if err := json.NewEncoder(client).Encode(req); err != nil {
		t.Fatal(err)
	}
	// net.Pipe blocks the response writer until the reader accepts the bytes.
	select {
	case <-d.RestartRequested():
		t.Fatal("exited before response write")
	case <-time.After(30 * time.Millisecond):
	}
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var resp wire.Response
	if err := json.Unmarshal(line, &resp); err != nil || !resp.OK || resp.ID != "choose" {
		t.Fatalf("response: %s %v", line, err)
	}
	select {
	case <-d.RestartRequested():
	case <-time.After(time.Second):
		t.Fatal("restart not signaled")
	}
}

func TestDisableLegacyServicesRetainsCredentialsUntilRestart(t *testing.T) {
	paths := freshSelectionDaemon(t).paths
	if err := os.WriteFile(paths.SessionFile(), []byte("saved credentials"), 0600); err != nil {
		t.Fatal(err)
	}
	d := New(zerolog.Nop(), paths)
	if len(d.activeServices) != 3 {
		t.Fatal("legacy services not enabled")
	}
	resp := d.dispatch(context.Background(), wire.Request{Method: wire.MethodSetEnabledServices, Params: wire.SetEnabledServicesParams{EnabledServices: []string{}}})
	if !resp.OK || !resp.Result.(wire.ConfigResult).RestartRequired {
		t.Fatalf("disable: %+v", resp)
	}
	if d.Status().State == wire.StateDisabled {
		t.Fatal("old process claimed disabled before exit")
	}
	data, err := os.ReadFile(paths.SessionFile())
	if err != nil || string(data) != "saved credentials" {
		t.Fatal("disable modified credentials")
	}
	reloaded := New(zerolog.Nop(), paths)
	if err := reloaded.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer reloaded.Stop()
	if reloaded.Status().State != wire.StateDisabled || reloaded.PluginConfig().RestartRequired {
		t.Fatal("restart did not activate empty selection")
	}
}
