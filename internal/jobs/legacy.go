package jobs

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
)

// ReadEvidence accepts a durable run or existing typesafe_cli:1 JSONL envelopes.
func ReadEvidence(id, dir string, fn func(Result) error) error { return readEvidence(id, dir, fn) }
func readEvidence(id, dir string, fn func(Result) error) error {
	if st, e := os.Stat(pathFor(id, dir)); e == nil && st.IsDir() {
		return ReadResults(id, dir, fn)
	}
	f, e := os.Open(id)
	if e != nil {
		return e
	}
	defer f.Close()
	return DecodeEvidence(f, fn)
}

// DecodeEvidence reads a stream of result JSON values or legacy envelopes using
// the same validation as inspection and collection patterns. It retains one
// record at a time. A record can exceed the input item limit because it contains
// original data and prepared state for every stage; consumers bound collections.
func DecodeEvidence(reader io.Reader, fn func(Result) error) error {
	var e error
	dec := json.NewDecoder(reader)
	i := 0
	for {
		var raw json.RawMessage
		if e = dec.Decode(&raw); errors.Is(e, io.EOF) {
			return nil
		} else if e != nil {
			return fmt.Errorf("record %d: %w", i+1, e)
		}
		var probe struct {
			Version int `json:"typesafe_cli"`
		}
		if e = jsonv2.Unmarshal(raw, &probe); e != nil {
			return fmt.Errorf("record %d: %w", i+1, e)
		}
		var r Result
		if probe.Version != 0 {
			if probe.Version != 1 {
				return errors.New("unsupported saved envelope version")
			}
			var old struct {
				ID   string          `json:"id"`
				Data json.RawMessage `json:"data"`
				Runs []struct {
					Command        string                     `json:"command"`
					Invalid        map[string]json.RawMessage `json:"invalid"`
					Name           string                     `json:"name"`
					State          json.RawMessage            `json:"state"`
					Questions      map[string]json.RawMessage `json:"questions"`
					Response       json.RawMessage            `json:"response"`
					RequestedModel string                     `json:"requested_model"`
					RequestID      string                     `json:"request_id"`
					Error          json.RawMessage            `json:"error"`
					Result         json.RawMessage            `json:"result"`
				} `json:"runs"`
			}
			if e = jsonv2.Unmarshal(raw, &old); e != nil {
				return e
			}
			if old.ID == "" || len(old.Runs) == 0 || len(old.Data) == 0 {
				return errors.New("saved envelope missing id, data or runs")
			}
			r = Result{Version: 1, ID: old.ID, Data: old.Data, Index: i, Status: "complete"}
			names := map[string]bool{}
			for _, run := range old.Runs {
				if run.Name == "" || run.Command == "" || names[run.Name] {
					return errors.New("empty or duplicate saved run name, or missing command")
				}
				names[run.Name] = true
				ev := Evidence{Invalid: run.Invalid, Name: run.Name, Model: run.RequestedModel, RequestID: run.RequestID, State: run.State, Questions: run.Questions, Response: run.Response, Result: run.Result}
				if len(run.Error) > 0 && string(run.Error) != "null" {
					ev.Error = "saved run contains an error"
				}
				r.Stages = append(r.Stages, ev)
			}
			if r.Stages[len(r.Stages)-1].Error != "" {
				r.Status = "failed"
				r.Error = r.Stages[len(r.Stages)-1].Error
			}
		} else {
			if e = jsonv2.Unmarshal(raw, &r); e != nil {
				return e
			}
			if r.Version != 1 {
				return errors.New("unsupported run result version")
			}
			if r.ID == "" || r.Status == "" {
				return errors.New("file is neither saved envelopes nor job results")
			}
		}
		r, e = cleanValue(r)
		if e != nil {
			return e
		}
		if fn != nil {
			if e = fn(r); e != nil {
				return e
			}
		}
		i++
	}
}
