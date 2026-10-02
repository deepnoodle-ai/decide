package cli

import (
	"encoding/json"
	"testing"
)

func TestPackUnicodeBudgetSourceOrderAndHistory(t *testing.T) {
	a, out, diagnostics := selectionApp(t, selectionStream(t,
		savedSelectionRecord("a", "é", .9), // two UTF-8 bytes
		savedSelectionRecord("b", "xyz", .8),
		savedSelectionRecord("c", "q", .7)), nil)
	if code := a.runPack(t.Context(), []string{"--answer", "needed", "--budget-bytes", "3"}); code != 0 {
		t.Fatalf("exit %d: %s", code, diagnostics)
	}
	records := selectionOutput(t, out)
	selectedBytes := 0
	for i, expected := range []bool{true, false, true} {
		if len(records[i].Runs) != 2 {
			t.Fatalf("history lost: %+v", records[i])
		}
		var result struct {
			Selected  bool `json:"selected"`
			SizeBytes int  `json:"size_bytes"`
		}
		if err := json.Unmarshal(records[i].Runs[1].Result, &result); err != nil {
			t.Fatal(err)
		}
		if result.Selected != expected {
			t.Fatalf("selection[%d]: %+v", i, result)
		}
		if result.Selected {
			selectedBytes += result.SizeBytes
		}
	}
	if records[0].ID != "a" || records[1].ID != "b" || records[2].ID != "c" || selectedBytes != 3 {
		t.Fatalf("order/budget: %+v, bytes %d", records, selectedBytes)
	}
}

func TestPackTiesFollowCompactSelection(t *testing.T) {
	a, out, diagnostics := selectionApp(t, selectionStream(t,
		savedSelectionRecord("a", "a", .8), savedSelectionRecord("b", "b", .8)), nil)
	if code := a.runPack(t.Context(), []string{"--answer", "needed", "--budget-bytes", "1"}); code != 0 {
		t.Fatalf("exit %d: %s", code, diagnostics)
	}
	records := selectionOutput(t, out)
	if string(records[0].Runs[1].Result) == "" {
		t.Fatal("missing decision")
	}
	var first, second struct{ Selected bool }
	if err := json.Unmarshal(records[0].Runs[1].Result, &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(records[1].Runs[1].Result, &second); err != nil {
		t.Fatal(err)
	}
	if first.Selected || !second.Selected {
		t.Fatalf("tied selection: first %v second %v", first.Selected, second.Selected)
	}
}

func TestPackRejectsInvalidCollectionBeforeOutput(t *testing.T) {
	for _, kind := range []string{"not_string", "invalid_score", "failed_latest", "budget"} {
		t.Run(kind, func(t *testing.T) {
			first := savedSelectionRecord("a", "a", .8)
			last := savedSelectionRecord("b", "b", .9)
			args := []string{"--answer", "needed", "--budget-bytes", "10"}
			switch kind {
			case "not_string":
				last.Data = json.RawMessage(`{"text":"b"}`)
			case "invalid_score":
				last.Runs[0].Invalid = map[string]RecordError{"needed": {Kind: "validation", Message: "invalid", Type: "noul"}}
			case "failed_latest":
				last.Runs = append(last.Runs, Run{Name: "failed", Command: "judge", Error: &RecordError{Kind: "http", Message: "failed"}})
			case "budget":
				args[len(args)-1] = "0"
			}
			a, out, _ := selectionApp(t, selectionStream(t, first, last), nil)
			if code := a.runPack(t.Context(), args); code != 2 || out.Len() != 0 {
				t.Fatalf("exit %d partial %s", code, out)
			}
		})
	}
}
