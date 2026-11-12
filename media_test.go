package decide_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

func testPNG(t testing.TB) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// mediaStub is a stub transport that takes media.
type mediaStub struct {
	stubTransport
	check func(*decide.Request) error
}

func (m mediaStub) CheckMedia(req *decide.Request) error {
	if m.check == nil {
		return nil
	}
	return m.check(req)
}

func noulRequest(opts ...decide.RequestOption) *decide.Request {
	req := decide.NewRequest("a receipt", opts...)
	req.Questions["ok"] = decide.Noul("Is it readable?")
	return req
}

func TestMediaValidate(t *testing.T) {
	pngData := testPNG(t)
	cases := []struct {
		name string
		m    decide.Media
		want string // "" means valid
	}{
		{"png", decide.Media{MIMEType: "image/png", Data: pngData}, ""},
		{"alias and case", decide.Media{MIMEType: "Image/PNG; x=1", Data: pngData}, ""},
		{"url", decide.Media{MIMEType: "video/mp4", URL: "https://x.test/a.mp4?sig=1"}, ""},
		{"unknown bytes as declared", decide.Media{MIMEType: "audio/aiff", Data: []byte("FORM")}, ""},
		{"frame-sync mp3 as audio/mpeg", decide.Media{MIMEType: "audio/mpeg", Data: []byte{0xff, 0xfb, 0x90, 0x64}}, ""},
		{"webm as audio/webm", decide.Media{MIMEType: "audio/webm", Data: []byte("\x1a\x45\xdf\xa3\x9f\x42\x86\x81")}, ""},
		{"ftypqt mov as video/quicktime", decide.Media{MIMEType: "video/quicktime", Data: append([]byte{0, 0, 0, 0x14}, "ftypqt  \x00\x00\x02\x00qt  "...)}, ""},
		{"flac as audio/x-flac", decide.Media{MIMEType: "audio/x-flac", Data: []byte("fLaC\x00\x00\x00\x22")}, ""},
		{"flac as pdf", decide.Media{MIMEType: "application/pdf", Data: []byte("fLaC\x00\x00\x00\x22")}, "media[0] is declared application/pdf, but its bytes are FLAC"},
		{"no mime", decide.Media{Data: pngData}, "media[0] has no MIMEType"},
		{"text", decide.Media{MIMEType: "text/plain", Data: []byte("x")}, `media[0] MIME type "text/plain" is not an image, audio, video, or PDF type`},
		{"both", decide.Media{MIMEType: "image/png", Data: pngData, URL: "https://x.test/a.png"}, "media[0] sets both Data and URL; set one"},
		{"neither", decide.Media{MIMEType: "image/png"}, "media[0] has no Data or URL"},
		{"empty", decide.Media{MIMEType: "image/png", Data: []byte{}}, "media[0] Data is empty"},
		{"http", decide.Media{MIMEType: "image/png", URL: "http://x.test/a.png"}, "media[0] URL must be an absolute https URL"},
		{"gs", decide.Media{MIMEType: "application/pdf", URL: "gs://bucket/a.pdf"}, "media[0] URL must be an absolute https URL"},
		{"relative", decide.Media{MIMEType: "image/png", URL: "/a.png"}, "media[0] URL must be an absolute https URL"},
		{"png as jpeg", decide.Media{MIMEType: "image/jpeg", Data: pngData}, "media[0] is declared image/jpeg, but its bytes are PNG"},
		{"not a png", decide.Media{MIMEType: "image/png", Data: []byte("hello")}, "media[0] is declared image/png, but its bytes are not that format"},
		{"truncated png", decide.Media{MIMEType: "image/png", Data: pngData[:12]}, "media[0] has an invalid PNG header"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := noulRequest(decide.WithMedia(c.m)).Validate()
			if c.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, decide.ErrInvalidRequest) || !strings.HasSuffix(err.Error(), c.want) {
				t.Fatalf("err = %v, want ErrInvalidRequest ending %q", err, c.want)
			}
		})
	}
}

func TestMediaKindAndString(t *testing.T) {
	for mt, want := range map[string]decide.MediaKind{
		"image/jpg":       decide.MediaImage,
		"audio/x-wav":     decide.MediaAudio,
		"video/quicktime": decide.MediaVideo,
		"application/pdf": decide.MediaPDF,
		"text/plain":      "",
	} {
		if got := (decide.Media{MIMEType: mt}).Kind(); got != want {
			t.Errorf("%s Kind = %q, want %q", mt, got, want)
		}
	}
	if s := (decide.Media{MIMEType: "image/png", Data: make([]byte, 48213)}).String(); s != "image/png, 48213 bytes" {
		t.Errorf("String = %q", s)
	}
	m := decide.Media{MIMEType: "video/mp4", URL: "https://user:pw@x.test/clip.mp4?X-Goog-Signature=secret#t=1"}
	if s := m.String(); s != "video/mp4, https://x.test/clip.mp4" {
		t.Errorf("String = %q", s)
	}
}

func TestWithMediaAppends(t *testing.T) {
	a := decide.Media{MIMEType: "image/png", Data: []byte("a")}
	b := decide.Media{MIMEType: "image/png", Data: []byte("b")}
	req := decide.NewRequest("x", decide.WithMedia(a), decide.WithMedia(b))
	if len(req.Media) != 2 || req.Media[0].Data[0] != 'a' || req.Media[1].Data[0] != 'b' {
		t.Fatalf("Media = %v", req.Media)
	}
	if decide.NewRequest("x").Media != nil {
		t.Fatal("Media is not nil without WithMedia")
	}
}

func TestMarshalJSONWritesNoMedia(t *testing.T) {
	plain, err := noulRequest().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	with, err := noulRequest(decide.WithMedia(decide.Media{MIMEType: "image/png", Data: testPNG(t)})).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, with) {
		t.Fatalf("with media:\n%s\nwithout:\n%s", with, plain)
	}
}

func TestCustomTransportWithoutMediaGetsNoCall(t *testing.T) {
	var calls atomic.Int32
	tr := stubTransport{systemOne: func(context.Context, *decide.Request) (*decide.Response, error) {
		calls.Add(1)
		return &decide.Response{}, nil
	}}
	client := stubClient(t, tr)
	_, err := client.SystemOne(t.Context(), noulRequest(decide.WithMedia(decide.Media{MIMEType: "image/png", Data: testPNG(t)})))
	if !errors.Is(err, errors.ErrUnsupported) || !strings.Contains(err.Error(), "must implement decide.MediaTransport") {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("%d calls", calls.Load())
	}
	// Without media, the same transport is called as before.
	if _, err := client.SystemOne(t.Context(), noulRequest(), decide.WithCallValidation(false)); err != nil || calls.Load() != 1 {
		t.Fatalf("err = %v, calls = %d", err, calls.Load())
	}
}

func TestCheckMediaRunsOnceBeforeAttempts(t *testing.T) {
	var checks, calls atomic.Int32
	reject := errors.New("no")
	tr := mediaStub{
		stubTransport: stubTransport{systemOne: func(context.Context, *decide.Request) (*decide.Response, error) {
			calls.Add(1)
			return nil, &decide.APIError{StatusCode: 503}
		}},
		check: func(*decide.Request) error { checks.Add(1); return nil },
	}
	client := stubClient(t, tr, decide.WithRetryBackoff(time.Millisecond, time.Millisecond))
	req := noulRequest(decide.WithMedia(decide.Media{MIMEType: "image/png", Data: testPNG(t)}))
	if _, err := client.SystemOne(t.Context(), req); err == nil {
		t.Fatal("no error")
	}
	if checks.Load() != 1 || calls.Load() != 3 {
		t.Fatalf("checks = %d, calls = %d; want 1 and 3 (a 503 is retried)", checks.Load(), calls.Load())
	}
	tr.check = func(*decide.Request) error { return reject }
	calls.Store(0)
	if _, err := stubClient(t, tr).SystemOne(t.Context(), req); !errors.Is(err, reject) || calls.Load() != 0 {
		t.Fatalf("err = %v, calls = %d", err, calls.Load())
	}
}

func TestMediaAttemptTimeout(t *testing.T) {
	var got time.Duration
	tr := mediaStub{stubTransport: stubTransport{systemOne: func(ctx context.Context, req *decide.Request) (*decide.Response, error) {
		dl, _ := ctx.Deadline()
		got = time.Until(dl).Round(time.Second)
		return &decide.Response{Answers: map[string]decide.Answer{}}, nil
	}}}
	media := decide.WithMedia(decide.Media{MIMEType: "image/png", Data: testPNG(t)})
	cases := []struct {
		name   string
		client []decide.ClientOption
		call   []decide.CallOption
		req    *decide.Request
		want   time.Duration
	}{
		{"text", nil, nil, noulRequest(), 10 * time.Second},
		{"media", nil, nil, noulRequest(media), 5 * time.Minute},
		{"client option wins", []decide.ClientOption{decide.WithAttemptTimeout(time.Minute)}, nil, noulRequest(media), time.Minute},
		{"call option wins", nil, []decide.CallOption{decide.WithCallAttemptTimeout(2 * time.Minute)}, noulRequest(media), 2 * time.Minute},
	}
	for _, c := range cases {
		client := stubClient(t, tr, c.client...)
		if _, err := client.SystemOne(t.Context(), c.req, append(c.call, decide.WithCallValidation(false))...); err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s: attempt timeout %s, want %s", c.name, got, c.want)
		}
	}
}

func TestMediaAttemptThatTimesOutIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	tr := mediaStub{stubTransport: stubTransport{systemOne: func(ctx context.Context, req *decide.Request) (*decide.Response, error) {
		calls.Add(1)
		<-ctx.Done()
		return nil, ctx.Err()
	}}}
	client := stubClient(t, tr, decide.WithAttemptTimeout(10*time.Millisecond), decide.WithRetryBackoff(time.Millisecond, time.Millisecond))
	_, err := client.SystemOne(t.Context(), noulRequest(decide.WithMedia(decide.Media{MIMEType: "image/png", Data: testPNG(t)})))
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("err = %v, calls = %d; want one attempt", err, calls.Load())
	}
	calls.Store(0)
	if _, err := client.SystemOne(t.Context(), noulRequest()); err == nil || calls.Load() != 3 {
		t.Fatalf("text request: err = %v, calls = %d; want 3 attempts", err, calls.Load())
	}
}

func TestLogSummarizesMedia(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	tr := mediaStub{stubTransport: answering(map[string]decide.Answer{"ok": decidetest.NoulAnswer(0.9)})}
	client := stubClient(t, tr, decide.WithLogger(logger), decide.WithLogBodies(true))
	data := append(testPNG(t), bytes.Repeat([]byte("Z"), 3000)...)
	if _, err := client.SystemOne(t.Context(), noulRequest(decide.WithMedia(decide.Media{MIMEType: "image/png", Data: data}))); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `media="[image/png, 3076 bytes]"`) || strings.Contains(out, "WlpaWlpa") {
		t.Fatalf("log:\n%s", out)
	}
}

func TestHTTPTransportRejectsMedia(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	client, err := decide.NewClient(decide.WithoutEnvironment(), decide.WithAPIKey("test-key-00000000"), decide.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		m     decide.Media
		reads string
	}{
		{decide.Media{MIMEType: "image/png", Data: testPNG(t)}, "images"},
		{decide.Media{MIMEType: "application/pdf", Data: []byte("%PDF-1.7")}, "PDFs"},
		{decide.Media{MIMEType: "audio/wav", Data: []byte("RIFF\x00\x00\x00\x00WAVE")}, "audio"},
		{decide.Media{MIMEType: "video/mp4", URL: "https://x.test/a.mp4"}, "videos"},
	} {
		_, err := client.SystemOne(t.Context(), noulRequest(decide.WithMedia(c.m)))
		want := "typesafe does not take media yet (media[0], " + c.m.MIMEType + "); choose a provider that reads " + c.reads + ", such as backend.Gemini"
		if !errors.Is(err, errors.ErrUnsupported) || errors.Is(err, decide.ErrInvalidRequest) || !strings.HasSuffix(err.Error(), want) {
			t.Errorf("%s: err = %v\nwant suffix %s", c.m.MIMEType, err, want)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("%d requests sent", hits.Load())
	}
}

func TestDecidetestRejectsMediaByDefault(t *testing.T) {
	srv := decidetest.NewServer(t)
	_, err := decide.Eval(t.Context(), srv.Client(t), "a receipt", decide.Noul("Readable?"),
		decide.WithMedia(decide.Media{MIMEType: "image/png", Data: testPNG(t)}))
	if !errors.Is(err, errors.ErrUnsupported) || len(srv.Requests()) != 0 {
		t.Fatalf("err = %v, %d requests", err, len(srv.Requests()))
	}
	want := "typesafe does not take media yet (media[0], image/png); choose a provider that reads images, such as backend.Gemini" +
		" (in a test, pass decidetest.AcceptMedia() to decidetest.NewServer to accept media)"
	if !strings.HasSuffix(err.Error(), want) {
		t.Fatalf("err = %v\nwant suffix %s", err, want)
	}
	// Text requests and models still work on the default client.
	if _, err := decide.Eval(t.Context(), srv.Client(t), "a receipt", decide.Noul("Readable?")); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Client(t).Models.List(t.Context()); err != nil {
		t.Fatal(err)
	}
	// A connection option still works; media is still rejected.
	_, err = decide.Eval(t.Context(), srv.Client(t, decide.WithUserAgent("app/1")), "a receipt", decide.Noul("Readable?"),
		decide.WithMedia(decide.Media{MIMEType: "image/png", Data: testPNG(t)}))
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("with WithUserAgent: err = %v", err)
	}
}

func TestEvalAndPickTakeMedia(t *testing.T) {
	srv := decidetest.NewServer(t, decidetest.AcceptMedia())
	client := srv.Client(t)
	photo := decide.Media{MIMEType: "image/png", Data: testPNG(t)}
	clip := decide.Media{MIMEType: "video/mp4", URL: "https://x.test/clip.mp4"}
	if _, err := decide.Eval(t.Context(), client, map[string]string{"store": "Acme"}, decide.Noul("Readable?"),
		decide.WithMedia(photo, clip), decide.WithRequestExtra("trace", "t1")); err != nil {
		t.Fatal(err)
	}
	if _, err := decide.Pick(t.Context(), client, "a receipt", "Which total?",
		[]decide.Candidate[int]{{Item: 1, Description: "$10"}, {Item: 2, Description: "$12"}}, decide.WithMedia(photo)); err != nil {
		t.Fatal(err)
	}
	recs := srv.Requests()
	if len(recs) != 2 {
		t.Fatalf("%d requests", len(recs))
	}
	got := recs[0].Request
	if len(got.Media) != 2 || got.Media[0].MIMEType != "image/png" || !bytes.Equal(got.Media[0].Data, photo.Data) ||
		got.Media[1].URL != clip.URL || got.Media[1].Data != nil {
		t.Fatalf("Media = %v", got.Media)
	}
	if raw, _ := got.State.(json.RawMessage); !jsonEqual(t, raw, []byte(`{"store":"Acme"}`)) {
		t.Fatalf("State = %s", raw)
	}
	if len(got.Extra) != 1 || got.Extra["trace"] == nil {
		t.Fatalf("Extra = %v", got.Extra)
	}
	if len(recs[1].Request.Media) != 1 {
		t.Fatalf("Pick media = %v", recs[1].Request.Media)
	}
}

func TestDecidetestMediaRetries(t *testing.T) {
	srv := decidetest.NewServer(t, decidetest.AcceptMedia())
	srv.FailNext(503)
	photo := decide.Media{MIMEType: "image/png", Data: testPNG(t)}
	if _, err := decide.Eval(t.Context(), srv.Client(t), "a receipt", decide.Noul("Readable?"), decide.WithMedia(photo)); err != nil {
		t.Fatal(err)
	}
	recs := srv.Requests()
	if len(recs) != 2 || len(recs[0].Request.Media) != 1 || len(recs[1].Request.Media) != 1 {
		t.Fatalf("records = %d", len(recs))
	}
	// Files that are wrong on every provider still fail.
	bad := decide.Media{MIMEType: "image/jpeg", Data: photo.Data}
	_, err := decide.Eval(t.Context(), srv.Client(t), "a receipt", decide.Noul("Readable?"), decide.WithMedia(bad))
	if !errors.Is(err, decide.ErrInvalidRequest) {
		t.Fatalf("err = %v", err)
	}
}
