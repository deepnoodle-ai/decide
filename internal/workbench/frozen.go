package workbench

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/deepnoodle-ai/decide/internal/catalog"
	"github.com/deepnoodle-ai/decide/internal/jobs"
)

// frozenExport reads execution's recorded definition rather than re-resolving a
// catalog name, connection profile, model alias, or ambient provider settings.
type frozenDefinition struct {
	Version int                      `json:"version"`
	Options jobs.Options             `json:"options"`
	Skill   catalog.Skill            `json:"skill"`
	Pattern catalog.Pattern          `json:"pattern"`
	Skills  map[string]catalog.Skill `json:"skills"`
}

func frozenExport(path string, o jobs.Options, sum jobs.Summary) (jobs.Options, *frozenDefinition, error) {
	if sum.Path == "" {
		return o, nil, nil
	}
	f, err := os.Open(filepath.Join(sum.Path, "config.json"))
	if err != nil {
		return o, nil, fmt.Errorf("read recorded run configuration: %w", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 16<<20+1))
	if err != nil {
		return o, nil, err
	}
	if len(raw) > 16<<20 {
		return o, nil, fmt.Errorf("recorded configuration exceeds 16 MiB export limit")
	}
	var d frozenDefinition
	if err = json.Unmarshal(raw, &d); err != nil {
		return o, nil, err
	}
	if d.Version != 1 {
		return o, nil, fmt.Errorf("unsupported recorded configuration version %d", d.Version)
	}
	if containsCredential(string(raw)) {
		return o, nil, fmt.Errorf("recorded configuration contains a configured provider credential; remove it before exporting")
	}
	o = d.Options
	o.Params = nil
	o.Profile = ""
	o.Model = sum.Model
	if o.Model == "" {
		o.Model = d.Options.Model
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return o, nil, err
	}
	if d.Options.Pattern != "" {
		o.Pattern = filepath.Join(abs, "pattern.json")
		o.SkillDefinition = nil
		references := map[string]string{}
		for i, name := range sortedKeys(d.Skills) {
			references[name] = filepath.Join(abs, "skills", fmt.Sprintf("skill-%d", i))
		}
		p := d.Pattern
		if p.Skill != "" {
			p.Skill = references[p.Skill]
		}
		if p.Branches != nil {
			branches := map[string]string{}
			for k, name := range p.Branches {
				branches[k] = references[name]
			}
			p.Branches = branches
		}
		for i, stage := range p.Stages {
			p.Stages[i].Skill = references[stage.Skill]
		}
		d.Pattern = p
	} else {
		skill := d.Skill
		skill.Resolved = true
		o.SkillDefinition = &skill
		o.Skill = skill.Name
	}
	return o, &d, nil
}

func writeFrozen(path string, d *frozenDefinition) error {
	if d == nil {
		return nil
	}
	write := func(name string, v any) error {
		return os.WriteFile(filepath.Join(path, name), []byte(pretty(v)+"\n"), 0600)
	}
	if err := write("recorded-config.json", d); err != nil {
		return err
	}
	if d.Options.Pattern == "" {
		return nil
	}
	if err := write("pattern.json", d.Pattern); err != nil {
		return err
	}
	for i, name := range sortedKeys(d.Skills) {
		dir := filepath.Join(path, "skills", fmt.Sprintf("skill-%d", i))
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		skill := d.Skills[name]
		skill.Resolved = true
		if err := os.WriteFile(filepath.Join(dir, "skill.json"), []byte(pretty(skill)+"\n"), 0600); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill.Documentation), 0600); err != nil {
			return err
		}
	}
	return nil
}
