package decidetest

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/deepnoodle-ai/decide"
)

// mediaKey is the request member that carries files from a client built
// with AcceptMedia to the fake server.
const mediaKey = "decidetest_media"

type wireMedia struct {
	MIMEType string `json:"mime_type"`
	Data     []byte `json:"data,omitempty"`
	URL      string `json:"url,omitempty"`
}

// mediaTransport is the TypeSafe transport with media accepted: it sends
// the files under mediaKey, and the server moves them back into
// Request.Media.
type mediaTransport struct{ *decide.HTTPTransport }

var _ decide.MediaTransport = mediaTransport{}

// CheckMedia accepts every file that passed Request.Validate.
func (mediaTransport) CheckMedia(*decide.Request) error { return nil }

// SystemOne sends req with its files under mediaKey.
func (t mediaTransport) SystemOne(ctx context.Context, req *decide.Request) (*decide.Response, error) {
	if len(req.Media) == 0 {
		return t.HTTPTransport.SystemOne(ctx, req)
	}
	r := *req
	r.Media = nil
	files := make([]wireMedia, len(req.Media))
	for i, m := range req.Media {
		files[i] = wireMedia{MIMEType: m.MIMEType, Data: m.Data, URL: m.URL}
	}
	r.Extra = maps.Clone(req.Extra)
	if r.Extra == nil {
		r.Extra = map[string]any{}
	}
	r.Extra[mediaKey] = files
	resp, err := t.HTTPTransport.SystemOne(ctx, &r)
	return resp, err
}

// String describes the transport with the API key redacted.
func (t mediaTransport) String() string { return "decidetest media " + t.HTTPTransport.String() }

// rejectTransport is the TypeSafe transport, which rejects media, with a
// hint for a test author in its error.
type rejectTransport struct{ *decide.HTTPTransport }

var _ decide.MediaTransport = rejectTransport{}

// CheckMedia returns the TypeSafe transport's error and names AcceptMedia.
func (t rejectTransport) CheckMedia(req *decide.Request) error {
	if err := t.HTTPTransport.CheckMedia(req); err != nil {
		return fmt.Errorf("%w (in a test, pass decidetest.AcceptMedia() to decidetest.NewServer to accept media)", err)
	}
	return nil
}

// String describes the transport with the API key redacted.
func (t rejectTransport) String() string { return t.HTTPTransport.String() }

// takeMedia moves the files a mediaTransport sent into req.Media.
func takeMedia(req *decide.Request) error {
	raw, ok := req.Extra[mediaKey]
	if !ok {
		return nil
	}
	delete(req.Extra, mediaKey)
	if len(req.Extra) == 0 {
		req.Extra = nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("decode %s: %w", mediaKey, err)
	}
	var files []wireMedia
	if err := json.Unmarshal(b, &files); err != nil {
		return fmt.Errorf("decode %s: %w", mediaKey, err)
	}
	for _, f := range files {
		req.Media = append(req.Media, decide.Media{MIMEType: f.MIMEType, Data: f.Data, URL: f.URL})
	}
	return nil
}
