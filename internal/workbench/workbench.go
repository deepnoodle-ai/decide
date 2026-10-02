// Package workbench presents dataset experiments and saved evidence with Wonton.
// File and model work runs in commands; only the event loop changes screen state.
package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/deepnoodle-ai/decide/internal/catalog"
	"github.com/deepnoodle-ai/decide/internal/jobs"
	"github.com/deepnoodle-ai/wonton/tui"
	"golang.org/x/term"
)

const windowLimit = 200

// Explore opens a source and judgment workbench. Sample runs are explicit model
// operations; opening the workbench and previewing sources never call a model.
func Explore(ctx context.Context, options jobs.Options) error {
	if err := terminalCheck("explore"); err != nil {
		return err
	}
	return tui.Run(newScreen(ctx, options, ""))
}

// Inspect browses a durable run or a legacy typesafe_cli:1 JSONL evidence file.
func Inspect(ctx context.Context, target, runDir string) error {
	if err := terminalCheck("inspect"); err != nil {
		return err
	}
	o := jobs.DefaultOptions()
	o.RunDir = runDir
	return tui.Run(newScreen(ctx, o, target))
}

func terminalCheck(command string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return fmt.Errorf("decide %s requires terminal stdin and stdout; use plan, run, or runs export for pipelines", command)
	}
	return nil
}

type experiment struct {
	Name    string
	Summary jobs.Summary
	Results []jobs.Result
	Options jobs.Options
}
type update struct {
	time.Time
	kind     string
	skills   []catalog.Skill
	patterns []catalog.Pattern
	skill    *catalog.Skill
	prepared []jobs.Prepared
	results  []jobs.Result
	result   *jobs.Result
	summary  jobs.Summary
	err      error
	message  string
	options  jobs.Options
}

func (e update) Timestamp() time.Time { return e.Time }

type screen struct {
	ctx                         context.Context
	cancel                      context.CancelFunc
	workCancel                  context.CancelFunc
	wg                          sync.WaitGroup
	stopMu                      sync.Mutex
	stopped                     bool
	runtime                     *tui.Runtime
	options                     jobs.Options
	lastOptions                 *jobs.Options
	target                      string
	initialized, busy, quitting bool
	width, height               int
	tab, index, stage           int
	scroll                      int
	skills                      []catalog.Skill
	patterns                    []catalog.Pattern
	skill                       *catalog.Skill
	prepared                    []jobs.Prepared
	sample                      []jobs.Prepared
	results                     []jobs.Result
	totalResults                int
	experiments                 []experiment
	summary                     jobs.Summary
	status, problem, filter     string
	edit, draft                 string
	pending                     []tui.Cmd
	patternLibrary              bool
	detail                      bool
}

func newScreen(ctx context.Context, o jobs.Options, target string) *screen {
	ctx, cancel := context.WithCancel(ctx)
	return &screen{ctx: ctx, cancel: cancel, options: o, target: target, width: 80, height: 24, status: "A little curiosity goes a long way. Pick sources, then preview a sample."}
}
func (s *screen) SetRuntime(r *tui.Runtime) {
	s.runtime = r
	ctx := s.ctx
	go func() { <-ctx.Done(); r.SendEvent(update{kind: "shutdown"}) }()
}
func (s *screen) Destroy() {
	s.stopMu.Lock()
	s.stopped = true
	s.stopMu.Unlock()
	s.cancel()
	if s.workCancel != nil {
		s.workCancel()
	}
	s.wg.Wait()
}

// background captures immutable arguments before a command starts. No display
// state is read from its goroutine, including progressive result callbacks.
func (s *screen) background(f func(context.Context) update) tui.Cmd {
	ctx, cancel := context.WithCancel(s.ctx)
	s.workCancel = cancel
	s.busy = true
	s.problem = ""
	return func() tui.Event {
		s.stopMu.Lock()
		if s.stopped {
			s.stopMu.Unlock()
			return update{kind: "shutdown"}
		}
		s.wg.Add(1)
		s.stopMu.Unlock()
		defer s.wg.Done()
		return f(ctx)
	}
}

func (s *screen) HandleEvent(event tui.Event) []tui.Cmd {
	if s.quitting {
		return nil
	}
	if s.ctx.Err() != nil {
		s.quitting = true
		return []tui.Cmd{tui.Quit()}
	}
	if !s.initialized {
		s.initialized = true
		target, o := s.target, cloneOptions(s.options)
		cmd := s.background(func(ctx context.Context) update {
			if target != "" {
				r, sum, err := loadEvidence(ctx, target, o.RunDir, "")
				return update{kind: "inspect", results: r, summary: sum, err: err}
			}
			skills, err := catalog.ListSkills()
			if err != nil {
				return update{kind: "library", err: err}
			}
			patterns, err := catalog.ListPatterns()
			if err != nil {
				return update{kind: "library", err: err}
			}
			var skill *catalog.Skill
			if o.Skill != "" {
				v, e := catalog.LoadSkill(o.Skill)
				if e != nil {
					return update{kind: "library", err: e}
				}
				skill = &v
			}
			return update{kind: "library", skills: skills, patterns: patterns, skill: skill}
		})
		// Preserve the initial terminal dimensions while scheduling loading.
		if e, ok := event.(tui.ResizeEvent); ok {
			s.width, s.height = e.Width, e.Height
		}
		return []tui.Cmd{cmd}
	}
	switch e := event.(type) {
	case tui.ResizeEvent:
		s.width, s.height = e.Width, e.Height
	case update:
		if e.kind == "result" {
			s.totalResults++
			s.results = appendWindow(s.results, *e.result)
			return nil
		}
		s.busy = false
		s.workCancel = nil
		if e.err != nil && e.kind != "run" {
			s.problem = e.err.Error()
			s.status = "Work stopped. Recorded runs keep their evidence."
			return nil
		}
		switch e.kind {
		case "library":
			s.skills, s.patterns, s.skill = e.skills, e.patterns, e.skill
			if s.skill == nil && len(s.skills) > 0 {
				v := s.skills[0]
				s.options.Skill = v.Name
				v.Name = filepath.Base(v.Name)
				s.skill = &v
			}
		case "preview":
			s.prepared = e.prepared
			s.sample = e.prepared
			s.tab = 2
			s.index = 0
			s.detail = false
			s.status = fmt.Sprintf("%d prepared sample items. Nothing sent to a model. Press s when ready.", len(e.prepared))
		case "run":
			s.lastOptions = &e.options
			if e.err != nil {
				s.problem = e.err.Error()
			}
			s.summary = e.summary
			s.results = e.results
			s.totalResults = e.summary.Completed + e.summary.Failed + e.summary.Dropped + e.summary.Uncertain
			s.tab = 3
			s.index = 0
			s.detail = false
			s.status = fmt.Sprintf("Run %s: %s · %d complete, %d failed, %d uncertain", e.summary.ID, e.summary.Status, e.summary.Completed, e.summary.Failed, e.summary.Uncertain)
			if e.message == "sample" {
				s.experiments = append(s.experiments, experiment{Name: fmt.Sprintf("Experiment %d", len(s.experiments)+1), Summary: e.summary, Results: e.results, Options: e.options})
				for len(s.experiments) > 1 && experimentBytes(s.experiments) > 32<<20 {
					s.experiments = s.experiments[1:]
				}
				if len(s.experiments) > 8 {
					s.experiments = s.experiments[len(s.experiments)-8:]
				}
			}
		case "inspect":
			s.results, s.summary = e.results, e.summary
			s.tab = 3
			s.index = 0
			s.detail = false
			s.status = fmt.Sprintf("Evidence notebook · %d visible records (window %d). / filters across the file.", len(e.results), windowLimit)
		case "export":
			s.status = e.message
		case "skill":
			s.skill = e.skill
			s.options.Skill = e.skill.Name
			s.sample = nil
			s.prepared = nil
			s.status = "Judgment updated. Preview before your next experiment."
		}
	case tui.KeyEvent:
		return s.key(e)
	}
	if len(s.pending) > 0 {
		p := s.pending
		s.pending = nil
		return p
	}
	return nil
}

func (s *screen) key(e tui.KeyEvent) []tui.Cmd {
	if e.Key == tui.KeyCtrlC {
		s.quitting = true
		if s.workCancel != nil {
			s.workCancel()
		}
		s.cancel()
		return []tui.Cmd{tui.Quit()}
	}
	if e.Key == tui.KeyEscape {
		if s.edit != "" {
			s.edit = ""
			return nil
		}
		if s.busy && s.workCancel != nil {
			s.workCancel()
			s.status = "Canceling work; preserving recorded evidence."
			return nil
		}
		s.detail = false
		return nil
	}
	if s.edit != "" {
		return nil
	}
	switch e.Key {
	case tui.KeyArrowUp:
		s.index = max(0, s.index-1)
		s.scroll = 0
	case tui.KeyArrowDown:
		s.index = min(max(0, s.count()-1), s.index+1)
		s.scroll = 0
	case tui.KeyArrowLeft:
		s.stage = max(0, s.stage-1)
		s.scroll = 0
	case tui.KeyArrowRight:
		s.stage++
		s.scroll = 0
	case tui.KeyPageDown:
		s.scroll += max(1, s.height-12)
	case tui.KeyPageUp:
		s.scroll = max(0, s.scroll-max(1, s.height-12))
	case tui.KeyEnter:
		if s.tab == 1 && s.patternLibrary && s.index < len(s.patterns) {
			s.options.Pattern = s.patterns[s.index].Name
			s.status = "Pattern selected. p previews, s samples, r runs the full selection."
		} else if s.tab == 1 && s.index < len(s.skills) {
			v := s.skills[s.index]
			s.options.Skill = v.Name
			v.Name = filepath.Base(v.Name)
			s.skill = &v
			s.options.Pattern = ""
			s.sample = nil
			s.prepared = nil
			s.status = "Skill selected. p previews its prepared inputs."
		} else {
			s.detail = !s.detail
			s.scroll = 0
		}
	}
	switch e.Rune {
	case 'q':
		s.quitting = true
		s.cancel()
		return []tui.Cmd{tui.Quit()}
	case '1', '2', '3', '4', '5':
		s.tab = int(e.Rune - '1')
		s.index = 0
		s.scroll = 0
		s.detail = false
	case 'b':
		if s.tab == 1 {
			s.patternLibrary = !s.patternLibrary
			s.index = 0
			s.scroll = 0
		}
	case '?':
		s.tab = 5
		s.scroll = 0
	case '/':
		if s.busy {
			s.status = "Finish or cancel current work before filtering evidence."
			return nil
		}
		s.edit = "filter"
		s.draft = s.filter
		return []tui.Cmd{tui.Focus("editor")}
	case 'a':
		if s.target == "" && !s.busy {
			s.begin("add source", "")
			return []tui.Cmd{tui.Focus("editor")}
		}
	case 'A':
		if s.target == "" && !s.busy {
			s.begin("sources", pretty(s.options.Sources))
			return []tui.Cmd{tui.Focus("editor")}
		}
	case 'i', 'x':
		if s.target == "" && !s.busy {
			mode := "include"
			if e.Rune == 'x' {
				mode = "exclude"
			}
			s.begin(mode, "")
			return []tui.Cmd{tui.Focus("editor")}
		}
	case 'd':
		if s.tab == 0 && s.target == "" && !s.busy && s.index < len(s.options.Sources.Sources) {
			source := s.options.Sources.Sources[s.index]
			s.options.Sources.Sources = append(s.options.Sources.Sources[:s.index], s.options.Sources.Sources[s.index+1:]...)
			s.index = max(0, min(s.index, len(s.options.Sources.Sources)-1))
			s.sample = nil
			s.prepared = nil
			s.status = "Removed source from this selection: " + source + ". Its files remain untouched."
		}
	case 'o':
		if s.target == "" && !s.busy {
			o := cloneOptions(s.options)
			o.Sources = jobs.DefaultOptions().Sources
			s.begin("execution", pretty(o))
			return []tui.Cmd{tui.Focus("editor")}
		}
	case 'v':
		if s.skill != nil && !s.busy {
			s.begin("questions", pretty(s.skill.Questions))
			return []tui.Cmd{tui.Focus("editor")}
		}
	case 't':
		if s.skill != nil && !s.busy {
			if len(s.skill.Parameters) == 0 {
				s.status = "This skill needs no parameters. v edits its questions."
				return nil
			}
			name := sortedKeys(s.skill.Parameters)[0]
			value := s.options.Params[name]
			if value == "" {
				raw := s.skill.Parameters[name].Default
				var str string
				if json.Unmarshal(raw, &str) == nil {
					value = str
				} else {
					value = string(raw)
				}
			}
			s.begin("parameter", name+"="+value)
			return []tui.Cmd{tui.Focus("editor")}
		}
	case 'T':
		if s.skill != nil && !s.busy {
			s.begin("parameters", pretty(s.options.Params))
			return []tui.Cmd{tui.Focus("editor")}
		}
	case 'p':
		if s.target == "" && !s.busy {
			return []tui.Cmd{s.preview()}
		}
	case 's':
		if s.target == "" && !s.busy {
			return s.run(false)
		}
	case 'r':
		if s.target == "" && !s.busy {
			s.begin("full", "run")
			return []tui.Cmd{tui.Focus("editor")}
		}
	case 'e':
		if !s.busy {
			s.begin("export", "")
			return []tui.Cmd{tui.Focus("editor")}
		}
	case 'c':
		s.tab = 4
		s.index = 0
		s.scroll = 0
	}
	return nil
}
func (s *screen) begin(mode, value string) { s.edit, s.draft = mode, value; s.problem = "" }
func (s *screen) count() int {
	switch s.tab {
	case 0:
		return len(s.options.Sources.Sources)
	case 1:
		if s.patternLibrary {
			return len(s.patterns)
		}
		return len(s.skills)
	case 2:
		return len(s.prepared)
	case 3:
		return len(s.results)
	case 4:
		return len(s.experiments)
	}
	return 1
}
func cloneOptions(o jobs.Options) jobs.Options {
	b, _ := json.Marshal(o)
	var c jobs.Options
	_ = json.Unmarshal(b, &c)
	c.NewClient = o.NewClient
	return c
}
func pretty(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(b)
}
func appendWindow(a []jobs.Result, r jobs.Result) []jobs.Result {
	const budget = 16 << 20
	if resultBytes(r) > budget {
		r = compactResult(r)
	}
	for len(a) > 0 && (len(a) >= windowLimit || resultsBytes(a)+resultBytes(r) > budget) {
		a[0] = jobs.Result{}
		a = a[1:]
	}
	return append(a, r)
}
func resultBytes(r jobs.Result) int { b, _ := json.Marshal(r); return len(b) }
func resultsBytes(a []jobs.Result) int {
	n := 0
	for _, r := range a {
		n += resultBytes(r)
	}
	return n
}
func experimentBytes(a []experiment) int {
	n := 0
	for _, e := range a {
		n += resultsBytes(e.Results)
	}
	return n
}
func compactResult(r jobs.Result) jobs.Result {
	r.Data = json.RawMessage(`"Large original data omitted from screen; inspect the exported evidence."`)
	stages := append([]jobs.Evidence(nil), r.Stages...)
	for i := range stages {
		stages[i].State = nil
		stages[i].Questions = nil
		stages[i].Response = nil
		stages[i].Result = json.RawMessage(`"Large evidence omitted from screen; retained in the run artifact."`)
	}
	r.Stages = stages
	return r
}

var errNoSkill = errors.New("select a skill in the library (2), then press Enter")
