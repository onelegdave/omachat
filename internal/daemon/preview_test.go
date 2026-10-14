package daemon

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
)

func writeTestImage(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// hostilePNG is a valid image carrying many compressed text chunks.
func hostilePNG(t *testing.T) []byte {
	t.Helper()
	var src bytes.Buffer
	if err := png.Encode(&src, image.NewGray(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	_, _ = zw.Write(bytes.Repeat([]byte("x"), 4<<20))
	_ = zw.Close()
	payload := append([]byte("Comment\x00\x00"), z.Bytes()...)
	var chunk bytes.Buffer
	_ = binary.Write(&chunk, binary.BigEndian, uint32(len(payload)))
	chunk.WriteString("zTXt")
	chunk.Write(payload)
	_ = binary.Write(&chunk, binary.BigEndian, crc32.ChecksumIEEE(append([]byte("zTXt"), payload...)))
	data := append([]byte{}, src.Bytes()[:33]...)
	for i := 0; i < 32; i++ {
		data = append(data, chunk.Bytes()...)
	}
	return append(data, src.Bytes()[33:]...)
}

func TestMediaResultsCarryOnlyReencodedPreviews(t *testing.T) {
	d := freshSelectionDaemon(t)
	original := filepath.Join(d.paths.WhatsAppMediaDir(), "document.png")
	if err := os.MkdirAll(filepath.Dir(original), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(original, hostilePNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, result := range []any{wire.MediaResult{Key: "k", Path: original}, &wire.MediaResult{Key: "k", Path: original}} {
		resp := d.previews.outbound(wire.NetworkWhatsApp, wire.Response{OK: true, Result: result}).(wire.Response)
		var got wire.MediaResult
		switch r := resp.Result.(type) {
		case wire.MediaResult:
			got = r
		case *wire.MediaResult:
			got = *r
		}
		if got.Path != original {
			t.Fatalf("original path changed to %q; external viewers need it", got.Path)
		}
		if filepath.Dir(got.Preview) != filepath.Join(d.paths.WhatsAppMediaDir(), previewDirName) {
			t.Fatalf("preview %q is not in the service's preview directory", got.Preview)
		}
		data, err := os.ReadFile(got.Preview)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("zTXt")) || len(data) > 4096 {
			t.Fatalf("preview kept the source's text chunks (%d bytes)", len(data))
		}
		if info, err := os.Stat(got.Preview); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("preview permissions: %v %v", info, err)
		}
	}

	video := filepath.Join(d.paths.WhatsAppMediaDir(), "clip.mp4")
	if err := os.WriteFile(video, []byte("\x00\x00\x00\x18ftypmp42 not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	resp := d.previews.outbound(wire.NetworkWhatsApp, wire.Response{OK: true, Result: wire.MediaResult{Path: video}}).(wire.Response)
	if got := resp.Result.(wire.MediaResult); got.Preview != "" || got.Path != video {
		t.Fatalf("non-image result: %+v", got)
	}
}

func TestPreviewRefusesLinksAndOutsideOriginals(t *testing.T) {
	d := freshSelectionDaemon(t)
	real := filepath.Join(d.paths.TelegramMediaDir(), "real.png")
	writeTestImage(t, real, 4, 4)
	link := filepath.Join(d.paths.TelegramMediaDir(), "link.png")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if got := d.previews.now(link, attachmentPreviewEdge); got != "" {
		t.Fatalf("symlinked original produced %q", got)
	}
	if got := d.previews.now("relative.png", attachmentPreviewEdge); got != "" {
		t.Fatalf("relative path produced %q", got)
	}
	// A file the user picked elsewhere is previewed in the cache, never beside it.
	outside := filepath.Join(t.TempDir(), "picked.png")
	writeTestImage(t, outside, 4, 4)
	got := d.previews.now(outside, attachmentPreviewEdge)
	if filepath.Dir(got) != filepath.Join(d.paths.Cache, previewDirName) {
		t.Fatalf("outside original previewed at %q", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(outside), previewDirName)); !os.IsNotExist(err) {
		t.Fatal("a preview directory was created beside a user's file")
	}
}

func TestConversationAvatarsArePreviewsAndRepublishWhenReady(t *testing.T) {
	d := freshSelectionDaemon(t)
	avatar := filepath.Join(d.paths.TelegramMediaDir(), "avatar.jpg")
	writeTestImage(t, avatar, 512, 512)
	d.tg.AddTestConversation(wire.Conversation{ID: "tg:7", Name: "Fixture", AvatarPath: avatar})
	events, unsubscribe := d.Subscribe()
	defer unsubscribe()

	list := d.previews.outbound(wire.NetworkTelegram, wire.Response{OK: true, Result: d.tg.Conversations(0)}).(wire.Response).Result.([]wire.Conversation)
	if len(list) != 1 || list[0].AvatarPath == avatar {
		t.Fatalf("original avatar reached the panel: %+v", list)
	}

	// The first sighting converts in the background, then republishes the
	// conversation's current state.
	deadline := time.After(10 * time.Second)
	for {
		select {
		case evt := <-events:
			if evt.Event != wire.EventConversation || evt.Network != wire.NetworkTelegram {
				continue
			}
			out := d.previews.outbound(evt.Network, evt).(wire.Event)
			conv := out.Data.(wire.Conversation)
			if conv.ID != "tg:7" {
				continue
			}
			if !strings.HasPrefix(conv.AvatarPath, filepath.Join(d.paths.TelegramMediaDir(), previewDirName)+string(filepath.Separator)) {
				t.Fatalf("republished avatar is %q", conv.AvatarPath)
			}
			cfg, _, err := image.DecodeConfig(mustOpen(t, conv.AvatarPath))
			if err != nil || cfg.Width != avatarPreviewEdge {
				t.Fatalf("avatar preview %dx%d: %v", cfg.Width, cfg.Height, err)
			}
			return
		case <-deadline:
			t.Fatal("conversation was not republished with its avatar preview")
		}
	}
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestUndecodableAvatarIsDroppedNotForwarded(t *testing.T) {
	d := freshSelectionDaemon(t)
	bad := filepath.Join(d.paths.MessengerMediaDir(), "avatar-1.img")
	if err := os.MkdirAll(filepath.Dir(bad), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0o600); err != nil {
		t.Fatal(err)
	}
	conv := wire.Conversation{ID: "fb:1", AvatarPath: bad}
	for i := 0; i < 50; i++ {
		got := d.previews.outbound(wire.NetworkMessenger, conv).(wire.Conversation)
		if got.AvatarPath != "" {
			t.Fatalf("undecodable avatar forwarded as %q", got.AvatarPath)
		}
		if _, pending := d.previews.lookup(bad, avatarPreviewEdge, nil); !pending {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("refused avatar stayed pending")
}

func TestDaemonWithoutPreviewCacheFailsClosed(t *testing.T) {
	var d Daemon
	conv := wire.Conversation{ID: "x", AvatarPath: "/tmp/original.png"}
	if got := d.previews.outbound("", wire.Event{Data: conv}).(wire.Event).Data.(wire.Conversation); got.AvatarPath != "" {
		t.Fatalf("avatar forwarded without a cache: %q", got.AvatarPath)
	}
}

func TestPanelsWithoutPreviewSupportOnlyReceivePreviews(t *testing.T) {
	d := freshSelectionDaemon(t)
	original := filepath.Join(d.paths.TelegramMediaDir(), "photo.png")
	writeTestImage(t, original, 8, 8)
	voice := filepath.Join(d.paths.TelegramMediaDir(), "voice.ogg")
	if err := os.WriteFile(voice, []byte("OggS not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	reply := func(params map[string]any, path string) wire.MediaResult {
		t.Helper()
		req := wire.Request{Network: wire.NetworkTelegram, Method: wire.MethodMedia, Params: params}
		return *d.previews.response(req, wire.Response{OK: true, Result: &wire.MediaResult{Key: "k", Path: path}}).Result.(*wire.MediaResult)
	}
	current := reply(map[string]any{"key": "k", "previews": true}, original)
	if current.Path != original || current.Preview == "" {
		t.Fatalf("current panel reply: %+v", current)
	}
	// A panel service left running from an earlier release decodes Path.
	legacy := reply(map[string]any{"key": "k"}, original)
	if legacy.Path != current.Preview {
		t.Fatalf("legacy panel was given %q instead of the preview", legacy.Path)
	}
	if got := reply(map[string]any{"key": "k"}, voice); got.Path != "" {
		t.Fatalf("legacy panel was given an undecodable original %q", got.Path)
	}
}
