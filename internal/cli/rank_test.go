package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func savedSelectionRecord(id string, data any, p float64) Envelope {
	raw, _ := json.Marshal(data)
	response := json.RawMessage(fmt.Sprintf(`{"model":"jev-1.13.0","answers":{"needed":{"type":"noul","noul":%g}},"usage":{"input_tokens":10,"output_tokens":1}}`, p))
	return Envelope{Version: 1, ID: id, Data: raw, Runs: []Run{{
		Name: "judgment", Command: "judge", State: raw,
		Questions: map[string]json.RawMessage{"needed": json.RawMessage(`{"type":"noul","instructions":"Is this needed?"}`)},
		Response:  response, RequestedModel: "jev-latest", RequestID: "test-request",
	}}}
}

func selectionStream(t *testing.T, records ...Envelope) string {
	t.Helper()
	var input bytes.Buffer
	for _, e := range records {
		if err := WriteRecord(&input, e); err != nil {
			t.Fatal(err)
		}
	}
	return input.String()
}

func TestRankSavedObservationsStableTiesAndHistory(t *testing.T) {
	first := savedSelectionRecord("a", "first", .5)
	first.Extra = map[string]json.RawMessage{"extension": json.RawMessage(`{"keep":true}`)}
	a, out, diagnostics := selectionApp(t, selectionStream(t,
		first, savedSelectionRecord("b", "second", .9), savedSelectionRecord("c", "third", .9)), nil)
	if code := a.runRank(t.Context(), []string{"--answer", "needed"}); code != 0 {
		t.Fatalf("exit %d: %s", code, diagnostics)
	}
	records := selectionOutput(t, out)
	for i, id := range []string{"b", "c", "a"} {
		if records[i].ID != id || len(records[i].Runs) != 2 || records[i].Runs[0].Name != "judgment" {
			t.Fatalf("rank[%d]: %+v", i, records[i])
		}
	}
	if string(records[2].Extra["extension"]) != `{"keep":true}` {
		t.Fatal("extension lost")
	}
}

func TestRankRejectsFailedLatestMissingAndTampered(t *testing.T) {
	for _, kind := range []string{"failed_latest", "missing_answer", "tampered", "duplicate_name", "duplicate_id"} {
		t.Run(kind, func(t *testing.T) {
			first := savedSelectionRecord("a", "first", .9)
			last := savedSelectionRecord("b", "second", .5)
			switch kind {
			case "failed_latest":
				last.Runs = append(last.Runs, Run{Name: "failed", Command: "judge", Error: &RecordError{Kind: "http", Message: "failed"}})
			case "missing_answer":
				last.Runs[0].Response = json.RawMessage(`{"model":"jev-1.13.0","answers":{},"usage":{}}`)
			case "tampered":
				last.Runs[0].Response = json.RawMessage(`{"model":"jev-1.13.0","answers":{"needed":{"type":"noul","noul":4}},"usage":{}}`)
			case "duplicate_name":
				last.Runs = append(last.Runs, Run{Name: "rank", Command: "rank"})
			case "duplicate_id":
				last.ID = first.ID
			}
			a, out, _ := selectionApp(t, selectionStream(t, first, last), nil)
			if code := a.runRank(t.Context(), []string{"--answer", "needed"}); code != 2 || out.Len() != 0 {
				t.Fatalf("exit %d partial %s", code, out)
			}
		})
	}
}

func TestRankExplicitSourceAndCaps(t *testing.T) {
	e := savedSelectionRecord("a", "first", .8)
	e.Runs = append(e.Runs, Run{Name: "failed", Command: "judge", Error: &RecordError{Kind: "http", Message: "failed"}})
	a, out, diagnostics := selectionApp(t, selectionStream(t, e), nil)
	if code := a.runRank(t.Context(), []string{"--answer", "needed", "--run", "judgment"}); code != 0 || len(selectionOutput(t, out)) != 1 {
		t.Fatalf("explicit source exit %d: %s", code, diagnostics)
	}
	a, out, _ = selectionApp(t, selectionStream(t, e, savedSelectionRecord("b", "second", .4)), nil)
	if code := a.runRank(t.Context(), []string{"--answer", "needed", "--max-records", "1"}); code != 2 || out.Len() != 0 {
		t.Fatalf("cap exit %d partial %s", code, out)
	}
}
