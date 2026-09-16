package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Each service owns a separate budget. Dotfiles are in-flight downloads and
// are never evicted; keep is the just-committed file being returned to the UI.
const MediaCacheBytes int64 = 256 << 20

var mediaPruneMu sync.Mutex

func PruneMedia(dir, keep string) error { return pruneMedia(dir, keep, MediaCacheBytes) }

func pruneMedia(dir, keep string, limit int64) error {
	mediaPruneMu.Lock()
	defer mediaPruneMu.Unlock()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	type item struct {
		path     string
		size     int64
		modified int64
	}
	var files []item
	var total int64
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		total += info.Size()
		if path != keep {
			files = append(files, item{path, info.Size(), info.ModTime().UnixNano()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modified < files[j].modified })
	for _, f := range files {
		if total <= limit {
			break
		}
		if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		total -= f.size
	}
	return nil
}
