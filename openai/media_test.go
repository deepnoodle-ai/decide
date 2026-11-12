package openai_test

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

const refundAnswer = `{"model":"gpt-6-luna","answers":[{"type":"predicate","name":"ok","probability":0.9}],"usage":{"input_tokens":1,"output_tokens":0}}`

func mediaRequest(state any, files ...decide.Media) *decide.Request {
	req := decide.NewRequest(state, decide.WithMedia(files...), decide.WithRequestExtra("store", false))
	decide.Ask(req, "ok", decide.Noul("Is it readable?"))
	return req
}

func TestMediaWire(t *testing.T) {
	png := mediatest.PNG(2, 2)
	files := []decide.Media{
		{MIMEType: "image/png", Data: png},
		{MIMEType: "image/jpg", Data: mediatest.JPEG(2, 2)},
		{MIMEType: "image/webp", Data: mediatest.WebP(2, 2)},
		{MIMEType: "image/png", URL: "https://x.test/receipt.png?sig=a&b=c"},
		{MIMEType: "image/jpeg", Data: mediatest.PhonePhoto()},
	}
	var got map[string]json.RawMessage
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, refundAnswer)
	})
	if _, err := client.SystemOne(context.Background(), mediaRequest(map[string]string{"store": "Acme"}, files...)); err != nil {
		t.Fatal(err)
	}
	var input []struct {
		Role    string `json:"role"`
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL string `json:"image_url"`
		} `json:"content"`
	}
	if err := json.Unmarshal(got["input"], &input); err != nil || len(input) != 1 || input[0].Role != "user" || len(input[0].Content) != 6 {
		t.Fatalf("input = %.300s", got["input"])
	}
	parts := input[0].Content
	if parts[0].Type != "input_text" || parts[0].Text != `{"store":"Acme"}` {
		t.Errorf("text part = %+v", parts[0])
	}
	if parts[1].Type != "input_image" || parts[1].ImageURL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(png) {
		t.Errorf("png part = %.80s", parts[1].ImageURL)
	}
	if !strings.HasPrefix(parts[2].ImageURL, "data:image/jpeg;base64,") || !strings.HasPrefix(parts[3].ImageURL, "data:image/webp;base64,") {
		t.Errorf("jpeg, webp = %.40s, %.40s", parts[2].ImageURL, parts[3].ImageURL)
	}
	if parts[4].ImageURL != "https://x.test/receipt.png?sig=a&b=c" {
		t.Errorf("url part = %s", parts[4].ImageURL)
	}
	if string(got["store"]) != "false" || string(got["model"]) != `"gpt-6-luna"` || got["questions"] == nil {
		t.Errorf("body = %v", got)
	}
}

func TestMediaRejectedBeforeSending(t *testing.T) {
	var hits atomic.Int32
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		io.WriteString(w, refundAnswer)
	})
	png := decide.Media{MIMEType: "image/png", Data: mediatest.PNG(1, 1)}
	many := make([]decide.Media, 1501)
	for i := range many {
		many[i] = png
	}
	cases := []struct {
		name  string
		media []decide.Media
		want  string
	}{
		{"pdf", []decide.Media{{MIMEType: "application/pdf", Data: mediatest.PDF()}},
			"openai does not take PDFs (media[0], application/pdf); it takes PNG, JPEG, and WebP images, as bytes or by URL"},
		{"audio", []decide.Media{{MIMEType: "audio/wav", Data: mediatest.WAV()}}, "openai does not take audio (media[0], audio/wav)"},
		{"video", []decide.Media{{MIMEType: "video/mp4", Data: mediatest.MP4()}}, "openai does not take videos (media[0], video/mp4)"},
		{"video url", []decide.Media{{MIMEType: "video/mp4", URL: "https://x.test/a.mp4"}}, "openai does not take videos (media[0], video/mp4)"},
		{"gif", []decide.Media{{MIMEType: "image/gif", Data: mediatest.GIF(1, 1)}}, "openai does not take image/gif (media[0])"},
		{"count", many, "openai takes at most 1500 files (the request has 1501)"},
	}
	for _, c := range cases {
		_, err := client.SystemOne(context.Background(), mediaRequest("a receipt", c.media...))
		if !errors.Is(err, errors.ErrUnsupported) || errors.Is(err, decide.ErrInvalidRequest) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v\nwant %s", c.name, err, c.want)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("%d requests sent", hits.Load())
	}
}

// A request without media is sent as before: input is the state's text.
func TestNoMediaInputIsText(t *testing.T) {
	var got map[string]json.RawMessage
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, refundAnswer)
	})
	if _, err := client.SystemOne(context.Background(), mediaRequest("a receipt")); err != nil {
		t.Fatal(err)
	}
	if string(got["input"]) != `"a receipt"` {
		t.Fatalf("input = %s", got["input"])
	}
}
