package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/mugenkunou/ws-tool/internal/agenttmux"
)

// `ws session <args…>` is agent-tmux, run with the same arguments: ws has no
// session commands, flags, help or JSON of its own, so the two never drift.
// agent-tmux replaces the ws process (exec), so the terminal and signals reach
// it directly.

// execProgram replaces the process with argv[0]. Tests swap it, since a real
// exec would replace the test binary.
var execProgram = syscall.Exec

// sessionArgs returns the words after `session` when args are `ws [global
// flags] session …`. Every word after it, flags included, belongs to
// agent-tmux. ok is false for any other command.
func sessionArgs(args []string) (rest []string, ok bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "session":
			return args[i+1:], true
		case a == "-w" || a == "--workspace":
			i++ // its value
		case strings.HasPrefix(a, "-") && a != "-" && a != "--":
			// another global flag before the command
		default:
			return nil, false
		}
	}
	return nil, false
}

// runSessionPassthrough execs agent-tmux with args. It returns only when the
// exec fails: 127 when agent-tmux is not installed.
func runSessionPassthrough(args []string, stderr io.Writer) int {
	bin, err := agenttmux.Path()
	if err != nil {
		fmt.Fprintln(stderr, "ws session runs agent-tmux, which is not installed. Install it:")
		for _, l := range agenttmux.InstallLines() {
			fmt.Fprintln(stderr, "  "+l)
		}
		return 127
	}
	if err := execProgram(bin, append([]string{agenttmux.Bin}, args...), os.Environ()); err != nil {
		fmt.Fprintf(stderr, "ws: run %s: %s\n", bin, err)
		return 126
	}
	return 0
}
