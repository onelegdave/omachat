package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Config holds daemon preferences that outlive a session.
//
// It lives beside the session rather than in the plugin's shell.json entry
// because the daemon needs it while running headless — the background cookie
// sync must know which browser profile to read, with no panel open.
type Config struct {
	// BrowserProfile is the profile name to take Google cookies from, as
	// reported by the browser scan (e.g. "Chrome / Profile 1"). Empty means
	// choose automatically.
	BrowserProfile string `json:"browserProfile,omitempty"`

	// GiphyAPIKey enables GIF search. GIPHY issues free keys; without one the
	// GIF picker explains how to get it rather than failing silently.
	GiphyAPIKey string `json:"giphyApiKey,omitempty"`

	// UiScale multiplies panel type. 1 is the theme default. 0 means unset
	// and is treated as 1 when read.
	UiScale float64 `json:"uiScale,omitempty"`

	// TelegramAPIID is the application api_id from my.telegram.org.
	TelegramAPIID int `json:"telegramApiID,omitempty"`

	// TelegramAPIHash is the application api_hash (32-character hex) from my.telegram.org.
	TelegramAPIHash string `json:"telegramApiHash,omitempty"`
}

type configAlias Config

// UnmarshalJSON unmarshals Config, accepting both camelCase (telegramApiID,
// telegramApiHash) and snake_case (telegram_api_id, telegram_api_hash) keys.
func (c *Config) UnmarshalJSON(data []byte) error {
	var aux struct {
		configAlias
		AltTelegramAPIID   int    `json:"telegram_api_id,omitempty"`
		AltTelegramAPIHash string `json:"telegram_api_hash,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*c = Config(aux.configAlias)
	if c.TelegramAPIID == 0 && aux.AltTelegramAPIID != 0 {
		c.TelegramAPIID = aux.AltTelegramAPIID
	}
	if c.TelegramAPIHash == "" && aux.AltTelegramAPIHash != "" {
		c.TelegramAPIHash = aux.AltTelegramAPIHash
	}
	return nil
}

// ConfigStore reads and writes the config file.
type ConfigStore struct {
	path string

	mu     sync.RWMutex
	loaded Config
}

// ConfigFile is where preferences are persisted.
func (p *Paths) ConfigFile() string { return filepath.Join(p.Data, "config.json") }

// NewConfigStore loads the config, treating a missing or corrupt file as
// defaults rather than an error: a bad config must never stop the daemon.
func NewConfigStore(path string) *ConfigStore {
	cs := &ConfigStore{path: path}
	f, err := os.Open(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return cs
		}
		return cs
	}
	defer f.Close()
	_ = json.NewDecoder(f).Decode(&cs.loaded)
	return cs
}

// Get returns a copy of the current config.
func (c *ConfigStore) Get() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.loaded
}

// SetBrowserProfile records the chosen profile and persists it atomically.
func (c *ConfigStore) SetBrowserProfile(name string) error {
	c.mu.Lock()
	c.loaded.BrowserProfile = name
	defer c.mu.Unlock()
	return c.saveLocked()
}

// SetGiphyAPIKey stores the GIF search key.
func (c *ConfigStore) SetGiphyAPIKey(key string) error {
	c.mu.Lock()
	c.loaded.GiphyAPIKey = key
	defer c.mu.Unlock()
	return c.saveLocked()
}

// SetUiScale persists the panel type scale, clamped to a usable range.
func (c *ConfigStore) SetUiScale(scale float64) error {
	if scale < 0.8 {
		scale = 0.8
	}
	if scale > 1.5 {
		scale = 1.5
	}
	c.mu.Lock()
	c.loaded.UiScale = scale
	defer c.mu.Unlock()
	return c.saveLocked()
}

// Caller holds mu so updates and their on-disk order agree.
func (c *ConfigStore) saveLocked() error {
	data, err := json.Marshal(&c.loaded)
	if err != nil {
		return err
	}
	return writePrivateJSON(c.path, data)
}

// SetTelegramCredentials records the Telegram API credentials and persists them atomically.
func (c *ConfigStore) SetTelegramCredentials(apiID int, apiHash string) error {
	c.mu.Lock()
	c.loaded.TelegramAPIID = apiID
	c.loaded.TelegramAPIHash = strings.TrimSpace(apiHash)
	defer c.mu.Unlock()
	return c.saveLocked()
}

// SetTelegramAPIID records the Telegram API ID and persists it atomically.
func (c *ConfigStore) SetTelegramAPIID(apiID int) error {
	c.mu.Lock()
	c.loaded.TelegramAPIID = apiID
	defer c.mu.Unlock()
	return c.saveLocked()
}

// SetTelegramAPIHash records the Telegram API hash and persists it atomically.
func (c *ConfigStore) SetTelegramAPIHash(apiHash string) error {
	c.mu.Lock()
	c.loaded.TelegramAPIHash = strings.TrimSpace(apiHash)
	defer c.mu.Unlock()
	return c.saveLocked()
}

// TelegramCredentials holds validated credentials for the Telegram MTProto client.
type TelegramCredentials struct {
	APIID   int
	APIHash string
}

// ErrTelegramUnconfigured indicates that Telegram API credentials have not been configured.
var ErrTelegramUnconfigured = errors.New("Telegram credentials not configured: api_id and api_hash required")

func isHex32(s string) bool {
	if len(s) != 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if !((b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')) {
			return false
		}
	}
	return true
}

// TelegramCredentials resolves and validates Telegram API credentials from Config.
// Config takes precedence over OMACHAT_TELEGRAM_API_ID and OMACHAT_TELEGRAM_API_HASH.
// Returns validated credentials or an explicit unconfigured or validation error.
// Secrets are never logged or exposed in error messages.
func (c Config) TelegramCredentials() (TelegramCredentials, error) {
	return ResolveTelegramCredentials(c)
}

// TelegramCredentials resolves and validates Telegram API credentials from the stored config.
func (c *ConfigStore) TelegramCredentials() (TelegramCredentials, error) {
	if c == nil {
		return Config{}.TelegramCredentials()
	}
	return c.Get().TelegramCredentials()
}

// ResolveTelegramCredentials resolves and validates Telegram credentials from Config with environment fallback.
func ResolveTelegramCredentials(c Config) (TelegramCredentials, error) {
	var apiID int
	var idProvided bool

	if c.TelegramAPIID != 0 {
		idProvided = true
		if c.TelegramAPIID <= 0 {
			return TelegramCredentials{}, errors.New("Telegram api_id must be a positive integer")
		}
		apiID = c.TelegramAPIID
	} else if envID := strings.TrimSpace(os.Getenv("OMACHAT_TELEGRAM_API_ID")); envID != "" {
		idProvided = true
		parsed, err := strconv.Atoi(envID)
		if err != nil {
			return TelegramCredentials{}, errors.New("OMACHAT_TELEGRAM_API_ID must be a valid integer")
		}
		if parsed <= 0 {
			return TelegramCredentials{}, errors.New("OMACHAT_TELEGRAM_API_ID must be a positive integer")
		}
		apiID = parsed
	}

	var apiHash string
	var hashProvided bool

	if c.TelegramAPIHash != "" {
		hashProvided = true
		h := strings.TrimSpace(c.TelegramAPIHash)
		if len(h) != 32 || !isHex32(h) {
			return TelegramCredentials{}, errors.New("Telegram api_hash must be a 32-character hexadecimal string")
		}
		apiHash = h
	} else if envHash := strings.TrimSpace(os.Getenv("OMACHAT_TELEGRAM_API_HASH")); envHash != "" {
		hashProvided = true
		if len(envHash) != 32 || !isHex32(envHash) {
			return TelegramCredentials{}, errors.New("OMACHAT_TELEGRAM_API_HASH must be a 32-character hexadecimal string")
		}
		apiHash = envHash
	}

	if !idProvided && !hashProvided {
		return TelegramCredentials{}, ErrTelegramUnconfigured
	}
	if !idProvided {
		return TelegramCredentials{}, fmt.Errorf("%w: api_id is missing", ErrTelegramUnconfigured)
	}
	if !hashProvided {
		return TelegramCredentials{}, fmt.Errorf("%w: api_hash is missing", ErrTelegramUnconfigured)
	}

	return TelegramCredentials{
		APIID:   apiID,
		APIHash: apiHash,
	}, nil
}
