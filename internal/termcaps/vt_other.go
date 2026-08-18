//go:build !windows

package termcaps

import "os"

// enableVTOutput is a no-op everywhere but Windows: a POSIX terminal already
// interprets escape sequences on its output stream.
func enableVTOutput(*os.File) (func(), error) { return func() {}, nil }
