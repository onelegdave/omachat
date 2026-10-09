package signal

import (
	"context"
	"os"
	"testing"
	"time"
)

// This opt-in test exercises the real external process without linking an
// account or touching the user's normal signal-cli data directory.
func TestRPCClientWithInstalledSignalCLI(t *testing.T) {
	if os.Getenv("OMACHAT_SIGNAL_CLI_INTEGRATION") != "1" {
		t.Skip("set OMACHAT_SIGNAL_CLI_INTEGRATION=1 to run against signal-cli")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := NewRPCClient()
	if err := client.Start(ctx, t.TempDir(), nil, nil); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var accounts []struct {
		Number string `json:"number"`
	}
	if err := client.Call(ctx, "listAccounts", nil, &accounts); err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 0 {
		t.Fatalf("isolated data directory unexpectedly contains accounts: %#v", accounts)
	}
}
