// Package handover runs programs that need the terminal (a recorded shell,
// a terminal editor) by suspending the TUI, and launches GUI programs (VS
// Code) without suspending. Either way the owner receives a DoneMsg tagged
// with a value it chose, so it can refresh afterwards.
package handover

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/workspace"
)

// DoneMsg reports that a handed-over or launched program finished (Run) or
// started (Launch). Tag is whatever the caller passed, typically a typed
// constant of its own.
type DoneMsg struct {
	Tag any
	Err error
}

// Run suspends the TUI, gives c the terminal, and resumes when it exits.
func Run(c *exec.Cmd, tag any) tea.Cmd {
	return tea.ExecProcess(c, func(err error) tea.Msg { return DoneMsg{Tag: tag, Err: err} })
}

// Launch starts c in the background (for GUI programs) without suspending.
func Launch(c *exec.Cmd, tag any) tea.Cmd {
	return func() tea.Msg {
		if err := c.Start(); err != nil {
			return DoneMsg{Tag: tag, Err: err}
		}
		go func() { _ = c.Wait() }()
		return DoneMsg{Tag: tag}
	}
}

// WS builds a command that runs this ws binary against the same workspace,
// for interactive CLI wizards the TUI hands the terminal to (e.g.
// `ws dotfile git setup`).
func WS(p workspace.Paths, args ...string) *exec.Cmd {
	bin, err := os.Executable()
	if err != nil {
		bin = "ws"
	}
	full := append([]string{"--workspace", p.Workspace, "--config", p.Config, "--manifest", p.Manifest}, args...)
	return exec.Command(bin, full...)
}

// Pause wraps c so the terminal waits for enter after it exits, letting the
// user read its output before the TUI redraws.
func Pause(c *exec.Cmd) *exec.Cmd {
	script := `"$@"; status=$?; printf '\n[ws] exit %s — press enter to return to the TUI ' "$status"; read _; exit $status`
	return exec.Command("sh", append([]string{"-c", script, "sh", c.Path}, c.Args[1:]...)...)
}

// terminalEditors need the terminal; anything else is assumed to be a GUI
// program and is launched without suspending the TUI.
var terminalEditors = map[string]bool{
	"vi": true, "vim": true, "nvim": true, "nano": true, "emacs": true,
	"micro": true, "hx": true, "helix": true, "kak": true, "pico": true, "ed": true,
}

// Editor opens path with editorCmd (which may include arguments, e.g.
// "code -n"): terminal editors take over the terminal, GUI editors launch.
func Editor(editorCmd, path string, tag any) tea.Cmd {
	fields := strings.Fields(editorCmd)
	if len(fields) == 0 {
		fields = []string{"vi"}
	}
	c := exec.Command(fields[0], append(fields[1:], path)...)
	if terminalEditors[filepath.Base(fields[0])] {
		return Run(c, tag)
	}
	return Launch(c, tag)
}

// Paged wraps c so its output goes through $PAGER (default less -R), for
// long read-only CLI output such as `ws ignore tree`.
func Paged(c *exec.Cmd) *exec.Cmd {
	script := `"$@" 2>&1 | ${PAGER:-less -R}`
	return exec.Command("sh", append([]string{"-c", script, "sh", c.Path}, c.Args[1:]...)...)
}
