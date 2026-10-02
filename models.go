package decide

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Model is one entry from GET /v1/models. The list contains aliases such as
// "jev-latest"; versioned IDs are accepted by the API whether or not they
// are listed.
type Model struct {
	Name        string                     // "name"
	Description string                     // "description"
	ReleaseDate string                     // "release_date"; YYYY-MM-DD, kept as a string
	Extra       map[string]json.RawMessage // unmodeled members, written back on marshal
}

// ModelList is the result of GET /v1/models.
type ModelList struct {
	Models    []Model
	RequestID string                     // x-typesafe-request-id
	Header    http.Header                // response headers; nil if the transport has none
	Raw       []byte                     // response body; nil if the transport has none
	Extra     map[string]json.RawMessage // unmodeled top-level members
}

// MarshalJSON writes name, description, release_date, then Extra. It has a
// value receiver so a Model marshals the same whether or not it is addressable.
func (m Model) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.field("name", m.Name)
	w.field("description", m.Description)
	w.field("release_date", m.ReleaseDate)
	w.rawExtra(m.Extra, "name", "description", "release_date")
	return w.bytes()
}

// UnmarshalJSON decodes a model leniently; unknown members go to Extra.
func (m *Model) UnmarshalJSON(data []byte) error {
	var v Model
	_, extra, err := decodeFields(data, map[string]any{
		"name":         &v.Name,
		"description":  &v.Description,
		"release_date": &v.ReleaseDate,
	})
	if err != nil {
		return fmt.Errorf("decide: decode model: %w", err)
	}
	v.Extra = extra
	*m = v
	return nil
}

// MarshalJSON writes {"models": [...]} then Extra.
func (l *ModelList) MarshalJSON() ([]byte, error) {
	models := l.Models
	if models == nil {
		models = []Model{}
	}
	w := newObjectWriter()
	w.field("models", models)
	w.rawExtra(l.Extra, "models")
	return w.bytes()
}

// UnmarshalJSON decodes a model list. It fails if data is not a JSON object;
// a missing "models" becomes an empty list.
func (l *ModelList) UnmarshalJSON(data []byte) error {
	var v ModelList
	_, extra, err := decodeFields(data, map[string]any{"models": &v.Models})
	if err != nil {
		return fmt.Errorf("decide: decode model list: %w", err)
	}
	if v.Models == nil {
		v.Models = []Model{}
	}
	v.Extra = extra
	*l = v
	return nil
}

// ModelsService lists models. Use it through Client.Models.
type ModelsService struct{ c *Client }

// List returns the models the API advertises. It uses the client's retry
// loop and logging. Results are not cached.
func (s *ModelsService) List(ctx context.Context) (*ModelList, error) {
	c := s.c
	p := retryPolicy{maxRetries: c.maxRetries, attemptTimeout: c.attemptTimeout}
	list, err := withRetry(ctx, c, p, "models", c.transport.ListModels,
		func(attempt int, d time.Duration, list *ModelList, err error) {
			if !c.logger.Enabled(ctx, slog.LevelDebug) {
				return
			}
			var requestID string
			if list != nil {
				requestID = list.RequestID
			}
			attrs := []slog.Attr{
				slog.String("op", "models"),
				slog.Int("attempt", attempt),
				slog.Duration("duration", d),
			}
			attrs = append(attrs, outcomeAttrs(err, requestID)...)
			c.logger.LogAttrs(ctx, slog.LevelDebug, "decide request", attrs...)
		})
	if err != nil {
		return nil, err
	}
	if list == nil {
		return nil, fmt.Errorf("%w: transport returned a nil model list", ErrDecode)
	}
	return list, nil
}
