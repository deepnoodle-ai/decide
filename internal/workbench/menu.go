package workbench

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/deepnoodle-ai/wonton/tui"
)

type menuAction struct {
	label, hint string
	tab         int
	key         rune
}

func (s *screen) menuActions() []menuAction {
	switch s.tab {
	case 10:
		return []menuAction{
			{"Settings", "Model, sample size, request budget, and judgment parameters.", 8, 0},
			{"Compare experiments", "Compare two runs of the same frozen sample.", 4, 0},
			{"Run the full selection", "Explicit confirmation before sending model requests.", 6, 'r'},
			{"Export this experiment", "Save evidence and a reproducible batch command.", 6, 'e'},
		}
	case 7:
		return []menuAction{
			{"Browse files and folders", "Enter opens folders; Space selects; Esc returns here.", 0, 'f'},
			{"Add a path or URL", "A file, directory, JSONL export, or public URL.", 0, 'a'},
			{"Review selected sources", "Remove sources or adjust file filters.", 0, 0},
			{"Include matching files", "For example **/*.go or **/*.{png,jpg}.", 0, 'i'},
			{"Exclude matching files", "For example **/*_test.go.", 0, 'x'},
		}
	case 8:
		return []menuAction{
			{"Choose model", "Provider and model used for the next experiment.", 8, 'o'},
			{"Sample size", "Start small: 1–200 items.", 8, 'k'},
			{"Switch first-items / random sample", "Random sampling scans all selected sources.", 8, 'z'},
			{"Concurrent requests", "How many provider calls run together.", 8, 'w'},
			{"Request budget", "Maximum provider attempts, including retries.", 8, 'B'},
			{"Edit judgment parameters", "Tune the selected reusable judgment.", 8, 't'},
			{"Advanced controls", "Questions, patterns, JSON mappings, and execution settings.", 9, 0},
		}
	case 9:
		return []menuAction{
			{"Edit questions", "Advanced JSON question editor.", 9, 'v'},
			{"Choose a composition pattern", "Combine judgments; configure definitions with patterns show.", 1, 0},
			{"Expand a JSON array into records", "JSON Pointer to the array; empty means the root array.", 9, 'm'},
			{"Select model state", "JSON Pointer relative to each record.", 9, 'u'},
			{"Select record ID field", "JSON Pointer, for example /id.", 9, 'I'},
			{"Edit source settings", "Advanced JSON source selection and byte limits.", 9, 'A'},
			{"Edit execution settings", "Advanced provider, pattern, and run configuration.", 9, 'O'},
		}
	default:
		return []menuAction{
			{"Choose data", "Files, folders, images, JSONL, or public URLs.", 7, 0},
			{"Choose a judgment", "A skill is a reusable set of questions.", 1, 0},
			{"Preview a small sample", "Read the first few records. No model calls.", 6, 'p'},
			{"Run the previewed sample", "Sends the prepared records to your model provider.", 6, 's'},
			{"Review answers", "Browse saved results; Enter reveals full evidence.", 3, 0},
			{"More actions", "Settings, comparison, full runs, and export.", 10, 0},
		}
	}
}

func (s *screen) home() {
	s.tab, s.index, s.scroll, s.detail = 6, 0, 0, false
	s.guided = true
	if len(s.options.Sources.Sources) > 0 || s.options.Sources.Manifest != "" {
		s.index = 1
		if s.skill != nil || s.options.Pattern != "" {
			s.index = 2
			if len(s.sample) > 0 {
				s.index = 3
			}
			if len(s.results) > 0 {
				s.index = 4
			}
		}
	}
}

func (s *screen) menuContent() string {
	title := "TRY A JUDGMENT"
	switch s.tab {
	case 7:
		title = "CHOOSE DATA"
	case 8:
		title = "SETTINGS"
	case 9:
		title = "ADVANCED CONTROLS"
	case 10:
		title = "MORE ACTIONS"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", title)
	if s.tab == 6 {
		skill := "not chosen"
		if s.skill != nil {
			skill = s.skill.Name
		}
		if s.options.Pattern != "" {
			skill = s.options.Pattern
		}
		fmt.Fprintf(&b, "Data: %d sources · Judgment: %s\n", len(s.options.Sources.Sources), skill)
		fmt.Fprintf(&b, "Preview: %d records · Answers: %d\n\n", len(s.sample), s.totalResults)
	}
	actions := s.menuActions()
	a := actions[min(s.index, len(actions)-1)]
	fmt.Fprintf(&b, "%s\n\n", a.hint)
	start := max(0, s.index-max(1, (s.height-16)/2))
	end := min(len(actions), start+max(3, s.height-16))
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "%s %s\n", cursor(i, s.index), actions[i].label)
	}

	return b.String()
}

func (s *screen) selectMenu() []tui.Cmd {
	if s.busy {
		return nil
	}
	a := s.menuActions()[s.index]
	if s.tab == 6 && (s.index == 2 || s.index == 3) {
		if (len(s.options.Sources.Sources) == 0 && s.options.Sources.Manifest == "") || (s.skill == nil && s.options.Pattern == "") {
			s.problem = "Choose data and a judgment first. Preview is free of model calls."
			return nil
		}
		if s.index == 3 && len(s.sample) == 0 {
			s.problem = "Preview the sample first so you can see exactly what will be sent."
			return nil
		}
	}
	s.problem = ""
	if s.tab == 9 && s.index == 1 {
		s.patternLibrary = true
	}
	s.tab, s.index, s.scroll = a.tab, 0, 0
	if a.key != 0 {
		return s.key(tui.KeyEvent{Rune: a.key})
	}
	return nil
}

func excerpt(text string, limit int) string {
	r := []rune(text)
	if len(r) <= limit {
		return text
	}
	return string(r[:limit]) + "…"
}

func questionDescriptions(questions map[string]json.RawMessage) string {
	var b strings.Builder
	for _, name := range sortedKeys(questions) {
		var q struct {
			Instructions string `json:"instructions"`
		}
		_ = json.Unmarshal(questions[name], &q)
		fmt.Fprintf(&b, "%s: %s\n", name, excerpt(q.Instructions, 240))
	}
	return b.String()
}
