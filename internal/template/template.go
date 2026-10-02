// Package template loads templates: named sets of typed questions that the
// decide command asks about every item in a dataset.
//
// A template is a directory containing template.json and an optional
// README.md. Templates are found, in order, in .decide/templates in the
// working directory, in DECIDE_HOME/templates, and among the built-in
// templates. The first match wins.
package template

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/deepnoodle-ai/decide"
)

// Input says what kind of content a template reads.
type Input string

const (
	// Text reads text files, records, and stdin. The default.
	Text Input = "text"
	// Image reads PNG, JPEG, and WebP files, one item per image.
	Image Input = "image"
)

// Units say how much of a text file is one item. The run's --each flag
// chooses one, then the template's Each, and otherwise the data does: a
// dataset (JSONL, a JSON array, or CSV) has one item per record, a .txt
// file or piped text one per line, and any other file is one item.
const (
	EachFile      = "file"
	EachLine      = "line"
	EachParagraph = "paragraph"
	EachSection   = "section"
	EachFunction  = "function"
)

// Units lists the values of --each and of a template's each.
var Units = []string{EachFile, EachLine, EachParagraph, EachSection, EachFunction}

// Where a template was found.
const (
	BuiltIn = "built-in"
	Project = "project"
	User    = "user"
)

// Template is a named set of questions.
type Template struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Input       Input                `json:"input,omitempty"`
	Each        string               `json:"each,omitempty"` // the default unit, such as "file"
	Parameters  map[string]Parameter `json:"parameters,omitempty"`
	Questions   Questions            `json:"questions"`
	Flags       map[string]Flag      `json:"flags,omitempty"`   // answers that need attention, by question
	Matches     map[string]Match     `json:"matches,omitempty"` // answers the user is looking for, by question

	Docs     string `json:"-"` // contents of README.md
	Location string `json:"-"` // BuiltIn, Project, or User
	Dir      string `json:"-"` // directory on disk; empty for built-in templates
}

// Parameter is a value substituted for {{name}} in question text. A
// parameter without a default must be set for each run.
type Parameter struct {
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
}

// Question is one named question, kept in the order the template defines it.
type Question struct {
	Key string
	Raw json.RawMessage
}

// Questions preserves the order of a JSON object of questions.
type Questions []Question

func (qs *Questions) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return errors.New("questions must be a JSON object")
	}
	*qs = nil
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		*qs = append(*qs, Question{Key: tok.(string), Raw: raw})
	}
	return nil
}

func (qs Questions) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, q := range qs {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(q.Key)
		b.Write(key)
		b.WriteByte(':')
		b.Write(q.Raw)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

//go:embed builtin
var builtins embed.FS

// Home is the directory for the user's templates and runs: DECIDE_HOME, or
// ~/.decide by default.
func Home() string {
	if dir := os.Getenv("DECIDE_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".decide"
	}
	return filepath.Join(home, ".decide")
}

// ProjectDir is where project templates live, relative to the working
// directory.
const ProjectDir = ".decide/templates"

// UserDir is where the user's own templates live.
func UserDir() string { return filepath.Join(Home(), "templates") }

// Load finds a template by name, or reads it from a path to a template
// directory or template.json file.
func Load(name string) (*Template, error) {
	if strings.ContainsAny(name, `/\`) || strings.HasSuffix(name, ".json") {
		dir := name
		if strings.HasSuffix(name, ".json") {
			dir = filepath.Dir(name)
		}
		if _, err := os.Stat(filepath.Join(dir, "template.json")); err != nil {
			return nil, &NotFoundError{Name: name}
		}
		return loadDir(dir, Project)
	}
	if !validName(name) {
		return nil, &NotFoundError{Name: name}
	}
	for _, loc := range []struct{ dir, where string }{{ProjectDir, Project}, {UserDir(), User}} {
		dir := filepath.Join(loc.dir, name)
		if _, err := os.Stat(filepath.Join(dir, "template.json")); err == nil {
			return loadDir(dir, loc.where)
		}
	}
	if _, err := fs.Stat(builtins, "builtin/"+name+"/template.json"); err == nil {
		return loadBuiltin(name)
	}
	return nil, &NotFoundError{Name: name}
}

// NotFoundError reports a template name that matches no template.
type NotFoundError struct{ Name string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("no template named %q", e.Name) }

func loadDir(dir, where string) (*Template, error) {
	path := filepath.Join(dir, "template.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s, err := parse(data, path)
	if err != nil {
		return nil, err
	}
	if docs, err := os.ReadFile(filepath.Join(dir, "README.md")); err == nil {
		s.Docs = string(docs)
	}
	s.Location, s.Dir = where, dir
	return s, nil
}

func loadBuiltin(name string) (*Template, error) {
	data, err := builtins.ReadFile("builtin/" + name + "/template.json")
	if err != nil {
		return nil, err
	}
	s, err := parse(data, name)
	if err != nil {
		return nil, err
	}
	docs, _ := builtins.ReadFile("builtin/" + name + "/README.md")
	s.Docs = string(docs)
	s.Location = BuiltIn
	return s, nil
}

func parse(data []byte, origin string) (*Template, error) {
	var s Template
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		var syntax *json.SyntaxError
		var typ *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syntax):
			origin += position(data, syntax.Offset)
		case errors.As(err, &typ):
			origin += position(data, typ.Offset)
		}
		return nil, fmt.Errorf("%s: %w", origin, err)
	}
	s.Normalize()
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", origin, err)
	}
	return &s, nil
}

// Normalize fills in the default input and reads the input values of
// earlier versions: "file" is text read one file at a time, and "record"
// is text.
func (s *Template) Normalize() {
	switch s.Input {
	case "", "record":
		s.Input = Text
	case "file":
		s.Input = Text
		if s.Each == "" {
			s.Each = EachFile
		}
	}
}

// position formats a byte offset as ":line:column".
func position(data []byte, offset int64) string {
	before := data[:min(int(offset), len(data))]
	line := bytes.Count(before, []byte("\n")) + 1
	col := len(before) - bytes.LastIndexByte(before, '\n') - 1
	return fmt.Sprintf(":%d:%d", line, col)
}

// List returns every available template, sorted by name, and an error for
// each template that could not be loaded. A project or user template hides a
// template of the same name further down the search order.
func List() ([]*Template, []error, error) {
	var broken []error
	seen := map[string]bool{}
	var out []*Template
	for _, loc := range []struct{ dir, where string }{{ProjectDir, Project}, {UserDir(), User}} {
		entries, err := os.ReadDir(loc.dir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, nil, err
		}
		for _, e := range entries {
			dir := filepath.Join(loc.dir, e.Name())
			if !e.IsDir() || seen[e.Name()] {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, "template.json")); err != nil {
				continue
			}
			seen[e.Name()] = true
			s, err := loadDir(dir, loc.where)
			if err != nil {
				broken = append(broken, err)
				continue
			}
			out = append(out, s)
		}
	}
	entries, _ := builtins.ReadDir("builtin")
	for _, e := range entries {
		if seen[e.Name()] {
			continue
		}
		s, err := loadBuiltin(e.Name())
		if err != nil {
			return nil, nil, err
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b *Template) int { return strings.Compare(a.Name, b.Name) })
	return out, broken, nil
}

var (
	namePattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	placeholderPattern = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_-]+)\s*\}\}`)
)

func validName(s string) bool { return namePattern.MatchString(s) }

func hasControl(s string) bool {
	return strings.ContainsFunc(s, unicode.IsControl)
}

// Validate checks that the template is complete and that every question is a
// valid yes/no (noul), choice, or score question.
func (s *Template) Validate() error {
	if !validName(s.Name) {
		return fmt.Errorf("name %q must use lowercase letters, numbers, hyphens, or underscores", s.Name)
	}
	if strings.TrimSpace(s.Description) == "" {
		return errors.New("description is required")
	}
	switch s.Input {
	case Text:
		if s.Each != "" && !slices.Contains(Units, s.Each) {
			return fmt.Errorf("each %q must be one of %s", s.Each, strings.Join(Units, ", "))
		}
	case Image:
		if s.Each != "" {
			return errors.New("an image template reads one image at a time, so it has no each")
		}
	default:
		return fmt.Errorf(`input %q must be "text" or "image"`, s.Input)
	}
	if len(s.Questions) == 0 {
		return errors.New("at least one question is required")
	}
	for name := range s.Parameters {
		if !placeholderPattern.MatchString("{{" + name + "}}") {
			return fmt.Errorf("parameter name %q must use letters, numbers, hyphens, or underscores", name)
		}
	}
	for _, q := range s.Questions {
		if hasControl(q.Key) {
			return fmt.Errorf("question %q must not contain control characters", q.Key)
		}
		var body struct {
			Criteria json.RawMessage `json:"criteria"`
		}
		var options Questions
		if json.Unmarshal(q.Raw, &body) == nil && json.Unmarshal(body.Criteria, &options) == nil {
			for _, o := range options {
				if hasControl(o.Key) {
					return fmt.Errorf("question %q option %q must not contain control characters", q.Key, o.Key)
				}
			}
		}
		for _, m := range placeholderPattern.FindAllStringSubmatch(string(q.Raw), -1) {
			if _, ok := s.Parameters[m[1]]; !ok {
				return fmt.Errorf("question %q uses {{%s}}, which is not a declared parameter", q.Key, m[1])
			}
		}
	}
	// Check the questions with placeholder values; real values arrive per run.
	values := map[string]string{}
	for name := range s.Parameters {
		values[name] = "example"
	}
	questions, err := s.With(values).Decode()
	if err != nil {
		return err
	}
	return s.checkFlags(questions)
}

// With returns a copy of the template with {{name}} placeholders replaced by
// values. Missing values fall back to parameter defaults; placeholders with
// neither are left in place.
func (s *Template) With(values map[string]string) *Template {
	out := *s
	out.Questions = make(Questions, len(s.Questions))
	for i, q := range s.Questions {
		out.Questions[i] = Question{Key: q.Key, Raw: substitute(q.Raw, s.Parameters, values)}
	}
	return &out
}

// substitute replaces placeholders in the raw JSON text, so member order is
// kept. Values are JSON-escaped; placeholders only appear inside strings.
func substitute(raw json.RawMessage, params map[string]Parameter, values map[string]string) json.RawMessage {
	out := placeholderPattern.ReplaceAllFunc(raw, func(m []byte) []byte {
		name := string(placeholderPattern.FindSubmatch(m)[1])
		val, ok := values[name]
		if !ok {
			val = params[name].Default
		}
		if val == "" && !ok {
			return m // no value yet; Resolve requires one before a run
		}
		quoted, _ := json.Marshal(val)
		return quoted[1 : len(quoted)-1]
	})
	return out
}

// Resolve applies parameter values given on the command line. Every
// parameter must have a value or a default.
func (s *Template) Resolve(values map[string]string) (*Template, error) {
	for name := range values {
		if _, ok := s.Parameters[name]; !ok {
			return nil, &ParamError{Template: s, Msg: fmt.Sprintf("%s has no parameter named %q", s.Name, name)}
		}
	}
	for _, name := range s.ParameterNames() {
		if _, ok := values[name]; !ok && s.Parameters[name].Default == "" {
			return nil, &ParamError{Template: s, Missing: name, Msg: fmt.Sprintf("%s needs a value for %q", s.Name, name)}
		}
	}
	return s.With(values), nil
}

// ParamError reports a missing or unknown parameter.
type ParamError struct {
	Template *Template
	Missing  string
	Msg      string
}

func (e *ParamError) Error() string { return e.Msg }

// ParameterNames returns parameter names in sorted order.
func (s *Template) ParameterNames() []string {
	names := make([]string, 0, len(s.Parameters))
	for name := range s.Parameters {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Decode converts the template's questions to typed questions.
func (s *Template) Decode() (map[string]decide.Question, error) {
	out := make(map[string]decide.Question, len(s.Questions))
	for _, q := range s.Questions {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(q.Raw, &fields); err != nil {
			return nil, fmt.Errorf("question %q must be a JSON object", q.Key)
		}
		if _, ok := fields["instructions"]; !ok {
			return nil, fmt.Errorf("question %q needs instructions", q.Key)
		}
		typed, err := decide.DecodeQuestion(q.Raw)
		if err != nil {
			return nil, fmt.Errorf("question %q: %w", q.Key, err)
		}
		switch typed.(type) {
		case *decide.NoulQuestion, *decide.ChoiceQuestion, *decide.ScoreQuestion:
		default:
			return nil, fmt.Errorf(`question %q has type %q; use "noul", "choice", or "score"`, q.Key, typed.QuestionType())
		}
		out[q.Key] = typed
	}
	req := decide.NewRequest("check")
	req.Questions = out
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return out, nil
}

// starter is the starting point for a new template.
const starter = `{
  "name": %q,
  "description": "Describe what this template decides about each item.",
  "questions": {
    "relevant": {
      "type": "noul",
      "instructions": "Is this item relevant to my project?"
    }
  }
}
`

// Create writes a new template to DECIDE_HOME/templates, or to the project's
// .decide/templates when project is true. It copies from when it is not nil.
func Create(name string, from *Template, project bool) (string, error) {
	if !validName(name) {
		return "", fmt.Errorf("%q is not a valid template name; use lowercase letters, numbers, hyphens, or underscores", name)
	}
	root := UserDir()
	if project {
		root = ProjectDir
	}
	dir := filepath.Join(root, name)
	if _, err := os.Stat(dir); err == nil {
		return "", fmt.Errorf("%s already exists", dir)
	}
	data := []byte(fmt.Sprintf(starter, name))
	if from != nil {
		c := *from
		c.Name = name
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false) // keep flags like "<= 1.5" readable
		enc.SetIndent("", "  ")
		if err := enc.Encode(&c); err != nil {
			return "", err
		}
		data = b.Bytes()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "template.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	if from != nil && from.Docs != "" {
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(from.Docs), 0o644); err != nil {
			return "", err
		}
	}
	return path, nil
}
