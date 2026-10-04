//go:build windows

package cache

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// lockHigh is the high 32 bits of where a segment's lock lies: far past
// any byte a segment holds, since Windows keeps other handles from writing
// a locked range.
const lockHigh = 1 << 30

// tryLock locks a segment that no process is writing, so it can be merged.
// Windows releases a lock when its process exits.
func tryLock(f *os.File) bool {
	ol := windows.Overlapped{OffsetHigh: lockHigh}
	err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol)
	return err == nil
}

// newSegment makes a segment for this process and locks it. Windows can't
// rename an open file, so the segment gets its name at once, and another
// process merging could lock it first. Then that segment is left to the
// merge, and another is made.
func newSegment(dir string) (*os.File, error) {
	for range 5 {
		f, err := os.OpenFile(filepath.Join(dir, segmentName()), os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, err
		}
		if tryLock(f) {
			return f, nil
		}
		f.Close()
	}
	return nil, errors.New("could not lock a new segment")
}

// syncDir does nothing: Windows can't sync a folder, and a rename there is
// written through.
func syncDir(string) error { return nil }
