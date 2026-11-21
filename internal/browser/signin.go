package browser

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SignInCookieNames are the Google sign-in cookies the diagnostics log
// tracks. They are used for diagnostics only; pairing and refresh still use
// googleWantedHosts.
var SignInCookieNames = []string{
	"SID", "HSID", "SSID", "APISID", "SAPISID", "OSID", "__Secure-OSID", "SIDCC",
	"__Secure-1PSID", "__Secure-3PSID", "__Secure-1PSIDTS", "__Secure-3PSIDTS",
	"__Secure-1PSIDCC", "__Secure-3PSIDCC", "__Secure-1PAPISID", "__Secure-3PAPISID",
}

// SignInCookieHosts are the exact cookie hosts the diagnostics read.
var SignInCookieHosts = []string{".google.com", "messages.google.com"}

// CookieMeta describes one browser cookie without its value. EncryptedHash
// is a SHA3-256 of the still-encrypted value, computed by sqlite3, so the
// value is never decrypted and never reaches OmaChat.
type CookieMeta struct {
	Name          string
	Host          string
	Created       time.Time
	LastUpdate    time.Time
	Expires       time.Time
	EncryptedLen  int
	EncryptedHash string
}

// SQLiteRunner runs one read-only query against a database URI and returns
// sqlite3's list-mode output.
type SQLiteRunner func(ctx context.Context, dbURI, query string) ([]byte, error)

// DefaultSQLiteRunner runs the sqlite3 command-line tool. It is the only
// program the metadata reader starts; it never runs secret-tool.
func DefaultSQLiteRunner(ctx context.Context, dbURI, query string) ([]byte, error) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return nil, fmt.Errorf("sqlite3 is required to read the browser cookie database: %w", err)
	}
	out, err := exec.CommandContext(ctx, "sqlite3", "-readonly", "-batch", "-noheader", "-list", "-separator", "|", dbURI, query).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("query cookie database: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("query cookie database: %w", err)
	}
	return out, nil
}

// ReadOnlyURI opens the live cookie database read-only without taking locks
// or writing a journal, so no copy of the database is made.
func ReadOnlyURI(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String() + "?mode=ro&immutable=1"
}

// ReadSignInMetadata returns metadata for the listed sign-in cookies only.
// It never decrypts a value and never uses the desktop keyring.
func ReadSignInMetadata(ctx context.Context, p Profile, run SQLiteRunner) ([]CookieMeta, error) {
	if run == nil {
		run = DefaultSQLiteRunner
	}
	uri := ReadOnlyURI(p.CookieDB)
	colsOut, err := run(ctx, uri, `SELECT name FROM pragma_table_info('cookies');`)
	if err != nil {
		return nil, err
	}
	cols := map[string]bool{}
	for _, line := range strings.Split(string(colsOut), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			cols[line] = true
		}
	}
	for _, required := range []string{"name", "host_key", "encrypted_value"} {
		if !cols[required] {
			return nil, fmt.Errorf("cookie database has no %s column", required)
		}
	}
	out, err := run(ctx, uri, signInMetadataQuery(cols))
	if err != nil {
		return nil, err
	}
	return parseSignInMetadata(out), nil
}

func quoteList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = "'" + strings.ReplaceAll(v, "'", "''") + "'"
	}
	return strings.Join(quoted, ",")
}

func signInMetadataQuery(cols map[string]bool) string {
	col := func(name string) string {
		if cols[name] {
			return name
		}
		return "0"
	}
	where := "host_key IN (" + quoteList(SignInCookieHosts) + ") AND name IN (" + quoteList(SignInCookieNames) + ")"
	if cols["top_frame_site_key"] {
		// Partitioned cookies are not the sign-in cookies a page would send.
		where += " AND top_frame_site_key = ''"
	}
	return "SELECT name, host_key, " + col("creation_utc") + ", " + col("last_update_utc") + ", " + col("expires_utc") +
		", length(encrypted_value), lower(hex(sha3(encrypted_value, 256))) FROM cookies WHERE " + where +
		" ORDER BY name, host_key;"
}

func parseSignInMetadata(out []byte) []CookieMeta {
	var metas []CookieMeta
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 7 {
			continue
		}
		length, _ := strconv.Atoi(parts[5])
		metas = append(metas, CookieMeta{
			Name:          parts[0],
			Host:          parts[1],
			Created:       chromeTime(parts[2]),
			LastUpdate:    chromeTime(parts[3]),
			Expires:       chromeTime(parts[4]),
			EncryptedLen:  length,
			EncryptedHash: parts[6],
		})
	}
	sort.Slice(metas, func(i, j int) bool {
		if metas[i].Name != metas[j].Name {
			return metas[i].Name < metas[j].Name
		}
		return metas[i].Host < metas[j].Host
	})
	return metas
}

// chromeEpochOffset is the number of microseconds between 1601-01-01 (the
// Chromium/Windows epoch) and the Unix epoch.
const chromeEpochOffset = 11644473600000000

func chromeTime(raw string) time.Time {
	micros, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || micros <= 0 {
		return time.Time{}
	}
	return time.UnixMicro(micros - chromeEpochOffset).UTC()
}
