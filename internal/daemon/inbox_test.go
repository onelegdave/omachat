package daemon

import (
	"github.com/onelegdave/omachat/internal/wire"
	"testing"
)

func TestInboxStatusUsesKnownSelfPhone(t *testing.T) {
	d := &Daemon{status: wire.Status{SelfPhone: "+12025550123"}}
	if d.Status().AccountIdentity != "+12025550123" {
		t.Fatal("wrong own account identity")
	}
	d.status.SelfPhone = ""
	if d.Status().AccountIdentity != "" {
		t.Fatal("guessed account identity")
	}
}
