package cloudflare_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/mediatest"
)

func TestMediaWire(t *testing.T) {
	files := []decide.Media{
		{MIMEType: "image/png", Data: mediatest.PNG(3, 2)},
		{MIMEType: "Image/JPG", Data: mediatest.JPEG(3, 2)},
		{MIMEType: "image/webp", Data: mediatest.WebP(3, 2)},
	}
	var body map[string]json.RawMessage
	client, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeEnvelope(w, &decide.Request{Model: "clef"})
	})
	req := noulRequest()
	req.Extra = map[string]any{"trace": "t1"}
	decide.WithMedia(files...)(req)
	if _, err := client.SystemOne(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	var images []struct {
		ContentType string `json:"content_type"`
		Base64      string `json:"base64"`
	}
	if err := json.Unmarshal(body["images"], &images); err != nil || len(images) != 3 {
		t.Fatalf("images = %s", body["images"])
	}
	for i, want := range []string{"image/png", "image/jpeg", "image/webp"} {
		data, _ := base64.StdEncoding.DecodeString(images[i].Base64)
		if images[i].ContentType != want || !bytes.Equal(data, files[i].Data) {
			t.Errorf("images[%d] = %s, %d bytes", i, images[i].ContentType, len(data))
		}
	}
	if string(body["trace"]) != `"t1"` || !strings.Contains(string(body["state"]), "Checkout is down.") || body["questions"] == nil {
		t.Errorf("body = %v", body)
	}
}

func TestMediaRejectedBeforeSending(t *testing.T) {
	var hits atomic.Int32
	client, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		writeEnvelope(w, &decide.Request{Model: "clef"})
	})
	png := decide.Media{MIMEType: "image/png", Data: mediatest.PNG(1, 1)}
	big := decide.Media{MIMEType: "image/png", Data: append(mediatest.PNG(1, 1), make([]byte, 3<<20)...)}
	cases := []struct {
		name  string
		state any
		media []decide.Media
		want  string
	}{
		{"pdf", nil, []decide.Media{{MIMEType: "application/pdf", Data: mediatest.PDF()}},
			"cloudflare does not take PDFs (media[0], application/pdf); it takes PNG, JPEG, and WebP images, as bytes"},
		{"audio", nil, []decide.Media{{MIMEType: "audio/wav", Data: mediatest.WAV()}}, "cloudflare does not take audio (media[0], audio/wav)"},
		{"video", nil, []decide.Media{{MIMEType: "video/mp4", Data: mediatest.MP4()}}, "cloudflare does not take videos (media[0], video/mp4)"},
		{"url", nil, []decide.Media{{MIMEType: "image/png", URL: "https://x.test/a.png"}},
			"cloudflare does not take media by URL (media[0]); attach the file's bytes in Data"},
		{"gif", nil, []decide.Media{{MIMEType: "image/gif", Data: mediatest.GIF(1, 1)}}, "cloudflare does not take image/gif (media[0])"},
		{"five", nil, []decide.Media{png, png, png, png, png}, "cloudflare takes at most 4 files (the request has 5)"},
		{"pixels", nil, []decide.Media{{MIMEType: "image/webp", Data: mediatest.WebP(5000, 4000)}},
			"cloudflare takes images of at most 16 megapixels (media[0] is 5000x4000, image/webp)"},
		{"phone photo", nil, []decide.Media{{MIMEType: "image/jpeg", Data: mediatest.PhonePhoto()}},
			"cloudflare takes images of at most 4 MiB (media[0] is "},
		{"total", nil, []decide.Media{big, big, big}, "cloudflare takes at most 8 MiB of files in all (the request has "},
		{"body", strings.Repeat("x", 6<<20), []decide.Media{big, big}, "cloudflare takes requests of at most 13 MiB (this one is "},
	}
	for _, c := range cases {
		req := noulRequest()
		decide.WithMedia(c.media...)(req)
		if c.state != nil {
			req.State = c.state
		}
		_, err := client.SystemOne(context.Background(), req)
		if !errors.Is(err, errors.ErrUnsupported) || errors.Is(err, decide.ErrInvalidRequest) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v\nwant %s", c.name, err, c.want)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("%d requests sent", hits.Load())
	}
}

func TestMediaWithoutClientIsChecked(t *testing.T) {
	var hits atomic.Int32
	_, transport := newClient(t, func(w http.ResponseWriter, r *http.Request) { hits.Add(1) })
	req := noulRequest()
	decide.WithMedia(decide.Media{MIMEType: "application/pdf", Data: mediatest.PDF()})(req)
	req.Model = "clef"
	if _, err := transport.SystemOne(context.Background(), req); !errors.Is(err, errors.ErrUnsupported) || hits.Load() != 0 {
		t.Fatalf("err = %v, hits = %d", err, hits.Load())
	}
}
