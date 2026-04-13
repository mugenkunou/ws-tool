package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// GhostMultiInput collects multiple values (tags) one at a time using a ghost
// panel. Each Enter commits the current value and loops. Empty Enter finishes.
// Already-entered values are shown above the prompt and excluded from
// suggestions.
type GhostMultiInput struct {
	Prompt  string   // label, e.g. "Tag"
	Entries []string // available suggestions
	NoColor bool
}

// Run collects tags interactively. Returns the list of entered values.
func (g *GhostMultiInput) Run(in io.Reader, out io.Writer) ([]string, error) {
	inFile, inOK := in.(*os.File)
	outFile, outOK := out.(*os.File)
	if inOK && outOK && isTTY(inFile.Fd()) && isTTY(outFile.Fd()) {
		return g.runRaw(inFile, outFile)
	}
	return g.runPlain(in)
}

func (g *GhostMultiInput) runPlain(in io.Reader) ([]string, error) {
	var result []string
	for {
		line, err := readLine(in)
		if err != nil {
			return result, err
		}
		if line == "" {
			return result, nil
		}
		result = append(result, line)
	}
}

func (g *GhostMultiInput) runRaw(in, out *os.File) ([]string, error) {
	const reserve = ghostMaxRows + 1
	for i := 0; i < reserve; i++ {
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "\033[%dA", reserve)

	old, err := setRaw(in.Fd())
	if err != nil {
		return g.runPlain(in)
	}
	defer func() { _ = restoreTermios(in.Fd(), old) }()

	var collected []string
	var buf []rune

	g.redrawMulti(out, collected, buf)

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
				val := strings.TrimSpace(string(buf))
				if val == "" {
					g.commitMulti(out, collected)
					return collected, nil
				}
				collected = append(collected, val)
				buf = nil

			case 3: // Ctrl+C
				g.commitMulti(out, collected)
				return nil, ErrCancelled

			case 4: // Ctrl+D
				g.commitMulti(out, collected)
				return collected, nil

			case 127, 8: // Backspace
				if len(buf) > 0 {
					buf = buf[:len(buf)-1]
				}

			case '\t':
				available := g.available(collected)
				matches := ghostFilter(available, string(buf))
				if len(matches) > 0 {
					buf = []rune(matches[0])
				}

			default:
				if b[0] >= 32 && b[0] < 127 {
					buf = append(buf, rune(b[0]))
				}
			}
		}

		g.redrawMulti(out, collected, buf)
	}

	g.commitMulti(out, collected)
	return collected, nil
}

// available returns suggestions excluding already-collected values.
func (g *GhostMultiInput) available(collected []string) []string {
	taken := make(map[string]struct{}, len(collected))
	for _, c := range collected {
		taken[strings.ToLower(c)] = struct{}{}
	}
	out := make([]string, 0, len(g.Entries))
	for _, e := range g.Entries {
		if _, ok := taken[strings.ToLower(e)]; !ok {
			out = append(out, e)
		}
	}
	return out
}

func (g *GhostMultiInput) redrawMulti(out *os.File, collected []string, buf []rune) {
	fmt.Fprint(out, "\r\033[J")

	// Show collected tags on the prompt line.
	prefix := g.Prompt + ": "
	collectedStr := ""
	if len(collected) > 0 {
		collectedStr = g.dim("["+strings.Join(collected, ", ")+"] ")
	}
	fmt.Fprint(out, prefix+collectedStr+string(buf))

	// Ghost panel: show available suggestions filtered by current input.
	available := g.available(collected)
	matches := ghostFilter(available, string(buf))
	total := len(matches)
	shown := total
	if shown > ghostMaxRows {
		shown = ghostMaxRows
	}

	ghostRows := 0
	for i := 0; i < shown; i++ {
		fmt.Fprintf(out, "\r\n  %s", g.dim(matches[i]))
		ghostRows++
	}
	if total > ghostMaxRows {
		fmt.Fprintf(out, "\r\n  %s", g.dim(fmt.Sprintf("(+ %d more, keep typing\u2026)", total-ghostMaxRows)))
		ghostRows++
	}

	if ghostRows > 0 {
		fmt.Fprintf(out, "\033[%dA", ghostRows)
	}
	col := len(prefix) + len(collectedStr) + len(buf)
	if col > 0 {
		fmt.Fprintf(out, "\r\033[%dC", col)
	} else {
		fmt.Fprint(out, "\r")
	}
}

func (g *GhostMultiInput) commitMulti(out *os.File, collected []string) {
	fmt.Fprint(out, "\r\033[J")
	if len(collected) > 0 {
		fmt.Fprintf(out, "%s: %s\r\n", g.Prompt, strings.Join(collected, ", "))
	} else {
		fmt.Fprintf(out, "%s: (none)\r\n", g.Prompt)
	}
}

func (g *GhostMultiInput) dim(s string) string {
	if g.NoColor {
		return s
	}
	return "\033[2m" + s + "\033[0m"
}
