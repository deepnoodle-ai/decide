//go:build unix

package cache

import (
	"os"
	"path/filepath"
	"syscall"
)

// tryLock locks a segment that no process is writing, so it can be merged.
// The operating system releases a lock when its process exits, even after
// a crash.
func tryLock(f *os.File) bool {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
}

// newSegment makes a segment for this process. It is locked under a
// hidden name before it is given a segment's name, so no other process
// can take it for idle and merge it away while it is written. Where the
// file system has no locks, segments are never merged, so the lock's
// result does not matter.
func newSegment(dir string) (*os.File, error) {
	name := segmentName()
	tmp := filepath.Join(dir, ".new-"+name)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	tryLock(f)
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		f.Close()
		os.Remove(tmp)
		return nil, err
	}
	return f, nil
}

// syncDir makes a rename in dir durable before the files it replaces are
// removed.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
