// Package store owns on-disk locations and session persistence.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"

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
	for _, dir := range []string{p.Data, p.Cache, p.Runtime, p.MediaDir(), p.WhatsAppMediaDir(), p.TelegramMediaDir(), p.MessengerMediaDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create %s: %w", dir, err)
		}
		f, err := os.OpenFile(dir, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return nil, fmt.Errorf("open to secure %s: %w", dir, err)
		}
		err = f.Chmod(0o700)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("secure %s: %w", dir, err)
		}
	}
	return p, nil
}

// SessionFile holds the paired-device credentials. Treat as a secret.
func (p *Paths) SessionFile() string { return filepath.Join(p.Data, "session.json") }

// WhatsAppDBFile is where the SQLite database for WhatsApp session is stored.
func (p *Paths) WhatsAppDBFile() string { return filepath.Join(p.Data, "whatsapp.db") }

// SocketPath is where the plugin connects.
func (p *Paths) SocketPath() string { return filepath.Join(p.Runtime, "daemon.sock") }

// MediaDir caches downloaded attachments and avatars for Google Messages.
func (p *Paths) MediaDir() string { return filepath.Join(p.Cache, "media") }

// WhatsAppMediaDir caches downloaded attachments and avatars for WhatsApp.
func (p *Paths) WhatsAppMediaDir() string { return filepath.Join(p.Cache, "media_whatsapp") }

// TelegramMediaDir caches downloaded attachments and avatars for Telegram.
func (p *Paths) TelegramMediaDir() string { return filepath.Join(p.Cache, "media_telegram") }

// TelegramSessionFile holds the session data for Telegram. Treat as a secret.
func (p *Paths) TelegramSessionFile() string { return filepath.Join(p.Data, "telegram.session") }

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

// ClearSession removes stored Google credentials, returning the Google client to unpaired.
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

// WhatsAppStoreFile holds the local cache of WhatsApp conversations and messages.
func (p *Paths) WhatsAppStoreFile() string { return filepath.Join(p.Data, "whatsapp_store.json") }

// ClearWhatsAppSession removes stored WhatsApp credentials/database, conversation cache, and media caches, returning WhatsApp to unpaired.
func (p *Paths) ClearWhatsAppSession() error {
	p.sessionMu.Lock()
	defer p.sessionMu.Unlock()
	for _, ext := range []string{"", "-wal", "-shm", "-journal"} {
		f := p.WhatsAppDBFile() + ext
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Remove(p.WhatsAppStoreFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.RemoveAll(p.WhatsAppMediaDir()); err != nil {
		return err
	}
	return os.MkdirAll(p.WhatsAppMediaDir(), 0o700)
}

// TelegramStoreFile holds the local cache of Telegram conversations and messages.
func (p *Paths) TelegramStoreFile() string { return filepath.Join(p.Data, "telegram_store.json") }

// ClearTelegramSession removes stored Telegram credentials/session, conversation cache, and media caches, returning Telegram to unpaired.
func (p *Paths) ClearTelegramSession() error {
	p.sessionMu.Lock()
	defer p.sessionMu.Unlock()
	if err := os.Remove(p.TelegramSessionFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(p.TelegramStoreFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.RemoveAll(p.TelegramMediaDir()); err != nil {
		return err
	}
	return os.MkdirAll(p.TelegramMediaDir(), 0o700)
}

// WritePrivateJSON uses a unique private temporary file (0600) in the destination
// directory. Readers see either the old complete document or the new one.
func WritePrivateJSON(path string, data []byte) error {
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

func writePrivateJSON(path string, data []byte) error {
	return WritePrivateJSON(path, data)
}

// WritePrivateFile writes data to path using a 0600 tempfile and rename so a
// destination symlink is not followed.
func WritePrivateFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".omachat-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// MessengerDBFile is where the SQLite database for Messenger session is stored (if mautrix-meta uses one).
func (p *Paths) MessengerDBFile() string { return filepath.Join(p.Data, "messenger.db") }

// MessengerMediaDir caches downloaded attachments and avatars for Messenger.
func (p *Paths) MessengerMediaDir() string { return filepath.Join(p.Cache, "media_messenger") }

// MessengerStoreFile holds the local cache of Messenger conversations and messages.
func (p *Paths) MessengerStoreFile() string { return filepath.Join(p.Data, "messenger_store.json") }

// ClearMessengerSession removes stored Messenger credentials/database, conversation cache, and media caches.
func (p *Paths) ClearMessengerSession() error {
	p.sessionMu.Lock()
	defer p.sessionMu.Unlock()
	if err := os.Remove(p.MessengerSessionFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, ext := range []string{"", "-wal", "-shm", "-journal"} {
		f := p.MessengerDBFile() + ext
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Remove(p.MessengerStoreFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.RemoveAll(p.MessengerMediaDir()); err != nil {
		return err
	}
	return os.MkdirAll(p.MessengerMediaDir(), 0o700)
}

// MessengerSessionFile holds the Messenger browser cookies.
func (p *Paths) MessengerSessionFile() string { return filepath.Join(p.Data, "messenger_session.json") }
