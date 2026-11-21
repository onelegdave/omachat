package browser

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadOnlyURIEscapesPathAndIsImmutable(t *testing.T) {
	got := ReadOnlyURI("/home/u/.config/google-chrome/Profile 1/Cookies")
	want := "file:///home/u/.config/google-chrome/Profile%201/Cookies?mode=ro&immutable=1"
	if got != want {
		t.Fatalf("ReadOnlyURI = %q, want %q", got, want)
	}
}

func TestSignInMetadataQueryIsRestricted(t *testing.T) {
	var queries []string
	run := func(_ context.Context, uri, query string) ([]byte, error) {
		if !strings.HasSuffix(uri, "?mode=ro&immutable=1") {
			t.Fatalf("database not opened read-only and immutable: %q", uri)
		}
		queries = append(queries, query)
		if strings.Contains(query, "pragma_table_info") {
			return []byte("creation_utc\nhost_key\ntop_frame_site_key\nname\nvalue\nencrypted_value\nexpires_utc\nlast_update_utc\n"), nil
		}
		return []byte("SID|.google.com|13300000000000000|13300000000000001|13400000000000000|40|abc\n" +
			"OSID|messages.google.com|0|0|0|30|def\n"), nil
	}
	metas, err := ReadSignInMetadata(context.Background(), Profile{CookieDB: "/x/Cookies"}, run)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 {
		t.Fatalf("queries = %d", len(queries))
	}
	q := queries[1]
	stripped := strings.ReplaceAll(strings.ReplaceAll(q, "length(encrypted_value)", ""), "sha3(encrypted_value, 256)", "")
	if strings.Contains(stripped, "value") || strings.Contains(q, "LIKE") {
		t.Fatalf("metadata query may only measure and hash the encrypted value, and must match exactly: %s", q)
	}
	if !strings.Contains(q, "host_key IN ('.google.com','messages.google.com')") || !strings.Contains(q, "top_frame_site_key = ''") {
		t.Fatalf("query is not restricted to exact sign-in hosts: %s", q)
	}
	for _, name := range SignInCookieNames {
		if !strings.Contains(q, "'"+name+"'") {
			t.Fatalf("query is missing %s: %s", name, q)
		}
	}
	if !strings.Contains(q, "sha3(encrypted_value, 256)") || !strings.Contains(q, "length(encrypted_value)") {
		t.Fatalf("query must only hash and measure the encrypted value: %s", q)
	}
	if len(metas) != 2 || metas[0].Name != "OSID" || metas[1].Name != "SID" {
		t.Fatalf("metas = %+v", metas)
	}
	if metas[1].EncryptedHash != "abc" || metas[1].EncryptedLen != 40 || metas[1].Created.IsZero() || !metas[0].Created.IsZero() {
		t.Fatalf("parsed metadata = %+v", metas[1])
	}
}

func TestSignInMetadataQueryCopesWithOlderSchema(t *testing.T) {
	q := signInMetadataQuery(map[string]bool{"name": true, "host_key": true, "encrypted_value": true, "creation_utc": true, "expires_utc": true})
	if strings.Contains(q, "last_update_utc") || strings.Contains(q, "top_frame_site_key") {
		t.Fatalf("query uses columns the database lacks: %s", q)
	}
	run := func(_ context.Context, _, query string) ([]byte, error) { return []byte("name\nhost_key\n"), nil }
	if _, err := ReadSignInMetadata(context.Background(), Profile{CookieDB: "/x"}, run); err == nil {
		t.Fatal("a database without encrypted_value must be reported, not queried")
	}
}

func TestChromeTime(t *testing.T) {
	got := chromeTime("13300000000000000")
	if want := time.Date(2022, 6, 18, 4, 26, 40, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("chromeTime = %v, want %v", got, want)
	}
	if !chromeTime("0").IsZero() || !chromeTime("junk").IsZero() {
		t.Fatal("empty Chrome times must be zero")
	}
}

// Against a real sqlite3 and a synthetic database: the reader must work on the
// live file without copying it and must never start secret-tool.
func TestReadSignInMetadataWithSQLiteNeverUsesKeyring(t *testing.T) {
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not installed")
	}
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "Profile 1")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(profileDir, "Cookies")
	schema := `CREATE TABLE cookies(creation_utc INTEGER, host_key TEXT, top_frame_site_key TEXT, name TEXT, value TEXT, encrypted_value BLOB, expires_utc INTEGER, last_update_utc INTEGER);
INSERT INTO cookies VALUES(13300000000000000,'.google.com','','SID','',x'7631310102',13400000000000000,13300000000000001);
INSERT INTO cookies VALUES(13300000000000000,'.google.com','https://other.example','SID','',x'76313103',0,0);
INSERT INTO cookies VALUES(13300000000000000,'.notgoogle.com','','SID','',x'76313104',0,0);
INSERT INTO cookies VALUES(13300000000000000,'.google.com','','NID','',x'76313105',0,0);`
	if out, err := exec.Command(sqlite, db, schema).CombinedOutput(); err != nil {
		t.Fatalf("create db: %v %s", err, out)
	}
	tools := filepath.Join(dir, "tools")
	if err := os.MkdirAll(tools, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "secret-tool-ran")
	if err := os.WriteFile(filepath.Join(tools, "secret-tool"), []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	before, _ := os.ReadDir(os.TempDir())

	metas, err := ReadSignInMetadata(context.Background(), Profile{Name: "Chrome / Profile 1", CookieDB: db}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 || metas[0].Name != "SID" || metas[0].Host != ".google.com" || metas[0].EncryptedLen != 5 || len(metas[0].EncryptedHash) != 64 {
		t.Fatalf("metas = %+v", metas)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("metadata reader started secret-tool")
	}
	after, _ := os.ReadDir(os.TempDir())
	for _, e := range after {
		if strings.HasPrefix(e.Name(), "gm-cookies-") && !containsEntry(before, e.Name()) {
			t.Fatalf("metadata reader copied the cookie database to %s", e.Name())
		}
	}
	entries, _ := os.ReadDir(profileDir)
	if len(entries) != 1 {
		t.Fatalf("reading must not create journal or other files: %v", entries)
	}
}

func containsEntry(entries []os.DirEntry, name string) bool {
	for _, e := range entries {
		if e.Name() == name {
			return true
		}
	}
	return false
}
