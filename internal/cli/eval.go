package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/x/calibrate"
)

func (a *App) runEval(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return a.offlineFailure(errors.New("eval requires dataset, fit, or compare"))
	}
	if args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(a.Out, "eval dataset --answer KEY [--label-field label]\neval fit --fit FILE --heldout FILE --config FILE\neval compare --artifact FILE --candidate FILE --tolerance FILE\nAll eval operations are offline.")
		return 0
	}
	if args[0] == "dataset" {
		return a.runEvalDataset(ctx, args[1:])
	}
	fs := a.flags("eval " + args[0])
	switch args[0] {
	case "fit":
		fitPath := fs.String("fit", "", "saved fit dataset file (required)")
		heldoutPath := fs.String("heldout", "", "disjoint held-out dataset file (required)")
		configPath := fs.String("config", "", "calibration config file (required)")
		if err := ParseFlags(fs, args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return a.offlineFailure(err)
		}
		if err := ctx.Err(); err != nil {
			return a.offlineFailure(err)
		}
		var fit, heldout calibrate.Dataset
		var config calibrate.Config
		if err := readCalibrationFile(*fitPath, &fit); err != nil {
			return a.offlineFailure(err)
		}
		if err := readCalibrationFile(*heldoutPath, &heldout); err != nil {
			return a.offlineFailure(err)
		}
		if err := readCalibrationFile(*configPath, &config); err != nil {
			return a.offlineFailure(err)
		}
		artifact, err := calibrate.Fit(fit, heldout, config)
		if err != nil {
			return a.offlineFailure(err)
		}
		if err := ctx.Err(); err != nil {
			return a.offlineFailure(err)
		}
		if err := writeJSON(a.Out, artifact); err != nil {
			return a.offlineFailure(err)
		}
		return 0
	case "compare":
		artifactPath := fs.String("artifact", "", "frozen calibration artifact file (required)")
		candidatePath := fs.String("candidate", "", "candidate held-out dataset file (required)")
		tolerancePath := fs.String("tolerance", "", "regression tolerance file (required)")
		if err := ParseFlags(fs, args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return a.offlineFailure(err)
		}
		if err := ctx.Err(); err != nil {
			return a.offlineFailure(err)
		}
		var artifact calibrate.Artifact
		var candidate calibrate.Dataset
		var tolerance calibrate.Tolerance
		if err := readCalibrationFile(*artifactPath, &artifact); err != nil {
			return a.offlineFailure(err)
		}
		if err := readCalibrationFile(*candidatePath, &candidate); err != nil {
			return a.offlineFailure(err)
		}
		if err := readCalibrationFile(*tolerancePath, &tolerance); err != nil {
			return a.offlineFailure(err)
		}
		comparison, err := calibrate.Compare(artifact, candidate, tolerance)
		if err != nil && !errors.Is(err, calibrate.ErrRegression) {
			return a.offlineFailure(err)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return a.offlineFailure(ctxErr)
		}
		if writeErr := writeJSON(a.Out, comparison); writeErr != nil {
			return a.offlineFailure(writeErr)
		}
		if errors.Is(err, calibrate.ErrRegression) {
			return 1
		}
		return 0
	default:
		return a.offlineFailure(fmt.Errorf("unknown eval operation %q", args[0]))
	}
}

func (a *App) runEvalDataset(ctx context.Context, args []string) int {
	fs, options := a.offlineFlags("eval dataset", true)
	answerKey := fs.String("answer", "", "saved answer key (required)")
	labelField := fs.String("label-field", "label", "top-level string label in data")
	if err := ParseFlags(fs, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return a.offlineFailure(err)
	}
	if err := ValidateCommon(*options); err != nil {
		return a.offlineFailure(err)
	}
	if *answerKey == "" || *labelField == "" {
		return a.offlineFailure(errors.New("--answer and --label-field must be nonempty"))
	}
	if err := ctx.Err(); err != nil {
		return a.offlineFailure(err)
	}
	records, err := ReadRecords(a.In, *options)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return a.offlineFailure(ctxErr)
	}
	if err != nil {
		return a.offlineFailure(err)
	}
	dataset, err := datasetFromRecords(ctx, records, options.Run, *answerKey, *labelField)
	if err != nil {
		return a.offlineFailure(err)
	}
	if err := writeJSON(a.Out, dataset); err != nil {
		return a.offlineFailure(err)
	}
	return 0
}

func datasetFromRecords(ctx context.Context, records []Envelope, runName, answerKey, labelField string) (calibrate.Dataset, error) {
	dataset := calibrate.Dataset{QuestionKey: answerKey}
	if err := ctx.Err(); err != nil {
		return dataset, err
	}
	if len(records) == 0 {
		return dataset, errors.New("eval dataset requires at least one record")
	}
	seen := make(map[string]bool, len(records))
	for _, env := range records {
		if err := ctx.Err(); err != nil {
			return dataset, err
		}
		if env.ID == "" || seen[env.ID] {
			return dataset, fmt.Errorf("empty or duplicate case ID %q", env.ID)
		}
		seen[env.ID] = true
		run, err := env.SelectedRun(runName)
		if err != nil {
			return dataset, fmt.Errorf("case %q: %w", env.ID, err)
		}
		response, err := run.ValidatedResponse()
		if err != nil {
			return dataset, fmt.Errorf("case %q: %w", env.ID, err)
		}
		question, ok := run.Questions[answerKey]
		if !ok {
			return dataset, fmt.Errorf("case %q: question %q missing", env.ID, answerKey)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, question); err != nil {
			return dataset, err
		}
		if response.Model == "" {
			return dataset, fmt.Errorf("case %q: resolved model missing", env.ID)
		}
		if len(dataset.Cases) == 0 {
			dataset.Question = append(json.RawMessage(nil), compact.Bytes()...)
			dataset.Model = response.Model
		} else if !bytes.Equal(dataset.Question, compact.Bytes()) || dataset.Model != response.Model {
			return dataset, fmt.Errorf("case %q: saved question or resolved model differs", env.ID)
		}
		state := bytes.TrimSpace(run.State)
		if len(state) == 0 || (state[0] != '"' && state[0] != '{' && state[0] != '[') || !json.Valid(state) {
			return dataset, fmt.Errorf("case %q: saved state must be string, object, or array", env.ID)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(env.Data, &fields); err != nil {
			return dataset, fmt.Errorf("case %q: data must be an object containing labels", env.ID)
		}
		var label string
		labelJSON, ok := fields[labelField]
		if !ok || json.Unmarshal(labelJSON, &label) != nil || bytes.Equal(bytes.TrimSpace(labelJSON), []byte("null")) {
			return dataset, fmt.Errorf("case %q: label field %q must be a string", env.ID, labelField)
		}
		answer, ok := response.Answers[answerKey]
		if !ok {
			return dataset, fmt.Errorf("case %q: answer %q missing", env.ID, answerKey)
		}
		if err := validateDatasetLabel(answer, label); err != nil {
			return dataset, fmt.Errorf("case %q: %w", env.ID, err)
		}
		var wire struct {
			Answers map[string]json.RawMessage `json:"answers"`
		}
		if err := json.Unmarshal(run.Response, &wire); err != nil {
			return dataset, err
		}
		dataset.Cases = append(dataset.Cases, calibrate.Case{ID: env.ID, State: run.State, Answer: wire.Answers[answerKey], Label: label})
	}
	return dataset, nil
}

func validateDatasetLabel(answer decide.Answer, label string) error {
	var probabilities map[string]float64
	switch answer := answer.(type) {
	case *decide.NoulAnswer:
		if label != "true" && label != "false" {
			return errors.New("Noul label must be true or false")
		}
	case *decide.ChoiceAnswer:
		probabilities = answer.Probabilities
		if _, ok := answer.Probabilities[label]; !ok {
			return fmt.Errorf("Choice label %q is not an option", label)
		}
	case *decide.ScoreAnswer:
		probabilities = answer.Probabilities
		index, err := strconv.Atoi(label)
		if err != nil || strconv.Itoa(index) != label {
			return fmt.Errorf("Score label %q is not a level index", label)
		}
		if _, ok := answer.Probabilities[label]; !ok {
			return fmt.Errorf("Score label %q is not a level", label)
		}
	default:
		return fmt.Errorf("unsupported calibration answer type %q", answer.AnswerType())
	}
	// x/calibrate's saved-dataset contract is stricter than the root client's
	// provisional sum tolerance. Do not emit datasets its Fit cannot read.
	if probabilities != nil {
		mass := 0.0
		for _, key := range slices.Sorted(maps.Keys(probabilities)) {
			p := probabilities[key]
			if math.IsNaN(p) || math.IsInf(p, 0) || p < 0 || p > 1 {
				return errors.New("calibration probabilities must be finite and in [0,1]")
			}
			mass += p
		}
		if mass <= 0 || math.Abs(mass-1) > 0.01 {
			return fmt.Errorf("calibration probability mass %g is not within 0.01 of 1", mass)
		}
	}
	return nil
}
