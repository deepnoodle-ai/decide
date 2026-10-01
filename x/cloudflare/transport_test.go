package cloudflare_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/deepnoodle-ai/sod"
	"github.com/deepnoodle-ai/sod/sodtest"
	"github.com/deepnoodle-ai/sod/x/cloudflare"
)

const token = "test-cloudflare-token-00000000"

func newClient(t *testing.T, handler http.HandlerFunc, opts ...sod.ClientOption) (*sod.Client, *cloudflare.Transport) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	transport, err := cloudflare.NewTransport(cloudflare.Config{AccountID: "account-1", APIToken: token, BaseURL: srv.URL + "/prefix/"})
	if err != nil {
		t.Fatal(err)
	}
	all := []sod.ClientOption{sod.WithoutEnvironment(), sod.WithTransport(transport), sod.WithModel("clef"), sod.WithRetryBackoff(0, 0)}
	client, err := sod.NewClient(append(all, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return client, transport
}

func noulRequest() *sod.Request {
	req := sod.NewRequest(map[string]any{"ticket": "Checkout is down."})
	sod.Ask(req, "urgent", sod.Noul("Is this urgent?"))
	return req
}

func writeEnvelope(w http.ResponseWriter, req *sod.Request) {
	w.Header().Set("CF-Ray", "ray-123")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true, "errors": []any{}, "messages": []any{"provider message"},
		"trace": "extra-metadata", "result": map[string]any{
			"model": req.Model + "-resolved", "answers": map[string]sod.Answer{"urgent": sodtest.NoulAnswer(0.93)},
			"usage": sod.Usage{InputTokens: 42, OutputTokens: 0}, "provider_field": 17,
		},
	})
}

func TestRoutesAndAnswers(t *testing.T) {
	for _, model := range []string{"clef", "clef-flash"} {
		t.Run(model, func(t *testing.T) {
			var body []byte
			client, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/prefix/accounts/account-1/ai/run/@cf/cloudflare/"+model {
					t.Errorf("unexpected route: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Content-Type") != "application/json" {
					t.Error("missing authorization or content type")
				}
				body, _ = io.ReadAll(r.Body)
				var req sod.Request
				if err := json.Unmarshal(body, &req); err != nil {
					t.Error(err)
				}
				if req.Model != model {
					t.Errorf("model = %q", req.Model)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": true, "errors": []any{}, "messages": []any{}, "provider_metadata": 8,
					"result": map[string]any{"model": model + "-resolved", "answers": map[string]sod.Answer{
						"urgent":   sodtest.NoulAnswer(0.9234567890123456),
						"team":     &sod.ChoiceAnswer{Choice: "technical", Probabilities: map[string]float64{"billing": 0.1234567890123456, "technical": 0.8765432109876544}, Confidence: 0.63},
						"severity": &sod.ScoreAnswer{Score: 1.45, Legend: map[string]any{"0": "minor", "1": "major", "2": "critical"}, Probabilities: map[string]float64{"0": 0.1, "1": 0.35, "2": 0.55}, Confidence: 0.23},
					}, "usage": map[string]any{"input_tokens": 42, "output_tokens": 0, "cached_tokens": 9}, "future": true},
				})
			})
			req := sod.NewRequest("Checkout is down.", sod.WithRequestModel(model), sod.WithRequestExtra("custom", 12))
			noul := sod.Ask(req, "urgent", sod.Noul("Urgent?"))
			choice := sod.Ask(req, "team", sod.Choice("Which team?", sod.Option("technical"), sod.Option("billing")))
			score := sod.Ask(req, "severity", sod.Score("Severity?", "minor", "major", "critical"))
			before, _ := json.Marshal(req)
			resp, err := client.SystemOne(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			na, _ := noul.From(resp)
			ca, _ := choice.From(resp)
			sa, _ := score.From(resp)
			if na.Noul != 0.9234567890123456 || ca.Probabilities["billing"] != 0.1234567890123456 || ca.Confidence != 0.63 || sa.Score != 1.45 || sa.Confidence != 0.23 {
				t.Fatalf("provider values changed: %+v %+v %+v", na, ca, sa)
			}
			if resp.Model != model+"-resolved" || resp.Usage.InputTokens != 42 || string(resp.Usage.Extra["cached_tokens"]) != "9" || string(resp.Extra["future"]) != "true" {
				t.Fatalf("missing response metadata: %+v", resp)
			}
			var raw map[string]json.RawMessage
			if json.Unmarshal(resp.Raw, &raw) != nil || string(raw["success"]) != "true" || !bytes.Contains(resp.Extra["cloudflare"], []byte("provider_metadata")) {
				t.Error("raw envelope or provider metadata was lost")
			}
			after, _ := json.Marshal(req)
			if !bytes.Equal(before, after) || !bytes.Equal(before, body) {
				t.Error("request was mutated or reordered")
			}
		})
	}
}

func TestMalformedEnvelopes(t *testing.T) {
	cases := []string{
		`null`, `[]`, `{}`, `{"success":null}`, `{"success":"true"}`, `{"success":true}`,
		`{"success":true,"result":null}`, `{"success":true,"result":[]}`,
		`{"success":true,"result":{}}`, `{"success":true,"result":{"answers":null}}`,
		`{"success":true,"result":{"answers":[]}}`, `not json`,
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			var attempts atomic.Int32
			client, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				_, _ = io.WriteString(w, body)
			})
			_, err := client.SystemOne(context.Background(), noulRequest())
			if !errors.Is(err, sod.ErrDecode) || attempts.Load() != 1 {
				t.Fatalf("err = %v; attempts = %d", err, attempts.Load())
			}
		})
	}
}

func TestInvalidAnswersRemainPartial(t *testing.T) {
	client, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":true,"result":{"model":"clef","answers":{"urgent":{"type":"noul"},"good":{"type":"noul","noul":0.8}},"usage":{"input_tokens":1,"output_tokens":0}}}`)
	})
	req := noulRequest()
	good := sod.Ask(req, "good", sod.Noul("Good?"))
	resp, err := client.SystemOne(context.Background(), req)
	if !errors.Is(err, sod.ErrInvalidAnswer) || resp == nil || resp.Invalid["urgent"] == nil {
		t.Fatalf("resp = %+v; err = %v", resp, err)
	}
	if answer, err := good.From(resp); err != nil || answer.Noul != 0.8 {
		t.Fatalf("valid answer lost: %+v %v", answer, err)
	}
}

func TestErrorsAndRetries(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   int
		want   error
		calls  int32
	}{
		{400, 5007, nil, 1}, {401, 1000, sod.ErrAuth, 1}, {403, 5018, sod.ErrAuth, 1},
		{408, 3007, nil, 3}, {422, 5004, sod.ErrValidation, 1},
		{429, 3040, sod.ErrRateLimited, 3}, {503, 9000, sod.ErrServer, 3},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			var attempts atomic.Int32
			client, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.Header().Set("Retry-After", "0.001")
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprintf(w, `{"success":false,"errors":[{"code":%d,"message":"first"},{"code":12,"message":"second"}]}`, tc.code)
			})
			_, err := client.SystemOne(context.Background(), noulRequest())
			var ae *sod.APIError
			var ce *cloudflare.Error
			if !errors.As(err, &ae) || !errors.As(err, &ce) || ae.StatusCode != tc.status || ae.Type != fmt.Sprint(tc.code) || ae.RetryAfter != time.Millisecond || len(ce.Errors) != 2 {
				t.Fatalf("error metadata: %v %#v", err, ae)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("sentinel lost: %v", err)
			}
			if attempts.Load() != tc.calls {
				t.Fatalf("attempts = %d, want %d", attempts.Load(), tc.calls)
			}
		})
	}
}

func TestUnsuccessful2xxIsNotRetried(t *testing.T) {
	for _, body := range []string{`{"success":false,"errors":[{"code":12,"message":"failure"}]}`, `{"success":true,"errors":[{"code":12,"message":"failure"}],"result":{"answers":{}}}`} {
		var attempts atomic.Int32
		client, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			_, _ = io.WriteString(w, body)
		})
		_, err := client.SystemOne(context.Background(), noulRequest())
		var ce *cloudflare.Error
		var ae *sod.APIError
		if !errors.As(err, &ce) || ce.StatusCode != 200 || errors.As(err, &ae) || attempts.Load() != 1 {
			t.Fatalf("2xx failure misrepresented: %v", err)
		}
	}
}

func TestRetryThenSuccessAndCancellation(t *testing.T) {
	var attempts atomic.Int32
	client, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(429)
			_, _ = io.WriteString(w, `{"success":false,"errors":[{"code":3040,"message":"busy"}]}`)
			return
		}
		var req sod.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		writeEnvelope(w, &req)
	})
	if _, err := client.SystemOne(context.Background(), noulRequest()); err != nil || attempts.Load() != 2 {
		t.Fatalf("retry failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.SystemOne(ctx, noulRequest()); !errors.Is(err, context.Canceled) || attempts.Load() != 2 {
		t.Fatalf("cancellation ignored: %v", err)
	}
	started := make(chan struct{})
	client, _ = newClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}, sod.WithMaxRetries(0))
	ctx, cancel = context.WithCancel(context.Background())
	go func() { <-started; cancel() }()
	if _, err := client.SystemOne(ctx, noulRequest()); !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight cancellation ignored: %v", err)
	}
}

func TestNoNetworkForUnsupportedAndInvalidRequests(t *testing.T) {
	var attempts atomic.Int32
	client, transport := newClient(t, func(w http.ResponseWriter, _ *http.Request) { attempts.Add(1) })
	if _, err := client.Models.List(context.Background()); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("model listing: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*sod.Request)
		want   error
	}{
		{"model", func(r *sod.Request) { r.Model = "../clef" }, sod.ErrInvalidRequest},
		{"scalar state", func(r *sod.Request) { r.State = 17 }, sod.ErrInvalidRequest},
		{"question ID", func(r *sod.Request) { r.Questions["bad/key"] = sod.Noul("Q?") }, sod.ErrInvalidRequest},
		{"long ID", func(r *sod.Request) { r.Questions[strings.Repeat("a", 101)] = sod.Noul("Q?") }, sod.ErrInvalidRequest},
		{"many questions", func(r *sod.Request) {
			for i := range 65 {
				r.Questions[fmt.Sprint(i)] = sod.Noul("Q?")
			}
		}, sod.ErrInvalidRequest},
		{"blank instructions", func(r *sod.Request) { r.Questions["urgent"] = sod.Noul(" ") }, sod.ErrInvalidRequest},
		{"one choice", func(r *sod.Request) { r.Questions["urgent"] = sod.Choice("Q?", sod.Option("only")) }, sod.ErrInvalidRequest},
		{"many levels", func(r *sod.Request) { r.Questions["urgent"] = sod.Score("Q?", make([]any, 11)...) }, sod.ErrInvalidRequest},
		{"unknown type", func(r *sod.Request) {
			r.Questions["urgent"] = &sod.RawQuestion{Type: "future", JSON: json.RawMessage(`{"type":"future","instructions":"Q?"}`)}
		}, errors.ErrUnsupported},
		{"video", func(r *sod.Request) { r.Extra = map[string]any{"videos": []any{}} }, errors.ErrUnsupported},
		{"URL image", func(r *sod.Request) { r.Extra = map[string]any{"images": []string{"https://example.com/pic.png"}} }, sod.ErrInvalidRequest},
		{"large request", func(r *sod.Request) { r.State = strings.Repeat("a", 13<<20) }, sod.ErrInvalidRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := noulRequest()
			tc.mutate(r)
			if _, err := client.SystemOne(context.Background(), r); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := transport.SystemOne(context.Background(), nil); !errors.Is(err, sod.ErrInvalidRequest) {
		t.Fatalf("direct nil request: %v", err)
	}
	if attempts.Load() != 0 {
		t.Fatalf("invalid requests made %d calls", attempts.Load())
	}
}

func TestConcurrency(t *testing.T) {
	client, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req sod.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		writeEnvelope(w, &req)
	})
	req := noulRequest()
	before, _ := json.Marshal(req)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			resp, err := client.SystemOne(context.Background(), req)
			if err != nil || resp.Header.Get("CF-Ray") != "ray-123" || resp.RequestID != "" {
				t.Errorf("response = %+v; err = %v", resp, err)
			}
		})
	}
	wg.Wait()
	after, _ := json.Marshal(req)
	if !reflect.DeepEqual(before, after) {
		t.Error("shared request changed")
	}
}

func TestRedaction(t *testing.T) {
	for _, status := range []int{200, 400} {
		var logs bytes.Buffer
		client, transport := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Echo-Token", token)
			w.WriteHeader(status)
			if status == 200 {
				_, _ = fmt.Fprintf(w, `{"success":true,"echo":%q,"result":{"model":"clef","answers":{"urgent":{"type":"noul","noul":0.8}},"usage":{"input_tokens":1,"output_tokens":0}}}`, token)
			} else {
				_, _ = fmt.Fprintf(w, `{"success":false,"errors":[{"code":1,"message":%q}]}`, token)
			}
		}, sod.WithLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))), sod.WithLogBodies(true))
		resp, err := client.SystemOne(context.Background(), noulRequest())
		printed := fmt.Sprintf("%v %+v %#v %v", *transport, transport, transport, cloudflare.Config{APIToken: token})
		if resp != nil {
			printed += fmt.Sprintf("%s %v %s", resp.Raw, resp.Header, resp.Extra["cloudflare"])
		}
		if err != nil {
			var ce *cloudflare.Error
			errors.As(err, &ce)
			printed += fmt.Sprintf("%v %+v %#v", err, ce, ce)
		}
		if strings.Contains(printed+logs.String(), token) {
			t.Fatal("credential leaked")
		}
	}
}

func TestConfigValidation(t *testing.T) {
	for _, cfg := range []cloudflare.Config{
		{AccountID: "account"}, {APIToken: token}, {AccountID: "../other", APIToken: token},
		{AccountID: "account", APIToken: token, BaseURL: "relative"},
		{AccountID: "account", APIToken: token, BaseURL: "https://user:secret@example.com"},
		{AccountID: "account", APIToken: token, BaseURL: "https://example.com?token=secret"},
		{AccountID: "account", APIToken: token, BaseURL: "https://example.com#fragment"},
	} {
		if _, err := cloudflare.NewTransport(cfg); err == nil {
			t.Errorf("accepted invalid config: %v", cfg)
		}
	}
}

func TestAlternateJSONEscapesAreRedacted(t *testing.T) {
	escaped := `\u0074` + token[1:] // the provider encodes the token's first letter
	for _, status := range []int{200, 400} {
		var logs bytes.Buffer
		client, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			if status == 200 {
				_, _ = fmt.Fprintf(w, `{"success":true,"metadata":"%s","result":{"model":"clef","future":["%s"],"answers":{"urgent":{"type":"noul","noul":0.9234567890123456,"extra":"%s"}},"usage":{"input_tokens":123456789,"output_tokens":0}}}`, escaped, escaped, escaped)
			} else {
				_, _ = fmt.Fprintf(w, `{"success":false,"errors":[{"code":1,"message":"%s"}]}`, escaped)
			}
		}, sod.WithLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))), sod.WithLogBodies(true))
		resp, err := client.SystemOne(context.Background(), noulRequest())
		if status == 200 {
			if err != nil {
				t.Fatal(err)
			}
			na := resp.Answers["urgent"].(*sod.NoulAnswer)
			if na.Noul != 0.9234567890123456 || resp.Usage.InputTokens != 123456789 ||
				!bytes.Contains(resp.Raw, []byte("0.9234567890123456")) || !bytes.Contains(resp.Raw, []byte("123456789")) {
				t.Fatal("redaction altered numbers")
			}
			if strings.Contains(fmt.Sprintf("%s %v %v", resp.Raw, resp.Extra, na.Extra), token) || bytes.Contains(resp.Raw, []byte(escaped)) {
				t.Fatal("escaped token survived in successful response")
			}
		} else {
			var ae *sod.APIError
			if !errors.As(err, &ae) || strings.Contains(err.Error()+ae.Message+string(ae.Body), token) || bytes.Contains(ae.Body, []byte(escaped)) {
				t.Fatal("escaped token survived in provider error")
			}
		}
		if strings.Contains(logs.String(), token) || strings.Contains(logs.String(), escaped) {
			t.Fatal("escaped credential reached logs")
		}
	}
}

func TestRedactionMatchesResponseDecoder(t *testing.T) {
	for _, prefix := range []string{`"preceding":"\ud800",`, "\"preceding\":\"\xff\","} {
		var logs bytes.Buffer
		client, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(400)
			_, _ = fmt.Fprintf(w, `{"success":false,%s"errors":[{"code":1,"message":"\u0074%s"}]}`, prefix, token[1:])
		}, sod.WithLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))), sod.WithLogBodies(true))
		_, err := client.SystemOne(context.Background(), noulRequest())
		var ae *sod.APIError
		if !errors.As(err, &ae) || strings.Contains(err.Error()+ae.Message+string(ae.Body)+logs.String(), token) {
			t.Fatal("decoder mismatch exposed credential")
		}
		if bytes.Contains(ae.Body, []byte(`\u0074`+token[1:])) {
			t.Fatal("escaped credential survived in raw body")
		}
	}
}

func TestMalformedResponseBodiesAreSuppressed(t *testing.T) {
	escaped := `\u0074` + token[1:]
	for _, tc := range []struct {
		name string
		body string
	}{
		{"syntax error before secret", `{"broken":,"message":"` + escaped + `"}`},
		{"syntax error after redacted prefix", `{"echo":"` + token + `","broken":,"message":"` + escaped + `"}`},
		{"truncated string", `{"message":"` + escaped},
		{"non-JSON", `upstream failed: ` + escaped},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			client, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, tc.body)
			}, sod.WithLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))), sod.WithLogBodies(true))
			_, err := client.SystemOne(context.Background(), noulRequest())
			var ae *sod.APIError
			var ce *cloudflare.Error
			if !errors.As(err, &ae) || !errors.As(err, &ce) || ae.StatusCode != http.StatusBadRequest {
				t.Fatalf("HTTP error classification lost: %v", err)
			}
			for _, exposed := range []string{err.Error(), ae.Message, string(ae.Body), string(ce.Body), logs.String()} {
				if strings.Contains(exposed, token) || strings.Contains(exposed, escaped) || strings.Contains(exposed, token[1:]) {
					t.Fatal("malformed response disclosed a recoverable credential")
				}
			}
			if !strings.Contains(string(ae.Body), "invalid JSON response body") || !bytes.Equal(ae.Body, ce.Body) {
				t.Fatal("suppressed response did not retain a safe diagnostic")
			}
		})
	}
}

// External question implementations with a supported wire type work without
// registration. The adapter must not restrict requests to concrete sod types.
type externalNoul struct{ *sod.NoulQuestion }

func TestExternalQuestion(t *testing.T) {
	client, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req sod.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		writeEnvelope(w, &req)
	})
	req := sod.NewRequest([]any{"ticket", map[string]any{"outage": true}})
	handle := sod.Ask(req, "urgent", &externalNoul{sod.Noul(map[string]any{"question": "Urgent?"})})
	resp, err := client.SystemOne(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if a, err := handle.From(resp); err != nil || a.Noul != 0.93 {
		t.Fatalf("external question failed: %+v %v", a, err)
	}
}

func TestResponseBoundAndBodyTimeout(t *testing.T) {
	var attempts atomic.Int32
	client, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		chunk := bytes.Repeat([]byte(" "), 1<<20)
		for range 33 {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})
	if _, err := client.SystemOne(context.Background(), noulRequest()); !errors.Is(err, sod.ErrDecode) || attempts.Load() != 1 {
		t.Fatalf("response limit failed: %v, attempts %d", err, attempts.Load())
	}
	client, _ = newClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"success":true,`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}, sod.WithMaxRetries(0), sod.WithAttemptTimeout(20*time.Millisecond))
	if _, err := client.SystemOne(context.Background(), noulRequest()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout did not cover body reading: %v", err)
	}
}
