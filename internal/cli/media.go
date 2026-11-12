package cli

import (
	"errors"
	"fmt"
	"path"
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

// mediaKinds lists the media kinds in the order messages name them.
var mediaKinds = []template.Input{template.Image, template.Audio, template.Video, template.PDF}

// mediaRule is the rule for which files a template takes, in two parts:
// one for text templates and one for media templates.
const (
	textTemplateRule  = "A text template also takes an image, audio, video, or PDF file that you name."
	mediaTemplateRule = "A media template takes only media files."
)

// mediaOptions returns the limits for each kind of media file provider
// reads. It is never nil: a provider that reads no media gets an empty
// map, which the walk reads as "send no media".
func mediaOptions(provider string) map[template.Input]source.MediaLimits {
	out := map[template.Input]source.MediaLimits{}
	for _, kind := range mediaKinds {
		if !slices.Contains(mediaProviders[kind], provider) {
			continue
		}
		l := source.MediaLimits{MaxBytes: mediaLimit(provider, kind)}
		if provider == "cloudflare" && kind == template.Image {
			l.MaxPixels = clefMaxPixels
		}
		out[kind] = l
	}
	return out
}

// walkError explains the errors a walk returns for files decide cannot
// read or send, with what to run next. Several named files that cannot be
// sent are listed in one error.
func walkError(s *template.Template, provider string, err error) error {
	var named *source.NamedErrors
	if errors.As(err, &named) {
		const shown = 10
		var b strings.Builder
		var hints []string
		for i, e := range named.Errs {
			label, reason, hint, ok := explain(s, provider, e, true)
			if !ok {
				label, reason = "", clean(e.Error())
			}
			if i < shown {
				if label != "" {
					fmt.Fprintf(&b, "\n  %s: %s", label, reason)
				} else {
					fmt.Fprintf(&b, "\n  %s", reason)
				}
			}
			if hint != "" && !slices.Contains(hints, hint) {
				hints = append(hints, hint)
			}
		}
		if n := len(named.Errs); n > shown {
			fmt.Fprintf(&b, "\n  and %d more", n-shown)
		}
		e := cli.Errorf("%d files you named can't be sent%s", len(named.Errs), b.String())
		if len(hints) > 0 {
			return e.Hint(strings.Join(hints, "\n"))
		}
		return e
	}
	if label, reason, hint, ok := explain(s, provider, err, false); ok {
		e := cli.Errorf("%s is %s", label, reason)
		if hint != "" {
			return e.Hint(hint)
		}
		return e
	}
	return err
}

// explain says why decide cannot read or send a file: its label, the
// reason as words that follow "<file> is", and a hint. In a list of
// several files, the hint names the file instead of saying "it". ok is
// false for an error that is not about one file.
func explain(s *template.Template, provider string, err error, list bool) (label, reason, hint string, ok bool) {
	var (
		stdin       *source.MediaError
		notText     *source.NotTextError
		notRead     *source.ProviderError
		format      *source.FormatError
		large       *source.TooLargeError
		pixels      *source.PixelsError
		unsupported *source.UnsupportedError
		invalid     *source.InvalidMediaError
	)
	switch {
	case errors.As(err, &notRead):
		return clean(notRead.Label), fmt.Sprintf("%s, and %s reads %s", source.MediaNoun(notRead.Kind),
			providerNames[provider], readsText(provider, notRead.Kind)), providerHint(notRead.Kind), true
	case errors.As(err, &format):
		label = clean(format.Label)
		it := "it"
		if list {
			it = label
		}
		if format.ByContent {
			// The file is of a format decide sends; its name does not say so.
			ext := source.MediaExt(format.Kind)
			return label, fmt.Sprintf("%s, and its name has no extension decide reads, such as %s", source.MediaNoun(format.Kind), ext),
				fmt.Sprintf("Name %s with its extension, such as %s%s, and run decide on that file.", it, label, ext), true
		}
		reason = fmt.Sprintf("%s, and decide sends %s only as %s", source.MediaNoun(format.Kind),
			source.MediaPlural(format.Kind), source.MediaFormats(format.Kind))
		return label, reason, saveAsHint(it, format.Kind), true
	case errors.As(err, &large):
		return clean(large.Label), fmt.Sprintf("%s, and %s takes %s files up to %s", source.HumanBytes(large.Size),
			providerNames[provider], source.MediaName(large.Input), source.HumanBytes(large.Limit)), largerHint(provider, large.Input), true
	case errors.As(err, &pixels):
		return clean(pixels.Label), fmt.Sprintf("%d×%d pixels, and %s takes images up to %d megapixels",
			pixels.Width, pixels.Height, providerNames[provider], pixels.Limit/1_000_000), pixelsHint(provider, pixels.Limit), true
	case errors.As(err, &unsupported):
		return clean(unsupported.Label), fmt.Sprintf("not an image, audio, video, or PDF file, and %s reads %s",
			s.Name, source.MediaPlural(unsupported.Input)), "See the templates that read text with: decide templates", true
	case errors.As(err, &invalid):
		return clean(invalid.Label), invalid.Reason, invalidHint(s, invalid.Input), true
	case errors.As(err, &notText):
		return clean(notText.Label), fmt.Sprintf("not text, and %s reads text", s.Name), textHint(notText.Label), true
	case errors.As(err, &stdin):
		return stdin.Label, fmt.Sprintf("%s, and %s reads text", source.MediaNoun(stdin.Kind), s.Name),
			fmt.Sprintf("decide reads images, audio, video, and PDFs only from files you name.\nSave it to a file, and run: decide run %s FILE", s.Name), true
	}
	return "", "", "", false
}

// readsText says what a provider reads, for a file of a kind it does not
// read, such as "text only" or "images, not audio".
func readsText(provider string, kind template.Input) string {
	var kinds []string
	for _, k := range mediaKinds {
		if slices.Contains(mediaProviders[k], provider) {
			kinds = append(kinds, source.MediaPlural(k))
		}
	}
	if len(kinds) == 0 {
		return "text only"
	}
	return orJoin(kinds, "and") + ", not " + source.MediaPlural(kind)
}

// providerHint names the providers that read a kind of media file, and
// how to choose one.
func providerHint(kind template.Input) string {
	ps := mediaProviders[kind]
	names := make([]string, len(ps))
	flags := make([]string, len(ps))
	for i, p := range ps {
		names[i], flags[i] = providerNames[p], "--provider "+p
	}
	verb := "run"
	if kind == template.Audio {
		verb = "runs"
	}
	what := source.MediaPlural(kind)
	return fmt.Sprintf("%s%s %s on %s.\nRun it with %s", strings.ToUpper(what[:1]), what[1:], verb, orJoin(names, "or"), orJoin(flags, "or"))
}

// saveAsHint says to save a file in a format decide sends.
func saveAsHint(it string, kind template.Input) string {
	if kind == template.PDF {
		return "Save " + it + " as a PDF, and run decide on that file."
	}
	return "Save " + it + " as " + source.MediaFormats(kind) + ", and run decide on that file."
}

// invalidHint says what to do with a media file that fails a check, and,
// for a media template, that a folder skips such files.
func invalidHint(s *template.Template, in template.Input) string {
	hint := "Open it to check it, and save it again as " + source.MediaFormats(in) + "."
	if in == template.PDF {
		hint = "Open it in a PDF viewer to check it, and save it again as a PDF."
	}
	if !s.Input.Media() {
		return hint
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

// namedMediaHint says how to send the media files a text template's
// folder walk skipped: name them. It is "" when the walk skipped none.
func namedMediaHint(s *template.Template, provider string, skipped *source.Skipped) string {
	if skipped == nil || skipped.FirstMedia == "" {
		return ""
	}
	kind := skipped.FirstMediaKind
	if !slices.Contains(mediaProviders[kind], provider) {
		provider = mediaProviders[kind][0]
	}
	label := filepath.ToSlash(skipped.FirstMedia)
	glob := path.Join(path.Dir(label), "*"+path.Ext(label))
	return fmt.Sprintf("In a folder, %s reads only text. To send the %s, name them:\ndecide run %s %s --provider %s",
		s.Name, source.MediaPlural(kind), s.Name, clean(glob), provider)
}

// textKinds are the files a text template reads.
const textKinds = "text, such as .txt, .md, .csv, .jsonl, and source files"

// textHint says what to do with a file that is not text and not media,
// such as a Word document or a zip file.
func textHint(label string) string {
	switch strings.ToLower(filepath.Ext(label)) {
	case ".docx", ".doc", ".odt", ".rtf", ".pages":
		return "Save it as plain text or Markdown, and run decide on that file."
	case ".xlsx", ".xls", ".ods", ".numbers":
		return "Save it as CSV, and run decide on that file."
	case ".zip", ".gz", ".tgz", ".tar", ".7z", ".rar":
		return "Unpack it, and run decide on the folder."
	}
	return "decide reads " + textKinds + ", and the images, audio, video, and PDFs you name."
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
// walk that found items but skipped some files for their size in bytes or
// in pixels.
func tooLargeNote(c *cli.Context, s *template.Template, opts source.Options, provider string) {
	if hint := skippedHint(s, opts, provider); hint != "" {
		fmt.Fprintln(c.Stderr(), dim(strings.ReplaceAll(hint, "\n", " ")))
	}
}

// skippedHint is the hint for files a walk skipped for their size: in
// bytes first, then in pixels. It is "" when none was skipped, or no
// provider takes larger files.
func skippedHint(s *template.Template, opts source.Options, provider string) string {
	switch {
	case opts.Skipped == nil:
	case opts.Skipped.TooLarge > 0:
		return largerHint(provider, s.Input)
	case opts.Skipped.Pixels > 0:
		return pixelsHint(provider, opts.Media[template.Image].MaxPixels)
	}
	return ""
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
	e := cli.Errorf("%s reads %s, which TypeSafe does not take", s.Name, source.MediaPlural(s.Input))
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
