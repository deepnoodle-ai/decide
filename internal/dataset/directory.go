package dataset

import (
	"bufio"
	"container/heap"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Directory enumeration uses a disk merge sort to avoid loading a flat folder
// of millions of files into memory. Each merge opens at most sixteen streams.
type nameIterator struct {
	f         *os.File
	scanner   *bufio.Scanner
	temporary string
	err       error
}

func (it *nameIterator) Next() (string, bool) {
	if !it.scanner.Scan() {
		it.err = it.scanner.Err()
		return "", false
	}
	var name string
	if err := json.Unmarshal(it.scanner.Bytes(), &name); err != nil {
		it.err = err
		return "", false
	}
	return name, true
}
func (it *nameIterator) Close() { _ = it.f.Close(); _ = os.RemoveAll(it.temporary) }
func directoryNames(ctx context.Context, dir string) (*nameIterator, error) {
	temporary, err := os.MkdirTemp("", "decide-directory-")
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(temporary)
		}
	}()
	directory, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	chunks := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		names, readErr := directory.Readdirnames(1024)
		if readErr != nil && readErr != io.EOF {
			return nil, readErr
		}
		if len(names) > 0 {
			sort.Strings(names)
			path := filepath.Join(temporary, fmt.Sprintf("0-%d", chunks))
			f, err := os.Create(path)
			if err != nil {
				return nil, err
			}
			buf := bufio.NewWriter(f)
			for _, name := range names {
				data, _ := json.Marshal(name)
				if _, err = buf.Write(append(data, '\n')); err != nil {
					_ = f.Close()
					return nil, err
				}
			}
			if err = buf.Flush(); err != nil {
				_ = f.Close()
				return nil, err
			}
			if err = f.Close(); err != nil {
				return nil, err
			}
			chunks++
		}
		if readErr == io.EOF {
			break
		}
	}
	if chunks == 0 {
		if err := os.WriteFile(filepath.Join(temporary, "0-0"), nil, 0600); err != nil {
			return nil, err
		}
		chunks = 1
	}
	pass := 0
	for chunks > 1 {
		next := 0
		for first := 0; first < chunks; first += 16 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			last := min(first+16, chunks)
			var paths []string
			for i := first; i < last; i++ {
				paths = append(paths, filepath.Join(temporary, fmt.Sprintf("%d-%d", pass, i)))
			}
			out := filepath.Join(temporary, fmt.Sprintf("%d-%d", pass+1, next))
			if err := mergeNames(ctx, paths, out); err != nil {
				return nil, err
			}
			for _, path := range paths {
				if err := os.Remove(path); err != nil {
					return nil, err
				}
			}
			next++
		}
		chunks = next
		pass++
	}
	f, err := os.Open(filepath.Join(temporary, fmt.Sprintf("%d-0", pass)))
	if err != nil {
		return nil, err
	}
	cleanup = false
	return &nameIterator{f: f, scanner: bufio.NewScanner(f), temporary: temporary}, nil
}

type nameHead struct {
	name  string
	index int
}
type nameHeap []nameHead

func (h nameHeap) Len() int           { return len(h) }
func (h nameHeap) Less(i, j int) bool { return h[i].name < h[j].name }
func (h nameHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *nameHeap) Push(v any)        { *h = append(*h, v.(nameHead)) }
func (h *nameHeap) Pop() any          { old := *h; n := len(old); v := old[n-1]; *h = old[:n-1]; return v }
func mergeNames(ctx context.Context, paths []string, out string) error {
	var files []*os.File
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
	}()
	var scanners []*bufio.Scanner
	heads := &nameHeap{}
	advance := func(index int) error {
		sc := scanners[index]
		if sc.Scan() {
			var name string
			if err := json.Unmarshal(sc.Bytes(), &name); err != nil {
				return err
			}
			heap.Push(heads, nameHead{name, index})
		}
		return sc.Err()
	}
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		files = append(files, f)
		scanners = append(scanners, bufio.NewScanner(f))
		if err := advance(len(scanners) - 1); err != nil {
			return err
		}
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := bufio.NewWriter(f)
	for heads.Len() > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		head := heap.Pop(heads).(nameHead)
		data, _ := json.Marshal(head.name)
		if _, err := buf.Write(append(data, '\n')); err != nil {
			return err
		}
		if err := advance(head.index); err != nil {
			return err
		}
	}
	if err := buf.Flush(); err != nil {
		return err
	}
	return f.Close()
}
