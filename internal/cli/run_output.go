package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/deepnoodle-ai/decide/internal/jobs"
	"golang.org/x/term"
)

type resultPrinter struct {
	out            io.Writer
	color, details bool
}

func newResultPrinter(out io.Writer, color string, details bool) resultPrinter {
	enabled := color == "always"
	if color == "" || color == "auto" {
		f, ok := out.(*os.File)
		enabled = ok && term.IsTerminal(int(f.Fd())) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	}
	return resultPrinter{out: out, color: enabled, details: details}
}

func (p resultPrinter) style(code, text string) string {
	if !p.color {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func readable(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
}

func (p resultPrinter) summary(s jobs.Summary) error {
	_, err := fmt.Fprintf(p.out, "%s  %d complete · %d failed · %d dropped · %d uncertain · %d requests\n%s %s\n%s decide inspect %s\n", p.style("1", "Run "+s.ID+": "+s.Status), s.Completed, s.Failed, s.Dropped, s.Uncertain, s.Requests, p.style("2", "Saved evidence:"), readable(s.Path), p.style("2", "Full evidence:"), s.ID)
	return err
}

func (p resultPrinter) result(r jobs.Result) error {
	name := r.Source.Path
	if name == "" {
		name = r.Source.URI
	}
	if name == "" {
		name = r.ID
	}
	if r.Source.Line > 0 {
		name += fmt.Sprintf(":%d", r.Source.Line)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", p.style("1", readable(name)), p.style("2", readable(r.Status)))
	if r.Error != "" {
		fmt.Fprintf(&b, "  %s\n", p.style("31", readable(r.Error)))
	}
	for _, stage := range r.Stages {
		var response struct {
			Answers map[string]json.RawMessage `json:"answers"`
		}
		_ = json.Unmarshal(stage.Response, &response)
		keys := make([]string, 0, len(response.Answers))
		width := 0
		for key := range response.Answers {
			keys = append(keys, key)
			width = max(width, len([]rune(readable(key))))
		}
		sort.Strings(keys)
		if len(r.Stages) > 1 {
			fmt.Fprintf(&b, "  %s\n", p.style("2", readable(stage.Name)))
		}
		for _, key := range keys {
			var a struct {
				Type          string             `json:"type"`
				Noul          *float64           `json:"noul"`
				Score         *float64           `json:"score"`
				Choice        string             `json:"choice"`
				Confidence    *float64           `json:"confidence"`
				Probabilities map[string]float64 `json:"probabilities"`
				Legend        map[string]any     `json:"legend"`
			}
			if err := json.Unmarshal(response.Answers[key], &a); err != nil {
				fmt.Fprintf(&b, "  %s  unavailable\n", readable(key))
				continue
			}
			value := "unsupported answer; see full evidence"
			switch a.Type {
			case "noul":
				if a.Noul != nil {
					value = fmt.Sprintf("%.0f%% yes", *a.Noul*100)
				}
			case "score":
				if a.Score != nil {
					levels := len(a.Probabilities)
					var q struct {
						Criteria []json.RawMessage `json:"criteria"`
					}
					_ = json.Unmarshal(stage.Questions[key], &q)
					levels = max(levels, len(a.Legend), len(q.Criteria))
					if levels > 0 {
						value = fmt.Sprintf("%.2f / %d", *a.Score, levels-1)
					} else {
						value = fmt.Sprintf("%.2f", *a.Score)
					}
				}
			case "choice":
				value = readable(a.Choice)
				if probability, ok := a.Probabilities[a.Choice]; ok {
					value += fmt.Sprintf(" · %.0f%% probability", probability*100)
				}
			}
			fmt.Fprintf(&b, "  %s  %s\n", p.style("2", fmt.Sprintf("%-*s", width, readable(key))), p.style("36", value))
			if !p.details {
				continue
			}
			if a.Confidence != nil {
				fmt.Fprintf(&b, "    %s %.0f%%\n", p.style("2", "Confidence:"), *a.Confidence*100)
			}
			levels := make([]string, 0, len(a.Probabilities))
			for level := range a.Probabilities {
				levels = append(levels, level)
			}
			sort.Slice(levels, func(i, j int) bool {
				if a.Type == "score" {
					x, e := strconv.Atoi(levels[i])
					y, f := strconv.Atoi(levels[j])
					if e == nil && f == nil {
						return x < y
					}
				}
				return levels[i] < levels[j]
			})
			for _, level := range levels {
				label := level
				if legend, ok := a.Legend[level].(string); ok {
					label += " · " + readable(legend)
				}
				probability := a.Probabilities[level]
				filled := max(0, min(10, int(math.Round(probability*10))))
				fmt.Fprintf(&b, "    %s %s  %s\n", p.style("36", fmt.Sprintf("%3.0f%%", probability*100)), p.style("36", strings.Repeat("█", filled))+p.style("2", strings.Repeat("░", 10-filled)), label)
			}
		}
		if len(response.Answers) == 0 && len(stage.Result) > 0 {
			fmt.Fprintf(&b, "  %s  %s\n", p.style("2", readable(stage.Name)), p.style("36", shortResult(stage.Result)))
		}
	}
	b.WriteByte('\n')
	_, err := io.WriteString(p.out, b.String())
	return err
}

func shortResult(raw json.RawMessage) string {
	text := readable(string(raw))
	r := []rune(text)
	if len(r) > 240 {
		return string(r[:240]) + "…"
	}
	return text
}
