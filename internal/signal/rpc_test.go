package signal

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The test executable impersonates signal-cli. No external client, account,
// package installation, or network request is involved.
func TestSignalRPCFixture(t *testing.T) {
	if os.Getenv("OMACHAT_SIGNAL_RPC_FIXTURE") != "1" {
		t.Skip("subprocess fixture only")
	}
	manual := false
	for _, arg := range os.Args {
		if arg == "--receive-mode=manual" {
			manual = true
		}
	}
	if !manual {
		os.Exit(2)
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     string
			Method string
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(3)
		}
		switch request.Method {
		case "stall":
			continue
		case "exit":
			os.Exit(4)
		case "error":
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID,
				"error": map[string]any{"code": -1, "message": "synthetic-private-value"}})
			continue
		case "subscribeReceive":
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "receive",
				"params": map[string]any{"subscription": 0, "result": map[string]any{
					"account": "+15550001111", "envelope": map[string]any{"sourceNumber": "+15550002222"}}}})
		}
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]bool{"ok": true}})
	}
	os.Exit(0)
}

func TestRPCStreamCancellationExitAndPrivateErrors(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	quoted := "'" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "'"
	script := fmt.Sprintf("#!/bin/sh\nexec %s -test.run=^TestSignalRPCFixture$ -- \"$@\"\n", quoted)
	if err := os.WriteFile(filepath.Join(bin, "signal-cli"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OMACHAT_SIGNAL_RPC_FIXTURE", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := NewRPCClient()
	defer client.Close()
	notifications := make(chan json.RawMessage, 1)
	exited := make(chan error, 1)
	if err := client.Start(ctx, t.TempDir(), func(raw json.RawMessage) { notifications <- raw }, func(err error) { exited <- err }); err != nil {
		t.Fatal(err)
	}
	var response struct{ OK bool }
	if err := client.Call(ctx, "echo", nil, &response); err != nil || !response.OK {
		t.Fatalf("response: %+v %v", response, err)
	}
	if err := client.Call(ctx, "subscribeReceive", nil, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-notifications:
	case <-ctx.Done():
		t.Fatal("missing receive notification")
	}
	if err := client.Call(ctx, "error", nil, nil); err == nil || strings.Contains(err.Error(), "synthetic-private-value") {
		t.Fatal("private upstream error exposed")
	}
	short, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	if err := client.Call(short, "stall", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	if err := client.Call(ctx, "echo", nil, &response); err != nil {
		t.Fatalf("timeout broke next call: %v", err)
	}
	if err := client.Call(ctx, "exit", nil, nil); err == nil {
		t.Fatal("child exit reported as success")
	}
	select {
	case <-exited:
	case <-ctx.Done():
		t.Fatal("missing exit callback")
	}
	client.mu.Lock()
	pending := len(client.pending)
	client.mu.Unlock()
	if pending != 0 {
		t.Fatalf("pending calls retained: %d", pending)
	}
	_ = client.Close()
	if err := client.Start(ctx, t.TempDir(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.Call(ctx, "echo", nil, &response); err != nil {
		t.Fatalf("restart: %v", err)
	}
}
