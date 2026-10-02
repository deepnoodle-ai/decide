package cli

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
)

const calibrationFileLimit = 64 << 20

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

func readCalibrationFile(path string, value any) error {
	if path == "" {
		return fmt.Errorf("a calibration file path is required")
	}
	raw, err := ReadJSONFile(path, calibrationFileLimit)
	if err != nil {
		return err
	}
	if err := jsonv2.Unmarshal(raw, value, jsonv2.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func writeJSON(w io.Writer, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, redact(string(b))+"\n")
	return err
}

func (a *App) offlineFlags(command string, collection bool) (*flag.FlagSet, *CommonOptions) {
	fs := a.flags(command)
	return fs, BindCommon(fs, command, false, collection)
}
