// Package tui provides interactive terminal input widgets for ws commands.
package tui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// ErrCancelled is returned when the user cancels input with Ctrl+C.
var ErrCancelled = errors.New("cancelled")

const ghostMaxRows = 5

// GhostInput renders a live-filter ghost panel below the prompt line as the
// user types. It is contextual, not autocomplete: the panel shows existing
// entries so the user can pick a distinctive new name or confirm deletion.
//
// Behaviour by mode:
//   - TTY (stdin+stdout are terminals): raw keypress loop with live panel.
//   - Non-TTY (pipe, test, --quiet):    plain buffered line read, no panel.
//
// For new: ghost panel is display-only (Tab does nothing).
// For delete: Tab completes the input to the first matching entry.
type GhostInput struct {
	Prompt      string   // label shown in prompt, e.g. "Name" or "Delete"
	Entries     []string // existing directory names (raw, may include .YYYY-MM suffix)
	TabComplete bool     // if true, Tab key completes to first match
	NoColor     bool
}

// Run reads a name from the user, rendering the ghost panel when possible.
// Returns the trimmed input, or ErrCancelled if the user pressed Ctrl+C.
func (g *GhostInput) Run(in io.Reader, out io.Writer) (string, error) {
	inFile, inOK := in.(*os.File)
	outFile, outOK := out.(*os.File)
	if inOK && outOK && isTTY(inFile.Fd()) && isTTY(outFile.Fd()) {
		return g.runRaw(inFile, outFile)
	}
	return readLine(in)
}

// runRaw is the interactive path: raw-mode keypress loop with ghost panel.
func (g *GhostInput) runRaw(in, out *os.File) (string, error) {
	// Reserve space for the ghost panel BEFORE entering raw mode so the
	// terminal's cooked-mode output processing (ONLCR) handles newlines
	// correctly. If we are near the bottom of the terminal, emitting blank
	// lines causes the terminal to scroll up, guaranteeing ghost panel space.
	const reserve = ghostMaxRows + 1 // 5 entries + 1 overflow line
	for i := 0; i < reserve; i++ {
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "\033[%dA", reserve) // move cursor back up to prompt line

	old, err := setRaw(in.Fd())
	if err != nil {
		// Raw mode unavailable: fall back to plain read.
		return readLine(in)
	}
	defer func() { _ = restoreTermios(in.Fd(), old) }()

	var buf []rune
	// Initial render: cursor is at col 0 of prompt line (blank lines below).
	g.redraw(out, buf)

	rb := make([]byte, 16)
	for {
		n, err := in.Read(rb)
		if err != nil || n == 0 {
			break
		}
		b := rb[:n]

		if n == 1 {
			switch b[0] {
			case '\r', '\n':
				// Enter: commit input.
				g.commit(out, buf)
				return strings.TrimSpace(string(buf)), nil

			case 3: // Ctrl+C
				g.commit(out, buf)
				return "", ErrCancelled

			case 4: // Ctrl+D (treat as commit)
				g.commit(out, buf)
				return strings.TrimSpace(string(buf)), nil

			case 127, 8: // Backspace / DEL
				if len(buf) > 0 {
					buf = buf[:len(buf)-1]
				}

			case '\t':
				if g.TabComplete {
					matches := ghostFilter(g.Entries, string(buf))
					if len(matches) > 0 {
						buf = []rune(ghostStrip(matches[0]))
					}
				}

			default:
				if b[0] >= 32 && b[0] < 127 {
					buf = append(buf, rune(b[0]))
				}
			}
		}
		// Escape sequences (arrow keys, etc.): ignore — n>=3, b[0]==27.

		g.redraw(out, buf)
	}

	g.commit(out, buf)
	return strings.TrimSpace(string(buf)), nil
}

// redraw erases the current prompt+ghost block and redraws it.
// Cursor always ends on the prompt line at the end of the input text.
func (g *GhostInput) redraw(out *os.File, buf []rune) {
	// \r  → col 0 of current line (prompt line, since cursor was left here)
	// \033[J → erase from cursor to end of screen (clears all ghost rows below)
	fmt.Fprint(out, "\r\033[J")

	matches := ghostFilter(g.Entries, string(buf))
	total := len(matches)
	shown := total
	if shown > ghostMaxRows {
		shown = ghostMaxRows
	}

	promptPrefix := g.Prompt + ": "
	fmt.Fprint(out, promptPrefix+string(buf))

	// Ghost rows: rendered below prompt line.
	ghostRows := 0
	for i := 0; i < shown; i++ {
		fmt.Fprintf(out, "\r\n  %s", g.dim(matches[i]))
		ghostRows++
	}
	if total > ghostMaxRows {
		fmt.Fprintf(out, "\r\n  %s", g.dim(fmt.Sprintf("(+ %d more, keep typing\u2026)", total-ghostMaxRows)))
		ghostRows++
	}

	// Move cursor back to prompt line, then to end of input.
	if ghostRows > 0 {
		fmt.Fprintf(out, "\033[%dA", ghostRows)
	}
	col := len(promptPrefix) + len(buf)
	if col > 0 {
		fmt.Fprintf(out, "\r\033[%dC", col)
	} else {
		fmt.Fprint(out, "\r")
	}
}

// commit clears the ghost panel and prints the finalized prompt line.
// Called just before returning from the input loop.
func (g *GhostInput) commit(out *os.File, buf []rune) {
	// \r\033[J: clear prompt line and all ghost rows below.
	fmt.Fprint(out, "\r\033[J")
	// Print the final prompt + value so terminal history shows it.
	// Use \r\n since we are still in raw mode (OPOST output processing varies).
	fmt.Fprintf(out, "%s: %s\r\n", g.Prompt, string(buf))
}

func (g *GhostInput) dim(s string) string {
	if g.NoColor {
		return s
	}
	return "\033[2m" + s + "\033[0m"
}

// ghostFilter returns entries whose base name (date suffix stripped) contains
// all whitespace-separated tokens in the input (case-insensitive, any order).
// When input is empty, all entries are returned.
func ghostFilter(entries []string, input string) []string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		out := make([]string, len(entries))
		copy(out, entries)
		return out
	}
	tokens := strings.Fields(strings.ToLower(trimmed))
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := strings.ToLower(ghostStrip(e))
		if matchAllTokens(name, tokens) {
			out = append(out, e)
		}
	}
	return out
}

// matchAllTokens returns true if haystack contains every token as a substring.
func matchAllTokens(haystack string, tokens []string) bool {
	for _, t := range tokens {
		if !strings.Contains(haystack, t) {
			return false
		}
	}
	return true
}

// ghostStrip removes a trailing .YYYY-MM date suffix for matching purposes.
func ghostStrip(name string) string {
	if idx := strings.LastIndex(name, "."); idx > 0 {
		if suf := name[idx+1:]; len(suf) == 7 && suf[4] == '-' {
			return name[:idx]
		}
	}
	return name
}

// readLine reads one line from r, trimming whitespace. Used as non-TTY fallback.
func readLine(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text()), nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", nil
}

// isTTY reports whether the file descriptor refers to a terminal.
func isTTY(fd uintptr) bool {
	return IsTTY(fd)
}

// IsTTY reports whether the file descriptor refers to a terminal (exported).
func IsTTY(fd uintptr) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}

// setRaw puts the terminal into raw input mode and returns the original state.
// Changes: disable canonical mode, echo, signal generation, extended input,
// CR-to-NL translation, and flow control. Output processing (OPOST/ONLCR)
// is left enabled so \n continues to work normally on output.
func setRaw(fd uintptr) (syscall.Termios, error) {
	var old syscall.Termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&old))); errno != 0 {
		return old, errno
	}
	raw := old
	raw.Lflag &^= syscall.ICANON | syscall.ECHO | syscall.ISIG | syscall.IEXTEN
	raw.Iflag &^= syscall.ICRNL | syscall.IXON
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&raw))); errno != 0 {
		return old, errno
	}
	return old, nil
}

func restoreTermios(fd uintptr, t syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&t)))
	if errno != 0 {
		return errno
	}
	return nil
}
