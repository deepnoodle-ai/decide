package jobs

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
)

// ReadEvidence reads a durable run or exported result JSONL.
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

// DecodeEvidence reads a stream of result JSON values using
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
		var r Result
		if e = jsonv2.Unmarshal(raw, &r); e != nil {
			return fmt.Errorf("record %d: %w", i+1, e)
		}
		if r.Version != 1 {
			return errors.New("unsupported run result version")
		}
		if r.ID == "" || r.Status == "" {
			return errors.New("run result missing id or status")
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
