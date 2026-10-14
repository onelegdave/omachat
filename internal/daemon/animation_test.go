package daemon

import (
	"bytes"
	"image/gif"
	"os"
	"os/exec"
	"path/filepath"
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

func TestGIFStyleClipsGetAnimatedPreviews(t *testing.T) {
	d := freshSelectionDaemon(t)
	clip := testClip(t, d.paths.WhatsAppMediaDir())
	req := wire.Request{Network: wire.NetworkWhatsApp, Method: wire.MethodMedia, Params: map[string]any{"key": "k", "animation": true}}
	resp := d.previews.mediaAnimation(req, wire.Response{OK: true, Result: wire.MediaResult{Key: "k", Path: clip}})
	got := resp.Result.(wire.MediaResult)
	if got.Path != clip || filepath.Dir(got.Preview) != filepath.Join(d.paths.WhatsAppMediaDir(), previewDirName) {
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
	if plain.Result.(wire.MediaResult).Preview != "" {
		t.Fatal("a video got an animation without asking")
	}
	entries, _ := os.ReadDir(d.paths.Cache)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == "" && e.Name()[0] == '.' {
			t.Fatalf("ffmpeg scratch directory left behind: %s", e.Name())
		}
	}
}

func TestAnimationRefusesUnrecognisedContainers(t *testing.T) {
	d := freshSelectionDaemon(t)
	dir := d.paths.MessengerMediaDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A playlist would let ffmpeg open other files; it is never handed over.
	d.previews.ffmpeg = "/nonexistent/ffmpeg-must-not-run"
	for name, data := range map[string]string{
		"playlist.mp4": "#EXTM3U\n#EXTINF:1,\nfile:///etc/passwd\n",
		"concat.mp4":   "ffconcat version 1.0\nfile '/etc/passwd'\n",
		"text.mp4":     "hello",
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := d.previews.animation(path); got != "" {
			t.Fatalf("%s produced %s", name, got)
		}
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
	if got := d.previews.animation(clip); got != "" {
		t.Fatalf("hung ffmpeg produced %s", got)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("hung ffmpeg held the conversion for %v", elapsed)
	}
}
