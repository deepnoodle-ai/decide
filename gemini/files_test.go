package gemini_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/mediatest"
)

func filesRequest(state any, files ...decide.Media) *decide.Request {
	req := decide.NewRequest(state, decide.WithMedia(files...), decide.WithRequestExtra("store", false))
	decide.Ask(req, "a", decide.Noul("A?"))
	return req
}

type block struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	MIMEType string `json:"mime_type"`
	Data     string `json:"data"`
	URI      string `json:"uri"`
}

func TestFilesWire(t *testing.T) {
	files := []decide.Media{
		{MIMEType: "image/png", Data: mediatest.PNG(2, 2)},
		{MIMEType: "audio/x-wav", Data: mediatest.WAV()},
		{MIMEType: "video/quicktime", Data: mediatest.MP4()},
		{MIMEType: "application/pdf", Data: mediatest.PDF()},
		{MIMEType: "video/mp4", URL: "https://storage.example.com/clip.mp4"},
		{MIMEType: "application/pdf", URL: "https://generativelanguage.googleapis.com/v1beta/files/abc123"},
		{MIMEType: "image/jpeg", Data: mediatest.PhonePhoto()},
	}
	var got map[string]json.RawMessage
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, yes)
	})
	if _, err := client.SystemOne(context.Background(), filesRequest("Contract #12.", files...)); err != nil {
		t.Fatal(err)
	}
	var blocks []block
	if err := json.Unmarshal(got["input"], &blocks); err != nil || len(blocks) != 8 {
		t.Fatalf("input = %.300s", got["input"])
	}
	if blocks[0] != (block{Type: "text", Text: "Contract #12.\n"}) {
		t.Errorf("text block = %+v", blocks[0])
	}
	want := []struct{ typ, mime string }{
		{"image", "image/png"}, {"audio", "audio/wav"}, {"video", "video/mov"}, {"document", "application/pdf"},
		{"video", "video/mp4"}, {"document", "application/pdf"}, {"image", "image/jpeg"},
	}
	for i, w := range want {
		b := blocks[i+1]
		if b.Type != w.typ || b.MIMEType != w.mime {
			t.Errorf("block %d = %s %s, want %s %s", i+1, b.Type, b.MIMEType, w.typ, w.mime)
		}
		if files[i].URL != "" {
			if b.URI != files[i].URL || b.Data != "" {
				t.Errorf("block %d uri = %q", i+1, b.URI)
			}
		} else if data, _ := base64.StdEncoding.DecodeString(b.Data); string(data) != string(files[i].Data) || b.URI != "" {
			t.Errorf("block %d data differs", i+1)
		}
	}
	if string(got["store"]) != "false" || string(got["model"]) != `"gemini-decision-1"` || got["questions"] == nil {
		t.Errorf("body = %v", got)
	}
}

func TestFilesRejectedBeforeSending(t *testing.T) {
	var hits atomic.Int32
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		io.WriteString(w, yes)
	})
	pdf := append(mediatest.PDF(), make([]byte, 38_000_000)...)
	cases := []struct {
		name  string
		media []decide.Media
		want  string
	}{
		{"svg", []decide.Media{{MIMEType: "image/svg+xml", Data: []byte("<svg/>")}},
			"gemini does not take image/svg+xml (media[0]); it takes images, audio, video, and PDFs, as bytes or by HTTPS URL"},
		{"youtube", []decide.Media{{MIMEType: "video/mp4", URL: "https://www.youtube.com/watch?v=1"}},
			"gemini does not take YouTube URLs (media[0], video/mp4)"},
		{"youtu.be", []decide.Media{{MIMEType: "video/mp4", URL: "https://youtu.be/1"}}, "gemini does not take YouTube URLs"},
		{"inline pdf", []decide.Media{{MIMEType: "application/pdf", Data: pdf}},
			"gemini takes at most 50 MB of inline base64 when a PDF is inline (the request has 50.7 MB)"},
	}
	for _, c := range cases {
		_, err := client.SystemOne(context.Background(), filesRequest("x", c.media...))
		if !errors.Is(err, errors.ErrUnsupported) || errors.Is(err, decide.ErrInvalidRequest) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v\nwant %s", c.name, err, c.want)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("%d requests sent", hits.Load())
	}
	// A PDF by URL does not lower the inline limit.
	big := decide.Media{MIMEType: "image/png", Data: append(mediatest.PNG(1, 1), make([]byte, 38_000_000)...)}
	byURL := decide.Media{MIMEType: "application/pdf", URL: "https://x.test/a.pdf"}
	if _, err := client.SystemOne(context.Background(), filesRequest("x", big, byURL)); err != nil {
		t.Fatal(err)
	}
}
