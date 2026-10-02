package cloudflare_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/cloudflare"
)

func pngData() []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	return buf.Bytes()
}

// webpHeader supplies a container header for dimension checks. The SDK does
// not decode pixels; the provider still validates complete image data.
func webpHeader(kind string, w, h int) []byte {
	var chunk []byte
	switch kind {
	case "VP8X":
		chunk = make([]byte, 10)
		for i := range 3 {
			chunk[4+i], chunk[7+i] = byte((w-1)>>(8*i)), byte((h-1)>>(8*i))
		}
	case "VP8L":
		chunk = make([]byte, 5)
		chunk[0] = 0x2f
		binary.LittleEndian.PutUint32(chunk[1:], uint32(w-1)|uint32(h-1)<<14)
	case "VP8 ":
		chunk = make([]byte, 10)
		copy(chunk[3:], []byte{0x9d, 0x01, 0x2a})
		binary.LittleEndian.PutUint16(chunk[6:], uint16(w))
		binary.LittleEndian.PutUint16(chunk[8:], uint16(h))
	}
	data := make([]byte, 20+len(chunk)+len(chunk)%2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WEBP"+kind)
	binary.LittleEndian.PutUint32(data[16:], uint32(len(chunk)))
	copy(data[20:], chunk)
	return data
}

func TestImagesWireAndOwnership(t *testing.T) {
	data := pngData()
	img, err := cloudflare.NewImage("image/png", data)
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 0 // NewImage owns an encoded copy.
	images := []cloudflare.Image{img}
	req := noulRequest()
	if err := cloudflare.SetImages(req, images...); err != nil {
		t.Fatal(err)
	}
	images[0].Base64 = "changed"
	before, _ := json.Marshal(req)
	client, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		var got decide.Request
		_ = json.NewDecoder(r.Body).Decode(&got)
		raw, _ := json.Marshal(got.Extra["images"])
		var wire []cloudflare.Image
		_ = json.Unmarshal(raw, &wire)
		if !reflect.DeepEqual(wire, []cloudflare.Image{img}) {
			t.Errorf("image wire data = %+v", wire)
		}
		writeEnvelope(w, &got)
	})
	if _, err := client.SystemOne(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(req)
	if !bytes.Equal(before, after) {
		t.Error("image request changed during send")
	}
	// The documented data URL shape also works through Request.Extra.
	req.Extra["images"] = []string{"data:image/png;base64," + img.Base64}
	// This handler expects objects, so use a second server for data URLs.
	client, _ = newClient(t, func(w http.ResponseWriter, r *http.Request) {
		var got decide.Request
		_ = json.NewDecoder(r.Body).Decode(&got)
		writeEnvelope(w, &got)
	})
	if _, err := client.SystemOne(context.Background(), req); err != nil {
		t.Fatal(err)
	}
}

func TestImageFormatsAndPixelLimits(t *testing.T) {
	var jpg bytes.Buffer
	_ = jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil)
	for _, tc := range []struct {
		mime string
		data []byte
	}{
		{"image/png", pngData()}, {"image/jpeg", jpg.Bytes()},
		{"image/webp", webpHeader("VP8X", 4000, 4000)},
		{"image/webp", webpHeader("VP8L", 1, 1)}, {"image/webp", webpHeader("VP8 ", 1, 1)},
	} {
		if _, err := cloudflare.NewImage(tc.mime, tc.data); err != nil {
			t.Errorf("%s: %v", tc.mime, err)
		}
	}
	for _, tc := range []struct {
		mime string
		data []byte
	}{
		{"image/png", jpg.Bytes()}, {"image/jpeg", pngData()}, {"image/gif", pngData()},
		{"image/png", nil}, {"image/webp", []byte("invalid")},
		{"image/webp", webpHeader("VP8X", 4001, 4000)},
		{"image/webp", webpHeader("VP8L", 4001, 4000)},
		{"image/webp", webpHeader("VP8 ", 4001, 4000)},
		{"image/png", make([]byte, (4<<20)+1)},
	} {
		if _, err := cloudflare.NewImage(tc.mime, tc.data); !errors.Is(err, decide.ErrInvalidRequest) {
			t.Errorf("accepted invalid %s image, err = %v", tc.mime, err)
		}
	}
}

func TestImageCountAndByteLimits(t *testing.T) {
	img, _ := cloudflare.NewImage("image/png", pngData())
	req := noulRequest()
	req.Extra = map[string]any{"preserve": true}
	before, _ := json.Marshal(req)
	if err := cloudflare.SetImages(req, img, img, img, img, img); !errors.Is(err, decide.ErrInvalidRequest) {
		t.Fatalf("image count accepted: %v", err)
	}
	after, _ := json.Marshal(req)
	if !bytes.Equal(before, after) {
		t.Error("SetImages mutated request on failure")
	}
	if err := cloudflare.SetImages(nil, img); !errors.Is(err, decide.ErrInvalidRequest) {
		t.Errorf("nil request: %v", err)
	}
	// A supported header with padding exercises byte limits without encoding
	// millions of pixels. The provider validates the full image stream.
	data := append(webpHeader("VP8X", 1, 1), make([]byte, (3<<20))...)
	large, err := cloudflare.NewImage("image/webp", data)
	if err != nil {
		t.Fatal(err)
	}
	if err := cloudflare.SetImages(req, large, large, large); !errors.Is(err, decide.ErrInvalidRequest) {
		t.Fatalf("combined byte limit accepted: %v", err)
	}
	bad := cloudflare.Image{ContentType: "image/png", Base64: "bad base64!"}
	if err := cloudflare.SetImages(req, bad); !errors.Is(err, decide.ErrInvalidRequest) {
		t.Fatalf("invalid base64 accepted: %v", err)
	}
	if err := cloudflare.SetImages(req); err != nil {
		t.Fatalf("empty array rejected: %v", err)
	}
	client, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		var got decide.Request
		_ = json.NewDecoder(r.Body).Decode(&got)
		writeEnvelope(w, &got)
	})
	for _, value := range []any{nil, "not-array", []any{nil}, []any{map[string]any{"content_type": "image/png"}}, []string{"data:image/png;base64,!"}} {
		req.Extra["images"] = value
		if _, err := client.SystemOne(context.Background(), req); !errors.Is(err, decide.ErrInvalidRequest) {
			t.Errorf("invalid image shape accepted: %T %v", value, err)
		}
	}
	// Even valid images cannot exceed the whole-request limit with state.
	if err := cloudflare.SetImages(req, img); err != nil {
		t.Fatal(err)
	}
	req.State = strings.Repeat("a", 13<<20)
	if _, err := client.SystemOne(context.Background(), req); !errors.Is(err, decide.ErrInvalidRequest) {
		t.Fatalf("whole-request limit accepted: %v", err)
	}
}

func TestWebPUntrustedChunkLengths(t *testing.T) {
	for _, size := range []uint32{0xffffffff, 0x80000000, 1000} {
		data := webpHeader("VP8X", 1, 1)
		binary.LittleEndian.PutUint32(data[16:20], size)
		if _, err := cloudflare.NewImage("image/webp", data); !errors.Is(err, decide.ErrInvalidRequest) {
			t.Fatalf("untrusted chunk size %d accepted: %v", size, err)
		}
	}
}

func ExampleSetImages() {
	var data bytes.Buffer
	_ = png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	img, err := cloudflare.NewImage("image/png", data.Bytes())
	if err != nil {
		panic(err)
	}
	req := decide.NewRequest("Does this image contain a receipt?")
	decide.Ask(req, "receipt", decide.Noul("Is a receipt visible?"))
	if err := cloudflare.SetImages(req, img); err != nil {
		panic(err)
	}
	images := req.Extra["images"].([]cloudflare.Image)
	fmt.Println(len(images), images[0].ContentType)
	// Output: 1 image/png
}
