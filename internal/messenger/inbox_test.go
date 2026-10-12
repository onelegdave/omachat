package messenger

import "testing"

func TestInboxStatusUsesOwnAccount(t *testing.T) {
	b := &Backend{selfID: 314159}
	if b.Status().AccountIdentity != "314159" {
		t.Fatal("wrong own account identity")
	}
	b.selfID = 0
	if b.Status().AccountIdentity != "" {
		t.Fatal("guessed account identity")
	}
}
