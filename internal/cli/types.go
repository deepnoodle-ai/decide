// Package cli implements the experimental TypeSafe command-line toolkit.
package cli

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/deepnoodle-ai/decide"
)

type Envelope struct {
	Version int                        `json:"typesafe_cli"`
	ID      string                     `json:"id"`
	Data    json.RawMessage            `json:"data"`
	Runs    []Run                      `json:"runs"`
	Extra   map[string]json.RawMessage `json:"-"`
}

type Run struct {
	Name           string                     `json:"name"`
	Command        string                     `json:"command"`
	State          json.RawMessage            `json:"state,omitempty"`
	Questions      map[string]json.RawMessage `json:"questions,omitempty"`
	Response       json.RawMessage            `json:"response,omitempty"`
	RequestedModel string                     `json:"requested_model,omitempty"`
	RequestID      string                     `json:"request_id,omitempty"`
	Error          *RecordError               `json:"error,omitempty"`
	Invalid        map[string]RecordError     `json:"invalid,omitempty"`
	Result         json.RawMessage            `json:"result,omitempty"`
	Extra          map[string]json.RawMessage `json:"-"`
}

type RecordError struct {
	Kind      string `json:"kind"`
	Message   string `json:"message"`
	Status    int    `json:"status,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Type      string `json:"type,omitempty"`
}

func (e RecordError) Error() string { return e.Message }

func marshalExtra(v any, extra map[string]json.RawMessage) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err = json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k, v := range extra {
		if _, ok := m[k]; !ok {
			m[k] = v
		}
	}
	return json.Marshal(m)
}
func (e Envelope) MarshalJSON() ([]byte, error) {
	type alias Envelope
	return marshalExtra(alias(e), e.Extra)
}
func (e *Envelope) UnmarshalJSON(b []byte) error {
	type alias Envelope
	var v alias
	if err := jsonv2.Unmarshal(b, &v); err != nil {
		return err
	}
	var m map[string]json.RawMessage
	if err := jsonv2.Unmarshal(b, &m); err != nil {
		return err
	}
	for _, k := range []string{"typesafe_cli", "id", "data", "runs"} {
		if _, ok := m[k]; !ok {
			return fmt.Errorf("envelope missing %s", k)
		}
		delete(m, k)
	}
	if v.Version != 1 || v.ID == "" || len(v.Data) == 0 || v.Runs == nil {
		return errors.New("invalid or unsupported saved envelope")
	}
	names := map[string]bool{}
	for _, r := range v.Runs {
		if r.Name == "" || r.Command == "" || names[r.Name] {
			return errors.New("empty or duplicate saved run name")
		}
		names[r.Name] = true
	}
	*e = Envelope(v)
	e.Extra = m
	return nil
}
func (r Run) MarshalJSON() ([]byte, error) { type alias Run; return marshalExtra(alias(r), r.Extra) }
func (r *Run) UnmarshalJSON(b []byte) error {
	type alias Run
	var v alias
	if err := jsonv2.Unmarshal(b, &v); err != nil {
		return err
	}
	var m map[string]json.RawMessage
	if err := jsonv2.Unmarshal(b, &m); err != nil {
		return err
	}
	for _, k := range []string{"name", "command", "state", "questions", "response", "requested_model", "request_id", "error", "invalid", "result"} {
		delete(m, k)
	}
	*r = Run(v)
	r.Extra = m
	return nil
}
func (e *Envelope) AppendRun(r Run) error {
	if r.Name == "" {
		return errors.New("run name cannot be empty")
	}
	for _, old := range e.Runs {
		if old.Name == r.Name {
			return fmt.Errorf("record %q already has run %q", e.ID, r.Name)
		}
	}
	e.Runs = append(e.Runs, r)
	return nil
}
func (e *Envelope) SelectedRun(name string) (*Run, error) {
	if len(e.Runs) == 0 {
		return nil, fmt.Errorf("record %q has no saved runs", e.ID)
	}
	if name == "" {
		return &e.Runs[len(e.Runs)-1], nil
	}
	for i := range e.Runs {
		if e.Runs[i].Name == name {
			return &e.Runs[i], nil
		}
	}
	return nil, fmt.Errorf("record %q has no run %q", e.ID, name)
}
func (r Run) Request() (*decide.Request, error) {
	req := decide.NewRequest(r.State, decide.WithRequestModel(r.RequestedModel))
	for k, raw := range r.Questions {
		q, err := questionDefinition(raw, "")
		if err != nil {
			return nil, fmt.Errorf("question %q: %w", k, err)
		}
		switch q.(type) {
		case *decide.NoulQuestion, *decide.ChoiceQuestion, *decide.ScoreQuestion:
		default:
			return nil, fmt.Errorf("unsupported saved question %q", k)
		}
		req.Questions[k] = q
	}
	if err := ValidateState(r.State); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return req, nil
}

// ValidatedResponse never substitutes an earlier run or trusts saved validation.
// It returns a partial response alongside errors for answer-specific failures.
func (r Run) ValidatedResponse() (*decide.Response, error) {
	req, err := r.Request()
	if err != nil {
		return nil, err
	}
	if len(r.Response) == 0 {
		return nil, errors.New("saved run has no response")
	}
	var checked json.RawMessage
	if err = jsonv2.Unmarshal(r.Response, &checked); err != nil {
		return nil, err
	}
	resp, err := decide.DecodeResponse(r.Response, req)
	if err != nil {
		return nil, err
	}
	resp.RequestID = r.RequestID
	resp.Invalid = map[string]*decide.AnswerError{}
	for k, q := range req.Questions {
		a := resp.Answers[k]
		var ae *decide.AnswerError
		switch {
		case a == nil:
			ae = &decide.AnswerError{Key: k, Type: q.QuestionType(), Reason: decide.ReasonMissingAnswer, Detail: "saved response is missing answer"}
		case a.AnswerType() != q.QuestionType():
			ae = &decide.AnswerError{Key: k, Type: q.QuestionType(), Reason: decide.ReasonTypeMismatch, Detail: "saved answer type differs"}
		default:
			if v, ok := q.(decide.AnswerValidator); ok {
				if ve := v.ValidateAnswer(a); ve != nil {
					if found, ok := errors.AsType[*decide.AnswerError](ve); ok {
						copy := *found
						copy.Key = k
						ae = &copy
					} else {
						ae = &decide.AnswerError{Key: k, Type: q.QuestionType(), Reason: decide.ReasonCustom, Err: ve}
					}
				}
			}
		}
		if ae != nil {
			resp.Invalid[k] = ae
		}
	}
	for k, a := range resp.Answers {
		if _, ok := req.Questions[k]; !ok {
			resp.Invalid[k] = &decide.AnswerError{Key: k, Type: a.AnswerType(), Reason: decide.ReasonUnexpectedAnswer, Detail: "no saved question"}
		}
	}
	for k, v := range r.Invalid {
		resp.Invalid[k] = &decide.AnswerError{Key: k, Type: v.Type, Reason: v.Reason, Detail: v.Message}
	}
	if r.Error != nil && (r.Error.Kind != "validation" || len(r.Invalid) == 0) {
		for k, q := range req.Questions {
			resp.Invalid[k] = &decide.AnswerError{Key: k, Type: q.QuestionType(), Reason: decide.ReasonCustom, Detail: r.Error.Message}
		}
		return resp, r.Error
	}
	if len(resp.Invalid) > 0 {
		invalid := &decide.InvalidAnswersError{}
		for _, k := range slices.Sorted(maps.Keys(resp.Invalid)) {
			invalid.Answers = append(invalid.Answers, resp.Invalid[k])
		}
		return resp, invalid
	}
	if r.Error != nil {
		return resp, r.Error
	}
	return resp, nil
}
