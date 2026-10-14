package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/onelegdave/omachat/internal/safeimage"
	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

// The panel decodes images inside the shared shell process, so it is only
// ever given previews this helper re-encoded (see internal/safeimage). Every
// path it could display passes through here on its way out: attachment
// previews on media results, and avatars on conversations in both results
// and events. The originals stay where they are for external viewers.
const (
	attachmentPreviewEdge = 1280
	avatarPreviewEdge     = 256
	previewDirName        = "previews"
	// maxPreviewDirBytes caps each preview directory. Previews are derived
	// data and are rebuilt on demand after eviction.
	maxPreviewDirBytes int64 = 64 << 20
	maxPreviewEntries        = 8192
	maxPreviewQueue          = 4096
	// previewVersion changes every cached name when the encoder changes.
	previewVersion = "1"
)

type avatarWaiter struct {
	network, conversationID string
}

type previewCache struct {
	paths *store.Paths

	// convertMu serialises decoding so concurrent requests cannot multiply
	// the per-image memory budget.
	convertMu sync.Mutex

	mu      sync.Mutex
	done    map[string]string // key -> preview path, "" when refused
	queue   map[string]previewJob
	running bool
	// waiting holds conversations sent without an avatar because theirs was
	// still converting, so they can be republished once it is ready.
	waiting map[avatarWaiter]string
	// republish re-sends a conversation's current state.
	republish func(avatarWaiter)
}

type previewJob struct {
	src, key, dir string
	edge          int
}

func newPreviewCache(paths *store.Paths) *previewCache {
	return &previewCache{
		paths:   paths,
		done:    make(map[string]string),
		queue:   make(map[string]previewJob),
		waiting: make(map[avatarWaiter]string),
	}
}

// previewDir keeps a preview beside its service's media, so unpairing a
// service removes its previews along with everything else it cached.
func (p *previewCache) previewDir(src string) string {
	dir := filepath.Dir(src)
	for _, media := range []string{p.paths.MediaDir(), p.paths.WhatsAppMediaDir(), p.paths.TelegramMediaDir(), p.paths.MessengerMediaDir()} {
		if dir == filepath.Clean(media) {
			return filepath.Join(dir, previewDirName)
		}
	}
	return filepath.Join(p.paths.Cache, previewDirName)
}

// job identifies one source file revision at one preview size.
func (p *previewCache) job(src string, edge int) (previewJob, bool) {
	if p.paths == nil || src == "" || !filepath.IsAbs(src) {
		return previewJob{}, false
	}
	src = filepath.Clean(src)
	info, err := os.Lstat(src)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > safeimage.MaxSourceBytes {
		return previewJob{}, false
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%d\x00%d\x00%d\x00%s", src, info.Size(), info.ModTime().UnixNano(), edge, previewVersion))
	return previewJob{src: src, key: hex.EncodeToString(sum[:16]), dir: p.previewDir(src), edge: edge}, true
}

// cached reports a finished preview. Evicted files are treated as missing.
func (p *previewCache) cached(job previewJob) (path string, known bool) {
	p.mu.Lock()
	path, known = p.done[job.key]
	p.mu.Unlock()
	if known {
		if path == "" {
			return "", true
		}
		if regularFile(path) {
			return path, true
		}
	}
	for _, format := range []safeimage.Format{safeimage.JPEG, safeimage.PNG, safeimage.GIF} {
		candidate := filepath.Join(job.dir, job.key+"."+string(format))
		if regularFile(candidate) {
			p.remember(job.key, candidate)
			return candidate, true
		}
	}
	return "", false
}

func regularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

func (p *previewCache) remember(key, path string) {
	p.mu.Lock()
	if len(p.done) >= maxPreviewEntries {
		p.done = make(map[string]string)
	}
	p.done[key] = path
	p.mu.Unlock()
}

// now returns a preview, converting synchronously when needed. It is for a
// request the user is waiting on, such as opening an attachment.
func (p *previewCache) now(src string, edge int) string {
	if p == nil {
		return ""
	}
	job, ok := p.job(src, edge)
	if !ok {
		return ""
	}
	if path, known := p.cached(job); known {
		return path
	}
	return p.convert(job)
}

// lookup returns a finished preview without blocking. A miss queues the
// conversion, registers waiter (when given) for a republish once it is done,
// and reports pending.
func (p *previewCache) lookup(src string, edge int, waiter *avatarWaiter) (path string, pending bool) {
	if p == nil {
		return "", false
	}
	job, valid := p.job(src, edge)
	known := false
	if valid {
		path, known = p.cached(job)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if waiter != nil {
		delete(p.waiting, *waiter)
	}
	if !valid || known {
		return path, false
	}
	if _, queued := p.queue[job.key]; !queued {
		if len(p.queue) >= maxPreviewQueue {
			return "", false
		}
		p.queue[job.key] = job
	}
	if waiter != nil {
		p.waiting[*waiter] = job.key
	}
	if !p.running {
		p.running = true
		go p.work()
	}
	return "", true
}

func (p *previewCache) work() {
	for {
		p.mu.Lock()
		var job previewJob
		found := false
		for _, queued := range p.queue {
			job, found = queued, true
			break
		}
		if !found {
			p.running = false
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()

		path := ""
		if cached, known := p.cached(job); known {
			path = cached
		} else {
			path = p.convert(job)
		}

		p.mu.Lock()
		delete(p.queue, job.key)
		var ready []avatarWaiter
		for waiter, key := range p.waiting {
			if key == job.key {
				delete(p.waiting, waiter)
				if path != "" {
					ready = append(ready, waiter)
				}
			}
		}
		republish := p.republish
		p.mu.Unlock()
		if republish != nil {
			for _, waiter := range ready {
				republish(waiter)
			}
		}
	}
}

func (p *previewCache) convert(job previewJob) (path string) {
	p.convertMu.Lock()
	defer p.convertMu.Unlock()
	if cached, known := p.cached(job); known {
		return cached
	}
	defer func() {
		// A decoder fault on hostile input refuses that image only.
		if recover() != nil {
			path = ""
		}
		p.remember(job.key, path)
	}()
	data, err := readPreviewSource(job.src)
	if err != nil {
		return ""
	}
	out, format, err := safeimage.Convert(data, job.edge)
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(job.dir, 0o700); err != nil {
		return ""
	}
	dst := filepath.Join(job.dir, job.key+"."+string(format))
	if err := store.WritePrivateFile(dst, out); err != nil {
		return ""
	}
	_, _ = pruneDir(job.dir, maxPreviewDirBytes, func(name string) bool { return name != filepath.Base(dst) })
	return dst
}

func readPreviewSource(src string) ([]byte, error) {
	f, err := os.OpenFile(src, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, safeimage.MaxSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > safeimage.MaxSourceBytes {
		return nil, safeimage.ErrTooLarge
	}
	return data, nil
}

// avatar swaps a conversation's avatar for its preview. A conversion still
// running leaves it blank for now and republishes the conversation later.
func (p *previewCache) avatar(network string, conversation wire.Conversation) wire.Conversation {
	waiter := avatarWaiter{network: network, conversationID: conversation.ID}
	conversation.AvatarPath, _ = p.lookup(conversation.AvatarPath, avatarPreviewEdge, &waiter)
	return conversation
}

// outbound applies previews to anything the panel will receive.
func (p *previewCache) outbound(network string, v any) any {
	if p == nil {
		// A daemon built without New has no cache. Fail closed: send no
		// avatars or previews rather than the originals.
		p = newPreviewCache(nil)
	}
	if network == "" {
		network = wire.NetworkGMessages
	}
	switch value := v.(type) {
	case wire.Event:
		value.Data = p.outbound(value.Network, value.Data)
		return value
	case wire.Response:
		value.Result = p.outbound(network, value.Result)
		return value
	case wire.Conversation:
		return p.avatar(network, value)
	case *wire.Conversation:
		if value == nil {
			return value
		}
		out := p.avatar(network, *value)
		return &out
	case []wire.Conversation:
		out := make([]wire.Conversation, len(value))
		for i, conversation := range value {
			out[i] = p.avatar(network, conversation)
		}
		return out
	case wire.MediaResult:
		value.Preview = p.now(value.Path, attachmentPreviewEdge)
		return value
	case *wire.MediaResult:
		if value == nil {
			return value
		}
		out := *value
		out.Preview = p.now(out.Path, attachmentPreviewEdge)
		return &out
	}
	return v
}

// response applies previews to a reply. A media reply to a panel that does
// not declare preview support carries the preview as its only path.
func (p *previewCache) response(req wire.Request, resp wire.Response) wire.Response {
	resp = p.outbound(req.Network, resp).(wire.Response)
	if req.Method != wire.MethodMedia {
		return resp
	}
	if params, err := decodeParams[wire.MediaParams](req.Params); err == nil && params.Previews {
		return resp
	}
	switch result := resp.Result.(type) {
	case wire.MediaResult:
		result.Path = result.Preview
		resp.Result = result
	case *wire.MediaResult:
		if result != nil {
			legacy := *result
			legacy.Path = legacy.Preview
			resp.Result = &legacy
		}
	}
	return resp
}
