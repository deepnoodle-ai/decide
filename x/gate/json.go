package gate

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
)

// Rules as data. Each built-in has MarshalJSON (value receiver, "type"
// first) and UnmarshalJSON (pointer receiver). Both are the v1 method
// signatures, which encoding/json/v2 also honors, so either package works.
// UnmarshalJSON decodes with encoding/json/v2, which rejects duplicate
// members by default; unknown members are rejected with
// json.RejectUnknownMembers(true), so a typo such as "alow" cannot read as a
// 0 bound. Bounds and inputs must also be present, for the same reason.
// UnmarshalJSON does not validate; DecodeRule does.

// Aliases drop the methods so the wire structs can embed the fields.
type (
	bandsWire      Bands
	allOfWire      AllOf
	minConfWire    MinConfidence
	marginWire     Margin
	asymmetricWire Asymmetric
	optionBandWire OptionBand
)

var (
	reqBands      = []string{"type", "input", "measure", "polarity", "allow", "review"}
	reqAllOf      = []string{"type", "inputs", "measure", "polarity", "allow", "review"}
	reqMinConf    = []string{"type", "inputs", "measure", "floor"}
	reqMargin     = []string{"type", "input", "top", "lead"}
	reqAsymmetric = []string{"type", "input", "measure", "floor", "options"}
	reqCompose    = []string{"type", "rules"}
)

func marshal(v any) ([]byte, error) { return json.Marshal(v, json.Deterministic(true)) }

// unmarshal decodes data into v strictly and checks that every name in
// required is present. A "type" member, when required, must equal typ.
func unmarshal(typ string, data []byte, v any, required []string) error {
	if err := json.Unmarshal(data, v, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("gate: decode %s: %w", typ, err)
	}
	var members map[string]jsontext.Value
	if err := json.Unmarshal(data, &members); err != nil {
		return fmt.Errorf("gate: decode %s: %w", typ, err)
	}
	for _, name := range required {
		if name == "type" {
			// Direct UnmarshalJSON calls may omit "type"; DecodeRule
			// requires it before dispatching here.
			continue
		}
		if _, ok := members[name]; !ok {
			return fmt.Errorf("gate: decode %s: missing member %q", typ, name)
		}
	}
	if raw, ok := members["type"]; ok {
		var got string
		if err := json.Unmarshal(raw, &got); err != nil || got != typ {
			return fmt.Errorf("gate: decode %s: type is %s", typ, raw)
		}
	}
	return nil
}

// MarshalJSON encodes r with "type": "bands" first.
func (r Bands) MarshalJSON() ([]byte, error) {
	return marshal(struct {
		Type string `json:"type"`
		bandsWire
	}{"bands", bandsWire(r)})
}

// UnmarshalJSON decodes a "bands" rule strictly. It does not validate.
func (r *Bands) UnmarshalJSON(data []byte) error {
	var w struct {
		Type string `json:"type"`
		bandsWire
	}
	if err := unmarshal("bands", data, &w, reqBands); err != nil {
		return err
	}
	*r = Bands(w.bandsWire)
	return nil
}

// MarshalJSON encodes r with "type": "all_of" first.
func (r AllOf) MarshalJSON() ([]byte, error) {
	return marshal(struct {
		Type string `json:"type"`
		allOfWire
	}{"all_of", allOfWire(r)})
}

// UnmarshalJSON decodes an "all_of" rule strictly. It does not validate.
func (r *AllOf) UnmarshalJSON(data []byte) error {
	var w struct {
		Type string `json:"type"`
		allOfWire
	}
	if err := unmarshal("all_of", data, &w, reqAllOf); err != nil {
		return err
	}
	*r = AllOf(w.allOfWire)
	return nil
}

// MarshalJSON encodes r with "type": "min_confidence" first.
func (r MinConfidence) MarshalJSON() ([]byte, error) {
	return marshal(struct {
		Type string `json:"type"`
		minConfWire
	}{"min_confidence", minConfWire(r)})
}

// UnmarshalJSON decodes a "min_confidence" rule strictly. It does not
// validate.
func (r *MinConfidence) UnmarshalJSON(data []byte) error {
	var w struct {
		Type string `json:"type"`
		minConfWire
	}
	if err := unmarshal("min_confidence", data, &w, reqMinConf); err != nil {
		return err
	}
	*r = MinConfidence(w.minConfWire)
	return nil
}

// MarshalJSON encodes r with "type": "margin" first.
func (r Margin) MarshalJSON() ([]byte, error) {
	return marshal(struct {
		Type string `json:"type"`
		marginWire
	}{"margin", marginWire(r)})
}

// UnmarshalJSON decodes a "margin" rule strictly. It does not validate.
func (r *Margin) UnmarshalJSON(data []byte) error {
	var w struct {
		Type string `json:"type"`
		marginWire
	}
	if err := unmarshal("margin", data, &w, reqMargin); err != nil {
		return err
	}
	*r = Margin(w.marginWire)
	return nil
}

// MarshalJSON encodes r with "type": "asymmetric" first; options are
// sorted by key.
func (r Asymmetric) MarshalJSON() ([]byte, error) {
	return marshal(struct {
		Type string `json:"type"`
		asymmetricWire
	}{"asymmetric", asymmetricWire(r)})
}

// UnmarshalJSON decodes an "asymmetric" rule strictly. It does not
// validate.
func (r *Asymmetric) UnmarshalJSON(data []byte) error {
	var w struct {
		Type string `json:"type"`
		asymmetricWire
	}
	if err := unmarshal("asymmetric", data, &w, reqAsymmetric); err != nil {
		return err
	}
	*r = Asymmetric(w.asymmetricWire)
	return nil
}

// UnmarshalJSON decodes an option band strictly. "allow" and "review" are
// required unless "always" is present.
func (b *OptionBand) UnmarshalJSON(data []byte) error {
	var w optionBandWire
	if err := unmarshal("option band", data, &w, nil); err != nil {
		return err
	}
	if w.Always == 0 {
		var members map[string]jsontext.Value
		_ = json.Unmarshal(data, &members) // already decoded once
		for _, name := range []string{"allow", "review"} {
			if _, ok := members[name]; !ok {
				return fmt.Errorf("gate: decode option band: missing member %q", name)
			}
		}
	}
	*b = OptionBand(w)
	return nil
}

type composeWire struct {
	Type  string           `json:"type"`
	Name  string           `json:"name,omitempty"`
	Rules []jsontext.Value `json:"rules"`
}

// MarshalJSON encodes r with "type": "compose" first. It fails for a rule
// that is not a json.Marshaler; custom rules are not decodable in v0.
func (r Compose) MarshalJSON() ([]byte, error) {
	w := composeWire{Type: "compose", Name: r.Name, Rules: make([]jsontext.Value, 0, len(r.Rules))}
	for i, child := range r.Rules {
		m, ok := child.(json.Marshaler)
		if !ok {
			return nil, fmt.Errorf("gate: encode compose: rule %d (%T) is not a json.Marshaler", i, child)
		}
		b, err := m.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("gate: encode compose: rule %d: %w", i, err)
		}
		w.Rules = append(w.Rules, b)
	}
	return marshal(w)
}

// UnmarshalJSON decodes a "compose" rule and its built-in children
// strictly. It does not validate.
func (r *Compose) UnmarshalJSON(data []byte) error {
	var w composeWire
	if err := unmarshal("compose", data, &w, reqCompose); err != nil {
		return err
	}
	rules := make([]Rule, 0, len(w.Rules))
	for i, raw := range w.Rules {
		child, err := decodeRule(raw)
		if err != nil {
			return fmt.Errorf("gate: decode compose: rule %d: %w", i, err)
		}
		rules = append(rules, child)
	}
	*r = Compose{Name: w.Name, Rules: rules}
	return nil
}

// DecodeRule decodes one built-in rule by its "type" member, rejects
// unknown members (a typo such as "alow" would read as a 0 bound),
// duplicate members, and missing bounds, decodes Compose children
// recursively, and validates. It returns value types (gate.Bands, not
// *gate.Bands). An unknown or missing "type" wraps ErrUnknownRule; a rule
// that fails validation wraps ErrInvalidRule.
func DecodeRule(data []byte) (Rule, error) {
	r, err := decodeRule(data)
	if err != nil {
		return nil, err
	}
	if err := r.(Validator).Validate(); err != nil {
		return nil, err
	}
	return r, nil
}

func decodeRule(data []byte) (Rule, error) {
	var head struct {
		Type *string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("gate: decode rule: %w", err)
	}
	if head.Type == nil {
		return nil, fmt.Errorf("%w: missing \"type\"", ErrUnknownRule)
	}
	switch *head.Type {
	case "bands":
		return decodeAs[Bands](data)
	case "all_of":
		return decodeAs[AllOf](data)
	case "min_confidence":
		return decodeAs[MinConfidence](data)
	case "margin":
		return decodeAs[Margin](data)
	case "asymmetric":
		return decodeAs[Asymmetric](data)
	case "compose":
		return decodeAs[Compose](data)
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownRule, *head.Type)
}

func decodeAs[R any, P interface {
	*R
	json.Unmarshaler
	Rule
}](data []byte) (Rule, error) {
	var r R
	if err := P(&r).UnmarshalJSON(data); err != nil {
		return nil, err
	}
	rule, ok := any(r).(Rule)
	if !ok {
		return nil, errors.New("gate: internal: not a rule")
	}
	return rule, nil
}
