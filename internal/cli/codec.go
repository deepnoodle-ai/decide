package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"unicode/utf8"
)

const ConfigMaxBytes = 1 << 20

type RecordReader struct {
	scanner        *bufio.Scanner
	options        CommonOptions
	line, position int
}

func NewRecordReader(r io.Reader, o CommonOptions) *RecordReader {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, min(4096, o.MaxRecordBytes+2)), o.MaxRecordBytes+2)
	return &RecordReader{scanner: s, options: o}
}
func (r *RecordReader) Next() (Envelope, error) {
	for r.scanner.Scan() {
		r.line++
		raw := bytes.Clone(r.scanner.Bytes())
		if len(raw) > r.options.MaxRecordBytes {
			return Envelope{}, fmt.Errorf("line %d exceeds max-record-bytes", r.line)
		}
		if r.options.Input == "jsonl" && len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		r.position++
		var data json.RawMessage
		if r.options.Input == "text" {
			if !utf8.Valid(raw) {
				return Envelope{}, fmt.Errorf("line %d contains invalid UTF-8", r.line)
			}
			data, _ = json.Marshal(string(raw))
		} else {
			if err := jsonv2.Unmarshal(raw, &data); err != nil {
				return Envelope{}, fmt.Errorf("line %d: %w", r.line, err)
			}
			var obj map[string]json.RawMessage
			if json.Unmarshal(data, &obj) == nil {
				if _, reserved := obj["typesafe_cli"]; reserved {
					var e Envelope
					if err := json.Unmarshal(data, &e); err != nil {
						return Envelope{}, fmt.Errorf("line %d: %w", r.line, err)
					}
					return e, nil
				}
			}
		}
		id := "r" + strconv.Itoa(r.position)
		var obj map[string]json.RawMessage
		if json.Unmarshal(data, &obj) == nil {
			var supplied string
			if json.Unmarshal(obj["id"], &supplied) == nil && supplied != "" {
				id = supplied
			}
		}
		return Envelope{Version: 1, ID: id, Data: data, Runs: []Run{}}, nil
	}
	if err := r.scanner.Err(); err != nil {
		return Envelope{}, fmt.Errorf("line %d: %w", r.line+1, err)
	}
	return Envelope{}, io.EOF
}
func ReadRecords(input io.Reader, o CommonOptions) ([]Envelope, error) {
	if err := ValidateCommon(o); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(input, int64(o.MaxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > o.MaxBytes {
		return nil, errors.New("input exceeds max-bytes")
	}
	reader := NewRecordReader(bytes.NewReader(b), o)
	var records []Envelope
	ids := map[string]bool{}
	for {
		e, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return records, nil
		}
		if err != nil {
			return nil, err
		}
		if len(records) >= o.MaxRecords {
			return nil, errors.New("input exceeds max-records")
		}
		if ids[e.ID] {
			return nil, fmt.Errorf("duplicate record ID %q", e.ID)
		}
		ids[e.ID] = true
		records = append(records, e)
	}
}
func WriteRecord(w io.Writer, e Envelope) error { return json.NewEncoder(w).Encode(e) }
func ReadJSONFile(path string, maxBytes int) (json.RawMessage, error) {
	if maxBytes < 1 || maxBytes > int(^uint(0)>>1)-1 {
		return nil, errors.New("invalid JSON file byte limit")
	}
	if path == "" {
		return nil, errors.New("JSON file path is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", path, maxBytes)
	}
	var raw json.RawMessage
	if err = jsonv2.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return raw, nil
}

// ValidateState enforces the native state union without decoding numeric leaves.
func ValidateState(raw json.RawMessage) error {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 {
		return errors.New("state is missing")
	}
	var checked json.RawMessage
	if err := jsonv2.Unmarshal(b, &checked); err != nil {
		return err
	}
	if b[0] != '{' && b[0] != '[' && b[0] != '"' {
		return errors.New("state must be a string, object, or array")
	}
	return nil
}
