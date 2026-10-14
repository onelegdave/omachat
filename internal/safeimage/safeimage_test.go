package safeimage

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func solid(w, h int, c color.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func pngChunk(kind string, data []byte) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, uint32(len(data)))
	b.WriteString(kind)
	b.Write(data)
	crc := crc32.NewIEEE()
	crc.Write([]byte(kind))
	crc.Write(data)
	_ = binary.Write(&b, binary.BigEndian, crc.Sum32())
	return b.Bytes()
}

// withChunksAfterIHDR inserts ancillary chunks after the 8-byte signature
// and the 25-byte IHDR chunk.
func withChunksAfterIHDR(src []byte, chunks ...[]byte) []byte {
	out := append([]byte{}, src[:33]...)
	for _, c := range chunks {
		out = append(out, c...)
	}
	return append(out, src[33:]...)
}

// The reported attack: compressed text chunks that a decoder expands and
// retains. The preview must decode, and none of the text may survive.
func TestPNGTextChunksAreDroppedWithoutExpansion(t *testing.T) {
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	_, _ = zw.Write(bytes.Repeat([]byte{'A'}, 8<<20))
	_ = zw.Close()
	ztxt := pngChunk("zTXt", append([]byte("Comment\x00\x00"), z.Bytes()...))
	itxt := pngChunk("iTXt", append([]byte("Note\x00\x01\x00\x00\x00"), z.Bytes()...))
	text := pngChunk("tEXt", []byte("Author\x00secret-marker"))
	var chunks [][]byte
	for i := 0; i < 64; i++ {
		chunks = append(chunks, ztxt, itxt)
	}
	chunks = append(chunks, text)
	src := withChunksAfterIHDR(encodePNG(t, solid(40, 30, color.NRGBA{10, 20, 30, 128})), chunks...)

	out, format, err := Convert(src, 1280)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if format != PNG {
		t.Fatalf("format = %s, want png for a translucent image", format)
	}
	for _, marker := range []string{"zTXt", "iTXt", "tEXt", "secret-marker"} {
		if bytes.Contains(out, []byte(marker)) {
			t.Fatalf("preview still contains %q", marker)
		}
	}
	if len(out) > 4096 {
		t.Fatalf("preview is %d bytes; the source text survived", len(out))
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 40 || b.Dy() != 30 {
		t.Fatalf("bounds = %v", b)
	}
}

func TestOpaqueImagesBecomeScaledJPEG(t *testing.T) {
	out, format, err := Convert(encodePNG(t, solid(2000, 500, color.NRGBA{200, 10, 10, 255})), 1000)
	if err != nil {
		t.Fatal(err)
	}
	if format != JPEG {
		t.Fatalf("format = %s, want jpg", format)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 1000 || cfg.Height != 250 {
		t.Fatalf("size = %dx%d, want 1000x250", cfg.Width, cfg.Height)
	}
}

func TestJPEGMetadataIsDropped(t *testing.T) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, solid(16, 16, color.NRGBA{0, 0, 255, 255}), nil); err != nil {
		t.Fatal(err)
	}
	src := b.Bytes()
	comment := []byte("private-comment")
	com := append([]byte{0xff, 0xfe, 0, byte(len(comment) + 2)}, comment...)
	src = append(append(append([]byte{}, src[:2]...), com...), src[2:]...)
	out, _, err := Convert(src, 256)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, comment) {
		t.Fatal("JPEG comment survived")
	}
}

func TestDimensionBombsAreRefusedBeforeDecoding(t *testing.T) {
	src := encodePNG(t, solid(1, 1, color.Black))
	// Rewrite IHDR to claim 16384x16384 RGBA, past the pixel budget.
	ihdr := make([]byte, 13)
	copy(ihdr, src[16:29])
	binary.BigEndian.PutUint32(ihdr[0:], 16384)
	binary.BigEndian.PutUint32(ihdr[4:], 16384)
	bomb := append(append(append([]byte{}, src[:8]...), pngChunk("IHDR", ihdr)...), src[33:]...)
	if _, _, err := Convert(bomb, 1280); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestUnsupportedFormatsAreRefused(t *testing.T) {
	for name, data := range map[string][]byte{
		"svg":  []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"tiff": {'I', 'I', 42, 0, 8, 0, 0, 0},
		"bmp":  []byte("BM\x00\x00"),
		"mp4":  []byte("\x00\x00\x00\x18ftypmp42"),
	} {
		if _, _, err := Convert(data, 256); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: err = %v, want ErrUnsupported", name, err)
		}
	}
}

func TestOversizedSourceIsRefused(t *testing.T) {
	data := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, MaxSourceBytes)...)
	if _, _, err := Convert(data, 256); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func animatedGIF(t *testing.T, frames, w, h int, comment bool) []byte {
	t.Helper()
	g := &gif.GIF{LoopCount: 0}
	for i := 0; i < frames; i++ {
		p := image.NewPaletted(image.Rect(0, 0, w, h), palette.Plan9)
		p.SetColorIndex(0, 0, uint8(i))
		g.Image = append(g.Image, p)
		g.Delay = append(g.Delay, 5)
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatal(err)
	}
	data := b.Bytes()
	if comment {
		// A comment extension right before the trailer.
		ext := append([]byte{0x21, 0xfe, 14}, []byte("secret-comment")...)
		ext = append(ext, 0)
		data = append(append(append([]byte{}, data[:len(data)-1]...), ext...), 0x3b)
	}
	return data
}

func TestAnimatedGIFStaysAnimatedAndLosesComments(t *testing.T) {
	out, format, err := Convert(animatedGIF(t, 3, 32, 32, true), 1280)
	if err != nil {
		t.Fatal(err)
	}
	if format != GIF {
		t.Fatalf("format = %s, want gif", format)
	}
	if bytes.Contains(out, []byte("secret-comment")) {
		t.Fatal("GIF comment survived")
	}
	g, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 3 {
		t.Fatalf("frames = %d, want 3", len(g.Image))
	}
}

func TestGIFOverFrameBudgetBecomesStill(t *testing.T) {
	out, format, err := Convert(animatedGIF(t, maxFrames+1, 4, 4, false), 1280)
	if err != nil {
		t.Fatal(err)
	}
	if format == GIF {
		t.Fatal("an animation over the frame budget stayed animated")
	}
	if _, _, err := image.Decode(bytes.NewReader(out)); err != nil {
		t.Fatal(err)
	}
}

func TestGIFLargerThanPreviewBecomesScaledStill(t *testing.T) {
	out, format, err := Convert(animatedGIF(t, 2, 600, 300, false), 256)
	if err != nil {
		t.Fatal(err)
	}
	if format == GIF {
		t.Fatal("an animation larger than the preview stayed animated")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 256 || cfg.Height != 128 {
		t.Fatalf("size = %dx%d", cfg.Width, cfg.Height)
	}
}

func TestGIFFrameAreaIsCheckedBeforeDecoding(t *testing.T) {
	data := animatedGIF(t, 1, 4, 4, false)
	// Claim a 65535x65535 logical screen and frame without adding data.
	binary.LittleEndian.PutUint16(data[6:], 65535)
	binary.LittleEndian.PutUint16(data[8:], 65535)
	i := bytes.IndexByte(data[13+3*256:], 0x2c) + 13 + 3*256
	binary.LittleEndian.PutUint16(data[i+5:], 65535)
	binary.LittleEndian.PutUint16(data[i+7:], 65535)
	if _, _, err := Convert(data, 256); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestWebP(t *testing.T) {
	still, _ := hex.DecodeString("524946461c000000574542505650384c0f0000002f038000000710f58ffe0722a2ff0100")
	out, format, err := Convert(still, 256)
	if err != nil {
		t.Fatalf("still WebP: %v", err)
	}
	if format != JPEG {
		t.Fatalf("format = %s", format)
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Fatal(err)
	}

	animated, _ := hex.DecodeString("524946460801000057454250565038580a00000012000000030000020000414e494d06000000ffffffff0000414e4d464400000000000000000003000002000064000000565038202c0000009001009d012a0400030002003425a00274ba00039800fef9936fff901fff901fff901fff203fe2177b203000414e4d464400000000000000000003000002000064000000565038202c0000009001009d012a0400030002003425a00274ba00039800fef9936fff901fff901fff901fff203fe2177b203000414e4d464400000000000000000003000002000064000000565038202c0000009001009d012a0400030002003425a00274ba00039800fef9936fff901fff901fff901fff203fe2177b203000")
	if _, _, err := Convert(animated, 256); err == nil {
		t.Fatal("animated WebP converted; it should open externally instead")
	}
}

func TestJPEGProgressiveDetection(t *testing.T) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, solid(8, 8, color.White), nil); err != nil {
		t.Fatal(err)
	}
	if jpegProgressive(b.Bytes()) {
		t.Fatal("baseline JPEG read as progressive")
	}
	sof2 := []byte{0xff, 0xd8, 0xff, 0xe0, 0, 4, 0, 0, 0xff, 0xc2, 0, 11}
	if !jpegProgressive(sof2) {
		t.Fatal("SOF2 not read as progressive")
	}
	if !jpegProgressive([]byte{0xff, 0xd8, 0x00}) {
		t.Fatal("unreadable structure should assume progressive")
	}
}
