package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestInboxStatusUsesOwnAccountWithoutDeviceSuffix(t *testing.T) {
	jid := types.NewADJID("12025550123", 0, 42)
	b := &Backend{gen: 3}
	b.handleEventFor(3, &events.PairSuccess{ID: jid})
	if got := b.Status().AccountIdentity; got != "12025550123@s.whatsapp.net" {
		t.Fatalf("wrong fixture identity: %q", got)
	}
	b.handleEventFor(2, &events.PairSuccess{ID: types.NewADJID("12025550456", 0, 99)})
	if b.Status().AccountIdentity != "12025550123@s.whatsapp.net" {
		t.Fatal("obsolete pairing changed account identity")
	}
	b.retireGeneration(3, true)
	if b.Status().AccountIdentity != "" {
		t.Fatal("guessed identity without a device")
	}
}
