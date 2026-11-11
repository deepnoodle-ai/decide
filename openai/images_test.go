package openai_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/openai"
)

func pngData() []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	return buf.Bytes()
}

const yes = `{"model":"gpt-6-luna","answers":[{"type":"predicate","name":"a","probability":0.9}]}`

func TestImagesWireAndOwnership(t *testing.T) {
	data := pngData()
	img, err := openai.NewImage("image/png", data)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	url := "data:image/png;base64," + encoded
	if string(img) != url {
		t.Fatalf("image = %q", img)
	}
	data[0] = 0 // NewImage owns an encoded copy.
	images := []openai.Image{img}
	req := decide.NewRequest("A receipt.", decide.WithRequestExtra("store", false))
	decide.Ask(req, "a", decide.Noul("A?"))
	if err := openai.SetImages(req, images...); err != nil {
		t.Fatal(err)
	}
	images[0] = "changed"
	before, _ := json.Marshal(req)
	var got map[string]any
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, yes)
	})
	if _, err := client.SystemOne(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	want := []any{map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "input_text", "text": "A receipt."},
		map[string]any{"type": "input_image", "image_url": url},
	}}}
	if !reflect.DeepEqual(got["input"], want) {
		t.Errorf("input = %#v", got["input"])
	}
	if _, ok := got["images"]; ok {
		t.Error("images sent as a top-level field")
	}
	if got["store"] != false {
		t.Errorf("other extra fields dropped: %v", got)
	}
	after, _ := json.Marshal(req)
	if !bytes.Equal(before, after) {
		t.Error("image request changed during send")
	}

	// The cloudflare.SetImages object shape is sent the same way.
	req.Extra["images"] = []map[string]string{{"content_type": "image/png", "base64": encoded}}
	if _, err := client.SystemOne(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got["input"], want) {
		t.Errorf("object shape input = %#v", got["input"])
	}

	// Base64 wrapped at 76 columns is sent without its line breaks.
	var wrapped strings.Builder
	for i := 0; i < len(encoded); i += 76 {
		wrapped.WriteString(encoded[i:min(i+76, len(encoded))] + "\r\n")
	}
	req.Extra["images"] = []map[string]string{{"content_type": "image/png", "base64": wrapped.String()}}
	if _, err := client.SystemOne(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got["input"], want) {
		t.Errorf("wrapped base64 input = %#v", got["input"])
	}

	// Without text state, only the images are sent.
	req.State = ""
	if _, err := client.SystemOne(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	want = []any{map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "input_image", "image_url": url},
	}}}
	if !reflect.DeepEqual(got["input"], want) {
		t.Errorf("empty state input = %#v", got["input"])
	}

	// No images: the input stays a string, never an empty content list.
	if err := openai.SetImages(req); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SystemOne(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if got["input"] != "" {
		t.Errorf("no images input = %#v", got["input"])
	}
}

func TestImageFormats(t *testing.T) {
	var jpg bytes.Buffer
	_ = jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil)
	webp := []byte("RIFF\x04\x00\x00\x00WEBPVP8X")
	for _, tc := range []struct {
		mime string
		data []byte
	}{{"image/png", pngData()}, {"image/jpeg", jpg.Bytes()}, {"image/webp", webp}} {
		if _, err := openai.NewImage(tc.mime, tc.data); err != nil {
			t.Errorf("%s: %v", tc.mime, err)
		}
	}
	for _, tc := range []struct {
		mime string
		data []byte
	}{
		{"image/png", jpg.Bytes()}, {"image/jpeg", pngData()}, {"image/gif", pngData()},
		{"image/png", nil}, {"image/webp", []byte("invalid")},
	} {
		if _, err := openai.NewImage(tc.mime, tc.data); !errors.Is(err, decide.ErrInvalidRequest) {
			t.Errorf("accepted invalid %s image, err = %v", tc.mime, err)
		}
	}
}

func TestInvalidImagesAreNotSent(t *testing.T) {
	img, _ := openai.NewImage("image/png", pngData())
	req := decide.NewRequest("hello")
	decide.Ask(req, "a", decide.Noul("A?"))
	req.Extra = map[string]any{"preserve": true}
	before, _ := json.Marshal(req)
	if err := openai.SetImages(req, img, "https://example.com/a.png"); !errors.Is(err, decide.ErrInvalidRequest) {
		t.Fatalf("remote URL accepted: %v", err)
	}
	after, _ := json.Marshal(req)
	if !bytes.Equal(before, after) {
		t.Error("SetImages mutated request on failure")
	}
	if err := openai.SetImages(nil, img); !errors.Is(err, decide.ErrInvalidRequest) {
		t.Errorf("nil request: %v", err)
	}
	if err := openai.SetImages(req); err != nil {
		t.Fatalf("empty array rejected: %v", err)
	}
	var calls atomic.Int32
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	for _, value := range []any{nil, "not-array", []any{nil}, []any{7},
		[]any{map[string]any{"content_type": "image/png"}},
		[]string{"data:image/png;base64,!"}, []string{"https://example.com/a.png"}} {
		req.Extra["images"] = value
		if _, err := client.SystemOne(context.Background(), req); !errors.Is(err, decide.ErrInvalidRequest) {
			t.Errorf("invalid image shape accepted: %#v %v", value, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("%d invalid requests were sent", calls.Load())
	}
}

func ExampleSetImages() {
	var data bytes.Buffer
	_ = png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	img, err := openai.NewImage("image/png", data.Bytes())
	if err != nil {
		panic(err)
	}
	req := decide.NewRequest("A photo of a receipt.")
	decide.Ask(req, "legible", decide.Noul("Is the total legible?"))
	if err := openai.SetImages(req, img); err != nil {
		panic(err)
	}
	fmt.Println(len(req.Extra["images"].([]openai.Image)))
	// Output: 1
}
