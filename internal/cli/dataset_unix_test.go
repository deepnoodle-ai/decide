//go:build darwin || linux || freebsd || openbsd || netbsd

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSourcesListSkipsFIFOAndRejectsDirectFIFO(t *testing.T) {
	dir := datasetEnvironment(t)
	fifo := filepath.Join(dir, "pipe.txt")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "regular.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	// Release a blocked reader if this regresses, so a failed test cannot leave
	// an active goroutine that prevents test cleanup.
	defer func() {
		fd, err := syscall.Open(fifo, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
		if err == nil {
			syscall.Close(fd)
		}
	}()
	for _, source := range []string{dir, fifo} {
		a, out, diagnostics := coreApp(t, "", nil)
		done := make(chan int, 1)
		go func() { done <- a.Run(t.Context(), []string{"sources", "list", source}) }()
		select {
		case code := <-done:
			if source == dir {
				if code != 0 || !strings.Contains(out.String(), "regular.txt") || strings.Contains(out.String(), "pipe.txt") {
					t.Fatalf("directory: code=%d output=%s diagnostics=%s", code, out, diagnostics)
				}
			} else if code != 2 || !strings.Contains(diagnostics.String(), "not a regular file") {
				t.Fatalf("direct FIFO: code=%d diagnostics=%s", code, diagnostics)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("sources list blocked on a FIFO")
		}
	}
}
