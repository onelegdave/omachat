package daemon

import (
	"context"
	"reflect"
	"testing"

	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

func TestKeyboardShortcutRPCWorksWithAllServicesDisabled(t *testing.T) {
	d := freshSelectionDaemon(t)
	if err := d.config.SetEnabledServices([]string{}); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"search": "Ctrl+Alt+f"}
	response := d.dispatch(context.Background(), wire.Request{Network: wire.NetworkTelegram, Method: wire.MethodSetKeyboardShortcuts, Params: wire.SetKeyboardShortcutsParams{Shortcuts: want}})
	if !response.OK {
		t.Fatal(response.Error)
	}
	if got := response.Result.(wire.ConfigResult).KeyboardShortcuts; !reflect.DeepEqual(got, want) {
		t.Fatalf("response shortcuts: %#v", got)
	}
	if got := store.NewConfigStore(d.paths.ConfigFile()).Get().KeyboardShortcuts; !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted shortcuts: %#v", got)
	}
	invalid := d.dispatch(context.Background(), wire.Request{Method: wire.MethodSetKeyboardShortcuts, Params: wire.SetKeyboardShortcutsParams{Shortcuts: map[string]string{"search": "i"}}})
	if invalid.OK {
		t.Fatal("conflicting shortcut accepted")
	}
	if got := d.PluginConfig().KeyboardShortcuts; !reflect.DeepEqual(got, want) {
		t.Fatalf("invalid request changed shortcuts: %#v", got)
	}
}
