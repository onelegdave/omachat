package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

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
	previewDirName        = store.PreviewDirName
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
	// the per-image memory budget. videoMu does the same for ffmpeg, which
	// runs far longer and must not hold up image previews.
	convertMu sync.Mutex
	videoMu   sync.Mutex
	// ffmpeg overrides the executable found on PATH; animationTimeout bounds
	// one run. Both exist for tests.
	ffmpeg           string
	animationTimeout time.Duration

	mu         sync.Mutex
	generation uint64
	done       map[string]string // key -> preview path, "" when refused
	queue      map[string]previewJob
	running    bool
	// animQueue holds clips for ffmpeg, converted one at a time by their own
	// worker so a slow clip never holds up a request or an image preview.
	animQueue   map[string]previewJob
	animRunning bool
	// waiting holds conversations sent without an avatar because theirs was
	// still converting, so they can be republished once it is ready.
	waiting map[avatarWaiter]string
	// republish re-sends a conversation's current state.
	republish func(avatarWaiter)
}

type previewJob struct {
	src, key, dir string
	edge          int
	// animation previews a GIF-style video clip through ffmpeg.
	animation bool
	// generation is the unpair count when the job was made; a job from
	// before an unpair writes nothing.
	generation uint64
}

func newPreviewCache(paths *store.Paths) *previewCache {
	return &previewCache{
		paths:     paths,
		done:      make(map[string]string),
		queue:     make(map[string]previewJob),
		animQueue: make(map[string]previewJob),
		waiting:   make(map[avatarWaiter]string),
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

// queueKey separates work queued before an unpair from fresh requests for the
// same file, so a stale job cannot absorb a new waiter.
func (j previewJob) queueKey() string {
	return strconv.FormatUint(j.generation, 10) + ":" + j.key
}

// forget removes a service's previews and the shared ones after it unpairs.
// It waits for an image conversion in progress, so none is written afterwards.
func (p *previewCache) forget(network string) {
	if p == nil || p.paths == nil {
		return
	}
	media := map[string]string{
		wire.NetworkGMessages: p.paths.MediaDir(),
		wire.NetworkWhatsApp:  p.paths.WhatsAppMediaDir(),
		wire.NetworkTelegram:  p.paths.TelegramMediaDir(),
		wire.NetworkMessenger: p.paths.MessengerMediaDir(),
	}[network]
	p.convertMu.Lock()
	defer p.convertMu.Unlock()
	// The generation moves first: a clip conversion still running in ffmpeg
	// checks it under mu before writing, so it either writes before the
	// removal below or not at all. Unpairing need not wait for ffmpeg.
	p.mu.Lock()
	p.generation++
	p.done = make(map[string]string)
	p.animQueue = make(map[string]previewJob)
	for waiter := range p.waiting {
		if waiter.network == network {
			delete(p.waiting, waiter)
		}
	}
	p.mu.Unlock()
	if media != "" {
		_ = os.RemoveAll(filepath.Join(media, previewDirName))
	}
	_ = os.RemoveAll(filepath.Join(p.paths.Cache, previewDirName))
}

// job identifies one source file revision at one preview size.
func (p *previewCache) job(src string, edge int) (previewJob, bool) {
	return p.jobFor(src, edge, false)
}

func (p *previewCache) jobFor(src string, edge int, animation bool) (previewJob, bool) {
	if p.paths == nil || src == "" || !filepath.IsAbs(src) {
		return previewJob{}, false
	}
	src = filepath.Clean(src)
	info, err := os.Lstat(src)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > safeimage.MaxSourceBytes {
		return previewJob{}, false
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%d\x00%d\x00%d\x00%t\x00%s", src, info.Size(), info.ModTime().UnixNano(), edge, animation, previewVersion))
	p.mu.Lock()
	generation := p.generation
	p.mu.Unlock()
	return previewJob{src: src, key: store.PreviewPrefix(src) + hex.EncodeToString(sum[:8]), dir: p.previewDir(src), edge: edge, animation: animation, generation: generation}, true
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
	if _, queued := p.queue[job.queueKey()]; !queued {
		if len(p.queue) >= maxPreviewQueue {
			return "", false
		}
		p.queue[job.queueKey()] = job
	}
	if waiter != nil {
		p.waiting[*waiter] = job.queueKey()
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
		delete(p.queue, job.queueKey())
		var ready []avatarWaiter
		for waiter, key := range p.waiting {
			if key == job.queueKey() {
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
	if job.animation {
		p.videoMu.Lock()
		defer p.videoMu.Unlock()
	} else {
		p.convertMu.Lock()
		defer p.convertMu.Unlock()
	}
	p.mu.Lock()
	stale := job.generation != p.generation
	p.mu.Unlock()
	if stale {
		return ""
	}
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
	var data []byte
	var err error
	if job.animation {
		// ffmpeg's GIF is untrusted too: it is re-encoded like any image.
		data, err = p.runFFmpeg(job.src)
	} else {
		data, err = readPreviewSource(job.src)
	}
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
	if job.animation {
		// forget does not wait for ffmpeg; see there.
		p.mu.Lock()
		stale := job.generation != p.generation
		if !stale {
			err = store.WritePrivateFile(dst, out)
		}
		p.mu.Unlock()
		if stale || err != nil {
			return ""
		}
	} else if err := store.WritePrivateFile(dst, out); err != nil {
		return ""
	}
	// Unpairing can delete the source while it converts. A preview written
	// after that would outlive the account, so it is removed again.
	if !regularFile(job.src) {
		_ = os.Remove(dst)
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
		// The event pump must keep draining its bounded queue, so events
		// carry only finished previews and never wait for a conversion.
		value.Data = p.apply(value.Network, value.Data, false)
		return value
	case wire.Response:
		value.Result = p.apply(network, value.Result, true)
		return value
	}
	return p.apply(network, v, true)
}

// apply previews one payload. wait allows converting before returning, for
// replies the user is waiting on.
func (p *previewCache) apply(network string, v any, wait bool) any {
	if network == "" {
		network = wire.NetworkGMessages
	}
	switch value := v.(type) {
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
	case wire.Message:
		return p.messagePreviews(value, wait)
	case *wire.Message:
		if value == nil {
			return value
		}
		out := p.messagePreviews(*value, wait)
		return &out
	case []wire.Message:
		out := make([]wire.Message, len(value))
		for i, m := range value {
			out[i] = p.messagePreviews(m, wait)
		}
		return out
	case wire.MessagesResult:
		value.Messages = p.apply(network, value.Messages, wait).([]wire.Message)
		return value
	case *wire.MessagesResult:
		if value == nil {
			return value
		}
		out := *value
		out.Messages = p.apply(network, out.Messages, wait).([]wire.Message)
		return &out
	case wire.SendMediaResult:
		value.Message = p.apply(network, value.Message, wait).(*wire.Message)
		value.CaptionMessage = p.apply(network, value.CaptionMessage, wait).(*wire.Message)
		return value
	case *wire.SendMediaResult:
		if value == nil {
			return value
		}
		out := *value
		out.Message = p.apply(network, out.Message, wait).(*wire.Message)
		out.CaptionMessage = p.apply(network, out.CaptionMessage, wait).(*wire.Message)
		return &out
	case wire.PickImageResult:
		value.Preview = p.now(value.Path, attachmentPreviewEdge)
		return value
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

// messagePreviews previews image attachments that already have a local file,
// such as a just-sent Telegram image. The rest are fetched with the media
// method. Most calls are cache hits: the file was previewed when it arrived.
func (p *previewCache) messagePreviews(m wire.Message, wait bool) wire.Message {
	if len(m.Attachments) == 0 {
		return m
	}
	attachments := make([]wire.Attachment, len(m.Attachments))
	for i, a := range m.Attachments {
		a.Preview = ""
		if a.Path != "" && (a.IsImage || a.IsGif) && !a.IsVideo && !a.IsAudio {
			if wait {
				a.Preview = p.now(a.Path, attachmentPreviewEdge)
			} else {
				// A miss is queued; the panel fetches it with the media
				// method if it is still missing when shown.
				a.Preview, _ = p.lookup(a.Path, attachmentPreviewEdge, nil)
			}
		}
		attachments[i] = a
	}
	m.Attachments = attachments
	return m
}
