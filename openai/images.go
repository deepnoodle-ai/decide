package openai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	"github.com/deepnoodle-ai/decide"
)

// Image is an embedded image as a base64 data URL, such as
// "data:image/png;base64,iVBOR...".
type Image string

// NewImage encodes data as a base64 data URL and checks its format.
// contentType must be image/png, image/jpeg, or image/webp.
func NewImage(contentType string, data []byte) (Image, error) {
	if err := checkImage(contentType, data); err != nil {
		return "", err
	}
	return Image("data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)), nil
}

// SetImages replaces req.Extra["images"] with a copy of images after
// validating them. It leaves req unchanged on failure. The transport sends
// them as input_image parts after the state. Do not call it while req is in
// use by another goroutine.
func SetImages(req *decide.Request, images ...Image) error {
	if req == nil {
		return invalid("request is nil")
	}
	copyImages := append([]Image{}, images...)
	if _, err := imageURLs(copyImages); err != nil {
		return err
	}
	if req.Extra == nil {
		req.Extra = make(map[string]any)
	}
	req.Extra["images"] = copyImages
	return nil
}

// imageURLs validates Extra["images"] and returns each image as a data URL.
// An image is a data URL or, as cloudflare.SetImages writes it, an object
// with content_type and base64 members.
func imageURLs(value any) ([]string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, invalid("images cannot be encoded")
	}
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil || entries == nil {
		return nil, invalid("images must be an array")
	}
	urls := make([]string, 0, len(entries))
	for _, entry := range entries {
		var contentType, data, dataURL string
		var obj struct {
			ContentType string `json:"content_type"`
			Base64      string `json:"base64"`
		}
		switch {
		case json.Unmarshal(entry, &dataURL) == nil:
			header, body, ok := strings.Cut(dataURL, ",")
			header = strings.ToLower(header)
			if !ok || !strings.HasPrefix(header, "data:") || !strings.HasSuffix(header, ";base64") {
				return nil, invalid("images require embedded base64 data, not remote URLs")
			}
			contentType, data = strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"), body
		case bytes.HasPrefix(entry, []byte("{")) && json.Unmarshal(entry, &obj) == nil:
			contentType, data = obj.ContentType, obj.Base64
		default:
			return nil, invalid("image must be a data URL or a content_type/base64 object")
		}
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, invalid("image contains invalid base64 data")
		}
		if err := checkImage(contentType, decoded); err != nil {
			return nil, err
		}
		// Encode again so line breaks that decoding skips never reach the API.
		urls = append(urls, "data:"+contentType+";base64,"+base64.StdEncoding.EncodeToString(decoded))
	}
	return urls, nil
}

// checkImage reads only the image header. The API validates the rest.
func checkImage(contentType string, data []byte) error {
	if len(data) == 0 {
		return invalid("image is empty")
	}
	switch contentType {
	case "image/webp":
		if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
			return invalid("image data does not match its content type")
		}
	case "image/png", "image/jpeg":
		_, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || "image/"+format != contentType {
			return invalid("image data does not match its content type")
		}
	default:
		return invalid("image content type must be image/png, image/jpeg, or image/webp")
	}
	return nil
}
