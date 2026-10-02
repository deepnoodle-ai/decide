// Package catalog loads declarative, local decision skills and execution patterns.
package catalog

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/patterns/gate"
)

const MaxConfigBytes = 1 << 20

type Parameter struct {
	Type        string          `json:"type"`
	Description string          `json:"description,omitempty"`
	Default     json.RawMessage `json:"default,omitempty"`
	Required    bool            `json:"required,omitempty"`
}

type Skill struct {
	Version       int                        `json:"version"`
	Name          string                     `json:"name"`
	Description   string                     `json:"description"`
	Inputs        []string                   `json:"inputs"`
	Parameters    map[string]Parameter       `json:"parameters,omitempty"`
	Questions     map[string]json.RawMessage `json:"questions"`
	State         string                     `json:"state,omitempty"`
	Policy        json.RawMessage            `json:"policy,omitempty"`
	Pattern       string                     `json:"pattern,omitempty"`
	Examples      []json.RawMessage          `json:"examples,omitempty"`
	Documentation string                     `json:"-"`
}

type Stage struct {
	Name      string          `json:"name"`
	Skill     string          `json:"skill"`
	Model     string          `json:"model,omitempty"`
	Answer    string          `json:"answer,omitempty"`
	Threshold *float64        `json:"threshold,omitempty"`
	Limit     int             `json:"limit,omitempty"`
	Policy    json.RawMessage `json:"policy,omitempty"`
}

type Pattern struct {
	Version     int               `json:"version"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Type        string            `json:"type"`
	Skill       string            `json:"skill,omitempty"`
	Stages      []Stage           `json:"stages,omitempty"`
	Selector    json.RawMessage   `json:"selector,omitempty"`
	Branches    map[string]string `json:"branches,omitempty"`
	Answer      string            `json:"answer,omitempty"`
	Run         string            `json:"run,omitempty"`
	BudgetBytes int               `json:"budget_bytes,omitempty"`
	MaxRecords  int               `json:"max_records,omitempty"`
	Policy      json.RawMessage   `json:"policy,omitempty"`
}

func Home() string {
	if p := os.Getenv("DECIDE_HOME"); p != "" {
		return p
	}
	if p := os.Getenv("XDG_CONFIG_HOME"); p != "" {
		return filepath.Join(p, "decide")
	}
	p, err := os.UserHomeDir()
	if err != nil {
		return ".decide"
	}
	return filepath.Join(p, ".config", "decide")
}

func readConfig(path string, dst any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxConfigBytes+1))
	if err != nil {
		return err
	}
	if len(b) > MaxConfigBytes {
		return fmt.Errorf("%s exceeds configuration byte limit", path)
	}
	if err := jsonv2.Unmarshal(b, dst, jsonv2.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func qualifiedPaths(kind, name string) ([]string, error) {
	if name == "" {
		return nil, errors.New("library name is required")
	}
	if strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") || filepath.IsAbs(name) {
		p := name
		if fi, e := os.Stat(p); e == nil && fi.IsDir() {
			p = filepath.Join(p, strings.TrimSuffix(kind, "s")+".json")
		}
		return []string{p}, nil
	}
	scope, base, has := strings.Cut(name, "/")
	if !has {
		base = name
		scope = ""
	}
	if !validName(base) {
		return nil, fmt.Errorf("invalid %s name %q", kind, name)
	}
	var paths []string
	for _, s := range []string{"builtin", "project", "user"} {
		if scope != "" && scope != s {
			continue
		}
		if s == "builtin" {
			if _, ok := builtins(kind)[base]; ok {
				paths = append(paths, "builtin/"+base)
			}
			continue
		}
		root := filepath.Join(".decide", kind)
		if s == "user" {
			root = filepath.Join(Home(), kind)
		}
		file := filepath.Join(root, base, strings.TrimSuffix(kind, "s")+".json")
		if _, e := os.Stat(file); e == nil {
			paths = append(paths, file)
		} else if !errors.Is(e, os.ErrNotExist) {
			return nil, e
		}
		// Patterns may be single-file definitions.
		if kind == "patterns" {
			file = filepath.Join(root, base+".json")
			if _, e := os.Stat(file); e == nil {
				paths = append(paths, file)
			}
		}
	}
	if scope != "" && scope != "builtin" && scope != "project" && scope != "user" {
		return nil, fmt.Errorf("unknown library scope %q", scope)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s %q not found; try decide %s list", strings.TrimSuffix(kind, "s"), name, kind)
	}
	if len(paths) > 1 {
		return nil, fmt.Errorf("ambiguous %s %q; qualify builtin/, project/, or user/", kind, name)
	}
	return paths, nil
}

func validName(s string) bool {
	return regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`).MatchString(s) && s != "." && s != ".."
}

func SkillPath(name string) (string, error) {
	p, e := qualifiedPaths("skills", name)
	if e != nil {
		return "", e
	}
	if strings.HasPrefix(p[0], "builtin/") {
		return "", errors.New("built-in skills are read-only; use skills new NAME --from " + name)
	}
	return p[0], nil
}

func LoadSkill(name string) (Skill, error) {
	var s Skill
	p, e := qualifiedPaths("skills", name)
	if e != nil {
		return s, e
	}
	if strings.HasPrefix(p[0], "builtin/") {
		e = json.Unmarshal([]byte(builtinSkills[strings.TrimPrefix(p[0], "builtin/")]), &s)
		s.Documentation = builtinDocs[s.Name]
	} else {
		e = readConfig(p[0], &s)
		if e == nil {
			b, err := readDocument(filepath.Join(filepath.Dir(p[0]), "SKILL.md"))
			if err == nil {
				s.Documentation = string(b)
			} else if !errors.Is(err, os.ErrNotExist) {
				e = err
			}
		}
	}
	if e != nil {
		return s, e
	}
	return s, ValidateSkill(s)
}

func readDocument(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxConfigBytes {
		return nil, errors.New("skill documentation exceeds configuration byte limit")
	}
	return b, nil
}

func LoadPattern(name string) (Pattern, error) {
	var p Pattern
	paths, e := qualifiedPaths("patterns", name)
	if e != nil {
		return p, e
	}
	if strings.HasPrefix(paths[0], "builtin/") {
		e = json.Unmarshal([]byte(builtinPatterns[strings.TrimPrefix(paths[0], "builtin/")]), &p)
	} else {
		e = readConfig(paths[0], &p)
	}
	if e != nil {
		return p, e
	}
	return p, ValidatePattern(p)
}

func builtins(kind string) map[string]string {
	if kind == "skills" {
		return builtinSkills
	}
	return builtinPatterns
}

func libraryNames(kind string) ([]string, error) {
	var names []string
	for n := range builtins(kind) {
		names = append(names, "builtin/"+n)
	}
	for _, scope := range []string{"project", "user"} {
		root := filepath.Join(".decide", kind)
		if scope == "user" {
			root = filepath.Join(Home(), kind)
		}
		entries, e := os.ReadDir(root)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return nil, e
		}
		for _, en := range entries {
			if en.IsDir() {
				names = append(names, scope+"/"+en.Name())
			} else if kind == "patterns" && strings.HasSuffix(en.Name(), ".json") {
				names = append(names, scope+"/"+strings.TrimSuffix(en.Name(), ".json"))
			}
		}
	}
	slices.Sort(names)
	return names, nil
}

func ListSkills() ([]Skill, error) {
	names, e := libraryNames("skills")
	if e != nil {
		return nil, e
	}
	out := []Skill{}
	for _, n := range names {
		s, e := LoadSkill(n)
		if e != nil {
			return nil, e
		}
		s.Name = n
		out = append(out, s)
	}
	return out, nil
}

func ListPatterns() ([]Pattern, error) {
	names, e := libraryNames("patterns")
	if e != nil {
		return nil, e
	}
	out := []Pattern{}
	for _, n := range names {
		p, e := LoadPattern(n)
		if e != nil {
			return nil, e
		}
		p.Name = n
		out = append(out, p)
	}
	return out, nil
}

func ValidateSkill(s Skill) error {
	if s.Version != 1 {
		return fmt.Errorf("skill %q: unsupported version %d", s.Name, s.Version)
	}
	if !validName(s.Name) || strings.TrimSpace(s.Description) == "" {
		return errors.New("skill requires a simple name and description")
	}
	if s.State != "" && s.State != "value" && s.State != "file" {
		return errors.New("skill state must be value or file")
	}
	if len(s.Inputs) == 0 {
		return errors.New("skill inputs cannot be empty")
	}
	for _, in := range s.Inputs {
		if !slices.Contains([]string{"json", "jsonl", "text", "lines", "image"}, in) {
			return fmt.Errorf("unsupported skill input %q", in)
		}
	}
	if len(s.Questions) == 0 {
		return errors.New("skill questions cannot be empty")
	}
	for name, p := range s.Parameters {
		if !validName(name) {
			return fmt.Errorf("invalid parameter name %q", name)
		}
		if !slices.Contains([]string{"string", "number", "integer", "boolean", "json"}, p.Type) {
			return fmt.Errorf("parameter %q has unsupported type %q", name, p.Type)
		}
		if len(p.Default) > 0 {
			if e := validateParameter(name, p, p.Default); e != nil {
				return e
			}
		}
	}
	// Placeholder-bearing questions are structurally validated with default or
	// neutral values; required parameters are enforced when resolving a run.
	values := map[string]string{}
	for name, p := range s.Parameters {
		if len(p.Default) == 0 {
			switch p.Type {
			case "string":
				values[name] = "example"
			case "number", "integer":
				values[name] = "1"
			case "boolean":
				values[name] = "false"
			default:
				values[name] = "{}"
			}
		}
	}
	resolved, e := ResolveParameters(s, values)
	if e != nil {
		return e
	}
	if _, e = Questions(resolved); e != nil {
		return e
	}
	if len(s.Policy) > 0 {
		if _, e := gate.DecodeRule(s.Policy); e != nil {
			return fmt.Errorf("skill policy: %w", e)
		}
	}
	for _, ex := range s.Examples {
		if !json.Valid(ex) {
			return errors.New("skill example is invalid JSON")
		}
	}
	return nil
}

func Questions(s Skill) (map[string]decide.Question, error) {
	qs := make(map[string]decide.Question, len(s.Questions))
	for name, raw := range s.Questions {
		var fields map[string]json.RawMessage
		if e := jsonv2.Unmarshal(raw, &fields); e != nil {
			return nil, e
		}
		if _, ok := fields["instructions"]; !ok {
			return nil, fmt.Errorf("question %q requires explicit instructions", name)
		}
		q, e := decide.DecodeQuestion(raw)
		if e != nil {
			return nil, e
		}
		switch q.(type) {
		case *decide.NoulQuestion, *decide.ChoiceQuestion, *decide.ScoreQuestion:
		default:
			return nil, fmt.Errorf("unsupported question type %q", q.QuestionType())
		}
		qs[name] = q
	}
	req := decide.NewRequest("preflight")
	req.Questions = qs
	if e := req.Validate(); e != nil {
		return nil, e
	}
	return qs, nil
}

var placeholder = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9._-]+)\s*\}\}`)

func ResolveParameters(s Skill, supplied map[string]string) (Skill, error) {
	values := map[string]any{}
	for n := range supplied {
		if _, ok := s.Parameters[n]; !ok {
			return s, fmt.Errorf("skill %q has no parameter %q", s.Name, n)
		}
	}
	for n, p := range s.Parameters {
		raw := p.Default
		if v, ok := supplied[n]; ok {
			if p.Type == "string" {
				raw, _ = json.Marshal(v)
			} else {
				raw = json.RawMessage(v)
			}
		}
		if len(raw) == 0 {
			if p.Required {
				return s, fmt.Errorf("parameter %q is required", n)
			}
			return s, fmt.Errorf("parameter %q needs a value or default", n)
		}
		if e := validateParameter(n, p, raw); e != nil {
			return s, e
		}
		var v any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if e := d.Decode(&v); e != nil {
			return s, e
		}
		values[n] = v
	}
	copy := s
	copy.Questions = make(map[string]json.RawMessage, len(s.Questions))
	for n, raw := range s.Questions {
		var v any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if e := d.Decode(&v); e != nil {
			return s, e
		}
		value, e := substitute(v, values)
		if e != nil {
			return s, e
		}
		b, e := json.Marshal(value)
		if e != nil {
			return s, e
		}
		copy.Questions[n] = b
	}
	return copy, nil
}

func validateParameter(n string, p Parameter, raw json.RawMessage) error {
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if !json.Valid(raw) || d.Decode(&v) != nil {
		return fmt.Errorf("parameter %q is invalid JSON", n)
	}
	ok := false
	switch p.Type {
	case "string":
		_, ok = v.(string)
	case "boolean":
		_, ok = v.(bool)
	case "json":
		ok = true
	case "number", "integer":
		if num, yes := v.(json.Number); yes {
			f, e := num.Float64()
			ok = e == nil && !math.IsInf(f, 0) && !math.IsNaN(f) && (p.Type != "integer" || math.Trunc(f) == f)
		}
	}
	if !ok {
		return fmt.Errorf("parameter %q must be %s", n, p.Type)
	}
	return nil
}

func substitute(v any, values map[string]any) (any, error) {
	switch value := v.(type) {
	case string:
		matches := placeholder.FindAllStringSubmatchIndex(value, -1)
		if len(matches) == 0 {
			return value, nil
		}
		if len(matches) == 1 && matches[0][0] == 0 && matches[0][1] == len(value) {
			n := value[matches[0][2]:matches[0][3]]
			x, ok := values[n]
			if !ok {
				return nil, fmt.Errorf("undeclared placeholder %q", n)
			}
			return x, nil
		}
		var out strings.Builder
		at := 0
		for _, m := range matches {
			out.WriteString(value[at:m[0]])
			n := value[m[2]:m[3]]
			x, ok := values[n]
			if !ok {
				return nil, fmt.Errorf("undeclared placeholder %q", n)
			}
			if s, ok := x.(string); ok {
				out.WriteString(s)
			} else {
				b, _ := json.Marshal(x)
				out.Write(b)
			}
			at = m[1]
		}
		out.WriteString(value[at:])
		return out.String(), nil
	case []any:
		out := make([]any, len(value))
		for i, x := range value {
			y, e := substitute(x, values)
			if e != nil {
				return nil, e
			}
			out[i] = y
		}
		return out, nil
	case map[string]any:
		out := map[string]any{}
		for k, x := range value {
			y, e := substitute(x, values)
			if e != nil {
				return nil, e
			}
			out[k] = y
		}
		return out, nil
	}
	return v, nil
}

func ValidatePattern(p Pattern) error {
	if p.Version != 1 || !validName(p.Name) || p.Description == "" {
		return errors.New("pattern requires version 1, a simple name, and description")
	}
	switch p.Type {
	case "map":
		if p.Skill == "" {
			return errors.New("map requires skill")
		}
	case "heads":
		q, e := decide.DecodeQuestion(p.Selector)
		if e != nil {
			return e
		}
		c, ok := q.(*decide.ChoiceQuestion)
		if !ok {
			return errors.New("heads selector must be Choice")
		}
		req := decide.NewRequest("preflight")
		req.Questions["selector"] = c
		if e := req.Validate(); e != nil {
			return e
		}
		if len(c.Criteria) != len(p.Branches) {
			return errors.New("heads branches must cover every selector option")
		}
		for _, o := range c.Criteria {
			if p.Branches[o.Key] == "" {
				return fmt.Errorf("heads missing branch %q", o.Key)
			}
		}
	case "funnel":
		if len(p.Stages) == 0 {
			return errors.New("funnel needs stages")
		}
		names := map[string]bool{}
		for _, s := range p.Stages {
			if s.Name == "" || s.Skill == "" || names[s.Name] {
				return errors.New("funnel stages need unique names and skills")
			}
			names[s.Name] = true
			if s.Limit < 0 {
				return errors.New("stage limit cannot be negative")
			}
			if s.Threshold != nil && (s.Answer == "" || math.IsNaN(*s.Threshold) || *s.Threshold < 0 || *s.Threshold > 1) {
				return errors.New("stage threshold requires answer and value in [0,1]")
			}
			if len(s.Policy) > 0 {
				if _, e := gate.DecodeRule(s.Policy); e != nil {
					return e
				}
			}
		}
	case "gate":
		if _, e := gate.DecodeRule(p.Policy); e != nil {
			return e
		}
	case "rank", "rank-pack":
		if p.Answer == "" {
			return errors.New("ranking requires answer")
		}
		if p.Type == "rank-pack" && p.BudgetBytes < 1 {
			return errors.New("rank-pack requires positive budget_bytes")
		}
	default:
		return fmt.Errorf("unsupported pattern type %q", p.Type)
	}
	if p.MaxRecords < 0 {
		return errors.New("max_records cannot be negative")
	}
	return nil
}

func CreateSkill(name, from string) (string, error) {
	scope, base, qualified := strings.Cut(name, "/")
	if !qualified {
		scope = "project"
		base = name
	}
	if scope != "project" && scope != "user" {
		return "", errors.New("new skills use project/ or user/ scope")
	}
	if !validName(base) {
		return "", errors.New("skill name must use letters, numbers, dots, hyphens, or underscores")
	}
	if from == "" {
		from = "builtin/code-risk"
	}
	s, e := LoadSkill(from)
	if e != nil {
		return "", e
	}
	s.Name = base
	root := filepath.Join(".decide", "skills")
	if scope == "user" {
		root = filepath.Join(Home(), "skills")
	}
	dir := filepath.Join(root, base)
	if e := os.MkdirAll(root, 0700); e != nil {
		return "", e
	}
	if e := os.Mkdir(dir, 0700); e != nil {
		return "", e
	}
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return "", e
	}
	path := filepath.Join(dir, "skill.json")
	if e := os.WriteFile(path, append(b, '\n'), 0600); e != nil {
		return "", e
	}
	if e := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(s.Documentation), 0600); e != nil {
		return "", e
	}
	return path, nil
}

// ParameterValues parses repeatable NAME=VALUE arguments without shell evaluation.
func ParameterValues(args []string) (map[string]string, error) {
	out := map[string]string{}
	for _, arg := range args {
		n, v, ok := strings.Cut(arg, "=")
		if !ok || n == "" {
			return nil, fmt.Errorf("parameter %q must be NAME=VALUE", arg)
		}
		if _, ok := out[n]; ok {
			return nil, fmt.Errorf("parameter %q repeated", n)
		}
		out[n] = v
	}
	return out, nil
}

// FormatParameter gives editors a human-readable default without losing JSON types.
func FormatParameter(p Parameter) string {
	if p.Type == "string" {
		var s string
		if json.Unmarshal(p.Default, &s) == nil {
			return s
		}
	}
	return string(p.Default)
}

// ParseInteger is used by interactive controls that must reject fractional values.
func ParseInteger(s string) (int, error) { return strconv.Atoi(s) }
