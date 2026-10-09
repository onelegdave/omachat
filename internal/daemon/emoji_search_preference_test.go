package daemon

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/onelegdave/omachat/internal/wire"
)

func TestEmojiSearchPreferenceRPCPersistsWhenAllServicesDisabled(t *testing.T) {
	d := freshSelectionDaemon(t)
	if err := d.config.SetEnabledServices([]string{}); err != nil {
		t.Fatal(err)
	}
	if d.PluginConfig().KeepPreviousEmojiSearchText {
		t.Fatal("emoji search preference should default to false")
	}

	response := d.dispatch(context.Background(), wire.Request{
		Method: "setEmojiSearchPreference",
		Params: map[string]any{"keepPreviousEmojiSearchText": true},
	})
	if !response.OK {
		t.Fatalf("setting emoji search preference failed: %s", response.Error)
	}
	encoded, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if result["keepPreviousEmojiSearchText"] != true {
		t.Fatalf("unexpected config response: %s", encoded)
	}

	data, err := os.ReadFile(d.paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted["keepPreviousEmojiSearchText"] != true {
		t.Fatalf("preference was not persisted: %s", data)
	}
}
