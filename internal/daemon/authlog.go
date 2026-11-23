package daemon

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"
)

// The sign-in diagnostics log. It is opt-in (Settings > Debug), written only
// by the helper, and never receives libgm's own logging. Cookie and token
// values are reduced to salted fingerprints before they reach it, and every
// text field is scrubbed for the live credentials before it is written.

const (
	authLogMaxBytes  = 1 << 20  // rotate to auth.log.1 past this size
	authLogBodyCap   = 64 << 10 // largest error response kept, after encoding
	authLogFieldCap  = 4 << 10  // largest ordinary text field
	authLogMaxLine   = 96 << 10 // largest single entry, after encoding
	authLogFPHexLen  = 8
	authLogFPNoValue = "-"
)

// logFields is one entry's payload. Values must be JSON-encodable.
type logFields map[string]any

// fingerprint is a salted, truncated SHA-256 of a secret. Only authLog.fp
// produces one; raw secrets are never placed in log fields.
type fingerprint string

// bodyText is an already-capped error response; it is exempt from the
// ordinary field cap.
type bodyText string

type logSalt struct {
	salt  []byte
	logID string
}

type rateWindow struct {
	start      time.Time
	count      int
	suppressed int
}

type authLog struct {
	path     string
	pathErr  error
	now      func() time.Time
	maxBytes int64

	enabled atomic.Bool
	salt    atomic.Pointer[logSalt]

	mu     sync.Mutex
	f      *os.File
	size   int64
	seq    uint64
	bodies map[[32]byte]uint64
	rate   map[string]*rateWindow
}

func newAuthLog(path string, pathErr error) *authLog {
	l := &authLog{path: path, pathErr: pathErr, now: time.Now, maxBytes: authLogMaxBytes}
	l.newSalt()
	return l
}

func (l *authLog) on() bool { return l != nil && l.enabled.Load() }

func (l *authLog) newSalt() {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	id := make([]byte, 4)
	_, _ = rand.Read(id)
	l.salt.Store(&logSalt{salt: salt, logID: hex.EncodeToString(id)})
}

func (l *authLog) logID() string { return l.salt.Load().logID }

// fp fingerprints a secret with this log's salt. Fingerprints are comparable
// within one log only; Delete log and turning logging on pick a new salt.
func (l *authLog) fp(value string) fingerprint {
	if value == "" {
		return authLogFPNoValue
	}
	s := l.salt.Load()
	h := sha256.New()
	h.Write(s.salt)
	h.Write([]byte(value))
	return fingerprint(hex.EncodeToString(h.Sum(nil))[:authLogFPHexLen])
}

func securePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(dir, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Chmod(0o700)
}

func openPrivateAppend(path string) (*os.File, int64, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, 0, errors.New("diagnostics log must be a regular file")
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

// enable opens the log, creating its folder (0700) and file (0600). It picks
// a new salt each time logging is switched on.
func (l *authLog) enable() error {
	if l.pathErr != nil {
		return l.pathErr
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		if err := securePrivateDir(filepath.Dir(l.path)); err != nil {
			return fmt.Errorf("create diagnostics folder: %w", err)
		}
		f, size, err := openPrivateAppend(l.path)
		if err != nil {
			return fmt.Errorf("open diagnostics log: %w", err)
		}
		l.f, l.size = f, size
		l.newSalt()
		l.bodies = make(map[[32]byte]uint64)
		l.rate = make(map[string]*rateWindow)
	}
	l.enabled.Store(true)
	return nil
}

// disable stops all writing immediately and closes the file.
func (l *authLog) disable() {
	l.enabled.Store(false)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
}

// remove deletes auth.log and auth.log.1. With logging on, it starts a new
// log with a new salt and reports true.
func (l *authLog) remove() (bool, error) {
	if l.pathErr != nil {
		return false, l.pathErr
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
	var firstErr error
	for _, path := range []string{l.path, l.path + ".1"} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	l.newSalt()
	l.bodies = make(map[[32]byte]uint64)
	l.rate = make(map[string]*rateWindow)
	if !l.enabled.Load() {
		return false, firstErr
	}
	f, size, err := openPrivateAppend(l.path)
	if err != nil {
		l.enabled.Store(false)
		if firstErr == nil {
			firstErr = err
		}
		return false, firstErr
	}
	l.f, l.size = f, size
	return true, firstErr
}

func (l *authLog) write(kind string, fields logFields) {
	if !l.on() {
		return
	}
	defer func() { _ = recover() }()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.writeLocked(kind, fields)
}

// writeLimited allows `limit` entries per key per window. Suppressed entries
// are summarized when the next window opens.
func (l *authLog) writeLimited(key string, limit int, window time.Duration, kind string, fields logFields) {
	if !l.on() {
		return
	}
	defer func() { _ = recover() }()
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w := l.rate[key]
	if w == nil || now.Sub(w.start) >= window {
		if w != nil && w.suppressed > 0 {
			l.writeLocked("suppressed", logFields{"key": key, "count": w.suppressed, "since": w.start.UTC().Format(time.RFC3339Nano)})
		}
		w = &rateWindow{start: now}
		if l.rate == nil {
			l.rate = make(map[string]*rateWindow)
		}
		l.rate[key] = w
	}
	if w.count >= limit {
		w.suppressed++
		return
	}
	w.count++
	l.writeLocked(kind, fields)
}

func (l *authLog) writeLocked(kind string, fields logFields) {
	if l.f == nil || !l.enabled.Load() {
		return
	}
	l.seq++
	entry := make(map[string]any, len(fields)+4)
	for k, v := range fields {
		if k == "_bodyHash" {
			continue
		}
		entry[k] = capField(v)
	}
	if hash, ok := fields["_bodyHash"].([32]byte); ok {
		if earlier, seen := l.bodies[hash]; seen {
			delete(entry, "body")
			delete(entry, "bodyEncoding")
			entry["bodySameAs"] = earlier
		} else {
			if l.bodies == nil {
				l.bodies = make(map[[32]byte]uint64)
			}
			l.bodies[hash] = l.seq
		}
	}
	entry["t"] = l.now().UTC().Format(time.RFC3339Nano)
	entry["seq"] = l.seq
	entry["kind"] = kind
	entry["log"] = l.logID()
	line, err := json.Marshal(entry)
	if err != nil {
		line, _ = json.Marshal(map[string]any{"t": entry["t"], "seq": l.seq, "kind": kind, "log": entry["log"], "encodeError": err.Error()})
	}
	if len(line)+1 > authLogMaxLine {
		line, _ = json.Marshal(map[string]any{"t": entry["t"], "seq": l.seq, "kind": kind, "log": entry["log"], "dropped": "entry too large", "len": len(line)})
	}
	line = append(line, '\n')
	if l.size > 0 && l.size+int64(len(line)) > l.maxBytes {
		l.rotateLocked()
		if l.f == nil {
			return
		}
	}
	n, _ := l.f.Write(line)
	l.size += int64(n)
}

func (l *authLog) rotateLocked() {
	_ = l.f.Close()
	l.f = nil
	_ = os.Rename(l.path, l.path+".1")
	f, size, err := openPrivateAppend(l.path)
	if err != nil {
		l.enabled.Store(false)
		return
	}
	l.f, l.size = f, size
	// Earlier entries now live in auth.log.1; keep later references local.
	l.bodies = make(map[[32]byte]uint64)
}

func capString(s string) string {
	if len(s) <= authLogFieldCap {
		return s
	}
	cut := authLogFieldCap
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("…[%d bytes truncated]", len(s)-cut)
}

func capField(v any) any {
	switch t := v.(type) {
	case string:
		return capString(t)
	case []string:
		out := make([]string, len(t))
		for i, s := range t {
			out[i] = capString(s)
		}
		return out
	case logFields:
		out := make(logFields, len(t))
		for k, inner := range t {
			if k == "_bodyHash" {
				continue
			}
			out[k] = capField(inner)
		}
		return out
	case []logFields:
		out := make([]logFields, len(t))
		for i, inner := range t {
			out[i] = capField(inner).(logFields)
		}
		return out
	default:
		return v
	}
}

func jsonLen(s string) int {
	b, _ := json.Marshal(s)
	return len(b)
}

// describeBody keeps an error response up to authLogBodyCap after encoding.
// Valid UTF-8 is kept as text; anything else is base64-encoded.
func describeBody(body []byte, contentType string, knownLen int) logFields {
	f := logFields{"bodyLen": knownLen}
	if contentType != "" {
		f["contentType"] = contentType
	}
	f["bodyContainsSessionCookieInvalid"] = containsFold(body, "SESSION_COOKIE_INVALID")
	if len(body) == 0 {
		return f
	}
	truncated := knownLen > len(body)
	if utf8.Valid(body) {
		text := string(body)
		for jsonLen(text) > authLogBodyCap {
			cut := len(text)*authLogBodyCap/jsonLen(text) - 8
			if cut < 0 {
				cut = 0
			}
			for cut > 0 && !utf8.RuneStart(text[cut]) {
				cut--
			}
			text = text[:cut]
			truncated = true
		}
		f["body"] = bodyText(text)
		f["bodyEncoding"] = "text"
	} else {
		raw := body
		// Leave room for the JSON quotes around the base64 text.
		if max := (authLogBodyCap - 2) / 4 * 3; len(raw) > max {
			raw = raw[:max]
			truncated = true
		}
		f["body"] = bodyText(base64.StdEncoding.EncodeToString(raw))
		f["bodyEncoding"] = "base64"
	}
	f["truncated"] = truncated
	f["_bodyHash"] = sha256.Sum256(body)
	return f
}

func containsFold(body []byte, needle string) bool {
	if len(body) < len(needle) {
		return false
	}
	n := []byte(needle)
	for i := 0; i+len(n) <= len(body); i++ {
		match := true
		for j := range n {
			c := body[i+j]
			if c >= 'a' && c <= 'z' {
				c -= 'a' - 'A'
			}
			if c != n[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
