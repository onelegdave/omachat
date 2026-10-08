package whatsapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const historyDatabaseFileName = "whatsapp_history.sqlite"

func cleanupHistoryIndexTemps(path string) error {
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), historyIndexTempPrefix) || !strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected non-regular WhatsApp history temp %q", entry.Name())
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func historyArtifactBytes(indexPath string) (int64, error) {
	dir := filepath.Dir(indexPath)
	paths := []string{indexPath}
	database := filepath.Join(dir, historyDatabaseFileName)
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		paths = append(paths, database+suffix)
	}
	var total int64
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return 0, err
		}
		if !info.Mode().IsRegular() {
			return 0, fmt.Errorf("WhatsApp history artifact %q is not a regular file", path)
		}
		if info.Size() > 0 && total > int64(^uint64(0)>>1)-info.Size() {
			return 0, errors.New("WhatsApp history artifact size overflow")
		}
		total += info.Size()
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return total, nil
		}
		return 0, err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), historyIndexTempPrefix) || !strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return 0, err
		}
		if !info.Mode().IsRegular() {
			return 0, fmt.Errorf("WhatsApp history temp %q is not a regular file", entry.Name())
		}
		if info.Size() > 0 && total > int64(^uint64(0)>>1)-info.Size() {
			return 0, errors.New("WhatsApp history artifact size overflow")
		}
		total += info.Size()
	}
	return total, nil
}

func historyDatabaseArtifactBytes(database string) (int64, error) {
	var total int64
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		path := database + suffix
		info, err := os.Lstat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return 0, err
		}
		if !info.Mode().IsRegular() {
			return 0, fmt.Errorf("WhatsApp history database artifact %q is not a regular file", path)
		}
		if info.Size() > 0 && total > int64(^uint64(0)>>1)-info.Size() {
			return 0, errors.New("WhatsApp history database artifact size overflow")
		}
		total += info.Size()
	}
	return total, nil
}

func checkHistoryAtomicWriteBudget(path string, newFileBytes, totalBudget int64) error {
	if totalBudget <= 0 || newFileBytes < 0 {
		return errors.New("WhatsApp history write budget must be positive")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := cleanupHistoryIndexTemps(path); err != nil {
		return err
	}
	current, err := historyArtifactBytes(path)
	if err != nil {
		return err
	}
	if newFileBytes > totalBudget || current > totalBudget-newFileBytes {
		return fmt.Errorf("WhatsApp history atomic write would exceed its %d-byte disk budget", totalBudget)
	}
	return nil
}

func saveChatStoreWithBudget(path string, stored *StoredChatData, totalBudget int64) error {
	if stored == nil {
		return nil
	}
	if totalBudget <= 0 {
		return errors.New("WhatsApp history disk budget must be positive")
	}
	boundStoredChat(stored)
	data, err := json.Marshal(*stored)
	if err != nil {
		return err
	}
	if err := checkHistoryAtomicWriteBudget(path, int64(len(data)), totalBudget); err != nil {
		return err
	}
	return writeHistoryIndexAtomically(path, data)
}
