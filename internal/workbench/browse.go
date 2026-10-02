package workbench

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/deepnoodle-ai/wonton/tui"
)

const directoryWindow = 100

type directoryPage struct {
	Path         string
	Entries      []os.DirEntry
	Offset, Next int
	More         bool
}

// Read a bounded window in filesystem order. Sorting the window makes browsing
// pleasant without materializing every name in a million-entry directory.
func readDirectory(ctx context.Context, path string, offset int) (*directoryPage, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		abs = filepath.Dir(abs)
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	p := &directoryPage{Path: abs, Offset: offset}
	seen := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries, err := f.ReadDir(128)
		for _, entry := range entries {
			if entry.Name() == ".git" || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			if seen < offset {
				seen++
				continue
			}
			if len(p.Entries) == directoryWindow {
				p.More = true
				p.Next = seen
				break
			}
			p.Entries = append(p.Entries, entry)
			seen++
		}
		if p.More || err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if !p.More {
		p.Next = seen
	}
	sort.Slice(p.Entries, func(i, j int) bool {
		a, b := p.Entries[i], p.Entries[j]
		if a.IsDir() != b.IsDir() {
			return a.IsDir()
		}
		return a.Name() < b.Name()
	})
	return p, nil
}

func (s *screen) browse(path string, offset int) []tui.Cmd {
	if s.busy {
		return nil
	}
	if strings.Contains(path, "://") {
		s.status = "URLs stay in your source selection. p previews their response; j explores JSON."
		return nil
	}
	s.status = "Opening the folder trail. Directory pages read names, not file contents."
	return []tui.Cmd{s.background(func(ctx context.Context) update {
		p, err := readDirectory(ctx, path, offset)
		return update{kind: "browse", browser: p, err: err}
	})}
}

func (s *screen) browserKey(e tui.KeyEvent) []tui.Cmd {
	if e.Rune == 'q' {
		s.quitting = true
		s.cancel()
		return []tui.Cmd{tui.Quit()}
	}
	if e.Rune >= '1' && e.Rune <= '5' {
		s.browser = nil
		return s.key(e)
	}
	if s.busy {
		return nil
	}
	p := s.browser
	switch e.Key {
	case tui.KeyArrowUp:
		s.index = max(0, s.index-1)
	case tui.KeyArrowDown:
		s.index = min(max(0, len(p.Entries)-1), s.index+1)
	case tui.KeyArrowLeft, tui.KeyBackspace:
		return s.browse(filepath.Dir(p.Path), 0)
	case tui.KeyEnter:
		if len(p.Entries) > 0 {
			e := p.Entries[s.index]
			if e.IsDir() {
				return s.browse(filepath.Join(p.Path, e.Name()), 0)
			}
			s.status = "Space adds this file; p previews your selected sources."
		}
	}
	switch e.Rune {
	case ' ':
		if len(p.Entries) > 0 {
			s.addBrowsedSource(filepath.Join(p.Path, p.Entries[s.index].Name()))
		}
	case '.':
		s.addBrowsedSource(p.Path)
	case 'n':
		if p.More {
			return s.browse(p.Path, p.Next)
		}
	case 'N':
		return s.browse(p.Path, max(0, p.Offset-directoryWindow))
	case 'p':
		s.browser = nil
		return s.key(e)
	case '?':
		s.browser = nil
		return s.key(e)
	}
	return nil
}
func (s *screen) addBrowsedSource(path string) {
	for _, selected := range s.options.Sources.Sources {
		abs, _ := filepath.Abs(selected)
		if abs == path {
			s.status = "Already on your trail: " + path
			return
		}
	}
	s.options.Sources.Sources = append(s.options.Sources.Sources, path)
	s.sample, s.prepared = nil, nil
	s.status = fmt.Sprintf("Added %s · %d sources packed for your next flight.", filepath.Base(path), len(s.options.Sources.Sources))
}
func (s *screen) browserContent() string {
	p := s.browser
	var b strings.Builder
	fmt.Fprintf(&b, "FOLDER TRAIL\n%s\n\nEnter opens a folder · Space adds selected · . adds this folder\n← goes up · n/N pages · Esc returns to sources\n\n", p.Path)
	start := max(0, s.index-4)
	end := min(len(p.Entries), start+max(5, s.height-14))
	for i := start; i < end; i++ {
		e := p.Entries[i]
		kind := "file"
		if e.IsDir() {
			kind = "dir/"
		}
		fmt.Fprintf(&b, "%s %-5s %s\n", cursor(i, s.index), kind, e.Name())
	}
	if len(p.Entries) == 0 {
		b.WriteString("No entries here. ← follows the trail back up.\n")
	}
	fmt.Fprintf(&b, "\nWindow %d–%d · more: %t\nWindows are sorted locally; ignore rules apply when previewing.\nNo files have been sent to a model.", p.Offset+1, p.Offset+len(p.Entries), p.More)
	return b.String()
}
func (s *screen) sampleLabel() string {
	if s.randomSample {
		return fmt.Sprintf("seeded %d-item sample (scans the selection)", sampleSize(s.options))
	}
	return fmt.Sprintf("first %d items (stops early)", sampleSize(s.options))
}
func (s *screen) cyclePreset() {
	presets := [][]string{nil, {"**/*.{go,py,js,ts,tsx,rs,java,rb,sh}"}, {"**/*.{json,jsonl}"}, {"**/*.{png,jpg,jpeg,webp}"}}
	names := []string{"all files", "code trail", "JSON orchard", "image gallery"}
	n := 0
	for i, p := range presets {
		if strings.Join(p, "|") == strings.Join(s.options.Sources.Include, "|") {
			n = (i + 1) % len(presets)
			break
		}
	}
	s.options.Sources.Include = presets[n]
	s.sample, s.prepared = nil, nil
	s.status = "Selection preset: " + names[n] + ". i adds a custom glob; p previews."
}
