package cli

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const ConfigMaxBytes = 1 << 20

func ReadJSONFile(path string, maxBytes int) (json.RawMessage, error) {
	if maxBytes < 1 || maxBytes > int(^uint(0)>>1)-1 {
		return nil, errors.New("invalid JSON file byte limit")
	}
	if path == "" {
		return nil, errors.New("JSON file path is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", path, maxBytes)
	}
	var raw json.RawMessage
	if err = jsonv2.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return raw, nil
}

func (a *App) offlineFailure(err error) int {
	if err == nil {
		return 0
	}
	status := a.fail(err)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 130
	}
	return status
}

func writeJSON(w io.Writer, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, redact(string(b))+"\n")
	return err
}

func redact(s string) string {
	for _, name := range []string{"TYPESAFE_API_KEY", "CLOUDFLARE_AUTH_TOKEN"} {
		key := strings.TrimSpace(os.Getenv(name))
		if len(key) == 0 {
			continue
		}
		encoded, _ := json.Marshal(key)
		if len(encoded) > 2 {
			s = strings.ReplaceAll(s, string(encoded[1:len(encoded)-1]), "[REDACTED]")
		}
		s = strings.ReplaceAll(s, key, "[REDACTED]")
	}
	return s
}

func (a *App) fail(err error) int {
	if err != nil {
		fmt.Fprintln(a.Err, redact(err.Error()))
	}
	return 2
}
