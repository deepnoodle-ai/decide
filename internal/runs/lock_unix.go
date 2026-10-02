//go:build unix

package runs

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// errBusy reports a run that another decide process is executing.
var errBusy = errors.New("this run is already running in another terminal")

// lock holds an advisory lock on the run while it executes. The operating
// system releases it when the process exits, even after a crash.
func lock(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errBusy
	}
	return func() { f.Close() }, nil
}

// Active reports whether a process is executing the run.
func (r *Run) Active() bool {
	unlock, err := lock(r.Dir)
	if err != nil {
		return errors.Is(err, errBusy)
	}
	unlock()
	return false
}
