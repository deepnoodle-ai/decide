package jobs

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func runRoot(dir string) string {
	if dir != "" {
		return dir
	}
	if d := os.Getenv("DECIDE_RUNS_DIR"); d != "" {
		return d
	}
	return ".decide/runs"
}

func pathFor(id, dir string) string {
	if _, err := os.Stat(filepath.Join(id, "summary.json")); err == nil {
		return id
	}
	return filepath.Join(runRoot(dir), id)
}

func itemPath(path, kind string, i int) string {
	return filepath.Join(path, kind, fmt.Sprintf("%012d.json", i))
}

func writeJSON(path string, v any) error {
	b, e := json.Marshal(v)
	b = []byte(sanitize(string(b)))
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".write-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(name, path)
}

func readJSON(path string, v any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}

func saveSummary(s *Summary) error {
	s.Updated = time.Now().UTC()
	return writeJSON(filepath.Join(s.Path, "summary.json"), s)
}

func Show(id, dir string) (Summary, error) {
	var s Summary
	e := readJSON(filepath.Join(pathFor(id, dir), "summary.json"), &s)
	if e == nil && s.Version != 1 {
		e = fmt.Errorf("unsupported run summary version %d", s.Version)
	}
	return s, e
}

func List(dir string) ([]Summary, error) {
	es, e := os.ReadDir(runRoot(dir))
	if os.IsNotExist(e) {
		return []Summary{}, nil
	}
	if e != nil {
		return nil, e
	}
	out := []Summary{}
	for _, ent := range es {
		if !ent.IsDir() {
			continue
		}
		s, e := Show(ent.Name(), dir)
		if e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	return out, nil
}

func ReadResults(id, dir string, fn func(Result) error) error {
	path := pathFor(id, dir)
	var summary Summary
	if err := readJSON(filepath.Join(path, "summary.json"), &summary); err != nil {
		return err
	}
	if summary.Version != 1 {
		return fmt.Errorf("unsupported run summary version %d", summary.Version)
	}
	for i := 0; i < summary.Items; i++ {
		var r Result
		err := readJSON(itemPath(path, "results", i), &r)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if r.Version != 1 {
			return fmt.Errorf("unsupported run result version %d", r.Version)
		}
		if fn != nil {
			if err = fn(r); err != nil {
				return err
			}
		}
	}
	return nil
}

func Export(id, dir string, w io.Writer) error {
	enc := json.NewEncoder(w)
	return ReadResults(id, dir, func(r Result) error { return enc.Encode(r) })
}

func appendAttempt(path string, a attempt) error {
	f, e := os.OpenFile(filepath.Join(path, "attempts.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	b, me := json.Marshal(a)
	if me != nil {
		f.Close()
		return me
	}
	_, e = f.Write(append([]byte(sanitize(string(b))), '\n'))
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	return e
}
