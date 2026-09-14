package store

import "os"

// HasAccountEvidence returns true if there is any stored account or session evidence.
func (p *Paths) HasAccountEvidence() bool {
	files := []string{
		p.SessionFile(),
		p.WhatsAppDBFile(),
		p.TelegramSessionFile(),
		p.WhatsAppStoreFile(),
		p.TelegramStoreFile(),
	}
	for _, f := range files {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			return true
		}
	}
	return false
}
