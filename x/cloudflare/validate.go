package cloudflare

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/deepnoodle-ai/sod"
)

func validate(req *sod.Request) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if req.Model != "clef" && req.Model != "clef-flash" {
		return invalid("model must be clef or clef-flash")
	}
	state, _ := json.Marshal(req.State) // req.Validate already checked encoding
	var stateString string
	if json.Unmarshal(state, &stateString) != nil && !object(state) && !strings.HasPrefix(strings.TrimSpace(string(state)), "[") {
		return invalid("state must be a string, object, or array")
	}
	if len(req.Questions) > 64 {
		return invalid("at most 64 questions are supported")
	}
	for key, q := range req.Questions {
		if len(key) > 100 || !identifier(key, true) {
			return invalid("question IDs must use ASCII letters, digits, '_', '.', '-' and contain at most 100 characters")
		}
		// Check the wire form, so external implementations of a supported
		// question type remain usable. Root validation already checks encoding.
		raw, err := marshalQuestion(q)
		if err != nil {
			return fmt.Errorf("%w: cloudflare question: %w", sod.ErrInvalidRequest, err)
		}
		var wire struct {
			Type         string          `json:"type"`
			Instructions json.RawMessage `json:"instructions"`
			Criteria     json.RawMessage `json:"criteria"`
		}
		if json.Unmarshal(raw, &wire) != nil || wire.Type != q.QuestionType() {
			return invalid("question has an invalid wire type")
		}
		switch wire.Type {
		case "noul", "choice", "score":
		default:
			return fmt.Errorf("%w: cloudflare supports noul, choice, and score questions", errors.ErrUnsupported)
		}
		if !content(wire.Instructions) {
			return invalid("question instructions must be a non-empty string, object, or array")
		}
		switch wire.Type {
		case "choice":
			var options map[string]json.RawMessage
			if json.Unmarshal(wire.Criteria, &options) != nil || len(options) < 2 || len(options) > 255 {
				return invalid("choice requires 2 to 255 options")
			}
			if _, empty := options[""]; empty {
				return invalid("choice option IDs must not be empty")
			}
		case "score":
			var levels []json.RawMessage
			if json.Unmarshal(wire.Criteria, &levels) != nil || len(levels) < 2 || len(levels) > 10 {
				return invalid("score requires 2 to 10 levels")
			}
		}
	}
	if _, videos := req.Extra["videos"]; videos {
		return fmt.Errorf("%w: cloudflare hosted video input", errors.ErrUnsupported)
	}
	if images, present := req.Extra["images"]; present {
		if err := validateImages(images); err != nil {
			return err
		}
	}
	return nil
}

func marshalQuestion(q sod.Question) (raw []byte, err error) {
	defer func() {
		if recover() != nil {
			err = invalid("question JSON encoding panicked")
		}
	}()
	return q.MarshalJSON()
}

func identifier(s string, dots bool) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-' || (dots && c == '.')) {
			return false
		}
	}
	return true
}

func content(raw json.RawMessage) bool {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s) != ""
	}
	trim := strings.TrimSpace(string(raw))
	return strings.HasPrefix(trim, "{") || strings.HasPrefix(trim, "[")
}

func invalid(message string) error {
	return fmt.Errorf("%w: cloudflare: %s", sod.ErrInvalidRequest, message)
}
