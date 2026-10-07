package whatsapp

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
)

func TestRequestHistoryPreservesChatAnchorAndSecondTimestamp(t *testing.T) {
	mock := NewMockClient()
	var captured types.MessageInfo
	var count int
	mock.RequestHistoryFunc = func(_ context.Context, anchor types.MessageInfo, requested int) error {
		captured = anchor
		count = requested
		return nil
	}
	chat, err := types.ParseJID("123456789@g.us")
	if err != nil {
		t.Fatal(err)
	}
	anchor := types.MessageInfo{ID: "synthetic-anchor", Chat: chat, IsFromMe: true, Timestamp: time.Unix(1700000000, 123000)}
	if err := requestHistory(context.Background(), mock, anchor, 50); err != nil {
		t.Fatal(err)
	}
	if captured.ID != anchor.ID || captured.Chat != chat || !captured.IsFromMe || captured.Timestamp.Unix() != 1700000000 || count != 50 {
		t.Fatalf("history request changed its anchor: got %+v count=%d", captured, count)
	}
}

func TestRequestHistoryRejectsUnsupportedClient(t *testing.T) {
	if err := requestHistory(context.Background(), NewMockClient(), types.MessageInfo{}, 50); err == nil {
		t.Fatal("history request without a configured sender unexpectedly succeeded")
	}
}

func TestRequestHistoryRejectsOutOfRangeCountBeforeSending(t *testing.T) {
	mock := NewMockClient()
	called := false
	mock.RequestHistoryFunc = func(context.Context, types.MessageInfo, int) error {
		called = true
		return nil
	}
	if err := requestHistory(context.Background(), mock, types.MessageInfo{}, 51); err == nil {
		t.Fatal("oversized history request unexpectedly succeeded")
	}
	if called {
		t.Fatal("invalid history request reached the client")
	}
}
