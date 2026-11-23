package browser

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestHasGoogleSignIn(t *testing.T) {
	cases := map[string]bool{
		"":                               false,
		"NID|.google.com|AB12\n":         false,
		"SID|accounts.google.com|AB12\n": false,
		"SID|.google.com|\n":             false,
		"NID|.google.com|AB\nSID|.google.com|76313000\n": true,
	}
	for out, want := range cases {
		if got := hasGoogleSignIn(out); got != want {
			t.Errorf("hasGoogleSignIn(%q) = %v; want %v", out, got, want)
		}
	}
}

// A signed-out profile is recognised from its cookie database alone, before
// the keyring is asked for anything.
func TestExtractGoogleCookiesNotSignedIn(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	db := filepath.Join(t.TempDir(), "Cookies")
	schema := `CREATE TABLE cookies (name TEXT, host_key TEXT, encrypted_value BLOB);
INSERT INTO cookies VALUES ('NID', '.google.com', x'763130deadbeef'), ('SIDCC', '.google.com', x'763130beef');`
	if out, err := exec.Command("sqlite3", db, schema).CombinedOutput(); err != nil {
		t.Fatalf("create fixture: %v: %s", err, out)
	}
	// KeyringApp is one no keyring entry exists for; it must never be asked.
	_, err := ExtractGoogleCookiesContext(context.Background(), Profile{Name: "Fixture", CookieDB: db, KeyringApp: "omachat-test-none"})
	if !errors.Is(err, ErrNotSignedIn) {
		t.Fatalf("err = %v; want ErrNotSignedIn", err)
	}
}
