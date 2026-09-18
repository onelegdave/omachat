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
	_, credErr := d.config.TelegramCredentials()
	return wire.ConfigResult{
		EnabledServices:          enabled,
		ServiceSelectionRequired: required,
		RestartRequired:          d.restartPending,
		UiScale:                  scale,
		TelegramConfigured:       credErr == nil,
		TelegramAPIID:            cfg.TelegramAPIID,
	}
}

func (d *Daemon) SetUiScale(scale float64) error {
	return d.config.SetUiScale(scale)
}
