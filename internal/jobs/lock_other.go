//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd

package jobs

import (
	"fmt"
	"os"
	"path/filepath"
)

func lock(path string) (func(), error) {
	p := filepath.Join(path, "execution.lock")
	if e := os.Mkdir(p, 0700); e != nil {
		return nil, fmt.Errorf("run is locked; check for an active executor before removing %s", p)
	}
	return func() { os.Remove(p) }, nil
}
