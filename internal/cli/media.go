package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/deepnoodle-ai/decide/internal/source"
	"github.com/deepnoodle-ai/decide/internal/template"
	"github.com/deepnoodle-ai/wonton/cli"
)

// providerNames are the providers' names for people.
var providerNames = map[string]string{"typesafe": "TypeSafe", "cloudflare": "Cloudflare", "openai": "OpenAI", "gemini": "Gemini"}

// clefMaxPixels is the most pixels Clef takes in one image.
const clefMaxPixels = 16_000_000

// mediaPlural names a media input's files in the plural, such as "images".
func mediaPlural(in template.Input) string {
	switch in {
	case template.Image:
		return "images"
	case template.PDF:
		return "PDFs"
	}
	return string(in)
}

// walkError explains the errors a walk returns for a file the template
// cannot read, with what to run next.
func walkError(s *template.Template, provider string, err error) error {
	var media *source.MediaError
	var notText *source.NotTextError
	var large *source.TooLargeError
	var invalid *source.InvalidMediaError
	var pixels *source.PixelsError
	switch {
	case errors.As(err, &media):
		return cli.Errorf("%s is %s, and %s reads text", media.Label, source.MediaNoun(media.Kind), s.Name).
			Hint(mediaHint(media.Kind))
	case errors.As(err, &notText):
		return cli.Errorf("%s is not text, and %s reads text", notText.Label, s.Name).
			Hint(textHint(notText.Label))
	case errors.As(err, &large):
		e := cli.Errorf("%s is %s, and %s takes %s files up to %s", large.Label, source.HumanBytes(large.Size),
			providerNames[provider], source.MediaName(large.Input), source.HumanBytes(large.Limit))
		if hint := largerHint(provider, large.Input); hint != "" {
			return e.Hint(hint)
		}
		return e
	case errors.As(err, &pixels):
		e := cli.Errorf("%s is %d×%d pixels, and %s takes images up to %d megapixels",
			pixels.Label, pixels.Width, pixels.Height, providerNames[provider], pixels.Limit/1_000_000)
		return e.Hint(pixelsHint(provider, pixels.Limit))
	case errors.As(err, &invalid):
		return cli.Errorf("%s is %s", invalid.Label, invalid.Reason).Hint(invalidHint(s, invalid.Input))
	}
	return err
}

// invalidHint says what to do with a media file that fails a check, and
// that a folder skips such files.
func invalidHint(s *template.Template, in template.Input) string {
	hint := "Open it to check it, and save it again as " + source.MediaFormats(in) + "."
	if in == template.PDF {
		hint = "Open it in a PDF viewer to check it, and save it again as a PDF."
	}
	return hint + fmt.Sprintf("\nIn a folder, decide skips files like this: decide run %s %s", s.Name, exampleData(s))
}

// pixelsHint names the providers that take larger images than provider,
// and the other way: a smaller image.
func pixelsHint(provider string, most int64) string {
	var names, flags []string
	for _, p := range mediaProviders[template.Image] {
		if p != provider {
			names = append(names, providerNames[p])
			flags = append(flags, "--provider "+p)
		}
	}
	return fmt.Sprintf("%s take larger images.\nRun it with %s, or resize it to %d megapixels or fewer.",
		orJoin(names, "and"), orJoin(flags, "or"), most/1_000_000)
}

// mediaHint says which templates read media files, and how to see one. With
// a kind, it names a built-in template that reads that kind.
func mediaHint(kind template.Input) string {
	hint := "Images, PDFs, audio, and video need a template that reads them."
	switch kind {
	case template.Image:
		return hint + "\nSee one with: decide templates show receipt-quality"
	case template.PDF:
		return hint + "\nSee one with: decide templates show invoice-check"
	case template.Audio, template.Video:
		return hint + fmt.Sprintf("\nMake one with: decide templates new my-%s-check\nThen set \"input\": %q in its template.json.", kind, kind)
	}
	return hint + "\nSee them with: decide templates show receipt-quality, or decide templates show invoice-check"
}

// textKinds are the files a text template reads.
const textKinds = "text, such as .txt, .md, .csv, .jsonl, and source files"

// textHint says what to do with a file that is not text and that no media
// template reads, such as a Word document or a zip file.
func textHint(label string) string {
	switch strings.ToLower(filepath.Ext(label)) {
	case ".docx", ".doc", ".odt", ".rtf", ".pages":
		return "Save it as plain text or Markdown, and run decide on that file."
	case ".xlsx", ".xls", ".ods", ".numbers":
		return "Save it as CSV, and run decide on that file."
	case ".zip", ".gz", ".tgz", ".tar", ".7z", ".rar":
		return "Unpack it, and run decide on the folder."
	}
	return "decide reads " + textKinds + "."
}

// largerHint names the providers that take larger media files than
// provider does, and how to choose one, or "" when none does.
func largerHint(provider string, in template.Input) string {
	limit := mediaLimit(provider, in)
	var larger []string
	for _, p := range mediaProviders[in] {
		if mediaLimit(p, in) > limit {
			larger = append(larger, p)
		}
	}
	if len(larger) == 0 {
		return ""
	}
	// The largest first.
	slices.SortStableFunc(larger, func(a, b string) int {
		la, lb := mediaLimit(a, in), mediaLimit(b, in)
		switch {
		case la > lb:
			return -1
		case la < lb:
			return 1
		}
		return 0
	})
	var takes, flags []string
	for _, p := range larger {
		takes = append(takes, fmt.Sprintf("%s takes up to %s", providerNames[p], source.HumanBytes(mediaLimit(p, in))))
		flags = append(flags, "--provider "+p)
	}
	return orJoin(takes, "and") + ".\nRun it with " + orJoin(flags, "or")
}

// tooLargeNote names the providers that take larger files, once, after a
// walk that found items but skipped some files for their size.
func tooLargeNote(c *cli.Context, s *template.Template, opts source.Options, provider string) {
	if opts.Skipped == nil || opts.Skipped.TooLarge == 0 {
		return
	}
	if hint := largerHint(provider, s.Input); hint != "" {
		fmt.Fprintln(c.Stderr(), dim(strings.ReplaceAll(hint, "\n", " ")))
	}
}

// orJoin joins words as "a, b, or c", with the conjunction given.
func orJoin(words []string, conj string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	case 2:
		return words[0] + " " + conj + " " + words[1]
	}
	return strings.Join(words[:len(words)-1], ", ") + ", " + conj + " " + words[len(words)-1]
}

// mediaCredentials explains a missing key for a media template whose
// provider decide chose: why not TypeSafe, and which keys work. It returns
// nil when --provider chose the provider or its key is set.
func mediaCredentials(c *cli.Context, s *template.Template, provider string) error {
	ok := mediaProviders[s.Input]
	if len(ok) == 0 || c.String("provider") != "" {
		return nil
	}
	if _, err := credentials(provider, false); err == nil {
		return nil
	}
	e := cli.Errorf("%s reads %s, which TypeSafe does not take", s.Name, mediaPlural(s.Input))
	var b strings.Builder
	if len(ok) == 1 {
		fmt.Fprintf(&b, "Set a key for %s, which does:\n", providerNames[ok[0]])
	} else {
		b.WriteString("Set a key for one of these providers, which do:\n")
	}
	for _, p := range ok {
		fmt.Fprintf(&b, "  %-10s  %s\n", providerNames[p], exportLine(p))
	}
	b.WriteString("To check your data first without a key, add --dry-run.")
	return e.Hint(b.String())
}

// exportLine shows how to set a provider's credentials.
func exportLine(provider string) string {
	if provider == "cloudflare" {
		return "export CLOUDFLARE_AUTH_TOKEN=... CLOUDFLARE_ACCOUNT_ID=..."
	}
	return "export " + keyVars[provider] + "=..."
}
