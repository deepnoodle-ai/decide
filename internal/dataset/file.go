package dataset

import (
	"fmt"
	"os"
)

// Check before opening: opening a FIFO can block even before an f.Stat check.
// Unix opens are also nonblocking so replacing a checked file with a FIFO
// cannot make the subsequent open wait for a writer.
func openRegularFile(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("source %s is not a regular file", path)
	}
	f, err := openSourceFile(path)
	if err != nil {
		return nil, err
	}
	info, err = f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("source %s is not a regular file", path)
	}
	return f, nil
}
