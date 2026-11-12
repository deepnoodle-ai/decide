package source

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide/internal/template"
)

func TestNamedMediaWithAnyTemplate(t *testing.T) {
	tree(t, map[string]string{
		"notes.txt":   "great\nbad\nok\n",
		"shot.png":    pngOf(2, 2),
		"call.mp3":    "ID3\x03\x00\x00\x00\x00\x00\x00",
		"receipt.pdf": asciiPDF,
		"photo.gif":   "GIF89a\x01\x00\x01\x00\x80\x00\x00",
		"shot":        pngOf(1, 1),
		"docs/a.png":  pngOf(1, 1),
		"docs/b.md":   "# b\n",
	})
	gemini := map[template.Input]MediaLimits{template.Image: {}, template.Audio: {}, template.Video: {}, template.PDF: {}}
	openai := map[template.Input]MediaLimits{template.Image: {}}

	// Text and media in one run, in the order named.
	items, _ := walk(t, []string{"notes.txt", "shot.png"}, "", Options{Input: template.Text, Media: gemini, Provider: "Gemini"})
	var units []string
	for _, it := range items {
		units = append(units, it.Unit)
	}
	if !reflect.DeepEqual(units, []string{"line", "line", "line", "image"}) ||
		string(items[3].State) != `{"content_type":"image/png","path":"shot.png"}` {
		t.Fatalf("units = %v, state = %s", units, items[len(items)-1].State)
	}

	// A folder gives a text template text only, whatever the provider reads.
	var skipped Skipped
	items, warnings := walk(t, []string{"docs"}, "", Options{Input: template.Text, Media: gemini, Skipped: &skipped})
	if got := labels(items); !reflect.DeepEqual(got, []string{"docs/b.md"}) ||
		!reflect.DeepEqual(warnings, []string{"Skipped 1 file that is not text: docs/a.png"}) ||
		!reflect.DeepEqual(skipped.Media, []SkippedMedia{{Label: "docs/a.png", Kind: template.Image}}) {
		t.Fatalf("labels = %v, warnings = %q, skipped = %+v", got, warnings, skipped)
	}

	// A media template takes a named media file of another kind.
	items, _ = walk(t, []string{"receipt.pdf"}, "", Options{Input: template.Image, Media: gemini})
	if len(items) != 1 || items[0].Unit != "pdf" {
		t.Fatalf("receipt.pdf: %+v", items)
	}

	walkErr := func(paths []string, opts Options) error {
		return Walk(context.Background(), paths, nil, opts, func(Item) error {
			t.Fatalf("%v: an item was read before the check", paths)
			return nil
		})
	}
	// A kind the provider does not read.
	var pe *ProviderError
	err := walkErr([]string{"notes.txt", "call.mp3"}, Options{Input: template.Text, Media: openai, Provider: "OpenAI"})
	if !errors.As(err, &pe) || pe.Label != "call.mp3" || pe.Kind != template.Audio || pe.Provider != "OpenAI" {
		t.Fatalf("call.mp3: %v", err)
	}
	// An empty map reads no media, as on TypeSafe.
	err = walkErr([]string{"shot.png"}, Options{Input: template.Text, Media: map[template.Input]MediaLimits{}, Provider: "TypeSafe"})
	if !errors.As(err, &pe) || pe.Kind != template.Image {
		t.Fatalf("shot.png on TypeSafe: %v", err)
	}
	// Formats decide does not send, by name and by content.
	var fe *FormatError
	if err := walkErr([]string{"photo.gif"}, Options{Input: template.Text, Media: gemini}); !errors.As(err, &fe) || fe.ByContent {
		t.Fatalf("photo.gif: %v", err)
	}
	if err := walkErr([]string{"shot"}, Options{Input: template.Text, Media: gemini}); !errors.As(err, &fe) || !fe.ByContent || fe.Kind != template.Image {
		t.Fatalf("shot: %v", err)
	}
	// A media template takes only media files.
	var ue *UnsupportedError
	if err := walkErr([]string{"notes.txt"}, Options{Input: template.Image, Media: gemini}); !errors.As(err, &ue) || ue.Input != template.Image {
		t.Fatalf("notes.txt: %v", err)
	}
	// Every failing named file is reported, in order, even past --limit.
	var ne *NamedErrors
	err = walkErr([]string{"call.mp3", "shot.png", "photo.gif", "receipt.pdf"},
		Options{Input: template.Text, Media: openai, Provider: "OpenAI", Limit: 1})
	if !errors.As(err, &ne) || len(ne.Errs) != 3 {
		t.Fatalf("named errors: %v", err)
	}
	for i, want := range []string{"call.mp3", "photo.gif", "receipt.pdf"} {
		if !strings.HasPrefix(ne.Errs[i].Error(), want+" ") {
			t.Errorf("error %d = %v, want %s", i, ne.Errs[i], want)
		}
	}
}
