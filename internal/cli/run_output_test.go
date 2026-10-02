package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide/internal/dataset"
	"github.com/deepnoodle-ai/decide/internal/jobs"
)

func decisionFixture() jobs.Result {
	return jobs.Result{Status: "complete", Source: dataset.Source{Path: "cmd/decide/main.go"}, Stages: []jobs.Evidence{{Name: "map", Response: json.RawMessage(`{"answers":{"risk":{"type":"noul","noul":0.31},"maintainability":{"type":"score","score":2.7,"confidence":0.68,"legend":{"0":"Hard to follow","1":"Fragile responsibilities","2":"Understandable with some friction","3":"Clear responsibilities","4":"Exceptionally clear"},"probabilities":{"0":0,"1":0,"2":0.3,"3":0.7,"4":0}}}}`)}}}
}

func TestDecisionOutputShowsAnswersBeforeMetadata(t *testing.T) {
	var out bytes.Buffer
	printer := newResultPrinter(&out, "never", false)
	if err := printer.result(decisionFixture()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"cmd/decide/main.go", "maintainability  2.70 / 4", "risk             31% yes"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q:\n%s", want, out.String())
		}
	}
	for _, unwanted := range []string{"confidence", "legend", "probabilities", "Clear responsibilities", "\x1b"} {
		if strings.Contains(out.String(), unwanted) {
			t.Fatalf("overview contains %q: %s", unwanted, out.String())
		}
	}
	out.Reset()
	printer.details = true
	if err := printer.result(decisionFixture()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Confidence: 68%", "70%", "3 · Clear responsibilities", "31% yes"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing detail %q: %s", want, out.String())
		}
	}
	if strings.Contains(out.String(), `"0":`) {
		t.Fatal("details must use labeled rows, not legend JSON")
	}
}

func TestDecisionColorsAreExplicitAndDoNotChangeData(t *testing.T) {
	var plain, colored bytes.Buffer
	if err := newResultPrinter(&plain, "never", false).result(decisionFixture()); err != nil {
		t.Fatal(err)
	}
	if err := newResultPrinter(&colored, "always", false).result(decisionFixture()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(colored.String(), "\x1b[0;1mcmd/decide/main.go") || !strings.Contains(colored.String(), "\x1b[0;36m31% yes") || !strings.Contains(colored.String(), "\x1b[0;2mmaintainability") {
		t.Fatalf("missing visual hierarchy: %q", colored.String())
	}
	t.Setenv("NO_COLOR", "1")
	auto := newResultPrinter(&bytes.Buffer{}, "auto", false)
	if auto.color {
		t.Fatal("NO_COLOR must disable automatic color")
	}
	if newResultPrinter(&bytes.Buffer{}, "auto", false).color {
		t.Fatal("a pipe/buffer must be plain")
	}
}

func TestDecisionOutputChoiceStageAndControlCharacters(t *testing.T) {
	var out bytes.Buffer
	r := jobs.Result{Status: "complete", Source: dataset.Source{Path: "ticket\x1b[31m.json", Line: 7}, Stages: []jobs.Evidence{
		{Name: "route", Response: json.RawMessage(`{"answers":{"queue":{"type":"choice","choice":"security","probabilities":{"security":0.76,"billing":0.24},"confidence":0.76}}}`)},
		{Name: "risk", Response: json.RawMessage(`{"answers":{"risk":{"type":"noul","noul":0.2}}}`)},
	}}
	if err := newResultPrinter(&out, "never", false).result(r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".json:7", "route", "security · 76% probability", "20% yes"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("untrusted source text injected an escape sequence")
	}
}
