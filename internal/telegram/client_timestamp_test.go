package telegram

import "testing"

func TestTelegramTimestampUsesWireMicroseconds(t *testing.T) {
	if got := telegramTimestamp(1_700_000_000); got != 1_700_000_000_000_000 {
		t.Fatalf("timestamp = %d, want Unix microseconds", got)
	}
}
