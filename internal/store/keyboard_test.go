package store

import (
	"reflect"
	"strings"
	"testing"
)

func TestKeyboardShortcutsPersistAndReturnAnIsolatedCopy(t *testing.T) {
	path := (&Paths{Data: t.TempDir()}).ConfigFile()
	config := NewConfigStore(path)
	want := map[string]string{"search": "Ctrl+f", "compose": "Ctrl+Shift+m"}
	if err := config.SetKeyboardShortcuts(want); err != nil {
		t.Fatal(err)
	}
	want["search"] = "broken"
	got := config.Get().KeyboardShortcuts
	if !reflect.DeepEqual(got, map[string]string{"search": "Ctrl+f", "compose": "Ctrl+Shift+m"}) {
		t.Fatalf("unexpected in-memory shortcuts: %#v", got)
	}
	got["search"] = "changed"
	reloaded := NewConfigStore(path).Get().KeyboardShortcuts
	if !reflect.DeepEqual(reloaded, map[string]string{"search": "Ctrl+f", "compose": "Ctrl+Shift+m"}) {
		t.Fatalf("shortcuts were not persisted independently: %#v", reloaded)
	}
}

func TestKeyboardShortcutsRejectInvalidAndConflictingAssignments(t *testing.T) {
	config := NewConfigStore((&Paths{Data: t.TempDir()}).ConfigFile())
	for name, shortcuts := range map[string]map[string]string{
		"unknown action":            {"notAnAction": "Ctrl+x"},
		"reserved navigation":       {"search": "Ctrl+Tab"},
		"duplicate assignments":     {"search": "Ctrl+x", "compose": "Ctrl+x"},
		"default binding collision": {"search": "i"},
		"malformed chord":           {"search": "Ctrl+Ctrl+x"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := config.SetKeyboardShortcuts(shortcuts); err == nil {
				t.Fatal("invalid shortcuts were accepted")
			}
		})
	}
	if len(config.Get().KeyboardShortcuts) != 0 {
		t.Fatalf("rejected values changed stored shortcuts: %#v", config.Get().KeyboardShortcuts)
	}
}

func TestEmptyKeyboardShortcutsRestoreDefaults(t *testing.T) {
	config := NewConfigStore((&Paths{Data: t.TempDir()}).ConfigFile())
	if err := config.SetKeyboardShortcuts(map[string]string{"search": "Ctrl+f"}); err != nil {
		t.Fatal(err)
	}
	if err := config.SetKeyboardShortcuts(map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if got := config.Get().KeyboardShortcuts; len(got) != 0 {
		t.Fatalf("empty assignment did not restore defaults: %#v", got)
	}
}

func TestUnifiedInboxPreferencePersists(t *testing.T) {
	config := NewConfigStore((&Paths{Data: t.TempDir()}).ConfigFile())
	if config.Get().UnifiedInboxEnabled {
		t.Fatal("unified inbox must default to off")
	}
	if err := config.SetUnifiedInboxEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !NewConfigStore(config.path).Get().UnifiedInboxEnabled {
		t.Fatal("unified inbox preference was not persisted")
	}
	if err := config.SetUnifiedInboxEnabled(false); err != nil {
		t.Fatal(err)
	}
	if NewConfigStore(config.path).Get().UnifiedInboxEnabled {
		t.Fatal("disabled preference was not persisted")
	}
}

func TestDefaultServiceShortcutsKeepNumberKeysOnly(t *testing.T) {
	for id, shortcuts := range defaultKeyboardShortcuts {
		if !strings.HasPrefix(id, "service.") {
			continue
		}
		for _, shortcut := range shortcuts {
			if strings.HasPrefix(shortcut, "Ctrl+") {
				t.Errorf("%s has fixed provider shortcut %q", id, shortcut)
			}
		}
	}
}
