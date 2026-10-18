package daemon

import (
	"context"
	"testing"

	"github.com/onelegdave/omachat/internal/buildinfo"
	"github.com/onelegdave/omachat/internal/wire"
)

func TestBuildInfoAvailableWithoutEnabledServices(t *testing.T) {
	d := &Daemon{}
	response := d.dispatch(context.Background(), wire.Request{ID: "build", Method: "buildInfo", Network: "telegram"})
	if !response.OK || response.ID != "build" || response.Result.(map[string]string)["sourceID"] != buildinfo.SourceID {
		t.Fatalf("build identity unavailable: %+v", response)
	}
	// Panels show remote content only from a helper that reports this.
	if got := response.Result.(map[string]string)["contentProtocol"]; got != wire.SafeClient {
		t.Fatalf("buildInfo contentProtocol = %q, want %q", got, wire.SafeClient)
	}
}
