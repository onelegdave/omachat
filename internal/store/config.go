package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Config holds daemon preferences that outlive a session.
//
// It lives beside the session rather than in the plugin's shell.json entry
// because the daemon needs it while running headless — the background cookie
// sync must know which browser profile to read, with no panel open.
type Config struct {
	// Nil migrates older installations; an explicit empty list disables all services.
	EnabledServices          *[]string `json:"enabledServices,omitempty"`
	ServiceSelectionRequired bool      `json:"serviceSelectionRequired,omitempty"`
	// BrowserProfile is the profile name to take Google cookies from, as
	// reported by the browser scan (e.g. "Chrome / Profile 1"). Empty means
	// choose automatically.
	BrowserProfile string `json:"browserProfile,omitempty"`

	KeyboardShortcuts           map[string]string `json:"keyboardShortcuts,omitempty"`
	UnifiedInboxEnabled         bool              `json:"unifiedInboxEnabled,omitempty"`
	KeepPreviousEmojiSearchText bool              `json:"keepPreviousEmojiSearchText,omitempty"`
	WhatsAppHistoryCacheMB      int               `json:"whatsappHistoryCacheMB,omitempty"`

	// UiScale multiplies panel type. 1 is the theme default. 0 means unset
	// and is treated as 1 when read.
	UiScale              float64           `json:"uiScale,omitempty"`
	NotificationsEnabled *bool             `json:"notificationsEnabled,omitempty"`
	NotificationPreviews bool              `json:"notificationPreviews,omitempty"`
	LastService          string            `json:"lastService,omitempty"`
	SidebarCollapsed     bool              `json:"sidebarCollapsed,omitempty"`
	LastConversations    map[string]string `json:"lastConversations,omitempty"`

	// AlwaysPopout opens OmaChat in its standalone window instead of the
	// anchored bar panel. PopoutMode is "floating" or "tiled".
	AlwaysPopout bool   `json:"alwaysPopout,omitempty"`
	PopoutMode   string `json:"popoutMode,omitempty"`

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
	cs.forgetRetiredKeys(path)
	return cs
}

// retiredConfigKeys were written by earlier releases and hold credentials the
// daemon no longer uses. They are deleted on startup so a removed feature does
// not leave its secret behind on disk.
var retiredConfigKeys = []string{"giphyApiKey"}

// forgetRetiredKeys removes retired keys from the config file if any are
// present. Failure is ignored: a stale key must never stop the daemon.
func (c *ConfigStore) forgetRetiredKeys(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return
	}
	stale := false
	for _, key := range retiredConfigKeys {
		if _, ok := raw[key]; ok {
			stale = true
		}
	}
	if !stale {
		return
	}
	_ = c.updateLocked(func(cfg map[string]any, loaded *Config) {
		for _, key := range retiredConfigKeys {
			delete(cfg, key)
		}
	})
}

// Get returns a copy of the current config.
func (c *ConfigStore) Get() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := c.loaded
	if out.WhatsAppHistoryCacheMB != 64 && out.WhatsAppHistoryCacheMB != 128 && out.WhatsAppHistoryCacheMB != 256 && out.WhatsAppHistoryCacheMB != 512 {
		out.WhatsAppHistoryCacheMB = 128
	}
	if out.EnabledServices != nil {
		services := append([]string{}, (*out.EnabledServices)...)
		out.EnabledServices = &services
	}
	if out.KeyboardShortcuts != nil {
		shortcuts := make(map[string]string, len(out.KeyboardShortcuts))
		for id, shortcut := range out.KeyboardShortcuts {
			shortcuts[id] = shortcut
		}
		out.KeyboardShortcuts = shortcuts
	}
	if out.NotificationsEnabled != nil {
		enabled := *out.NotificationsEnabled
		out.NotificationsEnabled = &enabled
	}
	if out.LastConversations != nil {
		conversations := make(map[string]string, len(out.LastConversations))
		for network, id := range out.LastConversations {
			conversations[network] = id
		}
		out.LastConversations = conversations
	}
	return out
}

func (c Config) NotificationsOn() bool {
	return c.NotificationsEnabled == nil || *c.NotificationsEnabled
}

func (c *ConfigStore) SetNotifications(enabled, previews *bool) error {
	if enabled == nil && previews == nil {
		return errors.New("notification preferences are required")
	}
	return c.updateLocked(func(cfg map[string]any, loaded *Config) {
		if enabled != nil {
			value := *enabled
			cfg["notificationsEnabled"] = value
			loaded.NotificationsEnabled = &value
		}
		if previews != nil {
			cfg["notificationPreviews"] = *previews
			loaded.NotificationPreviews = *previews
		}
	})
}

// SetChatView merges only supplied view preferences so service switches cannot
// overwrite another service's remembered conversation.
func (c *ConfigStore) SetChatView(network string, conversationID, lastService *string, sidebarCollapsed *bool) error {
	knownService := func(service string) bool {
		return service == "gmessages" || service == "whatsapp" || service == "telegram" || service == "messenger" || service == "signal"
	}
	if lastService != nil && !knownService(*lastService) {
		return errors.New("unknown last service")
	}
	if conversationID != nil && (!knownService(network) || len(*conversationID) > 1024) {
		return errors.New("invalid remembered conversation")
	}
	return c.updateLocked(func(cfg map[string]any, loaded *Config) {
		if lastService != nil {
			cfg["lastService"] = *lastService
			loaded.LastService = *lastService
		}
		if sidebarCollapsed != nil {
			cfg["sidebarCollapsed"] = *sidebarCollapsed
			loaded.SidebarCollapsed = *sidebarCollapsed
		}
		if conversationID != nil {
			if loaded.LastConversations == nil {
				loaded.LastConversations = make(map[string]string)
			}
			if *conversationID == "" {
				delete(loaded.LastConversations, network)
			} else {
				loaded.LastConversations[network] = *conversationID
			}
			cfg["lastConversations"] = loaded.LastConversations
		}
	})
}

// EnabledServices preserves legacy installations while fresh installs require a choice.
func (c *ConfigStore) EnabledServices(paths *Paths) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg := c.loaded
	if cfg.EnabledServices != nil {
		return append([]string{}, (*cfg.EnabledServices)...), cfg.ServiceSelectionRequired
	}
	_, err := os.Stat(c.path)
	if errors.Is(err, os.ErrNotExist) && (paths == nil || !paths.HasAccountEvidence()) {
		empty := []string{}
		c.loaded.EnabledServices = &empty
		c.loaded.ServiceSelectionRequired = true
		return []string{}, true
	}
	return []string{"gmessages", "whatsapp", "telegram"}, false
}

// updateLocked serializes writers, reloads disk state, and merges only the
// requested fields. The lock file stays in place across atomic config renames.
func (c *ConfigStore) updateLocked(modify func(cfg map[string]any, loaded *Config)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	lockPath := c.path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return fmt.Errorf("failed to open config lock file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("config lock must be a regular file")
	}
	if err := f.Chmod(0600); err != nil {
		return fmt.Errorf("secure config lock: %w", err)
	}

	for i := 0; i < 50; i++ {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			break
		}
		if err != syscall.EWOULDBLOCK {
			return fmt.Errorf("failed to lock config: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return errors.New("another process is currently updating the configuration, please try again")
	}

	var cfg map[string]any
	data, err := os.ReadFile(c.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to read config for update: %w", err)
		}
		cfg = make(map[string]any)
	} else {
		if !json.Valid(data) {
			return errors.New("configuration is invalid JSON; it was not changed")
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&cfg); err != nil || cfg == nil {
			return errors.New("configuration must be a JSON object; it was not changed")
		}
	}

	// Preserve only the unsaved fresh-install chooser, not stale credentials or
	// preferences removed by another writer. Everything else comes from disk.
	if _, exists := cfg["enabledServices"]; !exists && c.loaded.ServiceSelectionRequired {
		cfg["enabledServices"] = []string{}
		cfg["serviceSelectionRequired"] = true
	}

	var nextLoaded Config
	if b, err := json.Marshal(cfg); err != nil {
		return errors.New("configuration could not be encoded; it was not changed")
	} else if err := json.Unmarshal(b, &nextLoaded); err != nil {
		return errors.New("configuration has invalid field types; it was not changed")
	}

	modify(cfg, &nextLoaded)

	newData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// writePrivateJSON uses 0600 tempfile and rename, preserving safety
	if err := writePrivateJSON(c.path, append(newData, '\n')); err != nil {
		return err
	}

	c.loaded = nextLoaded

	return nil
}

// SetEnabledServices validates and persists before committing the in-memory value.
func (c *ConfigStore) SetEnabledServices(services []string) error {
	if services == nil {
		return errors.New("enabledServices must be an array, including [] for no services")
	}
	seen := make(map[string]bool)
	for _, service := range services {
		if service != "gmessages" && service != "whatsapp" && service != "telegram" && service != "messenger" && service != "signal" {
			return fmt.Errorf("unknown service %q", service)
		}
		if seen[service] {
			return fmt.Errorf("duplicate service %q", service)
		}
		seen[service] = true
	}
	canonical := []string{}
	for _, service := range []string{"gmessages", "whatsapp", "telegram", "messenger", "signal"} {
		if seen[service] {
			canonical = append(canonical, service)
		}
	}
	return c.updateLocked(func(cfg map[string]any, loaded *Config) {
		cfg["enabledServices"] = canonical
		cfg["serviceSelectionRequired"] = false
		loaded.EnabledServices = &canonical
		loaded.ServiceSelectionRequired = false
	})
}

// SetBrowserProfile records the chosen profile and persists it atomically.
func (c *ConfigStore) SetBrowserProfile(name string) error {
	return c.updateLocked(func(cfg map[string]any, loaded *Config) {
		cfg["browserProfile"] = name
		loaded.BrowserProfile = name
	})
}

// SetUiScale persists the panel type scale, clamped to a usable range.
func (c *ConfigStore) SetUiScale(scale float64) error {
	if scale < 0.8 {
		scale = 0.8
	}
	if scale > 1.5 {
		scale = 1.5
	}
	return c.updateLocked(func(cfg map[string]any, loaded *Config) {
		cfg["uiScale"] = scale
		loaded.UiScale = scale
	})
}

// SetWindowPreferences persists how OmaChat opens from the bar.
func (c *ConfigStore) SetWindowPreferences(alwaysPopout bool, mode string) error {
	if mode != "floating" && mode != "tiled" {
		return fmt.Errorf("invalid pop-out mode %q", mode)
	}
	return c.updateLocked(func(cfg map[string]any, loaded *Config) {
		cfg["alwaysPopout"] = alwaysPopout
		cfg["popoutMode"] = mode
		loaded.AlwaysPopout = alwaysPopout
		loaded.PopoutMode = mode
	})
}

// SetKeepPreviousEmojiSearchText persists whether the emoji picker keeps its last query.
func (c *ConfigStore) SetKeepPreviousEmojiSearchText(keep bool) error {
	return c.updateLocked(func(cfg map[string]any, loaded *Config) {
		cfg["keepPreviousEmojiSearchText"] = keep
		loaded.KeepPreviousEmojiSearchText = keep
	})
}

// SetWhatsAppHistoryCacheMB persists the local WhatsApp history budget.
func (c *ConfigStore) SetWhatsAppHistoryCacheMB(size int) error {
	if size != 64 && size != 128 && size != 256 && size != 512 {
		return fmt.Errorf("invalid WhatsApp history cache size %d MB", size)
	}
	return c.updateLocked(func(cfg map[string]any, loaded *Config) {
		cfg["whatsappHistoryCacheMB"] = size
		loaded.WhatsAppHistoryCacheMB = size
	})
}

var defaultKeyboardShortcuts = map[string][]string{
	"help": {"?", "Ctrl+Shift+p"}, "search": {"/", "Ctrl+f"}, "compose": {"i"},
	"history": {"m"}, "nextUnread": {"u"}, "newConversation": {"Ctrl+n"},
	"settings": {"Ctrl+,"}, "emojiPicker": {"Ctrl+e"}, "attach": {"Ctrl+o"},
	"refresh": {"r", "Ctrl+r"}, "toggleUnifiedInbox": {"Ctrl+Shift+a"}, "service.gmessages": {"1"}, "service.whatsapp": {"2"},
	"service.telegram": {"3"}, "service.messenger": {"4"}, "service.signal": {"5"},
}

func normalizeKeyboardShortcut(value string) (string, error) {
	parts := strings.Split(strings.TrimSpace(value), "+")
	if len(parts) == 0 || len(parts) > 4 {
		return "", fmt.Errorf("invalid shortcut")
	}
	modifiers := map[string]bool{}
	for _, part := range parts[:len(parts)-1] {
		key := strings.ToLower(strings.TrimSpace(part))
		aliases := map[string]string{"ctrl": "Ctrl", "control": "Ctrl", "alt": "Alt", "option": "Alt", "meta": "Meta", "super": "Meta", "shift": "Shift"}
		modifier, ok := aliases[key]
		if !ok || modifiers[modifier] {
			return "", fmt.Errorf("invalid or duplicated modifier")
		}
		modifiers[modifier] = true
	}
	key := strings.TrimSpace(parts[len(parts)-1])
	if len(key) != 1 || strings.ContainsRune("+ 	\r\n", rune(key[0])) {
		return "", fmt.Errorf("shortcut key must be one printable character")
	}
	key = strings.ToLower(key)
	if strings.ContainsRune("?/,.-", rune(key[0])) {
		delete(modifiers, "Shift")
	}
	var prefix []string
	for _, modifier := range []string{"Ctrl", "Alt", "Meta", "Shift"} {
		if modifiers[modifier] {
			prefix = append(prefix, modifier)
		}
	}
	return strings.Join(append(prefix, key), "+"), nil
}

func isReservedKeyboardShortcut(shortcut string) bool {
	return strings.Contains(" Tab Backtab Left Right Up Down Home End PageUp PageDown Enter Return Escape Space Backspace Delete ", " "+shortcut+" ")
}

func hasKeyboardModifier(shortcut string) bool {
	return strings.HasPrefix(shortcut, "Ctrl+") || strings.HasPrefix(shortcut, "Alt+") || strings.HasPrefix(shortcut, "Meta+")
}

func validateKeyboardShortcuts(shortcuts map[string]string) error {
	if len(shortcuts) > len(defaultKeyboardShortcuts) {
		return fmt.Errorf("too many shortcut assignments")
	}
	for id := range shortcuts {
		if _, ok := defaultKeyboardShortcuts[id]; !ok {
			return fmt.Errorf("unknown keyboard action %q", id)
		}
	}
	effective := make(map[string][]string, len(defaultKeyboardShortcuts))
	for id, defaults := range defaultKeyboardShortcuts {
		custom, ok := shortcuts[id]
		if !ok {
			effective[id] = defaults
			continue
		}
		if strings.TrimSpace(custom) == "" {
			effective[id] = nil
			continue
		}
		normalized, err := normalizeKeyboardShortcut(custom)
		if err != nil {
			return fmt.Errorf("invalid shortcut for %s: %w", id, err)
		}
		if isReservedKeyboardShortcut(strings.Split(normalized, "+")[len(strings.Split(normalized, "+"))-1]) {
			return fmt.Errorf("shortcut %q is reserved for keyboard navigation", normalized)
		}
		effective[id] = []string{normalized}
	}
	ids := make([]string, 0, len(effective))
	for id := range effective {
		ids = append(ids, id)
	}
	for i, firstID := range ids {
		for _, secondID := range ids[i+1:] {
			for _, first := range effective[firstID] {
				for _, second := range effective[secondID] {
					if first == second {
						return fmt.Errorf("shortcut %q is assigned to multiple actions", first)
					}
				}
			}
		}
	}
	return nil
}

// SetKeyboardShortcuts persists validated user overrides. Empty values unbind
// an action; removing an override restores that action's built-in shortcuts.
func (c *ConfigStore) SetKeyboardShortcuts(shortcuts map[string]string) error {
	if err := validateKeyboardShortcuts(shortcuts); err != nil {
		return err
	}
	copyOfShortcuts := make(map[string]string, len(shortcuts))
	for id, shortcut := range shortcuts {
		copyOfShortcuts[id] = shortcut
	}
	return c.updateLocked(func(cfg map[string]any, loaded *Config) {
		if len(copyOfShortcuts) == 0 {
			delete(cfg, "keyboardShortcuts")
			loaded.KeyboardShortcuts = nil
			return
		}
		cfg["keyboardShortcuts"] = copyOfShortcuts
		loaded.KeyboardShortcuts = copyOfShortcuts
	})
}

// SetUnifiedInboxEnabled persists the user's unified inbox preference.
func (c *ConfigStore) SetUnifiedInboxEnabled(enabled bool) error {
	return c.updateLocked(func(cfg map[string]any, loaded *Config) {
		cfg["unifiedInboxEnabled"] = enabled
		loaded.UnifiedInboxEnabled = enabled
	})
}

// SetTelegramCredentials records the Telegram API credentials and persists them atomically.
func (c *ConfigStore) SetTelegramCredentials(apiID int, apiHash string) error {
	apiHash = strings.TrimSpace(apiHash)
	return c.updateLocked(func(cfg map[string]any, loaded *Config) {
		cfg["telegramApiID"] = apiID
		cfg["telegramApiHash"] = apiHash
		loaded.TelegramAPIID = apiID
		loaded.TelegramAPIHash = apiHash
	})
}

// SetTelegramAPIID records the Telegram API ID and persists it atomically.
func (c *ConfigStore) SetTelegramAPIID(apiID int) error {
	return c.updateLocked(func(cfg map[string]any, loaded *Config) {
		cfg["telegramApiID"] = apiID
		loaded.TelegramAPIID = apiID
	})
}

// SetTelegramAPIHash records the Telegram API hash and persists it atomically.
func (c *ConfigStore) SetTelegramAPIHash(apiHash string) error {
	apiHash = strings.TrimSpace(apiHash)
	return c.updateLocked(func(cfg map[string]any, loaded *Config) {
		cfg["telegramApiHash"] = apiHash
		loaded.TelegramAPIHash = apiHash
	})
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
