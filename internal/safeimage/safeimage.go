// Package safeimage turns untrusted image bytes into a small, metadata-free
// preview for the shell to display.
//
// The panel runs inside the shared Omarchy shell process, so anything Qt
// decodes there can take the whole desktop shell down. Qt picks its decoder
// from the file's contents, keeps PNG text chunks and other metadata it
// expands, and loads any format a plugin is installed for. Only images this
// package wrote should reach it: a PNG, JPEG or GIF that Go's encoders built
// from pixels decoded here, within fixed dimension, memory and frame budgets.
package safeimage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

const (
	// MaxSourceBytes matches the attachment download cap.
	MaxSourceBytes = 25 << 20
	// maxSide rejects degenerate dimensions before any budget arithmetic.
	maxSide = 16384
	// maxDecodeBytes bounds the pixel memory one decode may allocate, and
	// maxPixels the work of scaling it down. 64 MP covers phone cameras.
	maxDecodeBytes = 256 << 20
	maxPixels      = 64 << 20
	// An animated GIF keeps its animation only within these budgets.
	// Otherwise its first frame becomes a still preview.
	maxFrames          = 500
	maxAnimationPixels = 64 << 20
	maxAnimationSide   = 1024
	jpegQuality        = 88
)

// Format names a preview's encoding. It is also its file extension.
type Format string

const (
	PNG  Format = "png"
	JPEG Format = "jpg"
	GIF  Format = "gif"
)

var (
	// ErrUnsupported means the bytes are not an image this package decodes.
	// The attachment is still available to open in an external viewer.
	ErrUnsupported = errors.New("safeimage: unsupported image format")
	// ErrTooLarge means decoding would exceed a budget.
	ErrTooLarge = errors.New("safeimage: image exceeds the preview limits")
)

// Convert decodes data and re-encodes it no larger than maxEdge pixels on its
// longest side. The result carries no metadata from the source.
func Convert(data []byte, maxEdge int) ([]byte, Format, error) {
	if len(data) > MaxSourceBytes {
		return nil, "", ErrTooLarge
	}
	if maxEdge <= 0 || maxEdge > maxSide {
		return nil, "", errors.New("safeimage: invalid preview size")
	}
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, "", err
		}
		if err := checkBudget(cfg, bytesPerPixel(cfg.ColorModel)); err != nil {
			return nil, "", err
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, "", err
		}
		return encodeStill(img, maxEdge)
	case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, "", err
		}
		if err := checkBudget(cfg, jpegBytesPerPixel(data, cfg.ColorModel)); err != nil {
			return nil, "", err
		}
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, "", err
		}
		return encodeStill(img, maxEdge)
	case bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")):
		return convertGIF(data, maxEdge)
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		// x/image/webp decodes still images only; an animated WebP is refused
		// by its decoder and stays available in an external viewer.
		cfg, err := webp.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, "", ErrUnsupported
		}
		if err := checkBudget(cfg, 8); err != nil {
			return nil, "", err
		}
		img, err := webp.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, "", ErrUnsupported
		}
		return encodeStill(img, maxEdge)
	}
	return nil, "", ErrUnsupported
}

func checkBudget(cfg image.Config, perPixel int64) error {
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxSide || cfg.Height > maxSide {
		return ErrTooLarge
	}
	pixels := int64(cfg.Width) * int64(cfg.Height)
	if pixels > maxPixels || pixels*perPixel > maxDecodeBytes {
		return ErrTooLarge
	}
	return nil
}

func bytesPerPixel(model color.Model) int64 {
	switch model {
	case color.GrayModel:
		return 1
	case color.Gray16Model:
		return 2
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	}
	if _, ok := model.(color.Palette); ok {
		return 1
	}
	return 4
}

// jpegBytesPerPixel estimates what Go's decoder allocates. A progressive
// JPEG also holds a 4-byte coefficient per pixel per component until the last
// scan, so it is far costlier than a baseline image of the same size.
func jpegBytesPerPixel(data []byte, model color.Model) int64 {
	components := int64(3)
	switch model {
	case color.GrayModel:
		components = 1
	case color.CMYKModel:
		components = 4
	}
	if jpegProgressive(data) {
		return components * 5
	}
	return components + 1
}

// jpegProgressive walks marker segments up to the first frame header. When
// the structure cannot be read it assumes the costlier progressive case.
func jpegProgressive(data []byte) bool {
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xff {
			return true
		}
		marker := data[i+1]
		if marker == 0xff {
			i++
			continue
		}
		switch {
		case marker == 0xc2 || marker == 0xc6 || marker == 0xca || marker == 0xce:
			return true
		case marker >= 0xc0 && marker <= 0xcf && marker != 0xc4 && marker != 0xc8 && marker != 0xcc:
			return false
		case marker == 0xd9 || marker == 0xda:
			return true
		}
		i += 2 + int(binary.BigEndian.Uint16(data[i+2:]))
	}
	return true
}

// encodeStill scales img into a fresh buffer and encodes that, so nothing but
// pixels survives. Opaque images become JPEG, which keeps previews of photos
// small; anything with transparency stays PNG.
func encodeStill(img image.Image, maxEdge int) ([]byte, Format, error) {
	b := img.Bounds()
	w, h := fit(b.Dx(), b.Dy(), maxEdge)
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	if w == b.Dx() && h == b.Dy() {
		draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	} else {
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	}
	var out bytes.Buffer
	if dst.Opaque() {
		if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return nil, "", err
		}
		return out.Bytes(), JPEG, nil
	}
	if err := png.Encode(&out, dst); err != nil {
		return nil, "", err
	}
	return out.Bytes(), PNG, nil
}

func fit(w, h, maxEdge int) (int, int) {
	if w <= maxEdge && h <= maxEdge {
		return w, h
	}
	if w >= h {
		return maxEdge, max(1, int(int64(h)*int64(maxEdge)/int64(w)))
	}
	return max(1, int(int64(w)*int64(maxEdge)/int64(h))), maxEdge
}

func convertGIF(data []byte, maxEdge int) ([]byte, Format, error) {
	scan, err := scanGIF(data)
	if err != nil {
		return nil, "", err
	}
	if scan.width > maxSide || scan.height > maxSide || scan.maxFramePixels > maxPixels {
		return nil, "", ErrTooLarge
	}
	animate := scan.frames > 1 && scan.frames <= maxFrames &&
		scan.totalPixels <= maxAnimationPixels &&
		scan.width <= min(maxEdge, maxAnimationSide) && scan.height <= min(maxEdge, maxAnimationSide)
	if !animate {
		// gif.Decode stops after the first frame.
		img, err := gif.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, "", err
		}
		return encodeStill(img, maxEdge)
	}
	all, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	// EncodeAll writes frames, timing, disposal and loop count only: no
	// comments or application extensions from the source survive.
	clean := &gif.GIF{
		Image:     all.Image,
		Delay:     all.Delay,
		LoopCount: all.LoopCount,
		Disposal:  all.Disposal,
		Config:    all.Config,
	}
	var out bytes.Buffer
	if err := gif.EncodeAll(&out, clean); err != nil {
		return nil, "", err
	}
	return out.Bytes(), GIF, nil
}

type gifScan struct {
	width, height  int
	frames         int
	totalPixels    int64
	maxFramePixels int64
}

// scanGIF walks the block structure without decompressing anything, so frame
// count and total frame area are known before any frame is decoded. LZW data
// can expand by three orders of magnitude, so the file size bounds nothing.
func scanGIF(data []byte) (gifScan, error) {
	var s gifScan
	if len(data) < 13 {
		return s, ErrUnsupported
	}
	s.width = int(binary.LittleEndian.Uint16(data[6:]))
	s.height = int(binary.LittleEndian.Uint16(data[8:]))
	if s.width == 0 || s.height == 0 {
		return s, ErrTooLarge
	}
	i := 13
	if flags := data[10]; flags&0x80 != 0 {
		i += 3 << ((flags & 7) + 1)
	}
	skipSubBlocks := func() bool {
		for i < len(data) {
			n := int(data[i])
			i++
			if n == 0 {
				return true
			}
			i += n
		}
		return false
	}
	for i < len(data) {
		switch data[i] {
		case 0x21: // extension: label, then sub-blocks
			i += 2
			if !skipSubBlocks() {
				return s, ErrUnsupported
			}
		case 0x2c: // image descriptor
			if i+10 > len(data) {
				return s, ErrUnsupported
			}
			fw := int64(binary.LittleEndian.Uint16(data[i+5:]))
			fh := int64(binary.LittleEndian.Uint16(data[i+7:]))
			flags := data[i+9]
			i += 10
			if flags&0x80 != 0 {
				i += 3 << ((flags & 7) + 1)
			}
			i++ // LZW minimum code size
			if !skipSubBlocks() {
				return s, ErrUnsupported
			}
			s.frames++
			s.totalPixels += fw * fh
			s.maxFramePixels = max(s.maxFramePixels, fw*fh)
			if s.frames > maxFrames && s.frames > 1 {
				// The first frame is all a still preview needs.
				return s, nil
			}
		case 0x3b: // trailer
			if s.frames == 0 {
				return s, ErrUnsupported
			}
			return s, nil
		default:
			return s, ErrUnsupported
		}
	}
	if s.frames == 0 {
		return s, ErrUnsupported
	}
	return s, nil
}
