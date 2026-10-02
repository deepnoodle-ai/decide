//go:build !unix

package runs

func lock(string) (func(), error) { return func() {}, nil }

// Active reports whether a process is executing the run. Without file
// locks it cannot tell, so it assumes the run is not active.
func (r *Run) Active() bool { return false }
