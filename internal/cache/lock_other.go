//go:build !unix && !windows

package cache

import (
	"os"
	"path/filepath"
)

// Without file locks, no process can tell whether another is writing a
// segment, so segments are never merged.

func tryLock(*os.File) bool { return false }

func newSegment(dir string) (*os.File, error) {
	return os.OpenFile(filepath.Join(dir, segmentName()), os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0o600)
}

func syncDir(string) error { return nil }
