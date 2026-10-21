// ABOUTME: Tests for the decide models command: key checks, model lists, and errors.
// ABOUTME: They use decidetest and fake HTTP servers, never a live provider.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

// runReal runs the app with its real credential lookup, as a user would.
func runReal(args ...string) output {
	var stdout, stderr bytes.Buffer
	app := &App{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr}
	code := app.Run(context.Background(), args)
	return output{stdout.String(), stderr.String(), code}
}

// clearProviderEnv unsets every variable the providers read.
func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"TYPESAFE_API_KEY", "TYPESAFE_BASE_URL",
		"CLOUDFLARE_AUTH_TOKEN", "CLOUDFLARE_ACCOUNT_ID", "CLOUDFLARE_BASE_URL",
		"OPENAI_API_KEY", "OPENAI_BASE_URL", "DECIDE_PROVIDER", "DECIDE_MODEL",
	} {
		t.Setenv(name, "")
	}
}

const typeSafeChecked = "Checked by listing models.\nNot checked: running decisions, credit, or access to a model."

func TestModelsTypeSafe(t *testing.T) {
	h := setup(t)
	out := h.run("", "models")
	if out.code != 0 {
		t.Fatalf("exit %d, stderr %q", out.code, out.stderr)
	}
	contains(t, out.stdout, "typesafe  key accepted", "jev-latest", "(default)", "jev-preview",
		"decide run sentiment notes.txt --model jev-latest", typeSafeChecked)
	var gets, posts int
	for _, r := range h.server.Requests() {
		switch {
		case r.Method == "GET" && r.Path == "/v1/models":
			gets++
		case r.Method == "POST":
			posts++
		}
	}
	if gets != 1 || posts != 0 {
		t.Fatalf("want one GET /v1/models and no POST, got %d GETs and %d POSTs", gets, posts)
	}
}

func TestModelsEmptyList(t *testing.T) {
	h := setup(t)
	h.server = decidetest.NewServer(t, decidetest.WithModels())
	out := h.run("", "models")
	contains(t, out.stdout, "typesafe  key accepted", "no models listed", typeSafeChecked)
}

func TestModelsJSON(t *testing.T) {
	h := setup(t)
	h.server = decidetest.NewServer(t, decidetest.WithModels(
		decide.Model{Name: "jev-latest"},
		decide.Model{Name: "jev-\x1b[31mred", Description: "plain"},
	))
	out := h.run("", "models", "--json")
	if out.code != 0 {
		t.Fatalf("exit %d, stderr %q", out.code, out.stderr)
	}
	var got struct {
		Provider string
		Listed   bool
		Checked  string
		Default  string
		Models   []decide.Model
	}
	if err := json.Unmarshal([]byte(out.stdout), &got); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%q", err, out.stdout)
	}
	if got.Provider != "typesafe" || !got.Listed || got.Checked != "list models" || got.Default != "jev-latest" || len(got.Models) != 2 {
		t.Fatalf("unexpected JSON: %+v", got)
	}
	if got.Models[1].Name != "jev-\x1b[31mred" {
		t.Fatalf("--json must keep the provider's text, got %q", got.Models[1].Name)
	}
	out = h.run("", "models")
	if strings.Contains(out.stdout, "\x1b") {
		t.Fatalf("text output has an escape character: %q", out.stdout)
	}
	contains(t, out.stdout, "jev- [31mred")
}

func TestModelsRejectedKey(t *testing.T) {
	for _, status := range []int{401, 403} {
		for _, args := range [][]string{{"models"}, {"models", "--json"}} {
			h := setup(t)
			h.server.FailNext(status)
			out := h.run("", args...)
			if out.code != 1 {
				t.Fatalf("%d %v: exit %d", status, args, out.code)
			}
			if out.stdout != "" {
				t.Fatalf("%d %v: stdout should be empty, got %q", status, args, out.stdout)
			}
			contains(t, out.stderr, "rejected TYPESAFE_API_KEY",
				"Check that TYPESAFE_API_KEY holds a valid key.", "decide models")
		}
	}
}

func TestModelsMissingKey(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		args []string
		want []string
	}{
		{"typesafe", nil, []string{"models"}, []string{"TYPESAFE_API_KEY is not set"}},
		{"cloudflare", nil, []string{"models", "--provider", "cloudflare"},
			[]string{"CLOUDFLARE_AUTH_TOKEN", "CLOUDFLARE_ACCOUNT_ID"}},
		{"openai", nil, []string{"models", "--provider", "openai"}, []string{"OPENAI_API_KEY is not set"}},
		{"provider from the environment", map[string]string{"DECIDE_PROVIDER": "openai"},
			[]string{"models"}, []string{"OPENAI_API_KEY is not set"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearProviderEnv(t)
			t.Setenv("DECIDE_HOME", t.TempDir())
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			out := runReal(tc.args...)
			if out.code != 1 || out.stdout != "" {
				t.Fatalf("exit %d, stdout %q", out.code, out.stdout)
			}
			contains(t, out.stderr, tc.want...)
		})
	}
}

// providerServer answers every request with status and records the last.
type providerServer struct {
	*httptest.Server
	mu     sync.Mutex
	method string
	path   string
	query  string
	auth   string
	body   string // empty: a body both providers accept
	hits   int
}

func newProviderServer(t *testing.T, status int) *providerServer {
	t.Helper()
	p := &providerServer{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.method, p.path, p.query, p.auth = r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		p.hits++
		body := p.body
		p.mu.Unlock()
		if body == "" {
			body = `{"success":true,"object":"list","data":[{"id":"gpt-4o"}],"secret":"body-must-not-print"}`
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(p.Close)
	return p
}

func TestModelsCloudflare(t *testing.T) {
	for _, status := range []int{200, 401, 403, 404, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			clearProviderEnv(t)
			t.Setenv("DECIDE_HOME", t.TempDir())
			srv := newProviderServer(t, status)
			t.Setenv("CLOUDFLARE_BASE_URL", srv.URL)
			t.Setenv("CLOUDFLARE_AUTH_TOKEN", "cf-secret-token")
			t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct123")
			out := runReal("models", "--provider", "cloudflare")
			all := out.stdout + out.stderr
			if strings.Contains(all, "cf-secret-token") || strings.Contains(all, "body-must-not-print") {
				t.Fatalf("output leaks a secret or a response body: %q", all)
			}
			if srv.method != "GET" || srv.path != "/accounts/acct123/ai/models/search" ||
				srv.query != "per_page=1" || srv.auth != "Bearer cf-secret-token" {
				t.Fatalf("request was %s %s?%s auth %q", srv.method, srv.path, srv.query, srv.auth)
			}
			switch status {
			case 200:
				if out.code != 0 {
					t.Fatalf("exit %d, stderr %q", out.code, out.stderr)
				}
				contains(t, out.stdout, "cloudflare  token accepted", "(", "clef", "(default)", "clef-flash",
					"Cloudflare does not list models. These are the ones decide knows.",
					"Checked by searching Workers AI models.\nNot checked: running decisions or credit.")
			case 401:
				if out.code != 1 || out.stdout != "" {
					t.Fatalf("exit %d, stdout %q", out.code, out.stdout)
				}
				contains(t, out.stderr, "The cloudflare provider rejected CLOUDFLARE_AUTH_TOKEN or CLOUDFLARE_ACCOUNT_ID (status 401)",
					"The token needs Workers AI access.", "decide models")
			case 403:
				if out.code != 1 || out.stdout != "" {
					t.Fatalf("exit %d, stdout %q", out.code, out.stdout)
				}
				contains(t, out.stderr, "Could not confirm CLOUDFLARE_AUTH_TOKEN or CLOUDFLARE_ACCOUNT_ID (status 403)",
					"decide checked GET /accounts/ACCOUNT_ID/ai/models/search", "Workers AI")
				if strings.Contains(out.stderr, "rejected") {
					t.Fatalf("a 403 must not say rejected: %q", out.stderr)
				}
			default:
				if out.code != 1 || out.stdout != "" {
					t.Fatalf("exit %d, stdout %q", out.code, out.stdout)
				}
				contains(t, out.stderr, "Could not check the cloudflare credentials: status "+strconv.Itoa(status),
					"CLOUDFLARE_BASE_URL")
				if strings.Contains(out.stderr, "rejected") {
					t.Fatalf("a %d must not blame the credentials: %q", status, out.stderr)
				}
			}
		})
	}
}

func TestModelsCloudflareJSON(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("DECIDE_HOME", t.TempDir())
	srv := newProviderServer(t, 200)
	t.Setenv("CLOUDFLARE_BASE_URL", srv.URL)
	t.Setenv("CLOUDFLARE_AUTH_TOKEN", "t")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "a")
	out := runReal("models", "--provider", "cloudflare", "--json")
	var got struct {
		Provider string
		Listed   bool
		Checked  string
		Default  string
		Models   []struct{ Name string }
	}
	if err := json.Unmarshal([]byte(out.stdout), &got); err != nil {
		t.Fatalf("%v: %q", err, out.stdout)
	}
	if got.Provider != "cloudflare" || got.Listed || got.Checked != "search models" || got.Default != "clef" || len(got.Models) != 2 || got.Models[0].Name != "clef" {
		t.Fatalf("unexpected JSON: %+v", got)
	}
}

func TestModelsOpenAIJSON(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("DECIDE_HOME", t.TempDir())
	srv := newProviderServer(t, 200)
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1")
	t.Setenv("OPENAI_API_KEY", "sk-secret")
	out := runReal("models", "--provider", "openai", "--json")
	var got struct {
		Provider string
		Checked  string
	}
	if err := json.Unmarshal([]byte(out.stdout), &got); err != nil {
		t.Fatalf("%v: %q", err, out.stdout)
	}
	if got.Provider != "openai" || got.Checked != "list models" {
		t.Fatalf("unexpected JSON: %+v", got)
	}
}

func TestModelsOpenAI(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		clearProviderEnv(t)
		t.Setenv("DECIDE_HOME", t.TempDir())
		srv := newProviderServer(t, 200)
		t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1")
		t.Setenv("OPENAI_API_KEY", "sk-secret")
		out := runReal("models", "--provider", "openai")
		if out.code != 0 {
			t.Fatalf("exit %d, stderr %q", out.code, out.stderr)
		}
		contains(t, out.stdout, "openai  key accepted", "gpt-6-luna",
			"OpenAI does not list Decisions models. These are the ones decide knows.",
			"Checked by listing models.\nNot checked: running decisions or credit.")
		if strings.Contains(out.stdout, "gpt-4o") || strings.Contains(out.stdout+out.stderr, "sk-secret") {
			t.Fatalf("output shows the provider's list or the key: %q", out.stdout)
		}
		if srv.method != "GET" || srv.path != "/v1/models" || srv.auth != "Bearer sk-secret" {
			t.Fatalf("request was %s %s auth %q", srv.method, srv.path, srv.auth)
		}
	})
	t.Run("rejected", func(t *testing.T) {
		clearProviderEnv(t)
		t.Setenv("DECIDE_HOME", t.TempDir())
		srv := newProviderServer(t, 401)
		t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1")
		t.Setenv("OPENAI_API_KEY", "sk-secret")
		out := runReal("models", "--provider", "openai")
		if out.code != 1 || out.stdout != "" {
			t.Fatalf("exit %d, stdout %q", out.code, out.stdout)
		}
		contains(t, out.stderr, "The openai provider rejected OPENAI_API_KEY (status 401)",
			"Check that OPENAI_API_KEY holds a valid key.", "decide models")
	})
}

func TestModelsShowsBaseURL(t *testing.T) {
	h := setup(t)
	t.Setenv("TYPESAFE_BASE_URL", "https://gateway.example.com:8443/v1?x=1")
	out := h.run("", "models")
	contains(t, out.stdout, "typesafe  key accepted  (https://gateway.example.com:8443)")
	t.Setenv("TYPESAFE_BASE_URL", "https://gateway.example.com/\x1b[31m")
	out = h.run("", "models")
	contains(t, out.stdout, "typesafe  key accepted")
	if strings.Contains(out.stdout, "gateway") || strings.Contains(out.stdout, "\x1b") {
		t.Fatalf("an invalid address must not be shown: %q", out.stdout)
	}
}

// providerEnv points a provider at base with a secret credential.
func providerEnv(t *testing.T, provider, base string) []string {
	t.Helper()
	clearProviderEnv(t)
	t.Setenv("DECIDE_HOME", t.TempDir())
	if provider == "cloudflare" {
		t.Setenv("CLOUDFLARE_BASE_URL", base)
		t.Setenv("CLOUDFLARE_AUTH_TOKEN", "cf-secret-token")
		t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct123")
		return []string{"models", "--provider", "cloudflare"}
	}
	t.Setenv("OPENAI_BASE_URL", base)
	t.Setenv("OPENAI_API_KEY", "sk-secret")
	return []string{"models", "--provider", "openai"}
}

func TestModelsNeverPrintsBaseURLPassword(t *testing.T) {
	for _, provider := range []string{"typesafe", "cloudflare", "openai"} {
		t.Run(provider, func(t *testing.T) {
			srv := newProviderServer(t, 200)
			base := strings.Replace(srv.URL, "http://", "http://user:hunter2@", 1)
			var args []string
			if provider == "typesafe" {
				h := setup(t)
				t.Setenv("TYPESAFE_BASE_URL", base)
				out := h.run("", "models")
				if strings.Contains(out.stdout+out.stderr, "hunter2") {
					t.Fatalf("output shows the password: %q %q", out.stdout, out.stderr)
				}
				return
			}
			args = providerEnv(t, provider, base)
			out := runReal(args...)
			if strings.Contains(out.stdout+out.stderr, "hunter2") {
				t.Fatalf("output shows the password: %q %q", out.stdout, out.stderr)
			}
			if out.code != 1 {
				t.Fatalf("a base URL with credentials must fail like a run: exit %d", out.code)
			}
		})
	}
}

func TestModelsDoesNotFollowRedirects(t *testing.T) {
	for _, provider := range []string{"cloudflare", "openai"} {
		t.Run(provider, func(t *testing.T) {
			target := newProviderServer(t, 200)
			redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL+"/leak?token=cf-secret-token&key=sk-secret", http.StatusFound)
			}))
			t.Cleanup(redirect.Close)
			out := runReal(providerEnv(t, provider, redirect.URL)...)
			all := out.stdout + out.stderr
			if out.code != 1 || out.stdout != "" {
				t.Fatalf("exit %d, stdout %q", out.code, out.stdout)
			}
			if target.hits != 0 {
				t.Fatalf("the redirect was followed")
			}
			if strings.Contains(all, "cf-secret-token") || strings.Contains(all, "sk-secret") {
				t.Fatalf("output leaks a secret: %q", all)
			}
			contains(t, out.stderr, "Could not check the "+provider+" credentials: status 302")
			if strings.Contains(out.stderr, "rejected") {
				t.Fatalf("a redirect must not blame the credentials: %q", out.stderr)
			}
		})
	}
}

func TestModelsRedirectToDeadURLHidesToken(t *testing.T) {
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/?token=sk-secret", http.StatusFound)
	}))
	t.Cleanup(redirect.Close)
	out := runReal(providerEnv(t, "openai", redirect.URL)...)
	if out.code != 1 || strings.Contains(out.stdout+out.stderr, "sk-secret") {
		t.Fatalf("exit %d, output leaks the key: %q", out.code, out.stdout+out.stderr)
	}
}

func TestModelsDialErrorHidesSecrets(t *testing.T) {
	// Nothing listens on port 1, and the base URL path carries the secrets.
	for _, tc := range []struct{ provider, base string }{
		{"openai", "http://127.0.0.1:1/sk-secret"},
		{"cloudflare", "http://127.0.0.1:1/cf-secret-token"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			out := runReal(providerEnv(t, tc.provider, tc.base)...)
			all := out.stdout + out.stderr
			if out.code != 1 {
				t.Fatalf("exit %d", out.code)
			}
			for _, secret := range []string{"sk-secret", "cf-secret-token", "acct123"} {
				if strings.Contains(all, secret) {
					t.Fatalf("output leaks %q: %q", secret, all)
				}
			}
			contains(t, out.stderr, "Could not check the "+tc.provider+" credentials")
		})
	}
}

func TestModelsRejectsUnexpectedAnswer(t *testing.T) {
	cases := []struct {
		name, provider, body string
	}{
		{"openai html login page", "openai", `<html><body>Please log in</body></html>`},
		{"openai json without a list", "openai", `{"hello":"world"}`},
		{"cloudflare html login page", "cloudflare", `<html>Sign in</html>`},
		{"cloudflare success false", "cloudflare", `{"success":false,"errors":[{"code":1000}],"result":[]}`},
		{"cloudflare no success field", "cloudflare", `{"object":"list","data":[]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newProviderServer(t, 200)
			srv.body = tc.body
			out := runReal(providerEnv(t, tc.provider, srv.URL)...)
			if out.code != 1 || out.stdout != "" {
				t.Fatalf("exit %d, stdout %q", out.code, out.stdout)
			}
			contains(t, out.stderr, "Could not check the "+tc.provider+" credentials: ")
			if strings.Contains(out.stderr, "rejected") || strings.Contains(out.stderr, "Please log in") {
				t.Fatalf("unexpected text: %q", out.stderr)
			}
		})
	}
}

func TestModelsTruncatedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "500")
		w.WriteHeader(200)
		w.Write([]byte(`{"object":"list","data":[`))
	}))
	t.Cleanup(srv.Close)
	out := runReal(providerEnv(t, "openai", srv.URL)...)
	if out.code != 1 || out.stdout != "" {
		t.Fatalf("exit %d, stdout %q", out.code, out.stdout)
	}
	contains(t, out.stderr, "Could not check the openai credentials: ")
}

func TestModelsUsesTheSameValidationAsRun(t *testing.T) {
	cases := []struct {
		name, provider, base, account string
	}{
		{"openai fragment", "openai", "https://example.com/v1#frag", ""},
		{"cloudflare fragment", "cloudflare", "https://example.com/client/v4#frag", "acct123"},
		{"cloudflare account id", "cloudflare", "", "acct/../x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := providerEnv(t, tc.provider, tc.base)
			if tc.account != "" {
				t.Setenv("CLOUDFLARE_ACCOUNT_ID", tc.account)
			}
			_, want := connect(tc.provider, "")
			if want == nil {
				t.Fatal("a real run accepts this configuration")
			}
			out := runReal(args...)
			if out.code != 1 || out.stdout != "" {
				t.Fatalf("exit %d, stdout %q", out.code, out.stdout)
			}
			contains(t, out.stderr, want.Error())
			if strings.Contains(out.stderr, "Could not check") {
				t.Fatalf("a configuration error is not a failed check: %q", out.stderr)
			}
		})
	}
}

func TestModelsOpenAIForbiddenIsNotRejected(t *testing.T) {
	srv := newProviderServer(t, 403)
	out := runReal(providerEnv(t, "openai", srv.URL+"/v1")...)
	if out.code != 1 || out.stdout != "" {
		t.Fatalf("exit %d, stdout %q", out.code, out.stdout)
	}
	contains(t, out.stderr, "Could not confirm OPENAI_API_KEY (status 403)",
		"decide checked GET /v1/models", "still work for decisions")
	if strings.Contains(out.stderr, "rejected") || strings.Contains(out.stderr, "valid key") {
		t.Fatalf("a 403 must not call the key rejected: %q", out.stderr)
	}
}

func TestModelsAddressHidesSecretsOnSuccess(t *testing.T) {
	for _, provider := range []string{"typesafe", "cloudflare", "openai"} {
		for _, form := range []struct{ name, suffix string }{
			{"path", "/v1/%s"},
			{"query", "/v1?key=%s"},
			{"path and query", "/%s/v1?token=%s"},
		} {
			for _, asJSON := range []bool{false, true} {
				t.Run(provider+" "+form.name+" json="+strconv.FormatBool(asJSON), func(t *testing.T) {
					srv := newProviderServer(t, 200)
					key := map[string]string{"typesafe": "ts-secret", "cloudflare": "cf-secret-token", "openai": "sk-secret"}[provider]
					suffix := strings.ReplaceAll(form.suffix, "%s", key)
					var out output
					if provider == "typesafe" {
						h := setup(t)
						t.Setenv("TYPESAFE_API_KEY", key)
						t.Setenv("TYPESAFE_BASE_URL", srv.URL+suffix)
						args := []string{"models"}
						if asJSON {
							args = append(args, "--json")
						}
						out = h.run("", args...)
					} else {
						args := providerEnv(t, provider, srv.URL+suffix)
						if asJSON {
							args = append(args, "--json")
						}
						out = runReal(args...)
					}
					// Cloudflare and OpenAI refuse a base URL with a query, as a run does.
					if out.code != 0 && (provider == "typesafe" || form.name == "path") {
						t.Fatalf("exit %d, stderr %q", out.code, out.stderr)
					}
					all := out.stdout + out.stderr
					for _, secret := range []string{key, "acct123"} {
						if strings.Contains(all, secret) {
							t.Fatalf("output leaks %q: %q", secret, all)
						}
					}
				})
			}
		}
	}
}

func TestModelsClientErrorHidesSecrets(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("DECIDE_HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "ts-secret")
	var stdout, stderr bytes.Buffer
	app := &App{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		NewClient: func(provider, model string) (*decide.Client, error) {
			return nil, errors.New("bad base URL https://u:pw@host/ts-secret?k=ts-secret")
		}}
	code := app.Run(context.Background(), []string{"models"})
	if code != 1 || stdout.Len() != 0 {
		t.Fatalf("exit %d, stdout %q", code, stdout.String())
	}
	for _, leak := range []string{"ts-secret", "u:pw"} {
		if strings.Contains(stderr.String(), leak) {
			t.Fatalf("stderr leaks %q: %q", leak, stderr.String())
		}
	}
	contains(t, stderr.String(), "bad base URL")
}

func TestModelsOpenAIAnswerShape(t *testing.T) {
	cases := []struct {
		name, body string
		ok         bool
	}{
		{"empty array", `{"object":"list","data":[]}`, true},
		{"data missing", `{"object":"list"}`, false},
		{"data null", `{"object":"list","data":null}`, false},
		{"data string", `{"object":"list","data":"nope"}`, false},
		{"data object", `{"object":"list","data":{"id":"x"}}`, false},
		{"error with 200", `{"error":{"message":"boom","type":"x"}}`, false},
		{"error beside a list", `{"object":"list","data":[],"error":{"message":"boom"}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newProviderServer(t, 200)
			srv.body = tc.body
			out := runReal(providerEnv(t, "openai", srv.URL)...)
			if tc.ok {
				if out.code != 0 {
					t.Fatalf("exit %d, stderr %q", out.code, out.stderr)
				}
				return
			}
			if out.code != 1 || out.stdout != "" {
				t.Fatalf("exit %d, stdout %q", out.code, out.stdout)
			}
			contains(t, out.stderr, "Could not check the openai credentials: ")
			if strings.Contains(out.stderr, "rejected") {
				t.Fatalf("must not blame the key: %q", out.stderr)
			}
		})
	}
}

func TestModelsConfigErrorHidesSecrets(t *testing.T) {
	// A base URL that a run rejects may be quoted in the error.
	args := providerEnv(t, "openai", "https://example.com/sk-secret?key=sk-secret#frag")
	out := runReal(args...)
	if out.code != 1 || strings.Contains(out.stdout+out.stderr, "sk-secret") {
		t.Fatalf("exit %d, output leaks the key: %q %q", out.code, out.stdout, out.stderr)
	}
}

func TestModelsSuccessLinesAreShort(t *testing.T) {
	check := func(t *testing.T, out output) {
		t.Helper()
		if out.code != 0 {
			t.Fatalf("exit %d, stderr %q", out.code, out.stderr)
		}
		for _, line := range strings.Split(out.stdout, "\n") {
			if len(line) > 72 {
				t.Errorf("line is %d characters, want at most 72: %q", len(line), line)
			}
		}
	}
	t.Run("typesafe", func(t *testing.T) {
		check(t, setup(t).run("", "models"))
	})
	t.Run("cloudflare", func(t *testing.T) {
		clearProviderEnv(t)
		t.Setenv("DECIDE_HOME", t.TempDir())
		srv := newProviderServer(t, 200)
		t.Setenv("CLOUDFLARE_BASE_URL", srv.URL)
		t.Setenv("CLOUDFLARE_AUTH_TOKEN", "cf-secret-token")
		t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct123")
		check(t, runReal("models", "--provider", "cloudflare"))
	})
	t.Run("openai", func(t *testing.T) {
		clearProviderEnv(t)
		t.Setenv("DECIDE_HOME", t.TempDir())
		srv := newProviderServer(t, 200)
		t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1")
		t.Setenv("OPENAI_API_KEY", "sk-secret")
		check(t, runReal("models", "--provider", "openai"))
	})
}

func TestModelsRetryHintKeepsProvider(t *testing.T) {
	for _, provider := range []string{"cloudflare", "openai"} {
		for _, status := range []int{401, 403, 500} {
			t.Run(provider+"/"+strconv.Itoa(status), func(t *testing.T) {
				clearProviderEnv(t)
				t.Setenv("DECIDE_HOME", t.TempDir())
				srv := newProviderServer(t, status)
				if provider == "cloudflare" {
					t.Setenv("CLOUDFLARE_BASE_URL", srv.URL)
					t.Setenv("CLOUDFLARE_AUTH_TOKEN", "cf-secret-token")
					t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct123")
				} else {
					t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1")
					t.Setenv("OPENAI_API_KEY", "sk-secret")
				}
				t.Setenv("DECIDE_PROVIDER", provider)
				out := runReal("models")
				if out.code != 1 {
					t.Fatalf("exit %d", out.code)
				}
				if provider == "openai" && status == 403 {
					return // OpenAI's 403 hint points to a small run, not a retry
				}
				contains(t, out.stderr, "decide models --provider "+provider)
			})
		}
	}
	t.Run("typesafe has no flag", func(t *testing.T) {
		h := setup(t)
		h.server.FailNext(401)
		out := h.run("", "models")
		contains(t, out.stderr, "decide models")
		lacks(t, out.stderr, "--provider")
	})
}

func TestModelsHelpSaysCredentialsAreChecked(t *testing.T) {
	out := runReal("models", "--help")
	all := out.stdout + out.stderr
	lacks(t, all, "credentials work")
	contains(t, all, "accepts your credentials")
}
