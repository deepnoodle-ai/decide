//go:build live

// Live tests call the real API. Run with:
//
//	TYPESAFE_API_KEY=... go test -tags live -run Live ./...
package cli

import (
	"bytes"
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestLiveDemoFlags runs the templates the Claude Code plugin depends on
// over their demo fixtures, and checks that the planted problems are
// flagged and nothing else is, as demo/README.md says.
func TestLiveDemoFlags(t *testing.T) {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		t.Skip("TYPESAFE_API_KEY is not set")
	}
	demo, err := filepath.Abs("../../demo")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DECIDE_HOME", t.TempDir())
	for _, tc := range []struct {
		template, data string
		flagged        []int  // the lines the template should flag, from 1
		question       string // a question whose yes the plugin reads alone
		over           []int  // the lines where that yes is at least 80%
	}{
		{"command-risk", "commands.txt", []int{5, 6, 7, 8, 9}, "severe", []int{6, 7, 9}},
	} {
		t.Run(tc.template, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			var stdout, stderr bytes.Buffer
			app := &App{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr}
			if code := app.Run(ctx, []string{"run", tc.template, filepath.Join(demo, tc.data), "--format", "csv"}); code != 0 {
				t.Fatalf("exit %d: %s", code, stderr.String())
			}
			rows, err := csv.NewReader(&stdout).ReadAll()
			if err != nil || len(rows) < 2 || rows[0][2] != "flagged" {
				t.Fatalf("csv: %v %v", err, rows)
			}
			var got []int
			for i, row := range rows[1:] {
				if row[2] == "true" {
					got = append(got, i+1)
				}
			}
			if !slices.Equal(got, tc.flagged) {
				t.Errorf("flagged lines %v, want %v\n%v", got, tc.flagged, rows)
			}
			col := slices.Index(rows[0], tc.question)
			if col < 0 {
				t.Fatalf("no %s column: %v", tc.question, rows[0])
			}
			got = nil
			for i, row := range rows[1:] {
				if p, err := strconv.ParseFloat(row[col], 64); err == nil && p >= 0.8 {
					got = append(got, i+1)
				}
			}
			if !slices.Equal(got, tc.over) {
				t.Errorf("%s at least 80%% on lines %v, want %v\n%v", tc.question, got, tc.over, rows)
			}
		})
	}
}
