package whatsapp

import (
	"errors"
	"fmt"
	"os"
)

func loadChatStoreWithBudget(path string, byteBudget int64) (*StoredChatData, error) {
	if byteBudget <= 0 {
		return newStoredChatData(), errors.New("WhatsApp history disk budget must be positive")
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return newStoredChatData(), nil
		}
		return newStoredChatData(), err
	}
	if info.Size() > byteBudget {
		return newStoredChatData(), fmt.Errorf("legacy WhatsApp history cache is %d bytes, above the %d-byte migration limit; preserving it without loading", info.Size(), byteBudget)
	}
	return loadChatStore(path)
}
