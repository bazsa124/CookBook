// Package media normalises uploaded photos: decode, apply EXIF rotation,
// shrink, re-encode as JPEG.
//
// Re-encoding is also the privacy step: phone photos carry GPS coordinates in
// EXIF, and a re-encoded JPEG carries no EXIF at all.
package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	MaxEdge = 1600 // plenty for a phone screen and an A4 PDF
	Quality = 82
)

var ErrNotImage = errors.New("not a supported image (jpeg, png, gif, webp)")

// Compress returns a JPEG no larger than MaxEdge on its long side.
func Compress(data []byte) ([]byte, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrNotImage
	}
	if format == "jpeg" {
		img = orient(img, exifOrientation(data))
	}
	img = shrink(img, MaxEdge)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, flatten(img), &jpeg.Options{Quality: Quality}); err != nil {
		return nil, err
	}
	// Always the re-encode, even if the original was smaller: the original
	// still carries its EXIF block.
	return out.Bytes(), nil
}

func shrink(img image.Image, max int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= max && h <= max {
		return img
	}
	if w >= h {
		h, w = h*max/w, max
	} else {
		w, h = w*max/h, max
	}
	dst := image.NewRGBA(image.Rect(0, 0, max1(w), max1(h)))
	// BiLinear, not CatmullRom: an old laptop resizing a 12 MP photo should
	// answer in well under a second.
	xdraw.BiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// flatten puts transparent PNGs on white instead of JPEG's default black.
func flatten(img image.Image) image.Image {
	if o, ok := img.(interface{ Opaque() bool }); ok && o.Opaque() {
		return img
	}
	dst := image.NewRGBA(img.Bounds())
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, img.Bounds().Min, draw.Over)
	return dst
}

// orient applies an EXIF orientation (1..8) to img.
func orient(img image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	swap := o >= 5
	dw, dh := w, h
	if swap {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// exifOrientation reads the Orientation tag from a JPEG's APP1 segment, or
// returns 1. Hand-rolled because it is ~40 lines and avoids a dependency.
func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		if marker == 0xDA || size < 2 { // start of scan: no more metadata
			return 1
		}
		seg := data[i+4 : min(len(data), i+2+size)]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	ifd := int(bo.Uint32(t[4:]))
	if ifd+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[ifd:]))
	for k := 0; k < n; k++ {
		e := ifd + 2 + k*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			return int(bo.Uint16(t[e+8:]))
		}
	}
	return 1
}
