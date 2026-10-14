package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
)

// WhatsApp and Messenger send GIFs as short looping videos. The panel used to
// play them with Qt Multimedia, which decodes inside the shell process. They
// are now turned into an animated GIF by ffmpeg in its own process, under the
// limits below, and that GIF is re-encoded by internal/safeimage like any
// other image. Without ffmpeg the clip opens in an external player.
const (
	animationEdge     = 480
	animationFPS      = 12
	animationSeconds  = 8
	animationFrames   = animationFPS * animationSeconds
	animationTimeout  = 20 * time.Second
	animationMaxBytes = 16 << 20
	maxAnimationQueue = 32
	// ffmpeg's own limits: decoded frame area, address space for data, CPU
	// seconds, and the size of any file it writes.
	ffmpegMaxPixels = 3840 * 2160
	ffmpegMaxData   = 1 << 30
	ffmpegMaxCPU    = 30
)

var errNoAnimation = errors.New("no animated preview")

// animationFormat names the ffmpeg demuxer for a recognised container. Forcing
// it stops ffmpeg from probing for playlist or concat formats that could
// reference other files.
func animationFormat(head []byte) string {
	switch {
	case len(head) >= 12 && bytes.Equal(head[4:8], []byte("ftyp")): // MP4, MOV, 3GP, F4V
		return "mov"
	case len(head) >= 4 && bytes.Equal(head[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}): // WebM, Matroska
		return "matroska"
	}
	return ""
}

// animation returns a finished animated preview for a GIF-style video clip.
// Otherwise it queues the clip for ffmpeg and reports pending; the panel asks
// again shortly. A full queue or a refused clip gets no preview.
func (p *previewCache) animation(src string) (path string, pending bool) {
	if p == nil {
		return "", false
	}
	job, ok := p.jobFor(src, attachmentPreviewEdge, true)
	if !ok {
		return "", false
	}
	if path, known := p.cached(job); known {
		return path, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, queued := p.animQueue[job.queueKey()]; !queued {
		if len(p.animQueue) >= maxAnimationQueue {
			return "", false
		}
		p.animQueue[job.queueKey()] = job
	}
	if !p.animRunning {
		p.animRunning = true
		go p.animWork()
	}
	return "", true
}

func (p *previewCache) animWork() {
	for {
		p.mu.Lock()
		var job previewJob
		found := false
		for _, queued := range p.animQueue {
			job, found = queued, true
			break
		}
		if !found {
			p.animRunning = false
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()
		p.convert(job)
		p.mu.Lock()
		delete(p.animQueue, job.queueKey())
		p.mu.Unlock()
	}
}

func (p *previewCache) runFFmpeg(src string) ([]byte, error) {
	head, err := readHead(src, 16)
	if err != nil {
		return nil, err
	}
	format := animationFormat(head)
	if format == "" {
		return nil, errNoAnimation
	}
	ffmpeg := p.ffmpeg
	if ffmpeg == "" {
		if ffmpeg, err = exec.LookPath("ffmpeg"); err != nil {
			return nil, errNoAnimation
		}
	}
	if err := os.MkdirAll(p.paths.Cache, 0o700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(p.paths.Cache, ".animation-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "preview.gif")

	timeout := p.animationTimeout
	if timeout == 0 {
		timeout = animationTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// trim ends the stream after the frame cap before scaling or palette
	// generation, which reads to the end: -frames:v alone bounds only output,
	// and odd timestamps can make fps emit far more frames than that.
	filter := fmt.Sprintf("fps=%d,trim=end_frame=%d,scale=w='min(iw,%d)':h='min(ih,%d)':force_original_aspect_ratio=decrease:flags=lanczos,"+
		"split[a][b];[a]palettegen=max_colors=128:stats_mode=diff[p];[b][p]paletteuse=dither=bayer",
		animationFPS, animationFrames, animationEdge, animationEdge)
	prlimit, err := exec.LookPath("prlimit")
	if err != nil {
		return nil, errNoAnimation
	}
	// prlimit sets the limits on itself and then executes ffmpeg, so they hold
	// from ffmpeg's first instruction, and a failure to set them runs nothing.
	cmd := exec.CommandContext(ctx, prlimit,
		"--data="+strconv.Itoa(ffmpegMaxData), "--cpu="+strconv.Itoa(ffmpegMaxCPU), "--fsize="+strconv.Itoa(animationMaxBytes),
		"--", ffmpeg,
		"-nostdin", "-hide_banner", "-loglevel", "error",
		"-protocol_whitelist", "file",
		"-threads", "2", "-filter_threads", "2",
		"-max_pixels", strconv.Itoa(ffmpegMaxPixels),
		"-probesize", "5000000", "-analyzeduration", "5000000",
		"-t", strconv.Itoa(animationSeconds),
		"-f", format, "-i", "file:"+src,
		"-map", "0:v:0", "-an", "-sn", "-dn",
		"-frames:v", strconv.Itoa(animationFrames),
		"-vf", filter,
		"-loop", "0", "-f", "gif", "-y", "file:"+out)
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	// ffmpeg does not outlive the helper. The death signal follows the thread
	// that started it, so this goroutine keeps its thread until ffmpeg exits.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := cmd.Run(); err != nil {
		return nil, errNoAnimation
	}
	f, err := os.Open(out)
	if err != nil {
		return nil, errNoAnimation
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, animationMaxBytes+1))
	if err != nil || len(data) > animationMaxBytes {
		return nil, errNoAnimation
	}
	return data, nil
}

func readHead(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	head := make([]byte, n)
	read, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	return head[:read], nil
}

// mediaAnimation adds an animated preview to a media reply when the panel
// asked for one, for a GIF-style clip.
func (p *previewCache) mediaAnimation(req wire.Request, resp wire.Response) wire.Response {
	if req.Method != wire.MethodMedia || !resp.OK {
		return resp
	}
	params, err := decodeParams[wire.MediaParams](req.Params)
	if err != nil || !params.Animation {
		return resp
	}
	switch result := resp.Result.(type) {
	case wire.MediaResult:
		if result.Preview == "" && result.Path != "" {
			result.Preview, result.Pending = p.animation(result.Path)
		}
		resp.Result = result
	case *wire.MediaResult:
		if result != nil && result.Preview == "" && result.Path != "" {
			out := *result
			out.Preview, out.Pending = p.animation(out.Path)
			resp.Result = &out
		}
	}
	return resp
}
