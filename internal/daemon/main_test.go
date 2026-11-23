package daemon

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/onelegdave/omachat/internal/browser"
)

// TestMain switches off the daemon package's own browser access, so no test
// here can read the browser profiles, cookie databases or keyring of the
// machine running it, whichever way it builds its daemon. (The Messenger
// backend reads the browser itself; no test in this package reaches it.)
func TestMain(m *testing.M) {
	errDisabled := errors.New("browser access disabled in tests")
	discoverBrowserProfiles = func() []browser.Profile { return nil }
	extractBrowserCookies = func(context.Context, browser.Profile) (map[string]string, error) {
		return nil, errDisabled
	}
	browserSQLite = func(context.Context, string, string) ([]byte, error) { return nil, errDisabled }
	os.Exit(m.Run())
}
