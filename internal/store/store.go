// Package store owns on-disk locations and session persistence.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
)

const appDir = "omachat"

// Paths resolves every directory the daemon writes to, honouring the XDG
// variables when set.
type Paths struct {
	sessionMu sync.Mutex
	Data      string
	Cache     string
	Runtime   string
}

func xdg(env, fallback string) (string, error) {
	if v := os.Getenv(env); v != "" {
		return filepath.Join(v, appDir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fallback, appDir), nil
}

// NewPaths resolves and creates the daemon's directories.
func NewPaths() (*Paths, error) {
	data, err := xdg("XDG_DATA_HOME", ".local/share")
	if err != nil {
		return nil, err
	}
	cache, err := xdg("XDG_CACHE_HOME", ".cache")
	if err != nil {
		return nil, err
	}
	// A socket in the runtime dir is cleaned up by the OS on logout, which is
	// what we want; fall back to the cache dir only if it is unset.
	runtime := cache
	if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
		runtime = filepath.Join(v, appDir)
	}
	p := &Paths{Data: data, Cache: cache, Runtime: runtime}
	for _, dir := range []string{p.Data, p.Cache, p.Runtime, p.MediaDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return p, nil
}

// SessionFile holds the paired-device credentials. Treat as a secret.
func (p *Paths) SessionFile() string { return filepath.Join(p.Data, "session.json") }

// SocketPath is where the plugin connects.
func (p *Paths) SocketPath() string { return filepath.Join(p.Runtime, "daemon.sock") }

// MediaDir caches downloaded attachments and avatars.
func (p *Paths) MediaDir() string { return filepath.Join(p.Cache, "media") }

// LoadSession reads persisted auth data. A missing file is not an error; it
// returns fresh auth data and paired=false so the caller can start pairing.
func (p *Paths) LoadSession() (auth *libgm.AuthData, paired bool, err error) {
	f, err := os.Open(p.SessionFile())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return libgm.NewAuthData(), false, nil
		}
		return nil, false, err
	}
	defer f.Close()

	auth = &libgm.AuthData{}
	if err := json.NewDecoder(f).Decode(auth); err != nil {
		// A corrupt session is unrecoverable, but it must not wedge the daemon
		// permanently: fall back to pairing again.
		return libgm.NewAuthData(), false, nil
	}
	return auth, IsPaired(auth), nil
}

// IsPaired reports whether a session represents a completed pairing.
//
// A tachyon token alone is NOT enough: starting a QR pairing registers a
// browser relay and mints a token before any phone has accepted, so an
// abandoned pairing leaves a token behind. Only completePairing fills in the
// browser device identity, so that is the honest signal — otherwise the daemon
// boots believing it is paired and dies with "not logged in".
func IsPaired(auth *libgm.AuthData) bool {
	return auth != nil && auth.TachyonAuthToken != nil && auth.Browser != nil
}

// SaveSession persists auth data atomically at mode 0600.
func (p *Paths) SaveSession(auth *libgm.AuthData) error {
	if auth == nil {
		return errors.New("auth data is nil")
	}
	p.sessionMu.Lock()
	defer p.sessionMu.Unlock()
	// Cookies rotate on HTTP responses. Encode while holding the same lock
	// as their writers, then release it before touching the filesystem.
	auth.CookiesLock.RLock()
	data, err := json.Marshal(auth)
	auth.CookiesLock.RUnlock()
	if err != nil {
		return err
	}
	return writePrivateJSON(p.SessionFile(), data)

}

// ClearSession removes stored credentials, returning the daemon to unpaired.
func (p *Paths) ClearSession() error {
	p.sessionMu.Lock()
	defer p.sessionMu.Unlock()
	if err := os.Remove(p.SessionFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.RemoveAll(p.MediaDir()); err != nil {
		return err
	}
	return os.MkdirAll(p.MediaDir(), 0o700)
}

// writePrivateJSON uses a unique private temporary file in the destination
// directory. Readers see either the old complete document or the new one.
func writePrivateJSON(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".omachat-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
