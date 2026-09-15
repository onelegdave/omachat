package store

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPythonLockExcludesGoConfigWriter(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for the cross-language lock regression")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	store := NewConfigStore(path)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-u", "-c", `
import fcntl, os, sys
fd = os.open(sys.argv[1] + ".lock", os.O_CREAT | os.O_RDWR, 0o600)
fcntl.flock(fd, fcntl.LOCK_EX)
print("locked", flush=True)
sys.stdin.readline()
`, path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stdin.Close(); cancel(); cmd.Wait() })
	reader := bufio.NewScanner(stdout)
	if !reader.Scan() || reader.Text() != "locked" {
		t.Fatal("Python lock holder did not start")
	}
	if err := store.SetUiScale(1.2); err == nil {
		t.Fatal("Go writer ignored Python's config lock")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("contended writer changed configuration")
	}
	if store.Get().UiScale != 0 {
		t.Fatal("failed write changed in-memory settings")
	}
}

// Exercise the real Python writer followed by a setter on a Go store loaded
// before that write. Neither process uses the user's actual configuration.
func TestTelegramScriptThenStaleGoWriter(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for the cross-language setup regression")
	}
	home := t.TempDir()
	path := filepath.Join(home, ".local", "share", "omachat", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"enabledServices":["telegram"],"futureSetting":{"keep":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	stale := NewConfigStore(path)
	script, err := filepath.Abs("../../scripts/configure-telegram.py")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "-c", `
import pathlib, runpy, sys
from unittest.mock import patch
with patch.object(pathlib.Path, "home", return_value=pathlib.Path(sys.argv[2])), patch("builtins.input", return_value="12345"), patch("getpass.getpass", return_value="a" * 32):
    runpy.run_path(sys.argv[1], run_name="__main__")
`, script, home)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synthetic setup failed: %v\n%s", err, output)
	}
	if err := stale.SetUiScale(1.2); err != nil {
		t.Fatal(err)
	}
	got := NewConfigStore(path).Get()
	if got.TelegramAPIID != 12345 || got.TelegramAPIHash != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || got.UiScale != 1.2 {
		t.Fatal("stale Go writer lost the script's credentials or its own scale update")
	}
	if got.EnabledServices == nil || len(*got.EnabledServices) != 1 || (*got.EnabledServices)[0] != "telegram" {
		t.Fatal("service opt-outs were lost")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["futureSetting"]; !ok {
		t.Fatal("unrelated unknown setting was lost")
	}
}

func TestConfigUpdateRejectsInvalidDiskWithoutMutation(t *testing.T) {
	for _, content := range []string{"", "null", "[]", "{", `{"uiScale":"invalid"}`} {
		t.Run(content, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			c := NewConfigStore(path)
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if err := c.SetUiScale(1.2); err == nil {
				t.Fatal("invalid configuration was accepted")
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != content {
				t.Fatal("invalid configuration was replaced")
			}
			if c.Get().UiScale != 0 {
				t.Fatal("failed update changed memory")
			}
		})
	}
}

func TestConfigUpdateDoesNotResurrectRemovedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := NewConfigStore(path)
	if err := c.SetTelegramCredentials(123, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"enabledServices":[],"futureInteger":9007199254740993}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.SetUiScale(1.2); err != nil {
		t.Fatal(err)
	}
	if c.Get().TelegramAPIID != 0 || c.Get().TelegramAPIHash != "" {
		t.Fatal("removed credentials resurrected")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["futureInteger"]) != "9007199254740993" {
		t.Fatal("unknown integer lost precision")
	}
}
