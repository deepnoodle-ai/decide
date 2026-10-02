// Package jobs prepares and durably executes dataset judgments. Artifacts retain
// frozen inputs and configuration; interrupted provider attempts require explicit retry.
package jobs

import (
	"encoding/json"
	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/catalog"
	"github.com/deepnoodle-ai/decide/internal/dataset"
	"time"
)

type Options struct {
	Progress           string                         `json:"progress,omitempty"`
	Skill              string                         `json:"skill,omitempty"`
	Pattern            string                         `json:"pattern,omitempty"`
	SkillDefinition    *catalog.Skill                 `json:"-"`
	Params             map[string]string              `json:"params,omitempty"`
	Sources            dataset.Options                `json:"sources"`
	Provider           string                         `json:"provider"`
	Model              string                         `json:"model"`
	Profile            string                         `json:"profile,omitempty"`
	BaseURL            string                         `json:"base_url,omitempty"`
	AccountID          string                         `json:"account_id,omitempty"`
	RunDir             string                         `json:"run_dir,omitempty"`
	Output             string                         `json:"output,omitempty"`
	Order              string                         `json:"order"`
	OnError            string                         `json:"on_error"`
	Snapshot           string                         `json:"snapshot"`
	Workers            int                            `json:"workers"`
	Retries            int                            `json:"retries"`
	MaxRequests        int                            `json:"max_requests"`
	MaxCollectionBytes int64                          `json:"max_collection_bytes"`
	MaxRecords         int                            `json:"max_records"`
	RateLimit          float64                        `json:"rate_limit"`
	RequestTimeout     time.Duration                  `json:"request_timeout"`
	NewClient          func() (*decide.Client, error) `json:"-"`
}

func DefaultOptions() Options {
	return Options{Sources: dataset.DefaultOptions(), Provider: "typesafe", Workers: 4, RequestTimeout: time.Minute, Order: "completion", OnError: "continue", Snapshot: "refs", MaxRecords: 10000, MaxCollectionBytes: 64 << 20}
}

type Prepared struct {
	Item      dataset.Item               `json:"item"`
	State     json.RawMessage            `json:"state"`
	Questions map[string]json.RawMessage `json:"questions"`
}
type Result struct {
	Version   int             `json:"decide_run"`
	Assets    []Asset         `json:"assets,omitempty"`
	ID        string          `json:"id"`
	Source    dataset.Source  `json:"source"`
	Data      json.RawMessage `json:"data"`
	Stages    []Evidence      `json:"stages"`
	Error     string          `json:"error,omitempty"`
	Status    string          `json:"status"`
	Index     int             `json:"index"`
	NextStage int             `json:"next_stage,omitempty"`
}
type Evidence struct {
	Invalid      map[string]json.RawMessage `json:"invalid,omitempty"`
	Name         string                     `json:"name"`
	Model        string                     `json:"model,omitempty"`
	RequestID    string                     `json:"request_id,omitempty"`
	State        json.RawMessage            `json:"state,omitempty"`
	Questions    map[string]json.RawMessage `json:"questions,omitempty"`
	Response     json.RawMessage            `json:"response,omitempty"`
	Result       json.RawMessage            `json:"result,omitempty"`
	Error        string                     `json:"error,omitempty"`
	InputTokens  int                        `json:"input_tokens,omitempty"`
	OutputTokens int                        `json:"output_tokens,omitempty"`
}
type Summary struct {
	Version      int       `json:"version"`
	ID           string    `json:"id"`
	Path         string    `json:"path"`
	Status       string    `json:"status"`
	Skill        string    `json:"skill,omitempty"`
	Pattern      string    `json:"pattern,omitempty"`
	Provider     string    `json:"provider,omitempty"`
	Model        string    `json:"model,omitempty"`
	Items        int       `json:"items"`
	Completed    int       `json:"completed"`
	Failed       int       `json:"failed"`
	Dropped      int       `json:"dropped"`
	Uncertain    int       `json:"uncertain"`
	Requests     int       `json:"requests"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	Started      time.Time `json:"started"`
	Updated      time.Time `json:"updated"`
}
type definition struct {
	Version int                      `json:"version"`
	Options Options                  `json:"options"`
	Skill   catalog.Skill            `json:"skill"`
	Pattern catalog.Pattern          `json:"pattern"`
	Skills  map[string]catalog.Skill `json:"skills"`
}
type Asset struct {
	Digest      string `json:"digest"`
	ContentType string `json:"content_type"`
}
type asset = Asset
type input struct {
	Prepared Prepared `json:"prepared"`
	Assets   []asset  `json:"assets,omitempty"`
}
type attempt struct {
	Index    int       `json:"index"`
	Stage    string    `json:"stage"`
	Number   int       `json:"number"`
	Status   string    `json:"status"`
	Time     time.Time `json:"time"`
	Evidence Evidence  `json:"evidence,omitempty"`
}
