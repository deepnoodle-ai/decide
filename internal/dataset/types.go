// Package dataset reads heterogeneous sources as a bounded stream of items.
package dataset

import "encoding/json"

// Options selects sources and bounds ingestion. ItemsSet preserves the distinction
// between no array expansion and an explicit empty pointer selecting the root.
type Options struct {
	Sources        []string `json:"sources,omitempty"`
	Include        []string `json:"include,omitempty"`
	Exclude        []string `json:"exclude,omitempty"`
	Format         string   `json:"format,omitempty"`
	Items          string   `json:"items,omitempty"`
	State          string   `json:"state,omitempty"`
	IDField        string   `json:"id_field,omitempty"`
	Manifest       string   `json:"manifest,omitempty"`
	NoIgnore       bool     `json:"no_ignore,omitempty"`
	FollowSymlinks bool     `json:"follow_symlinks,omitempty"`
	ItemsSet       bool     `json:"items_set,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	Sample         int      `json:"sample,omitempty"`
	Seed           int64    `json:"seed"`
	MaxItemBytes   int64    `json:"max_item_bytes"`
	MaxSourceBytes int64    `json:"max_source_bytes"`
}

func DefaultOptions() Options {
	return Options{Format: "auto", Seed: 1, MaxItemBytes: 16 << 20, MaxSourceBytes: 1 << 30}
}

// Item retains the original JSON value in Data and any selected model state
// separately. Source identity includes location even when an ID field is present.
type Item struct {
	ID     string          `json:"id"`
	Data   json.RawMessage `json:"data"`
	State  json.RawMessage `json:"state,omitempty"`
	Source Source          `json:"source"`
	Images []Image         `json:"images,omitempty"`
}

// Source records the consumed location. URI is an absolute file URI, public HTTP
// URL, or stdin:. Path is relative to the selected directory. Digest hashes the
// complete local file; for streaming HTTP/stdin inputs it hashes this record.
type Source struct {
	URI       string `json:"uri"`
	Path      string `json:"path,omitempty"`
	Format    string `json:"format"`
	Digest    string `json:"digest"`
	Line      int    `json:"line,omitempty"`
	Index     int    `json:"index,omitempty"`
	SizeBytes int64  `json:"size_bytes"`
}
type Image struct {
	ContentType string `json:"content_type"`
	Data        []byte `json:"data"`
}

type manifest struct {
	Version int              `json:"version"`
	Sources []manifestSource `json:"sources"`
}
type manifestSource struct {
	Path    string   `json:"path"`
	URL     string   `json:"url"`
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
	Format  string   `json:"format"`
	State   string   `json:"state"`
	IDField string   `json:"id_field"`
	Items   *string  `json:"items"`
}
