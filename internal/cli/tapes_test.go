package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

// typeOrEnter finds what a tape types and when it presses Enter, as
// site/src/lib/tapes.ts does.
var typeOrEnter = regexp.MustCompile("Type(?:@\\S+)?\\s+(?:\"([^\"]*)\"|'([^']*)'|`([^`]*)`)|\\bEnter\\b")

// tapeCommands returns every command a tape types, hidden ones too. A line
// that ends with a pipe continues on the next, as it does in the shell.
func tapeCommands(tape string) []string {
	var commands []string
	var typed strings.Builder
	for line := range strings.SplitSeq(tape, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, m := range typeOrEnter.FindAllStringSubmatch(line, -1) {
			if m[0] == "Enter" {
				if strings.HasSuffix(strings.TrimSpace(typed.String()), "|") {
					typed.WriteString("\n")
					continue
				}
				commands = append(commands, typed.String())
				typed.Reset()
				continue
			}
			typed.WriteString(m[1] + m[2] + m[3])
		}
	}
	return commands
}

func TestTapeCommands(t *testing.T) {
	got := tapeCommands("# a comment\nHide\nType \"cd shop\" Enter\nShow\nType `echo \"hi\"` Sleep 500ms Enter\nType@10ms 'a' Type \"b\"\nEnter\nType \"echo x |\" Enter\nType \"  cat\" Enter\n")
	want := []string{"cd shop", `echo "hi"`, "ab", "echo x |\n  cat"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestTapes runs each site recording's commands against a fake server, so
// a CLI change that breaks a page of the docs site fails here. Exit codes 0
// and 2 pass; the refund tape must flag exactly one ticket and exit 2.
func TestTapes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds decide")
	}
	for _, tool := range []string{"sh", "git", "go", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// Read the runner's caches before each tape gets an isolated home.
	goEnv := exec.Command("go", "env", "GOMODCACHE", "GOCACHE")
	goEnv.Dir = repo
	out, err := goEnv.Output()
	if err != nil {
		t.Fatalf("go env: %v", err)
	}
	caches := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(caches) != 2 || caches[0] == "" || caches[1] == "" {
		t.Fatalf("go env returned invalid cache paths: %q", out)
	}
	tapes, err := filepath.Glob(filepath.Join(repo, "site", "tapes", "*.tape"))
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "decide"), "./cmd/decide")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	server := decidetest.NewServer(t)
	// command-risk flags every command, so a tape that runs a command only
	// when decide passes it, as checked.sh does, never runs it here.
	for _, key := range []string{"destructive", "leak", "publish", "severe"} {
		server.Answer(key, decidetest.NoulAnswer(0.99))
	}

	for _, path := range tapes {
		name := strings.TrimSuffix(filepath.Base(path), ".tape")
		if name == "settings" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			tapeServer := server
			if name == "template-run" {
				tapeServer = decidetest.NewServer(t)
				tapeServer.Respond(func(req *decide.Request) (*decide.Response, error) {
					state, err := json.Marshal(req.State)
					if err != nil {
						return nil, err
					}
					p := 0.05
					if strings.Contains(string(state), "charged twice") {
						p = 0.95
					}
					return &decide.Response{Answers: map[string]decide.Answer{
						"needs_refund": decidetest.NoulAnswer(p),
					}}, nil
				})
			}
			tape, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			commands := tapeCommands(string(tape))
			if len(commands) == 0 {
				t.Fatal("the tape types no commands")
			}
			// Each command runs in one shell, in order, and stops the
			// script on any exit code but 0 and 2.
			var script strings.Builder
			script.WriteString("clear() { :; }\n")
			flaggedRuns := 0
			for i, c := range commands {
				fmt.Fprintf(&script, "%s\ns=$?\nif [ $s -ne 0 ] && [ $s -ne 2 ]; then echo \"command %d exited $s\" >&2; exit 1; fi\n", c, i+1)
				if name == "template-run" && strings.HasPrefix(c, "decide run ") {
					flaggedRuns++
					fmt.Fprintf(&script, "if [ $s -ne 2 ]; then echo \"command %d exited $s, want 2\" >&2; exit 1; fi\n(exit \"$s\")\n", i+1)
				}
			}
			if name == "template-run" && flaggedRuns != 1 {
				t.Fatalf("refund tape has %d run commands, want 1", flaggedRuns)
			}
			cmd := exec.Command("sh", "-c", script.String())
			cmd.Dir = t.TempDir()
			cmd.Env = append(cleanEnv(),
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"HOME="+t.TempDir(),
				"DECIDE_HOME="+t.TempDir(),
				"GOMODCACHE="+caches[0],
				"GOCACHE="+caches[1],
				"GOPROXY=off",
				"GOSUMDB=off",
				"REPO="+repo,
				"TYPESAFE_API_KEY=test-key-00000000",
				"TYPESAFE_BASE_URL="+tapeServer.URL,
			)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err = cmd.Run()
			if err != nil || strings.Contains(stderr.String(), "unknown flag") {
				t.Fatalf("site/tapes/%s.tape: %v\ncommands:\n  %s\nstderr:\n%s",
					name, err, strings.Join(commands, "\n  "), stderr.String())
			}
			if name == "template-run" {
				if !strings.Contains(stderr.String(), "! 1 flagged") || !strings.Contains(stdout.String(), "exit=2") {
					t.Fatalf("refund tape must flag one ticket and print exit=2\nstdout:\n%s\nstderr:\n%s", &stdout, &stderr)
				}
			}
		})
	}
}

// cleanEnv is the environment without the settings that would send decide
// to a real provider, or change how it runs.
func cleanEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "DECIDE_") || strings.HasPrefix(name, "TYPESAFE_") ||
			strings.HasPrefix(name, "OPENAI_") || strings.HasPrefix(name, "CLOUDFLARE_") ||
			name == "PATH" || name == "HOME" {
			continue
		}
		env = append(env, kv)
	}
	return env
}
