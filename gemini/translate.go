package gemini

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/deepnoodle-ai/decide"
)

const (
	maxQuestions = 1000
	maxOptions   = 255
)

var questionKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type decisionRequest struct {
	Model     string                      `json:"model"`
	Input     any                         `json:"input"` // a string, or text and media blocks
	Questions map[string]decisionQuestion `json:"questions"`
}

type decisionQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

type noulCriteria struct {
	True  string `json:"true_description,omitempty"`
	False string `json:"false_description,omitempty"`
}

// wireQuestion is a question in decide's native form. Translating the wire
// form keeps other packages' implementations of the built-in types usable.
type wireQuestion struct {
	Type         string          `json:"type"`
	Instructions json.RawMessage `json:"instructions"`
	Criteria     json.RawMessage `json:"criteria"`
}

// encodeRequest translates req to a Decisions API body. Extra members are
// added unless they would replace one of the body's own fields.
func encodeRequest(req *decide.Request) ([]byte, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	media, err := mediaBlocks(req.Extra)
	if err != nil {
		return nil, err
	}
	if len(req.Questions) > maxQuestions {
		return nil, invalid(fmt.Sprintf("at most %d questions are supported", maxQuestions))
	}
	state, _ := json.Marshal(req.State) // req.Validate already checked encoding
	body := decisionRequest{Model: req.Model, Input: text(state), Questions: make(map[string]decisionQuestion, len(req.Questions))}
	if len(media) > 0 {
		blocks := []any{}
		if s := text(state); s != "" {
			blocks = append(blocks, map[string]string{"type": "text", "text": s + "\n"})
		}
		for _, m := range media {
			blocks = append(blocks, m)
		}
		body.Input = blocks
	}
	for key, question := range req.Questions {
		if !questionKey.MatchString(key) {
			return nil, invalid(fmt.Sprintf("question key %q must be 1 to 64 letters, digits, _ or -", key))
		}
		wire, err := questionWire(question)
		if err != nil {
			return nil, err
		}
		q := decisionQuestion{Type: wire.Type, Instructions: text(wire.Instructions)}
		switch wire.Type {
		case "noul":
			var c struct{ True, False json.RawMessage }
			_ = json.Unmarshal(wire.Criteria, &c) // absent or null: no criteria
			if nc := (noulCriteria{True: text(c.True), False: text(c.False)}); nc != (noulCriteria{}) {
				q.Criteria = nc
			}
		case "choice":
			var options map[string]json.RawMessage
			if json.Unmarshal(wire.Criteria, &options) != nil || len(options) < 2 || len(options) > maxOptions {
				return nil, invalid("choice requires 2 to 255 options")
			}
			criteria := make(map[string]string, len(options))
			for k, v := range options {
				criteria[k] = text(v)
			}
			q.Criteria = criteria
		case "score":
			var levels []json.RawMessage
			if json.Unmarshal(wire.Criteria, &levels) != nil || len(levels) < 2 || len(levels) > maxOptions {
				return nil, invalid("score requires 2 to 255 levels")
			}
			criteria := make([]string, len(levels))
			for i, l := range levels {
				if criteria[i] = text(l); strings.TrimSpace(criteria[i]) == "" {
					return nil, invalid("score levels must not be empty")
				}
			}
			q.Criteria = criteria
		default:
			return nil, fmt.Errorf("%w: gemini supports noul, choice, and score questions", errors.ErrUnsupported)
		}
		if strings.TrimSpace(q.Instructions) == "" {
			return nil, invalid("question instructions must not be empty")
		}
		body.Questions[key] = q
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%w: gemini request: %w", decide.ErrInvalidRequest, err)
	}
	if len(req.Extra) == 0 {
		return raw, nil
	}
	var merged map[string]json.RawMessage
	_ = json.Unmarshal(raw, &merged)
	for k, v := range req.Extra {
		if _, taken := merged[k]; taken || k == "media" || k == "images" {
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("%w: gemini request field %q: %w", decide.ErrInvalidRequest, k, err)
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
		return wire, fmt.Errorf("%w: gemini question: %w", decide.ErrInvalidRequest, err)
	}
	if json.Unmarshal(raw, &wire) != nil || wire.Type != q.QuestionType() {
		return wire, invalid("question has an invalid wire type")
	}
	return wire, nil
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

// decodeResponse translates a Decisions API body to decide's native form.
// Answers are already keyed by question and typed as decide's are; the
// model is filled from the request when absent, usage is renamed, and a
// score's legend keeps the levels as asked rather than their text.
func decodeResponse(raw []byte, req *decide.Request) ([]byte, error) {
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil || body == nil {
		return nil, fmt.Errorf("%w: gemini response must be an object", decide.ErrDecode)
	}
	var answers map[string]map[string]json.RawMessage
	if raw := bytes.TrimSpace(body["answers"]); len(raw) == 0 || raw[0] != '{' || json.Unmarshal(raw, &answers) != nil {
		return nil, fmt.Errorf("%w: gemini response answers must be an object of objects", decide.ErrDecode)
	}
	for key, a := range answers {
		if levels := scoreLevels(req.Questions[key]); levels != nil && a != nil {
			var typ string
			_ = json.Unmarshal(a["type"], &typ)
			if typ == "score" {
				legend := make(map[string]json.RawMessage, len(levels))
				for i, l := range levels {
					legend[strconv.Itoa(i)] = l
				}
				a["legend"], _ = json.Marshal(legend)
			}
		}
	}
	body["answers"], _ = json.Marshal(answers)
	if _, ok := body["model"]; !ok {
		body["model"], _ = json.Marshal(req.Model)
	}
	var usage map[string]json.RawMessage
	if json.Unmarshal(body["usage"], &usage) == nil && usage != nil {
		if v, ok := usage["total_input_tokens"]; ok {
			usage["input_tokens"] = v
		}
		if v, ok := usage["total_output_tokens"]; ok {
			usage["output_tokens"] = v
		}
		body["usage"], _ = json.Marshal(usage)
	}
	return json.Marshal(body)
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
	return fmt.Errorf("%w: gemini: %s", decide.ErrInvalidRequest, message)
}
