package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/deepnoodle-ai/decide/internal/jobs"
)

func writeRunSummary(w io.Writer, s jobs.Summary) error {
	_, err := fmt.Fprintf(w, "Run %s: %s\n%d complete · %d failed · %d dropped · %d uncertain · %d requests\nSaved evidence: %s\nView answers: decide inspect %s\nExport JSONL: decide runs export %s\n", s.ID, s.Status, s.Completed, s.Failed, s.Dropped, s.Uncertain, s.Requests, s.Path, s.ID, s.ID)
	return err
}

func writeResultDetails(w io.Writer, r jobs.Result) error {
	name := r.Source.Path
	if name == "" {
		name = r.Source.URI
	}
	if name == "" {
		name = r.ID
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s\n", name, r.Status)
	if r.Error != "" {
		fmt.Fprintf(&b, "  %s\n", r.Error)
	}
	for _, stage := range r.Stages {
		var response struct {
			Answers map[string]json.RawMessage `json:"answers"`
		}
		_ = json.Unmarshal(stage.Response, &response)
		keys := make([]string, 0, len(response.Answers))
		for key := range response.Answers {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(response.Answers[key], &fields)
			fieldNames := make([]string, 0, len(fields))
			for field := range fields {
				if field != "type" {
					fieldNames = append(fieldNames, field)
				}
			}
			sort.Strings(fieldNames)
			var parts []string
			for _, field := range fieldNames {
				parts = append(parts, field+"="+string(fields[field]))
			}
			value := strings.Join(parts, " · ")
			if len(value) > 240 {
				value = value[:240] + "…"
			}
			fmt.Fprintf(&b, "  %s/%s: %s\n", stage.Name, key, value)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
