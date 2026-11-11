package gemini_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/cloudflare"
	"github.com/deepnoodle-ai/decide/gemini"
)

const yes = `{"answers":{"a":{"type":"noul","noul":0.9,"confidence":0.9}}}`

func TestMediaWire(t *testing.T) {
	pdf := []byte("%PDF-1.7 test")
	doc, err := gemini.NewMedia(gemini.KindDocument, "application/pdf", pdf)
	if err != nil {
		t.Fatal(err)
	}
	clip, err := gemini.MediaURI(gemini.KindVideo, "video/mp4", "https://example.com/clip.mp4")
	if err != nil {
		t.Fatal(err)
	}
	clip.Resolution = "low"
	var got map[string]any
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, yes)
	})
	req := decide.NewRequest("Contract #12.", decide.WithRequestExtra("store", false))
	decide.Ask(req, "a", decide.Noul("A?"))
	media := []gemini.Media{doc, clip}
	if err := gemini.SetMedia(req, media...); err != nil {
		t.Fatal(err)
	}
	media[0] = gemini.Media{} // SetMedia keeps a copy
	if _, err := client.SystemOne(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	want := []any{
		map[string]any{"type": "text", "text": "Contract #12.\n"},
		map[string]any{"type": "document", "mime_type": "application/pdf", "data": base64.StdEncoding.EncodeToString(pdf)},
		map[string]any{"type": "video", "mime_type": "video/mp4", "uri": "https://example.com/clip.mp4", "resolution": "low"},
	}
	if !reflect.DeepEqual(got["input"], want) {
		t.Errorf("input = %#v", got["input"])
	}
	if _, ok := got["media"]; ok || got["store"] != false {
		t.Errorf("extra fields: %v", got)
	}

	// cloudflare.SetImages writes images that the transport sends too, and
	// empty state sends only the media.
	req = decide.NewRequest("")
	decide.Ask(req, "a", decide.Noul("A?"))
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	pngData := buf.Bytes()
	im, err := cloudflare.NewImage("image/png", pngData)
	if err != nil {
		t.Fatal(err)
	}
	if err := cloudflare.SetImages(req, im); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SystemOne(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	want = []any{map[string]any{"type": "image", "mime_type": "image/png", "data": base64.StdEncoding.EncodeToString(pngData)}}
	if !reflect.DeepEqual(got["input"], want) {
		t.Errorf("image input = %#v", got["input"])
	}
}

func TestWrappedBase64IsSentWithoutBreaks(t *testing.T) {
	var got map[string]any
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, yes)
	})
	data := strings.Repeat("audio bytes ", 20)
	encoded := base64.StdEncoding.EncodeToString([]byte(data))
	req := decide.NewRequest("")
	decide.Ask(req, "a", decide.Noul("A?"))
	req.Extra = map[string]any{"media": []gemini.Media{{Kind: gemini.KindAudio, MIMEType: "audio/mpeg", Data: encoded[:76] + "\r\n" + encoded[76:]}}}
	if _, err := client.SystemOne(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(got["input"]); !strings.Contains(string(b), `"data":"`+encoded+`"`) {
		t.Fatalf("input = %s", b)
	}
}

func TestInvalidMedia(t *testing.T) {
	for name, make := range map[string]func() (gemini.Media, error){
		"empty":      func() (gemini.Media, error) { return gemini.NewMedia(gemini.KindAudio, "audio/mpeg", nil) },
		"wrong mime": func() (gemini.Media, error) { return gemini.NewMedia(gemini.KindAudio, "video/mp4", []byte("x")) },
		"octet-stream": func() (gemini.Media, error) {
			return gemini.NewMedia(gemini.KindDocument, "application/octet-stream", []byte("x"))
		},
		"kind": func() (gemini.Media, error) { return gemini.NewMedia("text", "text/plain", []byte("x")) },
		"youtube": func() (gemini.Media, error) {
			return gemini.MediaURI(gemini.KindVideo, "video/mp4", "https://www.youtube.com/watch?v=x")
		},
		"gs": func() (gemini.Media, error) {
			return gemini.MediaURI(gemini.KindVideo, "video/mp4", "gs://bucket/x.mp4")
		},
	} {
		if _, err := make(); !errors.Is(err, decide.ErrInvalidRequest) {
			t.Errorf("%s: %v", name, err)
		}
	}
	req := decide.NewRequest("x")
	bad := gemini.Media{Kind: gemini.KindAudio, MIMEType: "audio/mpeg", Data: "eA==", Resolution: "low"}
	if err := gemini.SetMedia(req, bad); !errors.Is(err, decide.ErrInvalidRequest) || req.Extra != nil {
		t.Fatalf("audio with a resolution: %v %v", err, req.Extra)
	}
	if k, ok := gemini.MediaKind("audio/flac"); !ok || k != gemini.KindAudio {
		t.Fatalf("MediaKind = %q %v", k, ok)
	}
}

func TestInlineLimits(t *testing.T) {
	var calls int
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	big := base64.StdEncoding.EncodeToString(make([]byte, 39_000_000)) // 52 MB of base64
	for name, m := range map[string]gemini.Media{
		"pdf over 50 MB": {Kind: gemini.KindDocument, MIMEType: "application/pdf", Data: big},
	} {
		req := decide.NewRequest("x")
		decide.Ask(req, "a", decide.Noul("A?"))
		req.Extra = map[string]any{"media": []gemini.Media{m}}
		if _, err := client.SystemOne(context.Background(), req); !errors.Is(err, decide.ErrInvalidRequest) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The same size as video is under the 100 MB limit.
	req := decide.NewRequest("x")
	decide.Ask(req, "a", decide.Noul("A?"))
	if err := gemini.SetMedia(req, gemini.Media{Kind: gemini.KindVideo, MIMEType: "video/mp4", Data: big}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("%d requests sent", calls)
	}
}
