package templates_test

import (
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/template"
	"github.com/deepnoodle-ai/decide/templates"
)

func TestInput(t *testing.T) {
	if got := templates.ReceiptQuality.Input(); got != decide.MediaImage {
		t.Errorf("ReceiptQuality.Input() = %q", got)
	}
	if got := templates.InvoiceCheck.Input(); got != decide.MediaPDF {
		t.Errorf("InvoiceCheck.Input() = %q", got)
	}
	for _, tmpl := range templates.All() {
		cli, err := template.LoadBuiltin(tmpl.Name())
		if err != nil {
			t.Fatal(err)
		}
		want := decide.MediaKind(cli.Input)
		if !cli.Input.Media() {
			want = ""
		}
		if got := tmpl.Input(); got != want {
			t.Errorf("%s.Input() = %q, want %q", tmpl.Name(), got, want)
		}
	}
}
