package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestServiceSelectionMigration(t *testing.T) {
	for _, evidence := range []string{"fresh", "config", "google", "whatsapp", "telegram", "messenger"} {
		t.Run(evidence, func(t *testing.T) {
			p := &Paths{Data: t.TempDir()}
			file := map[string]string{"config": p.ConfigFile(), "google": p.SessionFile(), "whatsapp": p.WhatsAppDBFile(), "telegram": p.TelegramSessionFile(), "messenger": p.MessengerSessionFile()}[evidence]
			if file != "" {
				if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			c := NewConfigStore(p.ConfigFile())
			services, required := c.EnabledServices(p)
			if evidence == "fresh" {
				if len(services) != 0 || !required {
					t.Fatalf("fresh: %v %v", services, required)
				}
				// Changing appearance before choosing services must not silently enable them.
				if err := c.SetUiScale(1.2); err != nil {
					t.Fatal(err)
				}
				services, required = NewConfigStore(p.ConfigFile()).EnabledServices(p)
				if len(services) != 0 || !required {
					t.Fatalf("appearance enabled services: %v %v", services, required)
				}
			} else if len(services) != 3 || required {
				t.Fatalf("migration: %v %v", services, required)
			}
		})
	}
}

func TestServiceSelectionPersistenceAndValidation(t *testing.T) {
	p := &Paths{Data: t.TempDir()}
	c := NewConfigStore(p.ConfigFile())
	if err := c.SetEnabledServices([]string{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["enabledServices"]) != "[]" {
		t.Fatalf("empty lost: %s", data)
	}
	services, required := NewConfigStore(p.ConfigFile()).EnabledServices(p)
	if services == nil || len(services) != 0 || required {
		t.Fatalf("empty reload: %v %v", services, required)
	}
	for _, invalid := range [][]string{nil, {"unknown"}, {"whatsapp", "whatsapp"}} {
		if err := c.SetEnabledServices(invalid); err == nil {
			t.Fatalf("accepted %v", invalid)
		}
	}
	if err := c.SetEnabledServices([]string{"telegram", "gmessages"}); err != nil {
		t.Fatal(err)
	}
	services, _ = c.EnabledServices(p)
	if !reflect.DeepEqual(services, []string{"gmessages", "telegram"}) {
		t.Fatal(services)
	}
	if err := c.SetEnabledServices([]string{"messenger", "whatsapp"}); err != nil {
		t.Fatal(err)
	}
	services, _ = c.EnabledServices(p)
	if !reflect.DeepEqual(services, []string{"whatsapp", "messenger"}) {
		t.Fatal(services)
	}
	// A failed atomic write must not change the running snapshot.
	c.path = filepath.Join(t.TempDir(), "missing", "config.json")
	if err := c.SetEnabledServices([]string{}); err == nil {
		t.Fatal("expected write failure")
	}
	if got, _ := c.EnabledServices(p); !reflect.DeepEqual(got, services) {
		t.Fatalf("failed write changed config: %v", got)
	}
}
