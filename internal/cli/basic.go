package cli

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"math"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/patterns/gate"
)

func (a *App) runBasic(ctx context.Context, command string, args []string) int {
	fs := a.flags(command)
	o := BindCommon(fs, command, true, false)
	var questionsFile, taxonomyFile, rubricFile, policyFile, question string
	var threshold float64
	var keepAll bool
	switch command {
	case "judge", "check":
		fs.StringVar(&questionsFile, "questions", "", "native named question JSON file")
	case "grep":
		fs.StringVar(&question, "question", "", "predicate instructions")
		fs.Float64Var(&threshold, "threshold", math.NaN(), "required probability threshold [0,1]")
		fs.BoolVar(&keepAll, "keep-all", false, "emit all records with match decisions")
	case "label":
		fs.StringVar(&taxonomyFile, "taxonomy", "", "Choice taxonomy JSON file")
	case "score":
		fs.StringVar(&rubricFile, "rubric", "", "named Score rubric JSON file")
	}
	if command == "check" {
		fs.StringVar(&policyFile, "policy", "", "patterns/gate JSON policy file")
	}
	if status, ok := a.parse(fs, args, o); !ok {
		return status
	}
	var questions map[string]decide.Question
	var err error
	switch command {
	case "judge", "check":
		questions, err = readQuestions(questionsFile, "")
	case "grep":
		if question == "" || math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0 || threshold > 1 {
			return a.fail(errors.New("grep requires question and a finite threshold in [0,1]"))
		}
		questions = map[string]decide.Question{"match": decide.Noul(question)}
	case "label":
		var raw json.RawMessage
		raw, err = ReadJSONFile(taxonomyFile, ConfigMaxBytes)
		if err == nil {
			var q decide.Question
			q, err = questionDefinition(raw, "choice")
			if err == nil {
				questions = map[string]decide.Question{"label": q}
			}
		}
	case "score":
		questions, err = readQuestions(rubricFile, "score")
	}
	if err != nil {
		return a.fail(err)
	}
	req := decide.NewRequest("configuration validation")
	req.Questions = questions
	if err := req.Validate(); err != nil {
		return a.fail(err)
	}
	var policy gate.Rule
	if command == "check" {
		raw, err := ReadJSONFile(policyFile, ConfigMaxBytes)
		if err != nil {
			return a.fail(err)
		}
		policy, err = gate.DecodeRule(raw)
		if err != nil {
			return a.fail(err)
		}
	}
	matches := 0
	rejected := false
	build := func(_ context.Context, e Envelope) (*decide.Request, error) {
		state := e.Data
		if o.StateField != "" {
			var fields map[string]json.RawMessage
			if err := jsonv2.Unmarshal(e.Data, &fields); err != nil {
				return nil, fmt.Errorf("record %q state-field requires object data", e.ID)
			}
			var ok bool
			state, ok = fields[o.StateField]
			if !ok {
				return nil, fmt.Errorf("record %q has no state field %q", e.ID, o.StateField)
			}
		}
		req := decide.NewRequest(state)
		req.Questions = questions
		return req, nil
	}
	finish := func(_ *Envelope, r *Run, resp *decide.Response, callErr error) error {
		if command == "check" {
			decision, err := evaluatePolicy(*r, policy)
			r.Result, _ = json.Marshal(decision)
			if decision.Outcome != gate.Allow {
				rejected = true
			}
			return err
		}
		if callErr != nil {
			return nil
		}
		switch command {
		case "grep":
			answer, err := decide.AnswerAs[*decide.NoulAnswer](resp, "match")
			if err != nil {
				return err
			}
			matched := answer.Noul >= threshold
			if matched {
				matches++
			}
			r.Result, _ = json.Marshal(struct {
				Matched bool `json:"matched"`
			}{matched})
		case "label":
			answer, err := decide.AnswerAs[*decide.ChoiceAnswer](resp, "label")
			if err != nil {
				return err
			}
			r.Result, _ = json.Marshal(struct {
				Label string `json:"label"`
			}{answer.Choice})
		}
		return nil
	}
	var emit func(Envelope, Run) bool
	if command == "grep" && !keepAll {
		emit = func(_ Envelope, r Run) bool {
			if r.Error != nil || len(r.Invalid) > 0 {
				return true
			}
			var result struct {
				Matched bool `json:"matched"`
			}
			_ = json.Unmarshal(r.Result, &result)
			return result.Matched
		}
	}
	status := a.RunLive(ctx, command, *o, build, finish, emit)
	if status == 0 && ((command == "grep" && matches == 0) || (command == "check" && rejected)) {
		return 1
	}
	return status
}

func readQuestions(path, kind string) (map[string]decide.Question, error) {
	raw, err := ReadJSONFile(path, ConfigMaxBytes)
	if err != nil {
		return nil, err
	}
	var definitions map[string]json.RawMessage
	if err = jsonv2.Unmarshal(raw, &definitions); err != nil {
		return nil, err
	}
	if len(definitions) == 0 {
		return nil, errors.New("question map cannot be empty")
	}
	questions := map[string]decide.Question{}
	for key, raw := range definitions {
		q, err := questionDefinition(raw, kind)
		if err != nil {
			return nil, fmt.Errorf("question %q: %w", key, err)
		}
		questions[key] = q
	}
	return questions, nil
}
func questionDefinition(raw json.RawMessage, kind string) (decide.Question, error) {
	var declared map[string]json.RawMessage
	if err := jsonv2.Unmarshal(raw, &declared); err != nil {
		return nil, err
	}
	if declared == nil {
		return nil, errors.New("question definition must be an object")
	}
	if _, ok := declared["instructions"]; !ok {
		return nil, errors.New("question definition requires explicit instructions (null is allowed)")
	}

	if kind != "" {
		var fields map[string]json.RawMessage
		if err := jsonv2.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
		if fields == nil {
			return nil, errors.New("question definition must be an object")
		}
		if saved, exists := fields["type"]; exists {
			var actual string
			if json.Unmarshal(saved, &actual) != nil || actual != kind {
				return nil, fmt.Errorf("question must have type %q", kind)
			}
		}
		fields["type"], _ = json.Marshal(kind)
		raw, _ = json.Marshal(fields)
	}
	q, err := decide.DecodeQuestion(raw)
	if err != nil {
		return nil, err
	}
	switch q := q.(type) {
	case *decide.NoulQuestion:
		return q, nil
	case *decide.ChoiceQuestion:
		if len(q.Criteria) > 255 {
			return nil, errors.New("Choice taxonomy exceeds 255 options")
		}
		return q, nil
	case *decide.ScoreQuestion:
		if len(q.Criteria) > 10 {
			return nil, errors.New("Score rubric exceeds 10 levels")
		}
		return q, nil
	default:
		return nil, errors.New("only Noul, Choice and Score questions are supported")
	}
}
