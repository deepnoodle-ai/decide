//go:build !unix

package cache

import "os"

// Without file locks, no process can tell whether another is writing a
// segment, so segments are never merged.

func lock(*os.File) {}

func tryLock(*os.File) bool { return false }
