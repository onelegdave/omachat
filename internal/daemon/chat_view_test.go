package daemon

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

func TestChatViewPersistence(t *testing.T) {
	d := freshSelectionDaemon(t)
	update := func(network string, params map[string]any) {
		t.Helper()
		resp := d.dispatch(context.Background(), wire.Request{Method: wire.MethodSetChatView, Network: network, Params: params})
		if !resp.OK {
			t.Fatal(resp.Error)
		}
	}
	update("telegram", map[string]any{"lastService": "telegram", "sidebarCollapsed": true, "conversationID": "demo-tg"})
	update("whatsapp", map[string]any{"conversationID": "demo-wa"})
	if err := d.config.SetUiScale(1.2); err != nil {
		t.Fatal(err)
	}
	loaded := store.NewConfigStore(d.paths.ConfigFile()).Get()
	want := map[string]string{"telegram": "demo-tg", "whatsapp": "demo-wa"}
	if loaded.LastService != "telegram" || !loaded.SidebarCollapsed || loaded.UiScale != 1.2 || !reflect.DeepEqual(loaded.LastConversations, want) {
		t.Fatalf("preferences did not survive reload: %+v", loaded)
	}
	info, err := os.Stat(d.paths.ConfigFile())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("config must remain private: %v", err)
	}
	// A consumer must not mutate the store's remembered selections through a snapshot.
	d.PluginConfig().LastConversations["telegram"] = "changed"
	if d.PluginConfig().LastConversations["telegram"] != "demo-tg" {
		t.Fatal("config snapshot aliases stored selections")
	}
	update("telegram", map[string]any{"sidebarCollapsed": false, "conversationID": ""})
	loaded = store.NewConfigStore(d.paths.ConfigFile()).Get()
	if loaded.SidebarCollapsed || len(loaded.LastConversations) != 1 || loaded.LastConversations["whatsapp"] != "demo-wa" {
		t.Fatalf("clearing one service changed another: %+v", loaded)
	}
}

func TestChatViewRejectsInvalidPreferences(t *testing.T) {
	d := freshSelectionDaemon(t)
	for _, params := range []map[string]any{
		{"lastService": "unknown"},
		{"sidebarCollapsed": "yes"},
		{"conversationID": 42},
	} {
		resp := d.dispatch(context.Background(), wire.Request{Method: wire.MethodSetChatView, Params: params})
		if resp.OK {
			t.Fatalf("accepted invalid preferences: %v", params)
		}
	}
	if _, err := os.Stat(d.paths.ConfigFile()); !os.IsNotExist(err) {
		t.Fatal("invalid preferences wrote a config file")
	}
}
