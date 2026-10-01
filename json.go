package sod

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"maps"
	"slices"
)

// objectWriter writes a JSON object with members in the order they are added.
// encoding/json sorts map keys, which loses the order that choice criteria
// and "type"-first answers need.
type objectWriter struct {
	buf bytes.Buffer
	n   int
	err error
}

func newObjectWriter() *objectWriter {
	w := &objectWriter{}
	w.buf.WriteByte('{')
	return w
}

// field writes one member. v is encoded with encoding/json.
func (w *objectWriter) field(key string, v any) {
	if w.err != nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		w.err = fmt.Errorf("sod: encode %q: %w", key, err)
		return
	}
	w.rawField(key, b)
}

// rawField writes one member whose value is already encoded.
func (w *objectWriter) rawField(key string, raw []byte) {
	if w.err != nil {
		return
	}
	if w.n > 0 {
		w.buf.WriteByte(',')
	}
	k, _ := json.Marshal(key) // strings always encode
	w.buf.Write(k)
	w.buf.WriteByte(':')
	w.buf.Write(raw)
	w.n++
}

// extra writes the members of extra sorted by key. A key in reserved is an
// error, because it would produce a duplicate member.
func (w *objectWriter) extra(extra map[string]any, reserved ...string) {
	if w.err != nil || len(extra) == 0 {
		return
	}
	keys := slices.Sorted(maps.Keys(extra))
	for _, k := range keys {
		if slices.Contains(reserved, k) {
			w.err = fmt.Errorf("sod: Extra key %q collides with a modeled field", k)
			return
		}
	}
	for _, k := range keys {
		w.field(k, extra[k])
	}
}

// rawExtra writes already-encoded members sorted by key, skipping any key in
// reserved (a modeled field always wins over a stale Extra entry).
func (w *objectWriter) rawExtra(extra map[string]json.RawMessage, reserved ...string) {
	for _, k := range slices.Sorted(maps.Keys(extra)) {
		if slices.Contains(reserved, k) {
			continue
		}
		v := extra[k]
		if len(v) == 0 {
			v = json.RawMessage("null")
		}
		w.rawField(k, v)
	}
}

func (w *objectWriter) bytes() ([]byte, error) {
	if w.err != nil {
		return nil, w.err
	}
	w.buf.WriteByte('}')
	return w.buf.Bytes(), nil
}

// decodeObject decodes data as a JSON object into raw members. JSON null is
// an error.
func decodeObject(data []byte) (map[string]json.RawMessage, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return nil, errors.New("sod: expected a JSON object")
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// orderedMember is one member of a JSON object, in wire order.
type orderedMember struct {
	Key   string
	Value json.RawMessage
}

// decodeOrderedObject walks a JSON object's tokens with a jsontext.Decoder
// so member order survives. Duplicate names and invalid UTF-8 are allowed,
// to stay lenient in what we accept (the v2 defaults reject both).
func decodeOrderedObject(data []byte) ([]orderedMember, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(data),
		jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	tok, err := dec.ReadToken()
	if err != nil {
		return nil, err
	}
	if tok.Kind() != jsontext.KindBeginObject {
		return nil, errors.New("sod: expected a JSON object")
	}
	var out []orderedMember
	for dec.PeekKind() != jsontext.KindEndObject {
		tok, err := dec.ReadToken()
		if err != nil {
			return nil, err
		}
		name := tok.String() // a Token is voided by the next Decoder call
		v, err := dec.ReadValue()
		if err != nil {
			return nil, err
		}
		out = append(out, orderedMember{Key: name, Value: bytes.Clone(v)})
	}
	if _, err := dec.ReadToken(); err != nil { // closing '}'
		return nil, err
	}
	return out, nil
}

// decodeAny decodes a question value (instructions, a criteria value, an
// option description, or a level). Absent or null is nil and a JSON string
// is a Go string; anything else stays json.RawMessage, as Request.State
// does, so object member order, which the model sees, survives a round
// trip.
func decodeAny(raw json.RawMessage) (any, error) {
	if isNull(raw) {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	if !json.Valid(raw) {
		return nil, errors.New("sod: invalid JSON value")
	}
	return json.RawMessage(bytes.Clone(raw)), nil
}

// isNull reports whether raw is absent or the JSON literal null.
func isNull(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || string(t) == "null"
}

// extraFrom collects the members of m not in known, keeping their raw bytes.
func extraFrom(m map[string]json.RawMessage, known ...string) map[string]any {
	var extra map[string]any
	for k, v := range m {
		if slices.Contains(known, k) {
			continue
		}
		if extra == nil {
			extra = make(map[string]any)
		}
		extra[k] = v
	}
	return extra
}
