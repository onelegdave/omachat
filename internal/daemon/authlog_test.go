package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readLogEntries(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(sc.Bytes(), &entry); err != nil {
			t.Fatalf("invalid JSON line %q: %v", sc.Text(), err)
		}
		out = append(out, entry)
	}
	return out
}

func entriesOfKind(entries []map[string]any, kind string) []map[string]any {
	var out []map[string]any
	for _, e := range entries {
		if e["kind"] == kind {
			out = append(out, e)
		}
	}
	return out
}

func newTestAuthLog(t *testing.T) (*authLog, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state", "omachat")
	path := filepath.Join(dir, "auth.log")
	return newAuthLog(path, nil), path
}

func TestAuthLogOffWritesNothing(t *testing.T) {
	l, path := newTestAuthLog(t)
	l.write("x", logFields{"a": 1})
	l.writeLimited("k", 1, time.Minute, "x", logFields{})
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("an unused log must not create its folder: %v", err)
	}
	if reopened, err := l.remove(); reopened || err != nil {
		t.Fatalf("remove while off = %v, %v", reopened, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("Delete log while off must not create the folder")
	}
}

func TestAuthLogPermissionsAndToggle(t *testing.T) {
	l, path := newTestAuthLog(t)
	if err := l.enable(); err != nil {
		t.Fatal(err)
	}
	l.write("first", logFields{"n": 1})
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("log mode = %v, %v", st.Mode(), err)
	}
	dst, _ := os.Stat(filepath.Dir(path))
	if dst.Mode().Perm() != 0o700 {
		t.Fatalf("folder mode = %v", dst.Mode())
	}
	l.disable()
	l.write("ignored", logFields{})
	if err := l.enable(); err != nil {
		t.Fatal(err)
	}
	l.write("second", logFields{})
	entries := readLogEntries(t, path)
	if len(entries) != 2 || entries[0]["kind"] != "first" || entries[1]["kind"] != "second" {
		t.Fatalf("entries = %v", entries)
	}
	for _, e := range entries {
		if e["t"] == nil || e["seq"] == nil || e["log"] == nil {
			t.Fatalf("entry missing t/seq/log: %v", e)
		}
	}
}

func TestAuthLogRejectsSymlink(t *testing.T) {
	l, path := newTestAuthLog(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := l.enable(); err == nil {
		t.Fatal("a symlinked log path must be refused")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("log followed a symlink")
	}
}

func TestAuthLogRotationKeepsOneOldFile(t *testing.T) {
	l, path := newTestAuthLog(t)
	l.maxBytes = 2048
	if err := l.enable(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		l.write("filler", logFields{"pad": strings.Repeat("x", 100), "i": i})
	}
	for _, p := range []string{path, path + ".1"} {
		st, err := os.Stat(p)
		if err != nil || st.Size() > 2048 || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s: size/mode = %v, %v", p, st, err)
		}
	}
	if _, err := os.Stat(path + ".2"); !os.IsNotExist(err) {
		t.Fatal("only one rotated file may be kept")
	}
	last := readLogEntries(t, path)
	if last[len(last)-1]["i"].(float64) != 199 {
		t.Fatal("newest entry not in auth.log")
	}
}

func TestAuthLogFingerprintsAreSaltedPerLog(t *testing.T) {
	l, _ := newTestAuthLog(t)
	if err := l.enable(); err != nil {
		t.Fatal(err)
	}
	a := l.fp("secret-value")
	if a != l.fp("secret-value") || len(a) != authLogFPHexLen || strings.Contains(string(a), "secret") {
		t.Fatalf("fingerprint = %q", a)
	}
	if l.fp("") != authLogFPNoValue {
		t.Fatal("an empty value must fingerprint as '-'")
	}
	id := l.logID()
	if _, err := l.remove(); err != nil {
		t.Fatal(err)
	}
	if l.fp("secret-value") == a || l.logID() == id {
		t.Fatal("Delete log must pick a new salt and log ID")
	}
}

func TestDescribeBodyCapsAfterEncoding(t *testing.T) {
	// Characters that JSON escapes six-fold still fit the cap once encoded.
	body := bytes.Repeat([]byte{0x01}, 200<<10)
	f := describeBody(body, "text/plain", len(body))
	enc, _ := json.Marshal(f["body"])
	if len(enc) > authLogBodyCap || f["truncated"] != true || f["bodyLen"] != len(body) || f["bodyEncoding"] != "text" {
		t.Fatalf("encoded body %d bytes, fields %v", len(enc), f["truncated"])
	}
	small := describeBody([]byte(`{"error":"SESSION_COOKIE_INVALID"}`), "application/json", 34)
	if small["truncated"] != false || small["body"] != bodyText(`{"error":"SESSION_COOKIE_INVALID"}`) || small["bodyContainsSessionCookieInvalid"] != true {
		t.Fatalf("small body = %v", small)
	}
	binary := describeBody([]byte{0xff, 0xfe, 0x00, 0x80}, "application/x-protobuf", 4)
	if binary["bodyEncoding"] != "base64" || binary["body"] != bodyText("//4AgA==") {
		t.Fatalf("binary body = %v", binary)
	}
	bigBinary := describeBody(bytes.Repeat([]byte{0xff}, 200<<10), "", 200<<10)
	if enc, _ := json.Marshal(bigBinary["body"]); len(enc) > authLogBodyCap || bigBinary["truncated"] != true {
		t.Fatalf("binary body not capped: %d", len(enc))
	}
}

func TestAuthLogDeduplicatesBodies(t *testing.T) {
	l, path := newTestAuthLog(t)
	if err := l.enable(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		l.write("authFailure", describeBody([]byte("same body"), "text/plain", 9))
	}
	entries := readLogEntries(t, path)
	if entries[0]["body"] != "same body" || entries[1]["body"] != nil || entries[1]["bodySameAs"] != entries[0]["seq"] {
		t.Fatalf("entries = %v", entries)
	}
	if _, has := entries[0]["_bodyHash"]; has {
		t.Fatal("internal hash leaked into the log")
	}
}

func TestAuthLogRateLimitSummarizes(t *testing.T) {
	l, path := newTestAuthLog(t)
	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }
	if err := l.enable(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		l.writeLimited("k", 3, time.Minute, "authFailure", logFields{"i": i})
	}
	now = now.Add(61 * time.Second)
	l.writeLimited("k", 3, time.Minute, "authFailure", logFields{"i": 7})
	entries := readLogEntries(t, path)
	if got := len(entriesOfKind(entries, "authFailure")); got != 4 {
		t.Fatalf("authFailure entries = %d, want 4", got)
	}
	s := entriesOfKind(entries, "suppressed")
	if len(s) != 1 || s[0]["count"].(float64) != 4 || s[0]["key"] != "k" {
		t.Fatalf("suppressed summary = %v", s)
	}
}

func TestAuthLogCapsOrdinaryFieldsAndOversizedEntries(t *testing.T) {
	l, path := newTestAuthLog(t)
	if err := l.enable(); err != nil {
		t.Fatal(err)
	}
	l.write("long", logFields{"err": strings.Repeat("é", authLogFieldCap)})
	huge := make([]string, 200)
	for i := range huge {
		huge[i] = strings.Repeat("y", authLogFieldCap)
	}
	l.write("huge", logFields{"list": huge})
	entries := readLogEntries(t, path)
	if s := entries[0]["err"].(string); len(s) > authLogFieldCap+64 || !strings.Contains(s, "truncated") {
		t.Fatalf("field not capped: %d bytes", len(s))
	}
	if entries[1]["dropped"] != "entry too large" {
		t.Fatalf("oversized entry = %v", entries[1])
	}
}
