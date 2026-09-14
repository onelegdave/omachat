package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
