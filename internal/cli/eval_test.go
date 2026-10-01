package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/x/calibrate"
	"github.com/deepnoodle-ai/decide/x/gate"
)

func TestEvalDatasetPreservesStateAndRejectsMixedSavedEvidence(t *testing.T) {
	first := offlineEnvelope(t, "a", "model sees only state", "true", 0.9)
	second := offlineEnvelope(t, "b", "another state", "false", 0.1)
	app, out, diagnostics := offlineApp(offlineInput(t, first, second))
	if got := app.runEval(context.Background(), []string{"dataset", "--answer", "relevant"}); got != 0 {
		t.Fatalf("exit %d: %s", got, diagnostics)
	}
	var dataset calibrate.Dataset
	if err := json.Unmarshal(out.Bytes(), &dataset); err != nil {
		t.Fatal(err)
	}
	if dataset.QuestionKey != "relevant" || dataset.Model != "jev-1.13.0" || len(dataset.Cases) != 2 || !bytes.Equal(dataset.Cases[0].State, first.Runs[0].State) {
		t.Fatalf("dataset %+v", dataset)
	}
	for _, scenario := range []string{"model", "question", "label", "state", "invalid", "latest", "duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			bad := offlineEnvelope(t, "b", "another state", "false", 0.1)
			switch scenario {
			case "model":
				bad.Runs[0].Response = bytes.Replace(bad.Runs[0].Response, []byte("jev-1.13.0"), []byte("jev-1.14.0"), 1)
			case "question":
				bad.Runs[0].Questions["relevant"] = offlineJSON(t, decide.Noul("Different rubric?"))
			case "label":
				bad.Data = json.RawMessage(`{"label":"maybe"}`)
			case "state":
				bad.Runs[0].State = json.RawMessage(`null`)
			case "invalid":
				bad.Runs[0].Invalid = map[string]RecordError{"relevant": {Kind: "validation", Message: "unusable", Reason: decide.ReasonMissingField}}
			case "latest":
				bad.Runs = append(bad.Runs, Run{Name: "later", Command: "judge", Error: &RecordError{Kind: "http", Message: "failed"}})
			case "duplicate":
				bad.ID = "a"
			}
			app, out, _ := offlineApp(offlineInput(t, first, bad))
			if got := app.runEval(context.Background(), []string{"dataset", "--answer", "relevant"}); got != 2 || out.Len() != 0 {
				t.Fatalf("exit %d output %s", got, out)
			}
		})
	}
}

func offlineDataset(t *testing.T, prefix, model string, probabilities []float64, labels []string) calibrate.Dataset {
	t.Helper()
	d := calibrate.Dataset{QuestionKey: "relevant", Question: offlineJSON(t, decide.Noul("Relevant?")), Model: model}
	for i, p := range probabilities {
		d.Cases = append(d.Cases, calibrate.Case{ID: prefix + string(rune('a'+i)), State: offlineJSON(t, "state "+string(rune('a'+i))), Answer: offlineJSON(t, map[string]any{"type": "noul", "noul": p}), Label: labels[i]})
	}
	return d
}

func TestEvalFrozenComparisonAndDisjointSplit(t *testing.T) {
	fit := offlineDataset(t, "f", "jev-1.13.0", []float64{0.9, 0.8, 0.7, 0.4}, []string{"true", "false", "true", "false"})
	heldout := offlineDataset(t, "h", "jev-1.13.0", []float64{0.95, 0.85, 0.2}, []string{"true", "false", "false"})
	fitFile, heldoutFile := offlineFile(t, fit), offlineFile(t, heldout)
	configFile := offlineFile(t, calibrate.Config{Measure: gate.MeasureNoul, NoulTarget: "true", MaxError: 0, MinAllowed: 1, ECEBins: 5})
	app, out, diagnostics := offlineApp("")
	if got := app.runEval(context.Background(), []string{"fit", "--fit", fitFile, "--heldout", heldoutFile, "--config", configFile}); got != 0 {
		t.Fatalf("fit exit %d: %s", got, diagnostics)
	}
	var artifact calibrate.Artifact
	if err := json.Unmarshal(out.Bytes(), &artifact); err != nil {
		t.Fatal(err)
	}
	artifactFile := offlineFile(t, artifact)
	if artifact.Cutoff != 0.9 || artifact.Baseline.ErrorRate == nil || *artifact.Baseline.ErrorRate != 0 {
		t.Fatalf("artifact %+v", artifact)
	}
	tolerance := offlineFile(t, calibrate.Tolerance{Metric: calibrate.MetricErrorRate, Direction: calibrate.HigherIsWorse, MaxDelta: 0})
	candidate := heldout
	candidate.Model = "jev-1.14.0"
	candidateFile := offlineFile(t, candidate)
	args := []string{"compare", "--artifact", artifactFile, "--candidate", candidateFile, "--tolerance", tolerance}
	app, out, diagnostics = offlineApp("")
	if got := app.runEval(context.Background(), args); got != 0 {
		t.Fatalf("unchanged exit %d: %s", got, diagnostics)
	}
	var comparison calibrate.Comparison
	if err := json.Unmarshal(out.Bytes(), &comparison); err != nil {
		t.Fatal(err)
	}
	if comparison.Exceeded || comparison.CandidateModel != "jev-1.14.0" {
		t.Fatalf("comparison %+v", comparison)
	}
	candidate.Cases = append([]calibrate.Case(nil), heldout.Cases...)
	candidate.Cases[1].Answer = offlineJSON(t, map[string]any{"type": "noul", "noul": 0.99})
	if err := os.WriteFile(candidateFile, offlineJSON(t, candidate), 0600); err != nil {
		t.Fatal(err)
	}
	app, out, diagnostics = offlineApp("")
	if got := app.runEval(context.Background(), args); got != 1 {
		t.Fatalf("regression exit %d: %s", got, diagnostics)
	}
	if err := json.Unmarshal(out.Bytes(), &comparison); err != nil {
		t.Fatal(err)
	}
	if !comparison.Exceeded || comparison.Deterioration != 0.5 {
		t.Fatalf("injected regression %+v", comparison)
	}
	candidate.Cases[0].State = offlineJSON(t, "changed held-out case")
	if err := os.WriteFile(candidateFile, offlineJSON(t, candidate), 0600); err != nil {
		t.Fatal(err)
	}
	app, out, _ = offlineApp("")
	if got := app.runEval(context.Background(), args); got != 2 || out.Len() != 0 {
		t.Fatalf("cohort mismatch exit %d output %s", got, out)
	}
	heldout.Cases[0].ID = fit.Cases[0].ID
	if err := os.WriteFile(heldoutFile, offlineJSON(t, heldout), 0600); err != nil {
		t.Fatal(err)
	}
	app, out, _ = offlineApp("")
	if got := app.runEval(context.Background(), []string{"fit", "--fit", fitFile, "--heldout", heldoutFile, "--config", configFile}); got != 2 || out.Len() != 0 {
		t.Fatalf("overlap exit %d output %s", got, out)
	}
}

func TestEvalStrictConfigurationAndLabelShapes(t *testing.T) {
	for _, raw := range []string{
		`{"measure":"noul","noul_target":"true","max_error":0,"min_allowed":1,"ece_bins":5,"max_eror":1}`,
		`{"measure":"noul","measure":"top","noul_target":"true","max_error":0,"min_allowed":1,"ece_bins":5}`,
	} {
		path := offlineFile(t, map[string]any{})
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		var config calibrate.Config
		if err := readCalibrationFile(path, &config); err == nil {
			t.Fatalf("accepted config %s", raw)
		}
	}
	choice := &decide.ChoiceAnswer{Probabilities: map[string]float64{"yes": 0.9, "no": 0.1}}
	if err := validateDatasetLabel(choice, "missing"); err == nil {
		t.Fatal("unknown choice label accepted")
	}
	score := &decide.ScoreAnswer{Probabilities: map[string]float64{"0": 0.2, "1": 0.8}}
	for _, label := range []string{"01", "-1", "2"} {
		if err := validateDatasetLabel(score, label); err == nil {
			t.Fatalf("invalid Score label %q accepted", label)
		}
	}
	if err := validateDatasetLabel(score, "1"); err != nil {
		t.Fatal(err)
	}
	choice.Probabilities = map[string]float64{"yes": 0.9, "no": 0.15}
	if err := validateDatasetLabel(choice, "yes"); err == nil {
		t.Fatal("accepted root-tolerated distribution incompatible with calibration")
	}
	app, out, _ := offlineApp("")
	if got := app.runEval(context.Background(), []string{"dataset", "--answer", "relevant"}); got != 2 || out.Len() != 0 {
		t.Fatalf("empty dataset exit %d output %s", got, out)
	}
}

func TestEvalLimitsCancellationAndOutputFailure(t *testing.T) {
	env := offlineEnvelope(t, "a", "state", "true", 0.9)
	input := offlineInput(t, env)
	app, out, _ := offlineApp(input)
	if got := app.runEval(context.Background(), []string{"dataset", "--answer", "relevant", "--max-bytes", "1"}); got != 2 || out.Len() != 0 {
		t.Fatalf("byte cap exit %d", got)
	}
	app, out, _ = offlineApp(input + strings.Replace(input, `"id":"a"`, `"id":"b"`, 1))
	if got := app.runEval(context.Background(), []string{"dataset", "--answer", "relevant", "--max-records", "1"}); got != 2 || out.Len() != 0 {
		t.Fatalf("record cap exit %d", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app, out, _ = offlineApp(input)
	if got := app.runEval(ctx, []string{"dataset", "--answer", "relevant"}); got != 130 || out.Len() != 0 {
		t.Fatalf("cancellation exit %d", got)
	}
	app, _, _ = offlineApp(input)
	app.Out = offlineBrokenWriter{}
	if got := app.runEval(context.Background(), []string{"dataset", "--answer", "relevant"}); got != 2 {
		t.Fatalf("write failure exit %d", got)
	}
}
