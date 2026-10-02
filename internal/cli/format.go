package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/deepnoodle-ai/decide/internal/runs"
	"github.com/deepnoodle-ai/decide/internal/template"
	"github.com/deepnoodle-ai/wonton/cli"
)

// Formats that run, runs view, and runs resume print results in.
var formats = []string{"text", "json", "csv", "md", "github"}

const formatHelp = "How to print results: text, json, csv, md (Markdown), or github (GitHub Actions annotations)"

// formatOf returns the format chosen with --format, or with --json, which
// is short for --format json.
func formatOf(c *cli.Context) (string, error) {
	f := c.String("format")
	if c.Bool("json") {
		if f != "" && f != "json" {
			return "", cli.Errorf("Use --json or --format %s, not both", f).
				Hint("--json is short for --format json.")
		}
		f = "json"
	}
	if f == "" {
		f = "text"
	}
	if f != "text" && c.Bool("details") {
		return "", cli.Errorf("--details only applies to text output, not --format %s", f).
			Hint("JSON output always includes every probability.")
	}
	return f, nil
}

// resultWriter prints a run's items as they are answered, and finishes once the
// run stops.
type resultWriter interface {
	item(item) error
	finish(run *runs.Run) error
}

func newOutput(c *cli.Context, format string, s *template.Template, failOn string) resultWriter {
	w := c.Stdout()
	switch format {
	case "json":
		return jsonOutput{json.NewEncoder(w)}
	case "csv":
		return newCSVOutput(w, s)
	case "md":
		return mdOutput{w}
	case "github":
		return &githubOutput{w: w, marks: marksOf(s), template: s, failOn: failOn}
	}
	return newPrinter(w, s, c.Bool("details"))
}

func (p *printer) finish(*runs.Run) error { return nil }

type jsonOutput struct{ enc *json.Encoder }

func (o jsonOutput) item(it item) error     { return o.enc.Encode(it.json()) }
func (o jsonOutput) finish(*runs.Run) error { return nil }

// plainAnswer is an answer in words, such as "yes 88%", "billing 91%", or
// "2.0 of 4". With note, a score includes its level's description.
func plainAnswer(a answer, note bool) string {
	switch a.Type {
	case "noul":
		switch {
		case a.Noul >= unsureBelow:
			return "yes " + percent(a.Noul)
		case a.Noul <= unsureAbove:
			return "no " + percent(1-a.Noul)
		}
		return "unsure, " + percent(a.Noul) + " yes"
	case "choice":
		return clean(a.Choice) + " " + percent(a.Probabilities[a.Choice])
	case "score":
		s := fmt.Sprintf("%.1f of %d", a.Score, levels(a)-1)
		if text := legend(a, int(math.Round(a.Score))); note && text != "" {
			s += " (" + text + ")"
		}
		return s
	}
	return clean(a.Type)
}

// answerOf decodes an item's answer to a question.
func answerOf(it item, key string) (answer, bool) {
	raw, ok := it.Answers[key]
	if !ok {
		return answer{}, false
	}
	var a answer
	return a, json.Unmarshal(raw, &a) == nil
}

// csvOutput writes one row per item: its source, status, and marks, then
// a column for each question, or two for a choice.
type csvOutput struct {
	w        *csv.Writer
	template *template.Template
	marks    marks
	types    map[string]string
	header   bool
}

func newCSVOutput(w io.Writer, s *template.Template) *csvOutput {
	o := &csvOutput{w: csv.NewWriter(w), template: s, marks: marksOf(s), types: map[string]string{}}
	for _, q := range s.Questions {
		var body struct {
			Type string `json:"type"`
		}
		json.Unmarshal(q.Raw, &body)
		o.types[q.Key] = body.Type
	}
	return o
}

func (o *csvOutput) item(it item) error {
	if !o.header {
		o.header = true
		row := []string{"source", "status", "flagged", "matched"}
		for _, q := range o.template.Questions {
			row = append(row, q.Key)
			if o.types[q.Key] == "choice" {
				row = append(row, q.Key+"_probability")
			}
		}
		if err := o.w.Write(cells(append(row, "error"))); err != nil {
			return err
		}
	}
	row := []string{it.Source, it.Status,
		strconv.FormatBool(o.marks.has(it.Result, flagged)), strconv.FormatBool(o.marks.has(it.Result, matched))}
	for _, q := range o.template.Questions {
		a, ok := answerOf(it, q.Key)
		switch {
		case o.types[q.Key] == "choice" && ok:
			row = append(row, a.Choice, number(a.Probabilities[a.Choice]))
		case o.types[q.Key] == "choice":
			row = append(row, "", "")
		case !ok:
			row = append(row, "")
		case a.Type == "noul":
			row = append(row, number(a.Noul))
		default:
			row = append(row, number(a.Score))
		}
	}
	if err := o.w.Write(cells(append(row, it.Error))); err != nil {
		return err
	}
	o.w.Flush()
	return o.w.Error()
}

func (o *csvOutput) finish(*runs.Run) error {
	o.w.Flush()
	return o.w.Error()
}

func number(f float64) string { return strconv.FormatFloat(f, 'f', 4, 64) }

// cells cleans text for a spreadsheet: control characters are flattened,
// and a cell that a spreadsheet would read as a formula starts with a
// quote.
func cells(row []string) []string {
	for i, s := range row {
		s = clean(s)
		if s != "" && strings.ContainsRune("=+-@", rune(s[0])) {
			s = "'" + s
		}
		row[i] = s
	}
	return row
}

// mdOutput writes a Markdown report of the whole run when it finishes.
type mdOutput struct{ w io.Writer }

func (mdOutput) item(item) error            { return nil }
func (o mdOutput) finish(r *runs.Run) error { return report(o.w, r) }

// reportRows is how many items each table in a report lists.
const reportRows = 100

// report writes a Markdown summary of a run, for a pull request comment
// or a GitHub Actions job summary. Items that are flagged, matched, or
// failed come first; the rest are in a collapsed table.
func report(w io.Writer, r *runs.Run) error {
	results, err := r.Results()
	if err != nil {
		return err
	}
	m := marksOf(r.Template)
	var top, rest []item
	counts := map[string]int{}
	for _, it := range group(results, m) {
		switch {
		case it.Status != "complete":
			counts["failed"]++
			top = append(top, it)
		case m.has(it.Result, flagged):
			counts["flagged"]++
			top = append(top, it)
		case m.has(it.Result, matched):
			counts["matched"]++
			top = append(top, it)
		default:
			rest = append(rest, it)
		}
	}
	answered := len(top) + len(rest) - counts["failed"]

	var b strings.Builder
	fmt.Fprintf(&b, "### decide %s\n\n", mdText(r.Template.Name))
	parts := []string{fmt.Sprintf("**%d answered**", answered)}
	for _, k := range []string{"flagged", "matched", "failed"} {
		if counts[k] > 0 {
			parts = append(parts, fmt.Sprintf("**%d %s**", counts[k], k))
		}
	}
	if len(m.flags) > 0 && counts["flagged"] == 0 && answered > 0 {
		parts = append(parts, "nothing flagged")
	}
	parts = append(parts, mdText(r.Provider+" "+r.Model), "run "+mdText(r.ID))
	b.WriteString(strings.Join(parts, " · ") + "\n\n")

	if len(top) > 0 {
		table(&b, r.Template, m, top)
	}
	if len(rest) > 0 {
		label := "item"
		if len(rest) > 1 {
			label = "items"
		}
		if len(top) == 0 {
			table(&b, r.Template, m, rest)
		} else {
			fmt.Fprintf(&b, "<details><summary>%d more %s</summary>\n\n", len(rest), label)
			table(&b, r.Template, m, rest)
			b.WriteString("</details>\n\n")
		}
	}
	_, err = io.WriteString(w, b.String())
	return err
}

// table writes items as a Markdown table, with a column for each
// question. An answer that flags or matches the item is bold.
func table(b *strings.Builder, s *template.Template, m marks, items []item) {
	b.WriteString("| | Item |")
	for _, q := range s.Questions {
		b.WriteString(" " + mdText(q.Key) + " |")
	}
	b.WriteString("\n| --- | --- |" + strings.Repeat(" --- |", len(s.Questions)) + "\n")
	for _, it := range items[:min(len(items), reportRows)] {
		mark := ""
		switch {
		case it.Status != "complete":
			mark = "failed"
		case m.has(it.Result, flagged):
			mark = "flagged"
		case m.has(it.Result, matched):
			mark = "matched"
		}
		fmt.Fprintf(b, "| %s | %s |", mark, mdText(it.Source))
		for _, q := range s.Questions {
			a, ok := answerOf(it, q.Key)
			cell := ""
			if ok {
				cell = mdText(plainAnswer(a, false))
				if v := m.judge(q.Key, a); v == flagged || v == matched {
					cell = "**" + cell + "**"
				}
			} else if it.Status != "complete" && q.Key == s.Questions[0].Key {
				cell = mdText(friendlyError(it.Error))
			}
			b.WriteString(" " + cell + " |")
		}
		b.WriteByte('\n')
	}
	if len(items) > reportRows {
		fmt.Fprintf(b, "\nAnd %d more. See them all with `decide runs view`.\n", len(items)-reportRows)
	}
	b.WriteByte('\n')
}

// mdText escapes text from outside the program for Markdown, so a file
// name or answer cannot add links, images, HTML, mentions, or references
// to issues. A word joiner after "@" and "#" keeps GitHub from linking
// them, and one inside "://" and "www." keeps it from linking addresses.
func mdText(s string) string {
	var b strings.Builder
	for _, r := range clean(s) {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '@', '#':
			b.WriteRune(r)
			b.WriteRune(wordJoiner)
		case '\\', '`', '*', '_', '[', ']', '(', ')', '!', '|', '~':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return unlink.Replace(b.String())
}

const wordJoiner = '\u2060'

var unlink = strings.NewReplacer("://", ":"+string(wordJoiner)+"//", "www.", "www"+string(wordJoiner)+".")

// githubOutput writes GitHub Actions workflow commands: a warning for each
// flagged item and a notice for each matched one, at the file and line it
// came from. When the run finishes, it adds a Markdown report to the job
// summary.
type githubOutput struct {
	w        io.Writer
	marks    marks
	template *template.Template
	failOn   string // "flagged" or "matched": those items fail the run
}

func (o *githubOutput) item(it item) error {
	if it.Status != "complete" {
		return nil
	}
	isFlagged, isMatched := o.marks.has(it.Result, flagged), o.marks.has(it.Result, matched)
	var level, word string
	var mark verdict
	switch {
	case o.failOn == "flagged" && isFlagged, o.failOn == "matched" && isMatched:
		// The item fails the job, so say so, and name the answers that
		// fail it, even when the item is both flagged and matched.
		level, word = "error", o.failOn
		mark = map[string]verdict{"flagged": flagged, "matched": matched}[word]
	case isFlagged:
		level, word, mark = "warning", "flagged", flagged
	case isMatched:
		level, word, mark = "notice", "matched", matched
	default:
		return nil
	}
	var keys, names, lines []string
	for _, q := range o.template.Questions {
		a, ok := answerOf(it, q.Key)
		if !ok {
			continue
		}
		prefix := "  "
		if o.marks.judge(q.Key, a) == mark {
			keys, names = append(keys, q.Key), append(names, clean(q.Key))
			prefix = "! "
			if mark == matched {
				prefix = "● "
			}
		}
		line := prefix + clean(q.Key) + ": " + plainAnswer(a, true)
		if w := it.where[q.Key]; w != "" {
			line += " in " + clean(w)
		}
		lines = append(lines, line)
	}
	props := []string{"title=" + escapeProperty(fmt.Sprintf("%s %s %s", o.template.Name, word, strings.Join(names, ", ")))}
	file, line := location(it.Source)
	if file != "" {
		props = append(props, "file="+escapeProperty(file))
		if line == 0 {
			line = partLine(it, keys)
		}
		if line > 0 {
			props = append(props, "line="+strconv.Itoa(line))
		}
	}
	msg := clean(it.Source) + "\n" + strings.Join(lines, "\n")
	_, err := fmt.Fprintf(o.w, "::%s %s::%s\n", level, strings.Join(props, ","), escapeData(msg))
	return err
}

// finish adds a report to the job summary.
func (o *githubOutput) finish(r *runs.Run) error {
	return addToSummary(func(w io.Writer) error { return report(w, r) })
}

// addToSummary writes to the job summary, when GitHub Actions names a
// file for one.
func addToSummary(write func(io.Writer) error) error {
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if err := write(f); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// emptyReport is the Markdown report of a run with nothing to judge, so a
// pull request comment from an earlier run does not go stale.
func emptyReport(w io.Writer, name, msg string) error {
	_, err := fmt.Fprintf(w, "### decide %s\n\n%s.\n", mdText(name), mdText(msg))
	return err
}

// location finds the file and line that an item's source names, such as
// "server.go:42" or "models.py#L88". It returns no file for stdin, and no
// line for a section, such as "README.md#install", or a change in one
// commit of several, such as "server.go@1a2b3c4:42".
func location(src string) (file string, line int) {
	if i := strings.Index(src, ": "); i >= 0 { // a change inside a .patch file
		return src[:i], 0
	}
	file = src
	if i := strings.LastIndexByte(file, '['); i >= 0 && strings.HasSuffix(file, "]") { // an element of a JSON array
		if _, err := strconv.Atoi(file[i+1 : len(file)-1]); err == nil {
			file = file[:i]
		}
	}
	if i := strings.LastIndex(file, "#L"); i >= 0 {
		if n, err := strconv.Atoi(file[i+2:]); err == nil {
			file, line = file[:i], n
		}
	}
	if line == 0 {
		if i := strings.LastIndexByte(file, ':'); i >= 0 {
			if n, err := strconv.Atoi(file[i+1:]); err == nil {
				file, line = file[:i], n
			}
		}
	}
	if i := strings.LastIndexByte(file, '#'); line == 0 && i > strings.LastIndexByte(file, '/') {
		file = file[:i]
	}
	if i := strings.LastIndexByte(file, '@'); i >= 0 && isCommit(file[i+1:]) {
		file, line = file[:i], 0
	}
	if file == "stdin" {
		return "", 0
	}
	return file, line
}

func isCommit(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// partLine is the first line of the part that decided a question, for an
// item judged in parts, such as 412 for "lines 412-655".
func partLine(it item, keys []string) int {
	for _, k := range keys {
		lines, ok := strings.CutPrefix(it.where[k], "lines ")
		if !ok {
			continue
		}
		from, _, _ := strings.Cut(lines, "-")
		if n, err := strconv.Atoi(from); err == nil {
			return n
		}
	}
	return 0
}

// escapeData escapes a workflow command's message, so text from outside
// the program cannot end the command or start another.
func escapeData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

func escapeProperty(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(clean(s))
}
