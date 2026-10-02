//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly && !solaris && !aix

package dataset

import "os"

func openSourceFile(path string) (*os.File, error) { return os.Open(path) }
