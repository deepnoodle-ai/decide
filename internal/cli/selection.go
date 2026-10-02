package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/x/pick"
)

type pickInput struct {
	State      json.RawMessage
	Candidates []json.RawMessage
}

func readPickInput(data json.RawMessage) (pickInput, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return pickInput{}, fmt.Errorf("pick: data must be an object with state and candidates")
	}
	state, ok := fields["state"]
	if !ok {
		return pickInput{}, fmt.Errorf("pick: missing state")
	}
	trimmed := bytes.TrimSpace(state)
	if len(trimmed) == 0 || !strings.ContainsRune("\"{[", rune(trimmed[0])) {
		return pickInput{}, fmt.Errorf("pick: state must be a string, object, or array")
	}
	raw, ok := fields["candidates"]
	if !ok || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		return pickInput{}, fmt.Errorf("pick: candidates must be an array")
	}
	var candidates []json.RawMessage
	if err := json.Unmarshal(raw, &candidates); err != nil {
		return pickInput{}, fmt.Errorf("pick: candidates: %w", err)
	}
	return pickInput{State: state, Candidates: candidates}, nil
}

func rawPicker(items []json.RawMessage) (*pick.Picker[json.RawMessage], error) {
	return pick.New(items,
		pick.KeyFunc(func(i int, _ json.RawMessage) string { return fmt.Sprintf("c%d", i+1) }),
		pick.Describe(func(item json.RawMessage) any {
			return map[string]json.RawMessage{"value": item}
		}),
	)
}

func (a *App) runPick(ctx context.Context, args []string) int {
	fs := a.flags("pick")
	o := BindCommon(fs, "pick", true, false)
	question := fs.String("question", "", "question selecting one supplied candidate")
	if code, ok := a.parse(fs, args, o); !ok {
		return code
	}
	if strings.TrimSpace(*question) == "" {
		return a.fail(fmt.Errorf("pick: --question is required"))
	}
	build := func(_ context.Context, e Envelope) (*decide.Request, error) {
		input, err := readPickInput(e.Data)
		if err != nil {
			return nil, err
		}
		req := decide.NewRequest(input.State)
		if len(input.Candidates) == 0 {
			// Validate state even when no model request is needed.
			req.Questions["pick"] = decide.Noul("Is a candidate available?")
			if err := req.Validate(); err != nil {
				return nil, err
			}
			return nil, nil
		}
		p, err := rawPicker(input.Candidates)
		if err != nil {
			return nil, err
		}
		req.Questions["pick"] = p.Question(*question)
		return req, nil
	}
	finish := func(e *Envelope, run *Run, resp *decide.Response, callErr error) error {
		if callErr != nil {
			return nil
		}
		input, err := readPickInput(e.Data)
		if err != nil {
			return err
		}
		if len(input.Candidates) == 0 {
			run.State = input.State
			run.Result = json.RawMessage(`{"picked":false,"abstained":true}`)
			return nil
		}
		p, err := rawPicker(input.Candidates)
		if err != nil {
			return err
		}
		answer, err := decide.AnswerAs[*decide.ChoiceAnswer](resp, "pick")
		if err != nil {
			return err
		}
		result, err := p.Read(answer)
		if err != nil {
			return err
		}
		out := struct {
			Picked    bool            `json:"picked"`
			Abstained bool            `json:"abstained"`
			Item      json.RawMessage `json:"item,omitempty"`
			Index     *int            `json:"index,omitempty"`
		}{Picked: result.Picked, Abstained: !result.Picked}
		if result.Picked {
			out.Item, out.Index = result.Item, &result.Index
		}
		run.Result, err = json.Marshal(out)
		return err
	}
	return a.RunLive(ctx, "pick", *o, build, finish, nil)
}

func (a *App) runJoin(ctx context.Context, args []string) int {
	fs := a.flags("join")
	o := BindCommon(fs, "join", true, false)
	question := fs.String("question", "", "relation to judge between supplied left and right values")
	if code, ok := a.parse(fs, args, o); !ok {
		return code
	}
	if strings.TrimSpace(*question) == "" {
		return a.fail(fmt.Errorf("join: --question is required"))
	}
	build := func(_ context.Context, e Envelope) (*decide.Request, error) {
		var pair map[string]json.RawMessage
		if err := json.Unmarshal(e.Data, &pair); err != nil || pair == nil {
			return nil, fmt.Errorf("join: data must contain left and right")
		}
		left, hasLeft := pair["left"]
		right, hasRight := pair["right"]
		if !hasLeft || !hasRight {
			return nil, fmt.Errorf("join: data must contain left and right")
		}
		req := decide.NewRequest(map[string]json.RawMessage{"left": left, "right": right})
		req.Questions["relation"] = decide.Noul(*question)
		return req, nil
	}
	finish := func(_ *Envelope, run *Run, resp *decide.Response, callErr error) error {
		if callErr != nil {
			return nil
		}
		answer, err := decide.AnswerAs[*decide.NoulAnswer](resp, "relation")
		if err != nil {
			return err
		}
		run.Result, err = json.Marshal(struct {
			Noul float64 `json:"noul"`
		}{answer.Noul})
		return err
	}
	return a.RunLive(ctx, "join", *o, build, finish, nil)
}
