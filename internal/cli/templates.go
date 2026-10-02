package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/deepnoodle-ai/decide/internal/template"
	"github.com/deepnoodle-ai/wonton/cli"
)

func (a *App) addTemplates(app *cli.App) {
	g := app.Group("templates").
		Alias("template").
		Description("List, explain, and create templates").
		Run(a.templatesList)
	g.Command("list").
		Description("List the templates you can run").
		Run(a.templatesList)
	g.Command("show").
		Description("Explain what a template asks and how to run it").
		AddArg(&cli.Arg{Name: "template", Description: "The template to explain, like sentiment", Required: true}).
		Run(a.templatesShow)
	g.Command("new").
		Description("Create your own template").
		Long(`Create a template in ~/.decide/templates/NAME, ready to edit.

A template is a template.json file with a name, a description, an input type
(file, record, or image), and one or more questions. Start from scratch or
copy an existing template with --from.

Examples:
  decide templates new my-routing --from ticket-routing
  decide templates new my-template --project`).
		AddArg(&cli.Arg{Name: "name", Description: "A name for the new template, like my-triage", Required: true}).
		Flags(
			cli.String("from").Help("Start from a copy of this template"),
			cli.Bool("project").Help("Save it in this folder's .decide/templates, to share with your team"),
		).
		Run(a.templatesNew)
}

func (a *App) templatesList(c *cli.Context) error {
	all, broken, err := template.List()
	if err != nil {
		return err
	}
	width := 0
	for _, s := range all {
		width = max(width, len(s.Name))
	}
	w := c.Stdout()
	fmt.Fprintf(w, "%s\n\n", bold("Templates"))
	for _, s := range all {
		where := ""
		if s.Location != template.BuiltIn {
			where = "  " + dim("("+s.Location+")")
		}
		fmt.Fprintf(w, "  %-*s  %s%s\n", width, s.Name, clean(s.Description), where)
	}
	for _, err := range broken {
		fmt.Fprintf(c.Stderr(), "\n%s\n", failed("Could not load a template: "+clean(err.Error())))
	}
	fmt.Fprintf(w, "\n%s  decide templates show sentiment\n", dim("Learn about one:"))
	fmt.Fprintf(w, "%s          echo \"I love it\" | decide run sentiment\n", dim("Try one:"))
	fmt.Fprintf(w, "%s         decide templates new NAME\n", dim("Make one:"))
	return nil
}

func (a *App) templatesShow(c *cli.Context) error {
	original, err := loadTemplate(c.Arg(0))
	if err != nil {
		return err
	}
	s := original.With(nil) // show questions with default values filled in
	w := c.Stdout()
	fmt.Fprintf(w, "%s  %s\n", bold(s.Name), dim("("+where(s)+")"))
	fmt.Fprintf(w, "%s\n\n", clean(s.Description))
	fmt.Fprintf(w, "%s %s\n\n", bold("Reads:"), reads(s))

	fmt.Fprintf(w, "%s\n", bold("Questions"))
	m := marksOf(s)
	for _, q := range s.Questions {
		var body struct {
			Instructions any             `json:"instructions"`
			Criteria     json.RawMessage `json:"criteria"`
		}
		json.Unmarshal(q.Raw, &body)
		fmt.Fprintf(w, "\n  %s  %s\n", value(clean(q.Key)), dim("answered "+describe(q.Raw)))
		fmt.Fprint(w, wrap(clean(fmt.Sprint(body.Instructions)), 72, "    "))
		var levels []any
		var options template.Questions // a choice's options, in order
		switch {
		case json.Unmarshal(body.Criteria, &levels) == nil:
			for i, l := range levels {
				fmt.Fprintf(w, "    %s %s\n", dim(fmt.Sprintf("%d", i)), clean(fmt.Sprint(l)))
			}
		case json.Unmarshal(body.Criteria, &options) == nil:
			width := 0
			for _, o := range options {
				width = max(width, len(o.Key))
			}
			for _, o := range options {
				var desc any
				json.Unmarshal(o.Raw, &desc)
				fmt.Fprintf(w, "    %s  %s\n", dim(fmt.Sprintf("%-*s", width, clean(o.Key))), clean(fmt.Sprint(desc)))
			}
		}
		if when := flagText(m.flags[q.Key]); when != "" {
			fmt.Fprintf(w, "    %s %s\n", failed("flagged when"), when)
		}
		if when := flagText(m.matches[q.Key]); when != "" {
			fmt.Fprintf(w, "    %s %s\n", good("matches when"), when)
		}
	}

	if len(s.Parameters) > 0 {
		fmt.Fprintf(w, "\n%s\n", bold("Parameters"))
		for _, name := range s.ParameterNames() {
			p := s.Parameters[name]
			fmt.Fprintf(w, "\n  %s  %s\n", value(name), clean(p.Description))
			if p.Default != "" {
				fmt.Fprintf(w, "    %s %s\n", dim("default:"), clean(p.Default))
			} else {
				fmt.Fprintf(w, "    %s\n", dim("required"))
			}
		}
	}
	if docs := strings.TrimSpace(s.Docs); docs != "" {
		fmt.Fprintf(w, "\n%s\n\n", bold("Notes"))
		for _, line := range strings.Split(docs, "\n") {
			if line == "" {
				fmt.Fprintln(w)
				continue
			}
			fmt.Fprintf(w, "  %s\n", printable(line))
		}
	}

	fmt.Fprintf(w, "\n%s\n", bold("Run it"))
	fmt.Fprintf(w, "  decide run %s %s%s\n", s.Name, exampleData(s), exampleParams(s))
	if s.Location == template.BuiltIn {
		fmt.Fprintf(w, "\n%s\n", bold("Make your own version"))
		fmt.Fprintf(w, "  decide templates new my-%s --from %s\n", s.Name, s.Name)
	} else {
		fmt.Fprintf(w, "\n%s\n  %s\n", bold("Edit it"), clean(filepath.Join(s.Dir, "template.json")))
	}
	return nil
}

// reads says what a template reads and what one item is.
func reads(s *template.Template) string {
	switch {
	case s.Input == template.Image:
		return "images, one item per PNG, JPEG, or WebP file"
	case s.Each != "":
		return fmt.Sprintf("text, one item per %s (change it with --each)", s.Each)
	}
	return "text, one item per record of JSONL, JSON, or CSV, per line of .txt or stdin, and per file otherwise (change it with --each)"
}

func where(s *template.Template) string {
	switch s.Location {
	case template.BuiltIn:
		return "built-in"
	case template.User:
		return "your template"
	}
	return "project template"
}

func exampleParams(s *template.Template) string {
	var b strings.Builder
	for _, name := range s.ParameterNames() {
		if s.Parameters[name].Default == "" {
			fmt.Fprintf(&b, ` -p %s="..."`, name)
		}
	}
	return b.String()
}

func (a *App) templatesNew(c *cli.Context) error {
	name := c.Arg(0)
	var from *template.Template
	if src := c.String("from"); src != "" {
		var err error
		if from, err = loadTemplate(src); err != nil {
			return err
		}
	}
	path, err := template.Create(name, from, c.Bool("project"))
	if err != nil {
		return err
	}
	w := c.Stdout()
	fmt.Fprintf(w, "%s %s\n\n", good("Created"), path)
	fmt.Fprintf(w, "Edit it to describe your questions, then check it with:\n")
	fmt.Fprintf(w, "  decide templates show %s\n", name)
	fmt.Fprintf(w, "  decide run %s %s --dry-run\n", name, exampleData(from))
	return nil
}
