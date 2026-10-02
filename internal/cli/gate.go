package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/patterns/gate"
)

// evaluatePolicy retains the operational error separately from the decision:
// an explicit policy may allow missing inputs, but execution still failed.
func evaluatePolicy(run Run, rule gate.Rule) (gate.Decision, error) {
	var result struct {
		Abstained bool `json:"abstained"`
	}
	if run.Command == "pick" && len(run.Response) == 0 && len(run.Result) > 0 {
		if err := json.Unmarshal(run.Result, &result); err != nil {
			return rule.Evaluate(gate.Inputs{}), fmt.Errorf("saved result: %w", err)
		}
	}
	if run.Command == "pick" && result.Abstained && len(run.Response) == 0 && run.Error == nil && len(run.Invalid) == 0 {
		return rule.Evaluate(gate.NewInputs(gate.Abstained("pick"))), nil
	}
	response, err := run.ValidatedResponse()
	inputs := gate.FromResponse(response)
	// A saved whole-run failure can retain a structurally valid response.
	// Mark all its inputs failed rather than route on those answer values.
	if err != nil && (response == nil || len(response.Invalid) == 0) {
		failed := make([]gate.Input, 0, len(run.Questions))
		for name := range run.Questions {
			failed = append(failed, gate.Failed(name, err))
		}
		inputs = gate.NewInputs(failed...)
	}
	if run.Command == "pick" && err == nil {
		all := make([]gate.Input, 0, len(run.Questions))
		for name, input := range inputs.All() {
			answer, isChoice := response.Answers[name].(*decide.ChoiceAnswer)
			if name == "pick" && isChoice && answer.Choice == "none" {
				input = gate.Abstained(name)
			}
			all = append(all, input)
		}
		inputs = gate.NewInputs(all...)
	}
	return rule.Evaluate(inputs), err
}

func (a *App) runGate(ctx context.Context, args []string) int {
	fs, options := a.offlineFlags("gate", false)
	policyPath := fs.String("policy", "", "JSON patterns/gate policy file (required)")
	if err := ParseFlags(fs, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return a.offlineFailure(err)
	}
	if err := ValidateCommon(*options); err != nil {
		return a.offlineFailure(err)
	}
	if *policyPath == "" {
		return a.offlineFailure(errors.New("--policy is required"))
	}
	raw, err := ReadJSONFile(*policyPath, ConfigMaxBytes)
	if err != nil {
		return a.offlineFailure(err)
	}
	rule, err := gate.DecodeRule(raw)
	if err != nil {
		return a.offlineFailure(err)
	}
	reader := NewRecordReader(a.In, *options)
	status := 0
	for {
		if err := ctx.Err(); err != nil {
			return a.offlineFailure(err)
		}
		env, err := reader.Next()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return a.offlineFailure(ctxErr)
		}
		if errors.Is(err, io.EOF) {
			return status
		}
		if err != nil {
			return a.offlineFailure(err)
		}
		source, sourceErr := env.SelectedRun(options.Run)
		run := Run{Name: options.As, Command: "gate"}
		var decision gate.Decision
		if sourceErr != nil {
			decision = rule.Evaluate(gate.Inputs{})
		} else {
			decision, sourceErr = evaluatePolicy(*source, rule)
		}
		result := struct {
			gate.Decision
			SourceRun string `json:"source_run"`
		}{Decision: decision}
		if source != nil {
			result.SourceRun = source.Name
		}
		run.Result, err = json.Marshal(result)
		if err != nil {
			return a.offlineFailure(err)
		}
		if sourceErr != nil {
			status = 2
			run.Error = &RecordError{Kind: "saved", Message: sourceErr.Error()}
		}
		if err := env.AppendRun(run); err != nil {
			return a.offlineFailure(err)
		}
		if err := WriteRecord(a.Out, env); err != nil {
			return a.offlineFailure(err)
		}
	}
}
