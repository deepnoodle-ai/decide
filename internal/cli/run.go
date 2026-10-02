package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/x/fanout"
)

type LiveBuilder func(context.Context, Envelope) (*decide.Request, error)
type LiveFinish func(*Envelope, *Run, *decide.Response, error) error

func (a *App) RunLive(ctx context.Context, command string, o CommonOptions, build LiveBuilder, finish LiveFinish, emit func(Envelope, Run) bool) int {
	if err := ValidateCommon(o); err != nil {
		return a.fail(err)
	}
	reader := NewRecordReader(a.In, o)
	exit := 0
	var client *decide.Client
	for {
		if ctx.Err() != nil {
			return 130
		}
		chunk := make([]Envelope, 0, o.ChunkSize)
		for len(chunk) < o.ChunkSize {
			e, err := reader.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				if ctx.Err() != nil {
					return 130
				}
				return a.fail(err)
			}
			chunk = append(chunk, e)
		}
		if len(chunk) == 0 {
			return exit
		}
		// Check names across this chunk before spending on any record in it.
		for i := range chunk {
			for _, old := range chunk[i].Runs {
				if old.Name == o.As {
					return a.fail(fmt.Errorf("record %q already has run %q", chunk[i].ID, o.As))
				}
			}
		}
		requests := make([]*decide.Request, len(chunk))
		responses := make([]*decide.Response, len(chunk))
		errs := make([]error, len(chunk))
		sent := make([]bool, len(chunk))
		runs := make([]Run, len(chunk))
		var indices []int
		for i, e := range chunk {
			runs[i] = Run{Name: o.As, Command: command}
			requests[i], errs[i] = build(ctx, e)
			if req := requests[i]; req != nil {
				if o.Model != "" {
					req.Model = o.Model
				} else if req.Model == "" {
					req.Model = strings.TrimSpace(os.Getenv("TYPESAFE_DEFAULT_MODEL"))
					if req.Model == "" {
						req.Model = "jev-latest"
					}
				}
				runs[i].RequestedModel = req.Model
				state, err := json.Marshal(req.State)
				if err != nil && errs[i] == nil {
					errs[i] = err
				}
				runs[i].State = state
				runs[i].Questions = map[string]json.RawMessage{}
				for key, q := range req.Questions {
					b, err := json.Marshal(q)
					if err != nil && errs[i] == nil {
						errs[i] = err
					}
					runs[i].Questions[key] = b
				}
				if errs[i] == nil {
					errs[i] = ValidateState(state)
				}
				if errs[i] == nil {
					errs[i] = req.Validate()
				}
				if errs[i] == nil {
					indices = append(indices, i)
				}
			}
		}
		if len(indices) > 0 {
			if client == nil {
				factory := a.NewClient
				if factory == nil {
					factory = func() (*decide.Client, error) { return decide.NewClient() }
				}
				var err error
				client, err = factory()
				if err == nil && client == nil {
					err = errors.New("client constructor returned nil")
				}
				if err != nil {
					for _, i := range indices {
						errs[i] = err
					}
				}
			}
			if client != nil {
				results, err := fanout.Map(ctx, client, indices, o.Workers, func(_ context.Context, _ int, i int) (*decide.Request, error) { return requests[i], nil })
				for _, i := range indices {
					sent[i] = true
				}
				for j, result := range results {
					i := indices[j]
					responses[i], errs[i] = result.Response, result.Err
				}
				if err != nil && ctx.Err() == nil {
					return a.fail(err)
				}
			}
		}
		for i := range chunk {
			run := &runs[i]
			resp := responses[i]
			err := errs[i]
			if resp != nil {
				if len(resp.Raw) > 0 {
					run.Response = bytes.Clone(resp.Raw)
				} else {
					var marshalErr error
					run.Response, marshalErr = json.Marshal(resp)
					if marshalErr != nil && err == nil {
						err = fmt.Errorf("encode response: %w", marshalErr)
					}
				}
				run.RequestID = redact(resp.RequestID)
				for key, ae := range resp.Invalid {
					if run.Invalid == nil {
						run.Invalid = map[string]RecordError{}
					}
					run.Invalid[key] = RecordError{Kind: "validation", Message: redact(ae.Error()), Reason: ae.Reason, Type: ae.Type}
				}
			}
			if err != nil {
				run.Error = recordError(err)
				if sent[i] && run.Error.Kind == "input" {
					run.Error.Kind = "transport"
				}
				if run.RequestID == "" {
					run.RequestID = run.Error.RequestID
				}
			}
			if finish != nil {
				if fe := finish(&chunk[i], run, resp, err); fe != nil {
					if run.Error == nil {
						run.Error = recordError(fe)
					}
				}
			}
			if run.Error != nil || len(run.Invalid) > 0 {
				exit = 2
			}
			if err := chunk[i].AppendRun(*run); err != nil {
				return a.fail(err)
			}
			if emit == nil || emit(chunk[i], *run) {
				if err := WriteRecord(a.Out, chunk[i]); err != nil {
					return a.fail(fmt.Errorf("write output: %w", err))
				}
			}
		}
		if ctx.Err() != nil {
			return 130
		}
	}
}
func redact(s string) string {
	key := strings.TrimSpace(os.Getenv("TYPESAFE_API_KEY"))
	if len(key) > 0 {
		s = strings.ReplaceAll(s, key, "[REDACTED]")
	}
	return s
}
func recordError(err error) *RecordError {
	result := &RecordError{Kind: "input", Message: redact(err.Error())}
	switch {
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		result.Kind = "canceled"
	case errors.Is(err, decide.ErrInvalidAnswer):
		result.Kind = "validation"
	case errors.Is(err, decide.ErrInvalidRequest):
		result.Kind = "request"
	default:
		if errors.Is(err, decide.ErrDecode) {
			result.Kind = "decode"
		}
		if _, ok := errors.AsType[*url.Error](err); ok {
			result.Kind = "transport"
		}
		if _, ok := errors.AsType[net.Error](err); ok {
			result.Kind = "transport"
		}
		if ae, ok := errors.AsType[*decide.APIError](err); ok {
			result.Kind = "http"
			result.Status = ae.StatusCode
			result.RequestID = redact(ae.RequestID)
		} else if errors.Is(err, decide.ErrNoAPIKey) {
			result.Kind = "transport"
		}
	}
	if ae, ok := errors.AsType[*decide.APIError](err); ok {
		result.Status = ae.StatusCode
		result.RequestID = redact(ae.RequestID)
	}
	return result
}
