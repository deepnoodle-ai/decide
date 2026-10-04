//go:build unix

package cache

import (
	"os"
	"syscall"
)

// lock holds an advisory lock on a segment while this process writes it.
// The operating system releases it when the process exits, even after a
// crash.
func lock(f *os.File) { syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) }

// tryLock locks a segment that no process is writing, so it can be merged.
func tryLock(f *os.File) bool {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
}
