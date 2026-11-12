// Package mediatest builds small media files for tests, so no fixture is
// committed. Each file has a real header that decide's checks read.
package mediatest

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"sync"
)

// PNG returns a w by h grayscale PNG.
func PNG(w, h int) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h)))
	return buf.Bytes()
}

// JPEG returns a w by h grayscale JPEG.
func JPEG(w, h int) []byte {
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h)), nil)
	return buf.Bytes()
}

// GIF returns a w by h GIF.
func GIF(w, h int) []byte {
	var buf bytes.Buffer
	_ = gif.Encode(&buf, image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White}), nil)
	return buf.Bytes()
}

// WebP returns a lossless WebP header of w by h. Its pixel data is not
// valid; decide reads headers only.
func WebP(w, h int) []byte {
	chunk := make([]byte, 5)
	chunk[0] = 0x2f
	binary.LittleEndian.PutUint32(chunk[1:], uint32(w-1)|uint32(h-1)<<14)
	data := make([]byte, 20+len(chunk)+len(chunk)%2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WEBPVP8L")
	binary.LittleEndian.PutUint32(data[16:], uint32(len(chunk)))
	copy(data[20:], chunk)
	return data
}

// PDF returns the start of a PDF.
func PDF() []byte { return []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n%%EOF\n") }

// WAV returns the start of a WAV file.
func WAV() []byte { return []byte("RIFF\x24\x00\x00\x00WAVEfmt \x10\x00\x00\x00") }

// MP4 returns the start of an MP4 file.
func MP4() []byte { return append([]byte{0, 0, 0, 0x18}, "ftypisom\x00\x00\x02\x00isomiso2"...) }

var (
	photoOnce sync.Once
	photo     []byte
)

// PhonePhoto returns a 4032x3024 JPEG of noise, the size of a photo from a
// phone camera. It is more than 4 MiB. It is built once per test binary.
func PhonePhoto() []byte {
	photoOnce.Do(func() {
		img := image.NewRGBA(image.Rect(0, 0, 4032, 3024))
		r := rand.New(rand.NewPCG(1, 2))
		for i := range img.Pix {
			img.Pix[i] = byte(r.Uint32())
		}
		var buf bytes.Buffer
		_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 75})
		photo = buf.Bytes()
	})
	return photo
}
