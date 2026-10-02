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
	return json.NewEncoder(w).Encode(value)
}

func (a *App) offlineFlags(command string, collection bool) (*flag.FlagSet, *CommonOptions) {
	fs := a.flags(command)
	return fs, BindCommon(fs, command, false, collection)
}
