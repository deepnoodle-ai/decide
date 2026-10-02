package main

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestInterruptExitsWithStdinPipeHeldOpen(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "decide")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	cmd := exec.Command(binary, "plan", "relevance", "-", "--format", "jsonl")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	// An offline plan produces one line without API access. Its
	// output confirms signal setup completed; stdin stays open after this line.
	if _, err = stdin.Write([]byte("\"a useful record\"\n")); err != nil {
		t.Fatal(err)
	}
	ready := make(chan error, 1)
	go func() { _, err := bufio.NewReader(stdout).ReadString('\n'); ready <- err }()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process did not become ready")
	}
	if err = cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 130 {
			t.Fatalf("exit=%v, want 130", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-exited
		t.Fatal("SIGINT did not exit while stdin remained open")
	}
}
