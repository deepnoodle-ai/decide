package workbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/deepnoodle-ai/wonton/tui"
)

type jsonBranch struct {
	Name, Pointer string
	Raw           json.RawMessage
}
type jsonFrame struct {
	Pointer       string
	Raw           json.RawMessage
	Offset, Index int
}
type jsonExplorer struct {
	Trail       []jsonFrame
	Entries     []jsonBranch
	Index, Next int
	More        bool
}

func pointerChild(parent, key string) string {
	return parent + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

// Only retain a bounded child window, rather than decoding a whole nested
// dataset into interface maps. Child values remain raw until opened.
func jsonChildren(raw json.RawMessage, pointer string, offset int) ([]jsonBranch, int, bool, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	t, err := d.Token()
	if err != nil {
		return nil, offset, false, err
	}
	delim, ok := t.(json.Delim)
	if !ok || (delim != '{' && delim != '[') {
		return nil, offset, false, nil
	}
	var children []jsonBranch
	i := 0
	for d.More() {
		key := strconv.Itoa(i)
		if delim == '{' {
			t, err := d.Token()
			if err != nil {
				return nil, i, false, err
			}
			key = t.(string)
		}
		if i >= offset+100 {
			return children, i, true, nil
		}
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			return nil, i, false, err
		}
		if i >= offset {
			children = append(children, jsonBranch{Name: key, Pointer: pointerChild(pointer, key), Raw: v})
		}
		i++
	}
	return children, i, false, nil
}
func (s *screen) openJSON() {
	var raw json.RawMessage
	if s.tab == 2 && len(s.prepared) > 0 {
		raw = s.prepared[min(s.index, len(s.prepared)-1)].Item.Data
	}
	if s.tab == 3 && len(s.results) > 0 {
		raw = s.results[min(s.index, len(s.results)-1)].Data
	}
	if !json.Valid(raw) {
		s.status = "Preview an item with p, then j follows its JSON branches."
		return
	}
	j := &jsonExplorer{Trail: []jsonFrame{{Raw: raw}}}
	s.jsonTrail = j
	s.refreshJSON()
	s.scroll = 0
}
func (s *screen) refreshJSON() {
	j := s.jsonTrail
	f := j.Trail[len(j.Trail)-1]
	rows, next, more, err := jsonChildren(f.Raw, f.Pointer, f.Offset)
	if err != nil {
		s.problem = err.Error()
		return
	}
	j.Entries, j.Next, j.More = rows, next, more
	j.Index = min(f.Index, max(0, len(rows)-1))
	s.problem = ""
}
func (s *screen) jsonKey(e tui.KeyEvent) []tui.Cmd {
	if e.Rune == 'q' {
		s.quitting = true
		s.cancel()
		return []tui.Cmd{tui.Quit()}
	}
	j := s.jsonTrail
	switch e.Key {
	case tui.KeyPageDown:
		s.scroll += max(1, s.height-12)
	case tui.KeyPageUp:
		s.scroll = max(0, s.scroll-max(1, s.height-12))
	case tui.KeyArrowUp:
		j.Index = max(0, j.Index-1)
	case tui.KeyArrowDown:
		j.Index = min(max(0, len(j.Entries)-1), j.Index+1)
	case tui.KeyArrowLeft, tui.KeyBackspace:
		if len(j.Trail) > 1 {
			j.Trail = j.Trail[:len(j.Trail)-1]
			s.refreshJSON()
		}
	case tui.KeyEnter, tui.KeyArrowRight:
		if len(j.Entries) > 0 {
			v := j.Entries[j.Index]
			used := len(v.Raw)
			for _, f := range j.Trail {
				used += len(f.Raw)
			}
			if used > 32<<20 || len(j.Trail) >= 64 {
				s.problem = "Branch trail reached its memory/depth limit. ← goes up; map a shallower array with m."
				return nil
			}
			j.Trail[len(j.Trail)-1].Index = j.Index
			j.Trail = append(j.Trail, jsonFrame{Pointer: v.Pointer, Raw: v.Raw})
			s.refreshJSON()
		}
	}
	switch e.Rune {
	case 'n':
		if j.More {
			j.Trail[len(j.Trail)-1].Offset = j.Next
			j.Trail[len(j.Trail)-1].Index = 0
			s.refreshJSON()
		}
	case 'N':
		j.Trail[len(j.Trail)-1].Offset = max(0, j.Trail[len(j.Trail)-1].Offset-100)
		j.Trail[len(j.Trail)-1].Index = 0
		s.refreshJSON()
	case 'm', 'M', 'u', 'I':
		if s.target != "" || s.busy {
			s.status = "Recorded data is read-only. Esc returns to evidence."
			return nil
		}
		f := j.Trail[len(j.Trail)-1]
		if len(j.Entries) > 0 && e.Rune != 'M' {
			v := j.Entries[j.Index]
			f.Pointer, f.Raw = v.Pointer, v.Raw
		}
		if e.Rune == 'm' || e.Rune == 'M' {
			if len(bytes.TrimSpace(f.Raw)) == 0 || bytes.TrimSpace(f.Raw)[0] != '[' {
				s.problem = "Choose an array to expand into items. Enter opens a branch; ← goes up."
				return nil
			}
			s.options.Sources.Items, s.options.Sources.ItemsSet = f.Pointer, true
			s.options.Sources.State = ""
		} else if e.Rune == 'I' {
			s.options.Sources.IDField = f.Pointer
		} else {
			s.options.Sources.State = f.Pointer
		}
		s.sample, s.prepared = nil, nil
		s.jsonTrail = nil
		s.problem = ""
		s.status = fmt.Sprintf("Mapping set to %q. p prepares the new items; j lets you keep exploring.", f.Pointer)
	}
	return nil
}
func (s *screen) jsonContent() string {
	j := s.jsonTrail
	f := j.Trail[len(j.Trail)-1]
	path := f.Pointer
	if path == "" {
		path = "(root)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "JSON BRANCHES\n%s\n\nEnter opens · ← goes up · n/N pages · Esc returns\nm selected array · M this array · u state · I record ID\n\n", path)
	start := max(0, j.Index-4)
	end := min(len(j.Entries), start+max(5, s.height-15))
	for i := start; i < end; i++ {
		v := j.Entries[i]
		fmt.Fprintf(&b, "%s %s  %s\n", cursor(i, j.Index), v.Name, jsonShape(v.Raw))
	}
	if len(j.Entries) == 0 {
		fmt.Fprintf(&b, "VALUE\n%s", previewJSON(f.Raw))
	} else {
		v := j.Entries[j.Index]
		fmt.Fprintf(&b, "\nPointer: %s\n%s\n", v.Pointer, jsonShape(v.Raw))
	}
	fmt.Fprintf(&b, "\nChild window %d–%d · more: %t\nPointers are relative to each input item. Expand arrays before mapping their rows.", f.Offset+1, j.Next, j.More)
	return b.String()
}
func jsonShape(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "empty"
	}
	switch raw[0] {
	case '{':
		return fmt.Sprintf("{ object } · %d bytes", len(raw))
	case '[':
		return fmt.Sprintf("[ array ] · %d bytes", len(raw))
	}
	text := string(raw[:min(len(raw), 90)])
	if len(raw) > 90 {
		text += "…"
	}
	return strings.ReplaceAll(text, "\n", " ")
}
