//go:build !windows

package reporter

import (
	"os"

	"golang.org/x/sys/unix"
)

// terminalWidth returns the column width of the controlling terminal for the
// given file, or 0 when the file is not a terminal or the ioctl fails
func terminalWidth(f *os.File) int {
	if f == nil {
		return 0
	}
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0
	}
	return int(ws.Col)
}
