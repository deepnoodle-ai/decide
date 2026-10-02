package dataset

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
)

// contextReader checks cancellation and enforces total source bytes. Reader
// cancellation cannot interrupt an arbitrary caller-owned blocking Read; HTTP
// requests carry the context and local reads are checked between reads.
type contextReader struct {
	ctx    context.Context
	r      io.Reader
	n, max int64
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	remaining := r.max - r.n
	if remaining < 0 {
		return 0, fmt.Errorf("source exceeds --max-source-bytes (%d)", r.max)
	}
	if int64(len(p)) > remaining+1 {
		p = p[:remaining+1]
	}
	n, err := r.r.Read(p)
	r.n += int64(n)
	if r.n > r.max {
		return n, fmt.Errorf("source exceeds --max-source-bytes (%d)", r.max)
	}
	return n, err
}
func detectFormat(path, ctype string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return "json"
	case ".jsonl", ".ndjson":
		return "jsonl"
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return "image"
	}
	media, _, _ := mime.ParseMediaType(ctype)
	if media == "application/json" {
		return "json"
	}
	if media == "application/x-ndjson" || media == "application/jsonl" {
		return "jsonl"
	}
	if strings.HasPrefix(media, "image/") {
		return "image"
	}
	return "text"
}
func readSource(ctx context.Context, r io.Reader, uri, path string, opts Options, emit func(Item) error) error {
	return readSourceDigest(ctx, r, uri, path, "", opts, emit)
}
func readSourceDigest(ctx context.Context, r io.Reader, uri, path, sourceDigest string, opts Options, emit func(Item) error) error {
	format := opts.Format
	if format == "" || format == "auto" {
		format = detectFormat(uri, "")
		if uri == "stdin:" {
			format = "jsonl"
		}
	}
	if (opts.ItemsSet || opts.Items != "") && format != "json" {
		return fmt.Errorf("--items requires JSON format")
	}
	switch format {
	case "json", "jsonl", "text", "lines", "image":
	default:
		return fmt.Errorf("unknown source format %q", format)
	}
	bounded := &contextReader{ctx: ctx, r: r, max: opts.MaxSourceBytes}
	var index int
	makeItem := func(raw []byte, line int, images []Image) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		index++
		if int64(len(raw)) > opts.MaxItemBytes {
			return fmt.Errorf("item in %s exceeds --max-item-bytes (%d)", uri, opts.MaxItemBytes)
		}
		var data json.RawMessage
		if format == "text" || format == "lines" {
			data, _ = json.Marshal(string(raw))
		} else {
			data = append(json.RawMessage(nil), raw...)
		}
		if !json.Valid(data) {
			return fmt.Errorf("invalid JSON in %s at line %d", uri, line)
		}
		location := fmt.Sprintf("%d:%d", line, index)
		if opts.IDField != "" {
			value, err := Pointer(data, opts.IDField)
			if err != nil {
				return fmt.Errorf("id field in %s: %w", uri, err)
			}
			location += ":" + string(value)
		}
		var state json.RawMessage
		if opts.State != "" {
			var err error
			state, err = Pointer(data, opts.State)
			if err != nil {
				return fmt.Errorf("state in %s: %w", uri, err)
			}
		}
		fingerprint := raw
		if len(images) > 0 {
			fingerprint = images[0].Data
		}
		d := sourceDigest
		if d == "" {
			d = digest(fingerprint)
		}
		item := Item{ID: digest([]byte(uri + "\x00" + location + "\x00" + digest(fingerprint))), Data: data, State: state, Source: Source{URI: uri, Path: path, Format: format, Digest: d, Line: line, Index: index, SizeBytes: int64(len(fingerprint))}, Images: images}
		return emit(item)
	}
	if format == "jsonl" || format == "lines" {
		br := bufio.NewReaderSize(bounded, 64<<10)
		line := 0
		for {
			raw, err := readLine(br, opts.MaxItemBytes)
			if err != nil && !errors.Is(err, io.EOF) {
				return fmt.Errorf("read %s: %w", uri, err)
			}
			if len(raw) > 0 || err == nil {
				line++
				raw = bytes.TrimSuffix(raw, []byte{'\n'})
				raw = bytes.TrimSuffix(raw, []byte{'\r'})
				if format == "lines" || len(bytes.TrimSpace(raw)) > 0 {
					if e := makeItem(raw, line, nil); e != nil {
						return e
					}
				}
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
		}
	}
	// Whole file formats deliberately fail rather than silently truncate.
	raw, err := io.ReadAll(io.LimitReader(bounded, opts.MaxItemBytes+1))
	if err != nil {
		return fmt.Errorf("read %s: %w", uri, err)
	}
	if int64(len(raw)) > opts.MaxItemBytes {
		return fmt.Errorf("item in %s exceeds --max-item-bytes (%d)", uri, opts.MaxItemBytes)
	}
	if format == "image" {
		ctype := http.DetectContentType(raw)
		if !strings.HasPrefix(ctype, "image/") {
			return fmt.Errorf("source %s is not a supported image", uri)
		}
		state, _ := json.Marshal(map[string]any{"path": path, "uri": uri, "content_type": ctype, "size_bytes": len(raw)})
		return makeItem(state, 0, []Image{{ContentType: ctype, Data: raw}})
	}
	if format == "json" && (opts.ItemsSet || opts.Items != "") {
		selected, err := Pointer(raw, opts.Items)
		if err != nil {
			return fmt.Errorf("items in %s: %w", uri, err)
		}
		selected = bytes.TrimSpace(selected)
		var values []json.RawMessage
		if err = json.Unmarshal(selected, &values); err != nil || len(selected) == 0 || selected[0] != '[' {
			return fmt.Errorf("items pointer in %s must select an array", uri)
		}
		for _, value := range values {
			if err := makeItem(value, 0, nil); err != nil {
				return err
			}
		}
		return nil
	}
	return makeItem(raw, 0, nil)
}
func readLine(r *bufio.Reader, max int64) ([]byte, error) {
	var data []byte
	for {
		part, err := r.ReadSlice('\n')
		if int64(len(data)+len(part)) > max+2 {
			return nil, fmt.Errorf("line exceeds --max-item-bytes (%d)", max)
		}
		data = append(data, part...)
		if err != bufio.ErrBufferFull {
			return data, err
		}
	}
}

// Pointer resolves an RFC 6901 JSON pointer without modifying the original item.
func Pointer(data json.RawMessage, pointer string) (json.RawMessage, error) {
	if !json.Valid(data) {
		return nil, fmt.Errorf("invalid JSON")
	}
	if pointer == "" {
		if !json.Valid(data) {
			return nil, fmt.Errorf("invalid JSON")
		}
		return append(json.RawMessage(nil), data...), nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("JSON pointer must start with /")
	}
	var value any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	for _, token := range strings.Split(pointer[1:], "/") {
		for i := 0; i < len(token); i++ {
			if token[i] == '~' {
				if i+1 >= len(token) || (token[i+1] != '0' && token[i+1] != '1') {
					return nil, fmt.Errorf("invalid JSON pointer escape")
				}
				i++
			}
		}
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch v := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = v[token]
			if !ok {
				return nil, fmt.Errorf("JSON pointer member %q not found", token)
			}
		case []any:
			i, err := strconv.Atoi(token)
			if err != nil || i < 0 || i >= len(v) || (len(token) > 0 && (token[0] == '+' || token[0] == '-')) || (len(token) > 1 && token[0] == '0') {
				return nil, fmt.Errorf("JSON pointer index %q invalid", token)
			}
			value = v[i]
		default:
			return nil, fmt.Errorf("JSON pointer traverses a scalar")
		}
	}
	return json.Marshal(value)
}
