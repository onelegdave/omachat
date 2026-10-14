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
	maxDecodeBytes = 192 << 20
	maxPixels      = 64 << 20
	// maxScaleScratch bounds the resampler's working buffer; see scaleInto.
	maxScaleScratch = 64 << 20
	// A progressive JPEG decodes every pixel's coefficients once per scan,
	// and the scan count costs only bytes. Encoders use about ten.
	maxJPEGScans    = 32
	maxJPEGScanWork = 512 << 20
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
		// Go's decoder skips ancillary chunks placed before IHDR, which could
		// then pose as the header pngBytesPerPixel reads. Require it first.
		if len(data) < 33 || !bytes.Equal(data[8:16], []byte("\x00\x00\x00\x0dIHDR")) {
			return nil, "", ErrUnsupported
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, "", err
		}
		if err := checkBudget(cfg, pngBytesPerPixel(data)); err != nil {
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
		progressive, scans, ok := scanJPEG(data)
		if !ok || scans > maxJPEGScans || int64(cfg.Width)*int64(cfg.Height)*int64(scans) > maxJPEGScanWork {
			return nil, "", ErrTooLarge
		}
		if err := checkBudget(cfg, jpegBytesPerPixel(progressive, cfg.ColorModel)); err != nil {
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

// pngBytesPerPixel reads IHDR rather than trusting DecodeConfig's color model:
// a later tRNS chunk turns gray or truecolor into NRGBA or NRGBA64 while
// decoding. An interlaced image also holds every pass beside the final image.
func pngBytesPerPixel(data []byte) int64 {
	if len(data) < 29 {
		return 8
	}
	depth, colorType, interlaced := data[24], data[25], data[28] != 0
	perPixel := int64(4)
	switch {
	case colorType == 3:
		perPixel = 1
	case depth == 16:
		perPixel = 8
	}
	if interlaced {
		perPixel *= 2
	}
	return perPixel
}

// jpegBytesPerPixel estimates what Go's decoder allocates. A progressive
// JPEG also holds a 4-byte coefficient per pixel per component until the last
// scan, so it is far costlier than a baseline image of the same size.
func jpegBytesPerPixel(progressive bool, model color.Model) int64 {
	components := int64(3)
	switch model {
	case color.GrayModel:
		components = 1
	case color.CMYKModel:
		components = 4
	}
	if progressive {
		return components * 5
	}
	return components + 1
}

// scanJPEG walks the marker segments and entropy-coded data, counting scans
// without decoding them. It reports false for a structure it cannot follow.
func scanJPEG(data []byte) (progressive bool, scans int, ok bool) {
	i := 2
	for i+1 < len(data) {
		if data[i] != 0xff {
			return false, 0, false
		}
		marker := data[i+1]
		switch {
		case marker == 0xff: // fill byte
			i++
			continue
		case marker == 0xd9: // end of image
			return progressive, scans, scans > 0
		case marker >= 0xd0 && marker <= 0xd7, marker == 0x01:
			i += 2
			continue
		}
		if i+4 > len(data) {
			return false, 0, false
		}
		length := int(binary.BigEndian.Uint16(data[i+2:]))
		if length < 2 {
			return false, 0, false
		}
		if marker == 0xc2 || marker == 0xc6 || marker == 0xca || marker == 0xce {
			progressive = true
		}
		next := i + 2 + length
		if marker == 0xda {
			scans++
			if scans > maxJPEGScans {
				return progressive, scans, true
			}
			// Entropy-coded data runs to the next marker other than a
			// stuffed zero or a restart marker.
			for next+1 < len(data) && !(data[next] == 0xff && data[next+1] != 0 && (data[next+1] < 0xd0 || data[next+1] > 0xd7)) {
				next++
			}
		}
		i = next
	}
	// Some encoders omit the end marker.
	return progressive, scans, scans > 0
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
		scaleInto(dst, img)
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

// scaleInto resamples src into dst. Catmull-Rom allocates a 32-byte scratch
// cell per destination column per source row, so a tall source first shrinks
// with the scratch-free approximate bilinear filter until that buffer fits.
func scaleInto(dst *image.NRGBA, src image.Image) {
	b, dw, dh := src.Bounds(), dst.Bounds().Dx(), dst.Bounds().Dy()
	if int64(dw)*int64(b.Dy())*32 > maxScaleScratch {
		ih := max(dh, min(b.Dy(), 2*dh, int(maxScaleScratch/(32*int64(dw)))))
		iw := max(dw, min(b.Dx(), 2*dw))
		mid := image.NewNRGBA(image.Rect(0, 0, iw, ih))
		draw.ApproxBiLinear.Scale(mid, mid.Bounds(), src, b, draw.Src, nil)
		src, b = mid, mid.Bounds()
	}
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
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
