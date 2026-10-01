package cloudflare

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	"github.com/deepnoodle-ai/sod"
)

const (
	maxRequestBytes = 13 << 20
	maxImageBytes   = 4 << 20
	maxTotalImages  = 8 << 20
	maxImagePixels  = 16_000_000
)

// Image is an embedded image in the Workers AI wire format.
type Image struct {
	ContentType string `json:"content_type"`
	Base64      string `json:"base64"`
}

// NewImage copies data into base64 and checks its format, dimensions, and
// size. contentType must be image/png, image/jpeg, or image/webp.
func NewImage(contentType string, data []byte) (Image, error) {
	if len(data) > maxImageBytes {
		return Image{}, invalid("each image must be at most 4 MiB")
	}
	im := Image{ContentType: contentType, Base64: base64.StdEncoding.EncodeToString(data)}
	if _, err := checkImage(im); err != nil {
		return Image{}, err
	}
	return im, nil
}

// SetImages replaces req.Extra["images"] with a copy of images after
// validating them. It leaves req unchanged on failure. It accepts at most
// four images, with at most 8 MiB of decoded data combined. Do not call it
// while req is in use by another goroutine.
func SetImages(req *sod.Request, images ...Image) error {
	if req == nil {
		return invalid("request is nil")
	}
	copyImages := append([]Image{}, images...)
	if err := validateImages(copyImages); err != nil {
		return err
	}
	if req.Extra == nil {
		req.Extra = make(map[string]any)
	}
	req.Extra["images"] = copyImages
	return nil
}

func validateImages(value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return invalid("images cannot be encoded")
	}
	if len(raw) > maxRequestBytes {
		return invalid("images exceed the 13 MiB request limit")
	}
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil || entries == nil {
		return invalid("images must be an array")
	}
	if len(entries) > 4 {
		return invalid("at most four images are supported")
	}
	total := 0
	for _, entry := range entries {
		var im Image
		var dataURL string
		if json.Unmarshal(entry, &dataURL) == nil {
			header, data, ok := strings.Cut(dataURL, ",")
			header = strings.ToLower(header)
			if !ok || !strings.HasPrefix(header, "data:") || !strings.HasSuffix(header, ";base64") {
				return invalid("images require embedded base64 data, not remote URLs")
			}
			im.ContentType = strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
			im.Base64 = data
		} else if json.Unmarshal(entry, &im) != nil {
			return invalid("image must be a data URL or a content_type/base64 object")
		}
		n, err := checkImage(im)
		if err != nil {
			return err
		}
		total += n
		if total > maxTotalImages {
			return invalid("combined images must be at most 8 MiB")
		}
	}
	return nil
}

func checkImage(im Image) (int, error) {
	if im.ContentType != "image/png" && im.ContentType != "image/jpeg" && im.ContentType != "image/webp" {
		return 0, invalid("image content_type must be image/png, image/jpeg, or image/webp")
	}
	if base64.StdEncoding.DecodedLen(len(im.Base64)) > maxImageBytes+2 {
		return 0, invalid("each image must be at most 4 MiB")
	}
	data, err := base64.StdEncoding.DecodeString(im.Base64)
	if err != nil || len(data) == 0 {
		return 0, invalid("image contains invalid or empty base64 data")
	}
	if len(data) > maxImageBytes {
		return 0, invalid("each image must be at most 4 MiB")
	}
	var width, height int
	if im.ContentType == "image/webp" {
		width, height, err = webpDimensions(data)
	} else {
		var cfg image.Config
		var format string
		cfg, format, err = image.DecodeConfig(bytes.NewReader(data))
		if err == nil && "image/"+format != im.ContentType {
			err = fmt.Errorf("content type does not match image data")
		}
		width, height = cfg.Width, cfg.Height
	}
	if err != nil || width <= 0 || height <= 0 {
		return 0, invalid("image data does not match its content_type or has an invalid header")
	}
	if int64(width)*int64(height) > maxImagePixels {
		return 0, invalid("each image must be at most 16 megapixels")
	}
	return len(data), nil
}

// webpDimensions reads only the bounded container and image headers. It
// does not decode pixels. The provider validates the complete image.
func webpDimensions(data []byte) (int, int, error) {
	bad := fmt.Errorf("invalid WebP header")
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, bad
	}
	for offset := 12; offset+8 <= len(data); {
		chunkSize := binary.LittleEndian.Uint32(data[offset+4 : offset+8])
		if uint64(chunkSize) > uint64(len(data)-offset-8) {
			return 0, 0, bad
		}
		n := int(chunkSize) // bounded by the input length before conversion
		chunk := data[offset+8 : offset+8+n]
		switch string(data[offset : offset+4]) {
		case "VP8X":
			if len(chunk) < 10 {
				return 0, 0, bad
			}
			read24 := func(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }
			return 1 + read24(chunk[4:7]), 1 + read24(chunk[7:10]), nil
		case "VP8L":
			if len(chunk) < 5 || chunk[0] != 0x2f {
				return 0, 0, bad
			}
			bits := binary.LittleEndian.Uint32(chunk[1:5])
			return 1 + int(bits&0x3fff), 1 + int((bits>>14)&0x3fff), nil
		case "VP8 ":
			if len(chunk) < 10 || !bytes.Equal(chunk[3:6], []byte{0x9d, 0x01, 0x2a}) {
				return 0, 0, bad
			}
			return int(binary.LittleEndian.Uint16(chunk[6:8]) & 0x3fff), int(binary.LittleEndian.Uint16(chunk[8:10]) & 0x3fff), nil
		}
		offset += 8 + n + n%2
	}
	return 0, 0, bad
}
