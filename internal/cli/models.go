// ABOUTME: The decide models command: checks provider credentials and lists model names.
// ABOUTME: Only TypeSafe lists models; Cloudflare and OpenAI get a read only credential check.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/backend"
	"github.com/deepnoodle-ai/wonton/cli"
)

const modelsHelp = `Check that your provider accepts your credentials, and list the model names
you can pass to --model.

TypeSafe lists its models. Cloudflare and OpenAI do not, so decide checks the
credentials with a read only request and shows the models it knows.
Nothing is asked of a model, so nothing is spent on answers.

Examples:
  decide models
  decide models --provider cloudflare
  decide models --json`

// Where each provider's base URL can be overridden.
var baseURLVars = map[string]string{
	"typesafe":   "TYPESAFE_BASE_URL",
	"cloudflare": "CLOUDFLARE_BASE_URL",
	"openai":     "OPENAI_BASE_URL",
}

func (a *App) addModels(app *cli.App) {
	app.Command("models").
		Description("Check your key and list models").
		Long(modelsHelp).
		Flags(
			cli.String("provider").Env("DECIDE_PROVIDER").Enum("typesafe", "cloudflare", "openai").
				Help("Model provider: typesafe, cloudflare, or openai (default: typesafe)"),
			cli.Bool("json").Help("Print the result as one JSON object"),
		).
		Run(a.models)
}

// modelsResult is what --json prints.
type modelsResult struct {
	Provider string         `json:"provider"`
	Listed   bool           `json:"listed"`
	Checked  string         `json:"checked"`
	Default  string         `json:"default"`
	Models   []decide.Model `json:"models"`
}

func (a *App) models(c *cli.Context) error {
	provider := c.String("provider")
	if provider == "" {
		provider = "typesafe"
	}
	res := modelsResult{Provider: provider, Checked: "list models", Default: defaultModels[provider], Models: []decide.Model{}}
	var err error
	if provider == "typesafe" {
		err = a.listTypeSafe(c, &res)
	} else {
		if provider == "cloudflare" {
			res.Checked = "search models"
		}
		err = checkProvider(c.Context(), provider)
		for _, name := range knownModels[provider] {
			res.Models = append(res.Models, decide.Model{Name: name})
		}
	}
	if err != nil {
		return redactedError{err}
	}
	if c.Bool("json") {
		return json.NewEncoder(c.Stdout()).Encode(res)
	}
	printModels(c.Stdout(), res)
	return nil
}

// listTypeSafe lists TypeSafe's models, which also shows the key is accepted.
func (a *App) listTypeSafe(c *cli.Context, res *modelsResult) error {
	client, err := a.NewClient(res.Provider, res.Default)
	if err != nil {
		return err
	}
	list, err := client.Models.List(c.Context())
	if err != nil {
		return checkError(res.Provider, err, os.Getenv("TYPESAFE_API_KEY"))
	}
	if !hasModelsMember(list.Raw) {
		return couldNotCheck(res.Provider, "the answer was not what "+res.Provider+" sends")
	}
	res.Listed = true
	res.Models = append(res.Models, list.Models...)
	return nil
}

// hasModelsMember reports whether body has a non null "models" member, so a
// reply that is not a model list is not taken as an accepted key.
func hasModelsMember(body []byte) bool {
	var answer struct {
		Models json.RawMessage `json:"models"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return false
	}
	return len(answer.Models) > 0 && string(answer.Models) != "null"
}

// redactedError hides every configured secret, and the user info of any URL,
// in an error's text, whatever produced it.
type redactedError struct{ error }

func (e redactedError) Error() string { return redact(e.error.Error(), configuredSecrets()...) }
func (e redactedError) Unwrap() error { return e.error }

// configuredSecrets lists every credential decide reads from the environment.
func configuredSecrets() []string {
	var secrets []string
	for _, name := range []string{"TYPESAFE_API_KEY", "CLOUDFLARE_AUTH_TOKEN", "CLOUDFLARE_ACCOUNT_ID", "OPENAI_API_KEY"} {
		if v := os.Getenv(name); v != "" {
			secrets = append(secrets, v)
		}
	}
	return secrets
}

// displayAddress reduces a base URL to scheme://host[:port]. The path, query,
// fragment, and user info are dropped because a key can hide in any of them,
// in forms no redaction can match, such as a URL encoded key. Text that is
// not an absolute URL is not shown.
func displayAddress(raw string) string {
	u, err := neturl.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return redact(u.Scheme+"://"+u.Host, configuredSecrets()...)
}

// checkError explains a failed check. Only a rejection blames the
// credentials; anything else says the check could not be made. secrets are
// removed from any text that reaches the terminal.
func checkError(provider string, err error, secrets ...string) error {
	var api *decide.APIError
	if errors.As(err, &api) && errors.Is(err, decide.ErrAuth) {
		return rejected(provider, api.StatusCode)
	}
	return couldNotCheck(provider, redact(friendlyError(err.Error()), secrets...))
}

func rejected(provider string, status int) error {
	hint := credentialHint(provider)
	if provider == "cloudflare" {
		hint += "\nThe token needs Workers AI access."
	}
	return cli.Errorf("The %s provider rejected %s (status %d)", provider, keyVars[provider], status).
		Hint(hint + "\nThen check again with: " + modelsCommand(provider))
}

// modelsCommand is the command to run again, with the provider when it is
// not the default.
func modelsCommand(provider string) string {
	if provider == "typesafe" {
		return "decide models"
	}
	return "decide models --provider " + provider
}

// forbidden reports a 403, which does not prove the credentials are bad: a
// restricted key can lack permission for the one request decide made.
func forbidden(provider string) error {
	if provider == "cloudflare" {
		return cli.Errorf("Could not confirm %s (status 403)", keyVars[provider]).
			Hint("decide checked GET /accounts/ACCOUNT_ID/ai/models/search.\n" +
				"Cloudflare answers 403 when the token lacks Workers AI access or the account ID is wrong.\n" +
				"Check both, then check again with: " + modelsCommand(provider))
	}
	return cli.Errorf("Could not confirm %s (status 403)", keyVars[provider]).
		Hint("decide checked GET /v1/models, and OpenAI refused it.\n" +
			"A restricted key can lack permission to list models and still work for decisions, " +
			"but a 403 can also mean the key is blocked.\n" +
			"Check the key's permissions, or try a small decide run.")
}

func couldNotCheck(provider, reason string) error {
	return cli.Errorf("Could not check the %s credentials: %s", provider, reason).
		Hint(fmt.Sprintf("Check %s and your connection, or try again later if the provider is down.\nTry again with: %s", baseURLVars[provider], modelsCommand(provider)))
}

// redact removes secrets, and the user info of any URL, from text that may
// carry them, such as a network error that quotes the request URL.
func redact(s string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[REDACTED]")
		}
	}
	return userInfo.ReplaceAllString(s, "${1}")
}

var userInfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s@"]*@`)

// checkProvider checks Cloudflare or OpenAI credentials with one
// authenticated GET. It builds the client a real run would build first, so
// a configuration a run rejects is rejected here with the same error. It
// never follows a redirect and never prints the response body.
func checkProvider(ctx context.Context, provider string) error {
	cfg, err := credentials(provider)
	if err != nil {
		return err
	}
	if _, err := backend.NewClient(cfg); err != nil {
		return err
	}
	secrets := []string{cfg.APIKey, cfg.AccountID}
	base := strings.TrimRight(cfg.BaseURL, "/")
	var url string
	if provider == "cloudflare" {
		if base == "" {
			base = "https://api.cloudflare.com/client/v4"
		}
		url = base + "/accounts/" + cfg.AccountID + "/ai/models/search?per_page=1"
	} else {
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		url = base + "/models"
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return couldNotCheck(provider, "the request could not be built")
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		var ue *neturl.Error
		if errors.As(err, &ue) {
			err = ue.Err // the wrapper repeats the request URL
		}
		return couldNotCheck(provider, redact(friendlyError(err.Error()), secrets...))
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return rejected(provider, resp.StatusCode)
	case resp.StatusCode == http.StatusForbidden:
		return forbidden(provider)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return couldNotCheck(provider, fmt.Sprintf("status %d", resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return couldNotCheck(provider, "the answer could not be read")
	}
	if !expectedAnswer(provider, body) {
		return couldNotCheck(provider, "the answer was not what "+provider+" sends")
	}
	return nil
}

// expectedAnswer reports whether body has the shape the provider's check
// returns, so a login page or a failure with a 200 status is not an ok.
func expectedAnswer(provider string, body []byte) bool {
	var answer struct {
		Success *bool           `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return false
	}
	if provider == "cloudflare" {
		return answer.Success != nil && *answer.Success
	}
	return (len(answer.Error) == 0 || string(answer.Error) == "null") && strings.HasPrefix(strings.TrimSpace(string(answer.Data)), "[")
}

func printModels(w io.Writer, res modelsResult) {
	what := "key accepted"
	if res.Provider == "cloudflare" {
		what = "token accepted"
	}
	line := fmt.Sprintf("%s  %s", bold(res.Provider), what)
	if v := displayAddress(strings.TrimSpace(os.Getenv(baseURLVars[res.Provider]))); v != "" {
		line += "  " + dim("("+printable(v)+")")
	}
	fmt.Fprintln(w, line)
	if len(res.Models) == 0 {
		fmt.Fprintln(w, "  no models listed")
	}
	width := 0
	for _, m := range res.Models {
		width = max(width, len(clean(m.Name)))
	}
	for _, m := range res.Models {
		name := clean(m.Name)
		if m.Name == res.Default {
			fmt.Fprintf(w, "  %-*s   %s\n", width, name, dim("(default)"))
		} else {
			fmt.Fprintf(w, "  %s\n", name)
		}
	}
	if len(res.Models) > 0 {
		fmt.Fprintln(w)
	}
	switch res.Provider {
	case "cloudflare":
		fmt.Fprintln(w, "Cloudflare does not list models. These are the ones decide knows.")
	case "openai":
		fmt.Fprintln(w, "OpenAI does not list Decisions models. These are the ones decide knows.")
	}
	if res.Provider == "typesafe" && len(res.Models) > 0 {
		fmt.Fprintf(w, "%s decide run sentiment notes.txt --model %s\n", dim("Choose one with:"), clean(res.Default))
	}
	switch res.Provider {
	case "cloudflare":
		fmt.Fprintln(w, "Checked by searching Workers AI models.")
		fmt.Fprintln(w, "Not checked: running decisions or credit.")
	case "openai":
		fmt.Fprintln(w, "Checked by listing models.")
		fmt.Fprintln(w, "Not checked: running decisions or credit.")
	default:
		fmt.Fprintln(w, "Checked by listing models.")
		fmt.Fprintln(w, "Not checked: running decisions, credit, or access to a model.")
	}
}
