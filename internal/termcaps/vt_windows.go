//go:build windows

package termcaps

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// enableVTOutput turns on ENABLE_VIRTUAL_TERMINAL_PROCESSING for the console
// attached to out, returning a function that puts the mode back.
//
// Without it a Windows console prints the query bytes instead of interpreting
// them, so a failure here means "do not write the batch at all" rather than
// "write it and hope".
func enableVTOutput(out *os.File) (func(), error) {
	h := windows.Handle(out.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return nil, errors.New("console mode unavailable (" + err.Error() + ")")
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return func() {}, nil
	}
	if err := windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
		return nil, errors.New("console does not support VT output (" + err.Error() + ")")
	}
	return func() { _ = windows.SetConsoleMode(h, mode) }, nil
}
