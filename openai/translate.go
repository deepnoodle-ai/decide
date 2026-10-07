package openai

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/deepnoodle-ai/decide"
)

const maxQuestions = 200

type decisionRequest struct {
	Model     string             `json:"model"`
	Input     string             `json:"input"`
	Questions []decisionQuestion `json:"questions"`
}

type decisionQuestion struct {
	Name         string           `json:"name"`
	Type         string           `json:"type"`
	Instructions string           `json:"instructions"`
	Choices      []decisionChoice `json:"choices,omitempty"`
	Levels       []decisionLevel  `json:"levels,omitempty"`
}

type decisionChoice struct {
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

type decisionLevel struct {
	Label string `json:"label"`
}

// wireQuestion is a question in decide's native form. Translating the wire
// form keeps other packages' implementations of the built-in types usable.
type wireQuestion struct {
	Type         string          `json:"type"`
	Instructions json.RawMessage `json:"instructions"`
	Criteria     json.RawMessage `json:"criteria"`
}

// encodeRequest translates req to a Decisions API body. Questions are sent
// in key order, and Extra members are added unless they would replace one
// of the body's own fields.
func encodeRequest(req *decide.Request) ([]byte, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	for _, key := range []string{"images", "videos"} {
		if _, ok := req.Extra[key]; ok {
			return nil, fmt.Errorf("%w: openai %s input", errors.ErrUnsupported, key)
		}
	}
	if len(req.Questions) > maxQuestions {
		return nil, invalid(fmt.Sprintf("at most %d questions are supported", maxQuestions))
	}
	state, _ := json.Marshal(req.State) // req.Validate already checked encoding
	body := decisionRequest{Model: req.Model, Input: text(state)}
	for _, key := range slices.Sorted(maps.Keys(req.Questions)) {
		wire, err := questionWire(req.Questions[key])
		if err != nil {
			return nil, err
		}
		q := decisionQuestion{Name: key, Instructions: text(wire.Instructions)}
		switch wire.Type {
		case "noul":
			q.Type = "predicate"
			var criteria struct{ True, False json.RawMessage }
			_ = json.Unmarshal(wire.Criteria, &criteria) // absent or null: no criteria
			if s := text(criteria.True); s != "" {
				q.Instructions += "\n\nYes means: " + s
			}
			if s := text(criteria.False); s != "" {
				q.Instructions += "\n\nNo means: " + s
			}
		case "choice":
			q.Type = "choice"
			options, err := orderedObject(wire.Criteria)
			if err != nil || len(options) < 2 {
				return nil, invalid("choice requires at least 2 options")
			}
			for _, o := range options {
				q.Choices = append(q.Choices, decisionChoice{Value: o.key, Description: text(o.value)})
			}
		case "score":
			q.Type = "score"
			var levels []json.RawMessage
			if json.Unmarshal(wire.Criteria, &levels) != nil || len(levels) < 2 {
				return nil, invalid("score requires at least 2 levels")
			}
			for _, l := range levels {
				q.Levels = append(q.Levels, decisionLevel{Label: text(l)})
			}
		default:
			return nil, fmt.Errorf("%w: openai supports noul, choice, and score questions", errors.ErrUnsupported)
		}
		if strings.TrimSpace(q.Instructions) == "" {
			return nil, invalid("question instructions must not be empty")
		}
		body.Questions = append(body.Questions, q)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%w: openai request: %w", decide.ErrInvalidRequest, err)
	}
	if len(req.Extra) == 0 {
		return raw, nil
	}
	var merged map[string]json.RawMessage
	_ = json.Unmarshal(raw, &merged)
	for k, v := range req.Extra {
		if _, taken := merged[k]; taken {
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("%w: openai request field %q: %w", decide.ErrInvalidRequest, k, err)
		}
		merged[k] = b
	}
	return json.Marshal(merged)
}

func questionWire(q decide.Question) (wire wireQuestion, err error) {
	defer func() {
		if recover() != nil {
			err = invalid("question JSON encoding panicked")
		}
	}()
	raw, err := q.MarshalJSON()
	if err != nil {
		return wire, fmt.Errorf("%w: openai question: %w", decide.ErrInvalidRequest, err)
	}
	if json.Unmarshal(raw, &wire) != nil || wire.Type != q.QuestionType() {
		return wire, invalid("question has an invalid wire type")
	}
	return wire, nil
}

type member struct {
	key   string
	value json.RawMessage
}

// orderedObject returns an object's members in the order they appear, since
// a choice's options are sent in the order they were given.
func orderedObject(raw json.RawMessage) ([]member, error) {
	d := jsontext.NewDecoder(bytes.NewReader(raw))
	tok, err := d.ReadToken()
	if err != nil || tok.Kind() != '{' {
		return nil, errors.New("not an object")
	}
	var out []member
	for d.PeekKind() != '}' {
		tok, err := d.ReadToken()
		if err != nil {
			return nil, err
		}
		name := tok.String() // the token is void after the next read
		value, err := d.ReadValue()
		if err != nil {
			return nil, err
		}
		out = append(out, member{name, json.RawMessage(bytes.Clone(value))})
	}
	return out, nil
}

// text returns a JSON string's value, "" for null or nothing, and the
// compact JSON text of any other value.
func text(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return string(raw)
	}
	return b.String()
}

// decodeResponse translates a Decisions API body to decide's native form:
// answers keyed by question, predicates as noul answers, and probability
// lists as objects. Members it does not translate are kept, so the client's
// validation reports an answer it cannot use, such as a refusal, against
// its own question.
func decodeResponse(raw []byte, req *decide.Request) ([]byte, error) {
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil || body == nil {
		return nil, fmt.Errorf("%w: openai response must be an object", decide.ErrDecode)
	}
	var list []map[string]json.RawMessage
	if json.Unmarshal(body["answers"], &list) != nil {
		return nil, fmt.Errorf("%w: openai response answers must be a list of objects", decide.ErrDecode)
	}
	answers := make(map[string]json.RawMessage, len(list))
	for _, a := range list {
		var name string
		if json.Unmarshal(a["name"], &name) != nil || name == "" {
			return nil, fmt.Errorf("%w: openai answer has no name", decide.ErrDecode)
		}
		if _, dup := answers[name]; dup {
			return nil, fmt.Errorf("%w: openai answered %q twice", decide.ErrDecode, name)
		}
		delete(a, "name")
		answers[name] = nativeAnswer(a, req.Questions[name])
	}
	body["answers"], _ = json.Marshal(answers)
	return json.Marshal(body)
}

func nativeAnswer(a map[string]json.RawMessage, q decide.Question) json.RawMessage {
	var typ string
	_ = json.Unmarshal(a["type"], &typ)
	switch typ {
	case "predicate":
		if p, ok := a["probability"]; ok {
			a["type"], _ = json.Marshal("noul")
			a["noul"] = p
			delete(a, "probability")
		}
	case "choice":
		var probs []struct {
			Value       string          `json:"value"`
			Probability json.RawMessage `json:"probability"`
		}
		if json.Unmarshal(a["probabilities"], &probs) == nil {
			m := make(map[string]json.RawMessage, len(probs))
			for _, p := range probs {
				m[p.Value] = p.Probability
			}
			a["probabilities"], _ = json.Marshal(m)
		}
	case "score":
		var probs []struct {
			Value       int             `json:"value"`
			Label       json.RawMessage `json:"label"`
			Probability json.RawMessage `json:"probability"`
		}
		if json.Unmarshal(a["probabilities"], &probs) == nil {
			levels := scoreLevels(q)
			m := make(map[string]json.RawMessage, len(probs))
			legend := make(map[string]json.RawMessage, len(probs))
			for _, p := range probs {
				k := strconv.Itoa(p.Value)
				m[k] = p.Probability
				legend[k] = p.Label
				if len(levels) == len(probs) && p.Value >= 0 && p.Value < len(levels) {
					legend[k] = levels[p.Value] // the level as asked, not its text
				}
			}
			a["probabilities"], _ = json.Marshal(m)
			if _, ok := a["legend"]; !ok {
				a["legend"], _ = json.Marshal(legend)
			}
		}
	}
	out, _ := json.Marshal(a)
	return out
}

func scoreLevels(q decide.Question) []json.RawMessage {
	if q == nil || q.QuestionType() != "score" {
		return nil
	}
	wire, err := questionWire(q)
	if err != nil {
		return nil
	}
	var levels []json.RawMessage
	_ = json.Unmarshal(wire.Criteria, &levels)
	return levels
}

func invalid(message string) error {
	return fmt.Errorf("%w: openai: %s", decide.ErrInvalidRequest, message)
}
