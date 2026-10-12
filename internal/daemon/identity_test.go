package daemon

import (
	"testing"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

func TestPairedPhoneIdentity(t *testing.T) {
	if got := pairedPhoneIdentity(nil); got != "" {
		t.Fatalf("no client: %q", got)
	}
	c := &libgm.Client{AuthData: libgm.NewAuthData()}
	if got := pairedPhoneIdentity(c); got != "" {
		t.Fatalf("unpaired: %q", got)
	}
	c.AuthData.Mobile = &gmproto.Device{SourceID: "phone-1"}
	if got := pairedPhoneIdentity(c); got != "gmessages:phone-1" {
		t.Fatalf("paired: %q", got)
	}
	d := &Daemon{client: c}
	if got := d.Status().AccountIdentity; got != "gmessages:phone-1" {
		t.Fatalf("status: %q", got)
	}
	d.status.SelfPhone = "+15035550100"
	if got := d.Status().AccountIdentity; got != "+15035550100" {
		t.Fatalf("self phone wins: %q", got)
	}
}
