package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/internal/catalog"
	"github.com/deepnoodle-ai/wonton/tui"
)

func TestNewcomerCanChoosePreviewRunAndReviewWithoutShortcuts(t *testing.T) {
	s := testScreen(t)
	s.skill, s.options.Skill = nil, ""
	s.home()
	var err error
	s.skills, err = catalog.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "note.txt")
	if err = os.WriteFile(path, []byte("restore the session"), 0600); err != nil {
		t.Fatal(err)
	}
	fake := decidetest.NewServer(t)
	var clients atomic.Int32
	s.options.NewClient = func() (*decide.Client, error) { clients.Add(1); return fake.Client(t), nil }
	enter := func() {
		for _, cmd := range s.key(tui.KeyEvent{Key: tui.KeyEnter}) {
			s.HandleEvent(cmd())
		}
	}
	down := func() { s.key(tui.KeyEvent{Key: tui.KeyArrowDown}) }
	enter() // Choose data.
	down()
	enter() // Add a path or URL.
	if s.edit != "add source" {
		t.Fatalf("no visible data editor: %s", s.edit)
	}
	s.submit(path)
	if s.tab != 6 || s.menuActions()[s.index].label != "Choose a judgment" {
		t.Fatal("no next step after adding data")
	}
	enter()
	for i, skill := range s.skills {
		if strings.HasSuffix(skill.Name, "/relevance") {
			s.index = i
		}
	}
	enter()
	if s.tab != 6 || s.menuActions()[s.index].label != "Preview a small sample" {
		t.Fatal("no next step after choosing judgment")
	}
	enter()
	if len(s.sample) != 1 || clients.Load() != 0 {
		t.Fatal("preview should prepare without model work")
	}
	content := s.content()
	if !strings.Contains(content, "Enter runs this sample") || strings.Contains(content, "Digest:") {
		t.Fatalf("preview hides next step or dumps internals: %s", content)
	}
	enter()
	if clients.Load() != 1 || len(s.results) != 1 || s.tab != 3 {
		t.Fatalf("sample workflow failed: %s", s.problem)
	}
	content = s.content()
	if !strings.Contains(content, "ANSWERS") || strings.Contains(content, "QUESTIONS") {
		t.Fatalf("answer overview should defer evidence: %s", content)
	}
	enter()
	if !strings.Contains(s.content(), "QUESTIONS") {
		t.Fatal("full evidence unavailable on request")
	}
	s.key(tui.KeyEvent{Key: tui.KeyEscape})
	s.key(tui.KeyEvent{Key: tui.KeyEscape})
	if s.tab != 6 {
		t.Fatal("Esc should return to main menu")
	}
}

func TestHomeRequiresPreviewBeforeModelWork(t *testing.T) {
	s := testScreen(t)
	s.options.Sources.Sources = []string{"not-read-yet"}
	s.tab, s.index = 6, 3
	if cmds := s.selectMenu(); len(cmds) != 0 || !strings.Contains(s.problem, "Preview the sample first") {
		t.Fatal("model work must require a prepared preview")
	}
}
