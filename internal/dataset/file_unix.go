//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly || solaris || aix

package dataset

import (
	"os"
	"syscall"
)

func openSourceFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
