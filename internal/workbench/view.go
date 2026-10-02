package workbench

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/deepnoodle-ai/decide/internal/jobs"
	"github.com/deepnoodle-ai/wonton/tui"
)

var amber = tui.Color(214)
var muted = tui.Color(248)

func (s *screen) View() tui.View {
	title := "decide / field notebook"
	if s.target != "" {
		title = "decide / evidence notebook"
	}
	header := tui.Stack(tui.Text(" %s", title).Bold().Fg(amber), tui.Text(" 1 Sources  2 Library  3 Preview  4 Evidence  5 Compare  ? Help").Fg(muted).Wrap(), tui.Divider()).Gap(0)
	var body tui.View
	if s.edit != "" {
		body = s.editorView()
	} else {
		body = tui.Scroll(tui.Text("%s", clean(s.content())).Wrap(), &s.scroll)
	}
	message := s.status
	color := muted
	if s.problem != "" {
		message = s.problem
		color = tui.ColorRed
	} else if s.busy {
		message = "Working · " + s.status
		color = amber
	}
	footer := tui.Stack(tui.Divider(), tui.Text(" %s", clean(message)).Fg(color).Wrap(), tui.Text(" p preview · s sample · r full · e export · Esc cancel · q quit").Fg(muted).Wrap()).Gap(0)
	return tui.Height(max(1, s.height), tui.PaddingLTRB(1, 0, 1, 0, tui.Stack(header, body, footer).Gap(0)))
}
func (s *screen) editorView() tui.View {
	s.draft = redactCredentials(s.draft)
	hint := "Edit JSON · Ctrl-S validates and saves · Esc discards · Enter adds a line"
	placeholder := "Type here"
	switch s.edit {
	case "add source":
		hint = "One path or URL · Enter adds · a adds another · Esc returns"
		placeholder = "./src or https://example.com/tickets.jsonl"
	case "include":
		hint = "Include matching files · Enter adds rule · Esc returns"
		placeholder = "**/*.go"
	case "exclude":
		hint = "Exclude matching files (exclusions win) · Enter adds rule · Esc returns"
		placeholder = "**/*_test.go"
	case "parameter":
		hint = "NAME=VALUE · Enter validates · Esc returns"
		placeholder = "focus=authorization"

	case "filter":
		hint = "Filter source, status, questions, or answers · Enter applies · Esc returns"
	case "export":
		hint = "New export directory · Enter writes evidence + config + command · Esc returns"
	case "full":
		hint = "Full dataset sends model requests. Type run and press Enter; Esc returns."
	}
	field := tui.InputField(&s.draft).ID("editor").Prompt("› ").Placeholder(placeholder).MaxHeight(max(1, s.height-13))
	multiline := s.edit == "sources" || s.edit == "execution" || s.edit == "parameters" || s.edit == "questions"
	field.Multiline(multiline).OnKey(func(e tui.KeyEvent) bool {
		if e.Key == tui.KeyCtrlS {
			s.submit(s.draft)
			return true
		}
		return false
	})
	if !multiline {
		field.OnSubmit(s.submit)
	}
	var guidance tui.View = tui.Empty()
	if s.edit == "parameter" && s.skill != nil {
		guidance = tui.Text("%s", clean(parameterInfo(s))).Wrap().Fg(muted)
	}
	return tui.Stack(tui.Text("%s", s.edit).Bold().Fg(amber), tui.Text("%s", hint).Wrap().Fg(muted), guidance, field).Gap(1)
}
func (s *screen) content() string {
	switch s.tab {
	case 0:
		return s.sourceContent()

	case 1:
		return s.libraryContent()
	case 2:
		if len(s.prepared) == 0 {
			return "TASTING FLIGHT\n\nFirst choose sources (1) and a skill (2).\np prepares a small sample, without asking a model.\ns sends that sample; r starts the full selection.\n\nEvery item keeps its source identity."
		}
		var b strings.Builder
		fmt.Fprintf(&b, "TASTING FLIGHT · %d prepared items\n\n", len(s.prepared))
		if !s.detail {
			for i, p := range s.prepared {
				fmt.Fprintf(&b, "%s %s\n", cursor(i, s.index), sourceName(p.Item.Source.URI, p.Item.Source.Path, p.Item.ID))
			}
			b.WriteString("\n↑/↓ choose · Enter opens prepared state · s runs this sample\n\n")
		}
		p := s.prepared[min(s.index, len(s.prepared)-1)]
		for i, img := range p.Item.Images {
			fmt.Fprintf(&b, "Image %d: %s · %d bytes (asset retained in run)\n", i+1, img.ContentType, len(img.Data))
		}
		fmt.Fprintf(&b, "Source: %s\nID: %s\nDigest: %s\nFormat: %s · %d bytes · %d images\n\nPREPARED STATE\n%s\n\nQUESTIONS\n%s", p.Item.Source.URI, p.Item.ID, p.Item.Source.Digest, p.Item.Source.Format, p.Item.Source.SizeBytes, len(p.Item.Images), previewJSON(p.State), pretty(p.Questions))
		return b.String()
	case 3:
		return s.evidenceContent()
	case 4:
		return s.compareContent()
	default:
		return helpText
	}
}
func (s *screen) judgmentInfo() string {
	if s.options.Pattern != "" {
		return "Pattern: " + s.options.Pattern
	}
	if s.skill == nil {
		return "No skill selected. 2 opens the library."
	}
	return fmt.Sprintf("Skill: %s\n%s\nt  Edit parameters · v  Edit questions", s.skill.Name, s.skill.Description)
}
func executionInfo(o jobs.Options) string {
	return fmt.Sprintf("Provider: %s · Model: %s\nWorkers: %d · Max requests: %d (0 unlimited)\nSnapshot: %s · Run directory: %s", o.Provider, o.Model, o.Workers, o.MaxRequests, o.Snapshot, o.RunDir)
}
func (s *screen) libraryContent() string {
	if s.patternLibrary {
		var b strings.Builder
		b.WriteString("PATTERN FIELD GUIDE\n\n↑/↓ browse · Enter chooses · b returns to skills\n\n")
		for i, p := range s.patterns {
			fmt.Fprintf(&b, "%s %s [%s] — %s\n", cursor(i, s.index), p.Name, p.Type, p.Description)
		}
		if len(s.patterns) > 0 {
			fmt.Fprintf(&b, "\n%s", pretty(s.patterns[min(s.index, len(s.patterns)-1)]))
		}
		return b.String()
	}

	var b strings.Builder
	b.WriteString("JUDGMENT LIBRARY\n\n↑/↓ browse · Enter chooses a skill · b patterns · t parameters · v questions\n\n")
	for i, v := range s.skills {
		fmt.Fprintf(&b, "%s %s — %s\n", cursor(i, s.index), v.Name, v.Description)
	}
	if len(s.skills) > 0 {
		v := s.skills[min(s.index, len(s.skills)-1)]
		fmt.Fprintf(&b, "\n%s\n\nInputs: %s\nParameters:\n%s\n\nQuestions:\n%s\n", v.Documentation, strings.Join(v.Inputs, ", "), pretty(v.Parameters), pretty(v.Questions))
	}
	b.WriteString("\nPATTERN FIELD GUIDE\n\n")
	for _, p := range s.patterns {
		fmt.Fprintf(&b, "%s [%s] — %s\n", p.Name, p.Type, p.Description)
	}
	b.WriteString("\nConfigure patterns with decide patterns show NAME; enter its path\nas Pattern in execution settings (o). Pattern runs retain stage evidence.")
	return b.String()
}
func (s *screen) evidenceContent() string {
	if len(s.results) == 0 {
		return fmt.Sprintf("EVIDENCE NOTEBOOK\n\n%d outcomes received.\n%s\n\nRun a sample with s, or open recorded evidence with decide inspect.\nFailures and uncertain requests remain explicit.", s.totalResults, s.status)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "EVIDENCE · %d shown · %d outcomes this session\nFilter: %s\n\n", len(s.results), s.totalResults, s.filter)
	if !s.detail {
		start := max(0, s.index-3)
		end := min(len(s.results), start+7)
		for i := start; i < end; i++ {
			r := s.results[i]
			fmt.Fprintf(&b, "%s %-10s %s\n", cursor(i, s.index), r.Status, sourceName(r.Source.URI, r.Source.Path, r.ID))
		}
		b.WriteString("\n↑/↓ select · Enter expands · ←/→ stage · PgUp/PgDn scroll · / filter\n\n")
	}
	r := s.results[min(s.index, len(s.results)-1)]
	fmt.Fprintf(&b, "%s\nSource: %s\nStatus: %s\n%s\n", r.ID, r.Source.URI, r.Status, r.Error)
	if len(r.Stages) > 0 {
		stage := min(s.stage, len(r.Stages)-1)
		e := r.Stages[stage]
		fmt.Fprintf(&b, "\nSTAGE %d/%d · %s\nModel: %s · Request: %s\nTokens: %d in / %d out\n", stage+1, len(r.Stages), e.Name, e.Model, e.RequestID, e.InputTokens, e.OutputTokens)
		if transformation(e) {
			b.WriteString("Local transformation · no provider request\n")
		} else if err := validateEvidence(e); err != nil {
			fmt.Fprintf(&b, "Validation: %v\n", err)
		} else {
			b.WriteString("Validation: questions and answers checked\n")
		}
		if e.Error != "" {
			fmt.Fprintf(&b, "Error: %s\n", e.Error)
		}
		fmt.Fprintf(&b, "\nANSWERS / DISTRIBUTIONS\n%s\n\nRESULT\n%s\n\nQUESTIONS\n%s\n\nSTATE\n%s", distributionText(e.Response), previewJSON(e.Result), pretty(e.Questions), previewJSON(e.State))
	}
	if s.detail {
		fmt.Fprintf(&b, "\n\nORIGINAL DATA\n%s", previewJSON(r.Data))
	}
	return b.String()
}
func (s *screen) compareContent() string {
	if len(s.experiments) < 2 {
		return "EXPERIMENT BENCH\n\nA good question gets better with a second look.\n\n1. p freezes a sample.\n2. s runs experiment one.\n3. t changes parameters, v edits questions, or o changes the model.\n4. s runs the same source items again.\n\nc compares results by source ID. Changing sources or skills requires\na fresh preview. Up to eight experiments stay in this notebook."
	}
	left, right := s.experiments[len(s.experiments)-2], s.experiments[len(s.experiments)-1]
	var b strings.Builder
	fmt.Fprintf(&b, "SAME-SAMPLE COMPARISON\n\n%s: %s · %s\n%s: %s · %s\n\n", left.Name, left.Summary.ID, left.Options.Model, right.Name, right.Summary.ID, right.Options.Model)
	byID := map[string]jobs.Result{}
	for _, r := range left.Results {
		byID[r.ID] = r
	}
	for _, r := range right.Results {
		a, ok := byID[r.ID]
		if !ok {
			fmt.Fprintf(&b, "%s — absent from prior sample\n", r.ID)
			continue
		}
		fmt.Fprintf(&b, "%s\n  before [%s]: %s\n  after  [%s]: %s\n\n", sourceName(r.Source.URI, r.Source.Path, r.ID), a.Status, briefAnswers(a), r.Status, briefAnswers(r))
	}
	b.WriteString("Raw probabilities are evidence, not action policies.\ne exports the latest experiment and its frozen judgment configuration.")
	return b.String()
}
func briefAnswers(r jobs.Result) string {
	if len(r.Stages) == 0 {
		return r.Error
	}
	return strings.ReplaceAll(distributionText(r.Stages[len(r.Stages)-1].Response), "\n", " ")
}
func cursor(i, selected int) string {
	if i == selected {
		return "›"
	}
	return " "
}
func sourceName(uri, path, id string) string {
	if path != "" {
		return path
	}
	if uri != "" {
		return uri
	}
	return id
}
func clean(s string) string {
	s = redactCredentials(s)
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return '�'
		}
		return r
	}, s)
}
func previewJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "(none)"
	}
	if len(raw) > 64<<10 {
		return fmt.Sprintf("%s\n… preview truncated (%d bytes total); full evidence remains in the run.", string(raw[:64<<10]), len(raw))
	}
	var v any
	if json.Unmarshal(raw, &v) == nil {
		return pretty(v)
	}
	return string(raw)
}
func sortedKeys[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for key := range m {
		k = append(k, key)
	}
	sort.Strings(k)
	return k
}
func distributionText(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return previewJSON(raw)
	}
	// Preserve the complete answer structure; identify probability-like numeric
	// leaves with proportional bars without assuming provider-specific schemas.
	var b strings.Builder
	var visit func(string, any)
	visit = func(path string, x any) {
		switch x := x.(type) {
		case map[string]any:
			for _, k := range sortedKeys(x) {
				visit(strings.TrimPrefix(path+"."+k, "."), x[k])
			}
		case []any:
			for i, v := range x {
				visit(fmt.Sprintf("%s[%d]", path, i), v)
			}
		case float64:
			if x >= 0 && x <= 1 {
				fmt.Fprintf(&b, "%s  %s %.4f\n", path, strings.Repeat("▰", int(x*16)), x)
			} else {
				fmt.Fprintf(&b, "%s: %g\n", path, x)
			}
		default:
			fmt.Fprintf(&b, "%s: %v\n", path, x)
		}
	}
	visit("", v)
	return b.String()
}

const helpText = `FIELD NOTES

1 Sources   a adds one file, directory, or URL; repeat for more sources.
            i adds an include glob; x adds an exclude glob.
            ↑/↓ selects a source; d removes it from the selection.
            A opens advanced source JSON (format, pointers, bounds).
            Use globs such as **/*.go; exclusions win.
2 Library   Browse reusable skills; Enter picks; t edits NAME=VALUE; T opens parameter JSON;
            v edits question JSON. Ctrl-S validates and saves edits.
3 Preview   p selects and freezes at most 200 sample items, offline.
            Inspect prepared state, source provenance and questions.
4 Evidence  s sends the frozen sample to a model. r explicitly starts
            the full selection. Both preserve durable run artifacts.
5 Compare   c compares the latest two experiments on source IDs.
            Keep the sample, change questions/parameters/model, rerun.

↑/↓ select   Enter detail   ←/→ stage   PgUp/PgDn scroll
/ filter evidence across its artifact   e export to a new directory
o execution settings   Esc cancel/edit back   q or Ctrl-C quit

Samples call models only when you press s. Full runs require typing run.
Preview reads your sources, including URLs, but never calls a model.
Closing cancels outstanding work and preserves resumable artifacts.
The screen keeps at most 200 records / 16 MiB; filters scan saved evidence.
Comparison history is capped at 32 MiB. JSONL records are capped at 16 MiB.
Exports include evidence, skill configuration, options and a batch command.
Images need the Cloudflare provider (Clef/Clef-flash); TypeSafe rejects
image work before submission. Preview shows MIME type and byte size.
Sample memory is bounded to 64 MiB; source byte limits are in A.
Credentials are read at execution and never saved in the notebook.`

func (s *screen) sourceContent() string {
	var b strings.Builder
	b.WriteString("SOURCE TRAIL\n\na Add a file, directory, or URL · i Include glob · x Exclude glob\n↑/↓ Select · d Remove selected source · A Advanced source JSON\n\n")
	if len(s.options.Sources.Sources) == 0 {
		b.WriteString("Your notebook is open. Where shall we look?\nPress a and try ./src, tickets.jsonl, or an HTTP URL.\n")
	} else {
		for i, source := range s.options.Sources.Sources {
			fmt.Fprintf(&b, "%s %s\n", cursor(i, s.index), source)
		}
	}
	d := s.options.Sources
	if d.Manifest != "" {
		fmt.Fprintf(&b, "Manifest: %s\n", d.Manifest)
	}
	fmt.Fprintf(&b, "\nInclude: %s\nExclude: %s\nFormat: %s\n\n%s\n\n%s\n\np previews up to %d prepared items, without model calls.\nDirectories honor ignore files; exclusions win.\nImages: Cloudflare Clef/Clef-flash required; TypeSafe has no image input.\nSource limits: %d bytes/item, %d bytes/source. Sample cap: 64 MiB.\n", listOrAll(d.Include, "all files"), listOrAll(d.Exclude, "none"), d.Format, s.judgmentInfo(), executionInfo(s.options), sampleSize(s.options), d.MaxItemBytes, d.MaxSourceBytes)
	return b.String()
}
func listOrAll(v []string, fallback string) string {
	if len(v) == 0 {
		return fallback
	}
	return strings.Join(v, ", ")
}
func parameterInfo(s *screen) string {
	var b strings.Builder
	for _, name := range sortedKeys(s.skill.Parameters) {
		p := s.skill.Parameters[name]
		fmt.Fprintf(&b, "%s (%s): %s\n", name, p.Type, p.Description)
	}
	return b.String()
}
