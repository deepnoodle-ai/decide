package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/deepnoodle-ai/decide/internal/skill"
	"github.com/deepnoodle-ai/wonton/cli"
)

func (a *App) addSkills(app *cli.App) {
	g := app.Group("skills").
		Alias("skill").
		Description("List, explain, and create skills").
		Run(a.skillsList)
	g.Command("list").
		Description("List the skills you can run").
		Run(a.skillsList)
	g.Command("show").
		Description("Explain what a skill asks and how to run it").
		AddArg(&cli.Arg{Name: "skill", Description: "The skill to explain, like sentiment", Required: true}).
		Run(a.skillsShow)
	g.Command("new").
		Description("Create your own skill").
		Long(`Create a skill in ~/.decide/skills/NAME, ready to edit.

A skill is a skill.json file with a name, a description, an input type
(file, record, or image), and one or more questions. Start from scratch or
copy an existing skill with --from.

Examples:
  decide skills new support-triage --from ticket-routing
  decide skills new my-skill --project`).
		AddArg(&cli.Arg{Name: "name", Description: "A name for the new skill, like my-triage", Required: true}).
		Flags(
			cli.String("from").Help("Start from a copy of this skill"),
			cli.Bool("project").Help("Save it in this folder's .decide/skills, to share with your team"),
		).
		Run(a.skillsNew)
}

func (a *App) skillsList(c *cli.Context) error {
	all, broken, err := skill.List()
	if err != nil {
		return err
	}
	width := 0
	for _, s := range all {
		width = max(width, len(s.Name))
	}
	w := c.Stdout()
	fmt.Fprintf(w, "%s\n\n", bold("Skills"))
	for _, s := range all {
		where := ""
		if s.Location != skill.BuiltIn {
			where = "  " + dim("("+s.Location+")")
		}
		fmt.Fprintf(w, "  %-*s  %s%s\n", width, s.Name, s.Description, where)
	}
	for _, err := range broken {
		fmt.Fprintf(c.Stderr(), "\n%s\n", failed("Could not load a skill: "+err.Error()))
	}
	fmt.Fprintf(w, "\n%s  decide skills show sentiment\n", dim("Learn about one:"))
	fmt.Fprintf(w, "%s          echo \"I love it\" | decide run sentiment\n", dim("Try one:"))
	fmt.Fprintf(w, "%s         decide skills new NAME\n", dim("Make one:"))
	return nil
}

func (a *App) skillsShow(c *cli.Context) error {
	original, err := loadSkill(c.Arg(0))
	if err != nil {
		return err
	}
	s := original.With(nil) // show questions with default values filled in
	w := c.Stdout()
	fmt.Fprintf(w, "%s  %s\n", bold(s.Name), dim("("+where(s)+")"))
	fmt.Fprintf(w, "%s\n\n", s.Description)
	fmt.Fprintf(w, "%s %s\n\n", bold("Reads:"), inputs[s.Input])

	fmt.Fprintf(w, "%s\n", bold("Questions"))
	flags := flagsOf(s)
	for _, q := range s.Questions {
		var body struct {
			Instructions any             `json:"instructions"`
			Criteria     json.RawMessage `json:"criteria"`
		}
		json.Unmarshal(q.Raw, &body)
		fmt.Fprintf(w, "\n  %s  %s\n", value(q.Key), dim("answered "+describe(q.Raw)))
		fmt.Fprint(w, wrap(fmt.Sprint(body.Instructions), 72, "    "))
		var levels []any
		var options skill.Questions // a choice's options, in order
		switch {
		case json.Unmarshal(body.Criteria, &levels) == nil:
			for i, l := range levels {
				fmt.Fprintf(w, "    %s %s\n", dim(fmt.Sprintf("%d", i)), fmt.Sprint(l))
			}
		case json.Unmarshal(body.Criteria, &options) == nil:
			width := 0
			for _, o := range options {
				width = max(width, len(o.Key))
			}
			for _, o := range options {
				var desc any
				json.Unmarshal(o.Raw, &desc)
				fmt.Fprintf(w, "    %s %s\n", dim(fmt.Sprintf("%-*s", width, o.Key)), fmt.Sprint(desc))
			}
		}
		if when := flagText(flags[q.Key]); when != "" {
			fmt.Fprintf(w, "    %s %s\n", failed("flagged when"), when)
		}
	}

	if len(s.Parameters) > 0 {
		fmt.Fprintf(w, "\n%s\n", bold("Parameters"))
		for _, name := range s.ParameterNames() {
			p := s.Parameters[name]
			fmt.Fprintf(w, "\n  %s  %s\n", value(name), p.Description)
			if p.Default != "" {
				fmt.Fprintf(w, "    %s %s\n", dim("default:"), p.Default)
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
			fmt.Fprintf(w, "  %s\n", line)
		}
	}

	fmt.Fprintf(w, "\n%s\n", bold("Run it"))
	fmt.Fprintf(w, "  decide run %s %s%s\n", s.Name, exampleData(s.Input), exampleParams(s))
	if s.Location == skill.BuiltIn {
		fmt.Fprintf(w, "\n%s\n", bold("Make your own version"))
		fmt.Fprintf(w, "  decide skills new my-%s --from %s\n", s.Name, s.Name)
	} else {
		fmt.Fprintf(w, "\n%s\n  %s\n", bold("Edit it"), filepath.Join(s.Dir, "skill.json"))
	}
	return nil
}

var inputs = map[skill.Input]string{
	skill.File:   "files, one item per file",
	skill.Record: "records, one item per line of text or JSONL, or per JSON array element",
	skill.Image:  "images, one item per PNG, JPEG, or WebP file",
}

func where(s *skill.Skill) string {
	switch s.Location {
	case skill.BuiltIn:
		return "built-in"
	case skill.User:
		return "your skill"
	}
	return "project skill"
}

func exampleParams(s *skill.Skill) string {
	var b strings.Builder
	for _, name := range s.ParameterNames() {
		if s.Parameters[name].Default == "" {
			fmt.Fprintf(&b, ` -p %s="..."`, name)
		}
	}
	return b.String()
}

func (a *App) skillsNew(c *cli.Context) error {
	name := c.Arg(0)
	var from *skill.Skill
	if src := c.String("from"); src != "" {
		var err error
		if from, err = loadSkill(src); err != nil {
			return err
		}
	}
	path, err := skill.Create(name, from, c.Bool("project"))
	if err != nil {
		return err
	}
	w := c.Stdout()
	fmt.Fprintf(w, "%s %s\n\n", good("Created"), path)
	fmt.Fprintf(w, "Edit it to describe your questions, then check it with:\n")
	fmt.Fprintf(w, "  decide skills show %s\n", name)
	fmt.Fprintf(w, "  decide run %s %s --dry-run\n", name, exampleData(skillInput(from)))
	return nil
}

func skillInput(s *skill.Skill) skill.Input {
	if s == nil {
		return skill.Record
	}
	return s.Input
}
