// Package editor resolves and launches the user's editor the same way for
// every command that opens one (spec §4.10): the ws.json "editor" setting,
// else $VISUAL, else $EDITOR, else vim.
package editor

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Default is the editor used when nothing else is set.
const Default = "vim"

// Command returns the editor argv for a ws.json "editor" value cfg: cfg itself
// when set, else $VISUAL, else $EDITOR, else vim. The value may carry
// arguments ("code -w", "nvim -p").
func Command(cfg string) []string { return Resolve(cfg, os.Getenv) }

// Resolve is Command with the environment injected, for tests.
func Resolve(cfg string, getenv func(string) string) []string {
	for _, s := range []string{cfg, getenv("VISUAL"), getenv("EDITOR")} {
		if argv := Split(s); len(argv) > 0 {
			return argv
		}
	}
	return []string{Default}
}

// Split splits a command line into words. Single and double quotes group a
// word, and a backslash escapes the next character outside single quotes.
func Split(s string) []string {
	var words []string
	var cur strings.Builder
	var quote rune
	inWord := false
	escaped := false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inWord = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}

// terminalEditors are editors that take over the terminal.
var terminalEditors = []string{
	"vim", "nvim", "vi", "view", "nano", "pico", "micro", "hx", "helix", "kak",
	"ed", "ne", "joe", "mg", "jed", "tilde", "emacs", "emacsclient", "vimx", "rvim",
}

// IsTerminal reports whether argv is an editor that runs in the terminal (vim,
// nvim, vi, nano, emacs -nw, hx, …). Any other editor, such as VS Code, is a
// GUI program and is started in the background. emacs counts as a terminal
// editor only with -nw (or -t / --no-window-system).
func IsTerminal(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	name := filepath.Base(argv[0])
	if !slices.Contains(terminalEditors, name) {
		return false
	}
	if name == "emacs" || name == "emacsclient" {
		return slices.ContainsFunc(argv[1:], func(a string) bool {
			return a == "-nw" || a == "--no-window-system" || a == "-t" || a == "-tty" || a == "--tty"
		})
	}
	return true
}

// Cmd builds the command that opens path in the editor.
func Cmd(argv []string, path string) *exec.Cmd {
	return exec.Command(argv[0], append(slices.Clone(argv[1:]), path)...)
}

// Open opens path in the editor. A terminal editor runs in the foreground on
// the process's own terminal and Open returns when it exits; its screen goes to
// stdout, which a caller whose stdout is captured (`cd "$(ws scratch open
// --print-path)"`) points at stderr. Any other editor is started in the
// background and Open returns at once. The error says which editor failed.
func Open(argv []string, path string, stdout io.Writer) error {
	c := Cmd(argv, path)
	if IsTerminal(argv) {
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, stdout, os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("%s: %w", argv[0], err)
		}
		return nil
	}
	if err := c.Start(); err != nil {
		return fmt.Errorf("%s: %w", argv[0], err)
	}
	go func() { _ = c.Wait() }()
	return nil
}
