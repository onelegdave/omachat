package daemon

import (
	"bytes"
	"image/gif"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
)

func testClip(t *testing.T, dir string) string {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is needed for animated previews")
	}
	clip := filepath.Join(dir, "clip.mp4")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(ffmpeg, "-loglevel", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=25:duration=2", "-pix_fmt", "yuv420p", clip)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg could not make a fixture: %v %s", err, out)
	}
	return clip
}

// waitAnimation polls until a queued clip finishes converting.
func waitAnimation(t *testing.T, d *Daemon, clip string) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		path, pending := d.previews.animation(clip)
		if !pending {
			return path
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("clip conversion did not finish")
	return ""
}

func TestGIFStyleClipsGetAnimatedPreviews(t *testing.T) {
	d := freshSelectionDaemon(t)
	clip := testClip(t, d.paths.WhatsAppMediaDir())
	req := wire.Request{Network: wire.NetworkWhatsApp, Method: wire.MethodMedia, Params: map[string]any{"key": "k", "animation": true}}
	// The first reply does not wait for ffmpeg: it reports pending.
	first := d.previews.mediaAnimation(req, wire.Response{OK: true, Result: wire.MediaResult{Key: "k", Path: clip}}).Result.(wire.MediaResult)
	if !first.Pending || first.Preview != "" || first.Path != clip {
		t.Fatalf("first reply: %+v", first)
	}
	waitAnimation(t, d, clip)
	got := d.previews.mediaAnimation(req, wire.Response{OK: true, Result: wire.MediaResult{Key: "k", Path: clip}}).Result.(wire.MediaResult)
	if got.Pending || got.Path != clip || filepath.Dir(got.Preview) != filepath.Join(d.paths.WhatsAppMediaDir(), previewDirName) {
		t.Fatalf("reply: %+v", got)
	}
	data, err := os.ReadFile(got.Preview)
	if err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) < 2 || len(g.Image) > animationFrames || g.Config.Width > animationEdge || g.Config.Height > animationEdge {
		t.Fatalf("animation: %d frames, %dx%d", len(g.Image), g.Config.Width, g.Config.Height)
	}
	// Without the request flag, a video gets no preview: it opens externally.
	plain := d.previews.mediaAnimation(wire.Request{Method: wire.MethodMedia, Params: map[string]any{"key": "k"}}, wire.Response{OK: true, Result: wire.MediaResult{Path: clip}})
	if r := plain.Result.(wire.MediaResult); r.Preview != "" || r.Pending {
		t.Fatal("a video got an animation without asking")
	}
	entries, _ := os.ReadDir(d.paths.Cache)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".animation-") {
			t.Fatalf("ffmpeg scratch directory left behind: %s", e.Name())
		}
	}
}

// A clip whose single frame lasts a minute must not make fps feed the
// palette filter hundreds of frames.
func TestLongFrameDurationsStayWithinTheFrameCap(t *testing.T) {
	d := freshSelectionDaemon(t)
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is needed for animated previews")
	}
	dir := d.paths.MessengerMediaDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	clip := filepath.Join(dir, "still.mp4")
	cmd := exec.Command(ffmpeg, "-loglevel", "error", "-y", "-f", "lavfi", "-i", "color=c=red:s=64x64:r=1:d=1", "-frames:v", "1", "-video_track_timescale", "1", "-pix_fmt", "yuv420p", clip)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("fixture: %v %s", err, out)
	}
	d.previews.animation(clip)
	path := waitAnimation(t, d, clip)
	if path == "" {
		return // refusing it is also within bounds
	}
	data, _ := os.ReadFile(path)
	if g, err := gif.DecodeAll(bytes.NewReader(data)); err == nil && len(g.Image) > animationFrames {
		t.Fatalf("%d frames", len(g.Image))
	}
}

func TestAnimationRefusesUnrecognisedContainers(t *testing.T) {
	d := freshSelectionDaemon(t)
	dir := d.paths.MessengerMediaDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A playlist would let ffmpeg open other files; it is never handed over.
	marker := filepath.Join(t.TempDir(), "ran")
	stub := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ntouch "+marker+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	d.previews.ffmpeg = stub
	for name, data := range map[string]string{
		"playlist.mp4": "#EXTM3U\n#EXTINF:1,\nfile:///etc/passwd\n",
		"concat.mp4":   "ffconcat version 1.0\nfile '/etc/passwd'\n",
		"text.mp4":     "hello",
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		d.previews.animation(path)
		if got := waitAnimation(t, d, path); got != "" {
			t.Fatalf("%s produced %s", name, got)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("ffmpeg ran for an unrecognised container")
	}
}

func TestHungFFmpegIsStopped(t *testing.T) {
	d := freshSelectionDaemon(t)
	clip := testClip(t, d.paths.TelegramMediaDir())
	script := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	d.previews.ffmpeg = script
	d.previews.animationTimeout = 300 * time.Millisecond
	start := time.Now()
	d.previews.animation(clip)
	if got := waitAnimation(t, d, clip); got != "" {
		t.Fatalf("hung ffmpeg produced %s", got)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("hung ffmpeg held the conversion for %v", elapsed)
	}
}

// ffmpeg starts under prlimit, so its limits are in place before it runs.
func TestFFmpegStartsUnderItsLimits(t *testing.T) {
	d := freshSelectionDaemon(t)
	clip := testClip(t, d.paths.WhatsAppMediaDir())
	report := filepath.Join(t.TempDir(), "limits")
	stub := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nulimit -d > "+report+"\nulimit -t >> "+report+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	d.previews.ffmpeg = stub
	d.previews.animation(clip)
	waitAnimation(t, d, clip)
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(data)); len(got) != 2 || got[0] != strconv.Itoa(ffmpegMaxData/1024) || got[1] != strconv.Itoa(ffmpegMaxCPU) {
		t.Fatalf("limits seen by ffmpeg: %q", data)
	}
}
