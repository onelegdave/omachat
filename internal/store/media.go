package store

import (
	"crypto/sha256"
	"encoding/hex"
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

// PreviewDirName is the subdirectory of a media cache holding the helper's
// re-encoded previews of files in that cache.
const PreviewDirName = "previews"

// PreviewPrefix starts the name of every preview of src, so the previews of
// one revoked file can be removed without knowing their sizes or versions.
func PreviewPrefix(src string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(src)))
	return hex.EncodeToString(sum[:12]) + "-"
}

// RemovePreviews deletes the previews of src kept beside it.
func RemovePreviews(src string) {
	dir := filepath.Join(filepath.Dir(filepath.Clean(src)), PreviewDirName)
	matches, _ := filepath.Glob(filepath.Join(dir, PreviewPrefix(src)+"*"))
	for _, match := range matches {
		_ = os.Remove(match)
	}
}

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

// safeMediaExtensions are the only suffixes a cached file can get. The cached
// file is handed to xdg-open, so a sender-chosen name such as "clip.desktop",
// or a MIME type the system maps to an active format, must not choose one.
var safeMediaExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".heic": true,
	".mp4": true, ".m4v": true, ".mov": true, ".webm": true, ".mkv": true, ".3gp": true, ".f4v": true,
	".m4a": true, ".aac": true, ".mp3": true, ".ogg": true, ".opus": true, ".amr": true, ".wav": true,
}

// SafeMediaExtension returns a cached media file's suffix from a fixed list.
// A name whose suffix is on the list keeps it; otherwise the MIME type picks
// one, and anything else becomes ".bin".
func SafeMediaExtension(mimeType, name string) string {
	if ext := strings.ToLower(filepath.Ext(name)); safeMediaExtensions[ext] {
		return ext
	}
	mimeType = strings.ToLower(mimeType)
	switch {
	case strings.Contains(mimeType, "jpeg"):
		return ".jpg"
	case strings.Contains(mimeType, "png"):
		return ".png"
	case strings.Contains(mimeType, "gif"):
		return ".gif"
	case strings.Contains(mimeType, "webp"):
		return ".webp"
	case strings.HasPrefix(mimeType, "audio/"):
		return ".audio"
	case strings.HasPrefix(mimeType, "video/"):
		return ".video"
	}
	return ".bin"
}
