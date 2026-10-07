package daemon

import "github.com/onelegdave/omachat/internal/wire"

// PluginConfig is the panel-safe snapshot of daemon preferences.
func (d *Daemon) PluginConfig() wire.ConfigResult {
	d.servicesMu.Lock()
	defer d.servicesMu.Unlock()
	cfg := d.config.Get()
	enabled, required := d.config.EnabledServices(d.paths)
	scale := cfg.UiScale
	if scale <= 0 {
		scale = 1
	}
	mode := cfg.PopoutMode
	if mode != "tiled" {
		mode = "floating"
	}
	_, credErr := d.config.TelegramCredentials()
	return wire.ConfigResult{
		EnabledServices:          enabled,
		ServiceSelectionRequired: required,
		RestartRequired:          d.restartPending,
		UiScale:                  scale,
		LastService:              cfg.LastService,
		SidebarCollapsed:         cfg.SidebarCollapsed,
		LastConversations:        cfg.LastConversations,
		NotificationsEnabled:     cfg.NotificationsOn(),
		NotificationPreviews:     cfg.NotificationPreviews,
		AlwaysPopout:             cfg.AlwaysPopout,
		PopoutMode:               mode,
		KeyboardShortcuts:        cfg.KeyboardShortcuts,
		TelegramConfigured:       credErr == nil,
		TelegramAPIID:            cfg.TelegramAPIID,
	}
}

func (d *Daemon) SetUiScale(scale float64) error {
	return d.config.SetUiScale(scale)
}

func (d *Daemon) SetWindowPreferences(alwaysPopout bool, mode string) error {
	return d.config.SetWindowPreferences(alwaysPopout, mode)
}

func (d *Daemon) SetKeyboardShortcuts(shortcuts map[string]string) error {
	return d.config.SetKeyboardShortcuts(shortcuts)
}

func (d *Daemon) SetKeepPreviousEmojiSearchText(keep bool) error {
	return d.config.SetKeepPreviousEmojiSearchText(keep)
}
