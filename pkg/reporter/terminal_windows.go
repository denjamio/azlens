//go:build windows

package reporter

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

type consoleScreenBufferInfo struct {
	dwSize              windows.Coord
	dwCursorPosition    windows.Coord
	wAttributes         uint16
	srWindow            windows.SmallRect
	dwMaximumWindowSize windows.Coord
}

// terminalWidth returns the visible column width of the console attached to
// the given file, or 0 when unavailable
func terminalWidth(f *os.File) int {
	if f == nil {
		return 0
	}
	var info consoleScreenBufferInfo
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleScreenBufferInfo")
	r1, _, _ := proc.Call(uintptr(f.Fd()), uintptr(unsafe.Pointer(&info)))
	if r1 == 0 {
		return 0
	}
	return int(info.srWindow.Right-info.srWindow.Left) + 1
}
