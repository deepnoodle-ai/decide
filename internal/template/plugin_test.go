package template

import (
	"os"
	"regexp"
	"slices"
	"strconv"
	"testing"
)

// TestPluginTemplatesAreBuiltin checks plugin/hooks/templates.ts against the
// templates: each one the Claude Code plugin runs is built in, asks each
// question the plugin reads, and flags it where the plugin does.
func TestPluginTemplatesAreBuiltin(t *testing.T) {
	src, err := os.ReadFile("../../plugin/hooks/templates.ts")
	if err != nil {
		t.Fatal(err)
	}
	isolate(t)
	entries := regexp.MustCompile(`name: '([a-z-]+)', flags: \{([^}]*)\}`).FindAllStringSubmatch(string(src), -1)
	if len(entries) == 0 {
		t.Fatal("plugin/hooks/templates.ts names no templates")
	}
	for _, m := range entries {
		tmpl, err := Load(m[1])
		if err != nil || tmpl.Location != BuiltIn {
			t.Errorf("the plugin runs %s, which is not a built-in template: %v", m[1], err)
			continue
		}
		for _, f := range regexp.MustCompile(`([a-z_]+): (null|[0-9.]+)`).FindAllStringSubmatch(m[2], -1) {
			name, at := f[1], f[2]
			if !slices.ContainsFunc(tmpl.Questions, func(q Question) bool { return q.Key == name }) {
				t.Errorf("the plugin reads %s from %s, which does not ask it", name, m[1])
				continue
			}
			flag, isFlagged := tmpl.Flags[name]
			if at == "null" {
				if isFlagged {
					t.Errorf("%s flags %s, but the plugin never does", m[1], name)
				}
				continue
			}
			want, _ := strconv.ParseFloat(at, 64)
			conds, err := flag.Conditions("noul")
			if !isFlagged || err != nil || len(conds) != 1 || conds[0].Answer != "yes" || conds[0].Op != ">=" || conds[0].Value != want {
				t.Errorf("%s flags %s as %v, but the plugin flags it at yes >= %v", m[1], name, flag, want)
			}
		}
	}
}
