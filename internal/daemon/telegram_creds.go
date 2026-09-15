package daemon

import (
	"context"
	"strings"

	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

// SetTelegramCredentials validates and persists Telegram API credentials,
// never logs or exposes the API hash, and updates the Telegram backend state.
func (d *Daemon) SetTelegramCredentials(ctx context.Context, apiID int, apiHash string) error {
	apiHash = strings.TrimSpace(apiHash)

	// Removal / deletion case
	if apiID == 0 && apiHash == "" {
		if err := d.config.SetTelegramCredentials(0, ""); err != nil {
			return err
		}
		if d.tg != nil && d.serviceEnabled(wire.NetworkTelegram) {
			d.tg.TriggerUpdateCredentials()
		}
		return nil
	}

	// Partial update support:
	// If only one field is provided, check if the other field is already configured.
	current := d.config.Get()
	if apiID == 0 && apiHash != "" && current.TelegramAPIID > 0 {
		apiID = current.TelegramAPIID
	}
	if apiID > 0 && apiHash == "" && current.TelegramAPIHash != "" {
		apiHash = current.TelegramAPIHash
	}

	// Validate credentials without leaking secret values in error messages
	if _, err := store.ResolveTelegramCredentials(store.Config{
		TelegramAPIID:   apiID,
		TelegramAPIHash: apiHash,
	}); err != nil {
		return err
	}

	// Persist to private config atomically
	if err := d.config.SetTelegramCredentials(apiID, apiHash); err != nil {
		return err
	}

	// Update the Telegram backend state
	if d.tg != nil && d.serviceEnabled(wire.NetworkTelegram) {
		d.tg.TriggerUpdateCredentials()
	}

	return nil
}
