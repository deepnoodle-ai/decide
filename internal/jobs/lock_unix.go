//go:build darwin || linux || freebsd || openbsd || netbsd

package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Advisory locks are released by the OS on process exit, including crashes.
func lock(path string) (func(), error) {
	p := filepath.Join(path, "execution.lock")
	f, e := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, fmt.Errorf("run is locked by another executor (%s)", p)
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
