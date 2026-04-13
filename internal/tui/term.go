package tui

import (
	"fmt"
	"io"
	"syscall"
	"unsafe"
)

// TermSize holds the terminal dimensions in rows and columns.
type TermSize struct {
	Rows uint16
	Cols uint16
}

// GetTermSize queries the terminal size via TIOCGWINSZ ioctl.
func GetTermSize(fd uintptr) (TermSize, error) {
	var ws struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		return TermSize{}, errno
	}
	return TermSize{Rows: ws.Row, Cols: ws.Col}, nil
}

// HideCursor hides the terminal cursor.
func HideCursor(w io.Writer) { fmt.Fprint(w, "\033[?25l") }

// ShowCursor shows the terminal cursor.
func ShowCursor(w io.Writer) { fmt.Fprint(w, "\033[?25h") }

// ClearScreen clears the entire screen and moves cursor to top-left.
func ClearScreen(w io.Writer) { fmt.Fprint(w, "\033[2J\033[H") }

// MoveTo moves the cursor to the given row and column (1-based).
func MoveTo(w io.Writer, row, col int) { fmt.Fprintf(w, "\033[%d;%dH", row, col) }

// ClearLine erases the current line.
func ClearLine(w io.Writer) { fmt.Fprint(w, "\033[2K") }

// AltScreen switches to the alternate screen buffer.
func AltScreen(w io.Writer) { fmt.Fprint(w, "\033[?1049h") }

// MainScreen switches back to the main screen buffer.
func MainScreen(w io.Writer) { fmt.Fprint(w, "\033[?1049l") }
