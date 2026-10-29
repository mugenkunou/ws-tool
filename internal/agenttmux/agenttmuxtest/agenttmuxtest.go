// Package agenttmuxtest lets tests run against a fake agent-tmux: a shell
// script on PATH that prints canned output and records its arguments. Tests
// never run the agent-tmux installed on the machine: Hide takes it off PATH.
package agenttmuxtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/agenttmux"
)

// StripPath returns path without agent-tmux. A PATH entry that holds one is
// replaced by a shadow directory that links to everything else in it, so
// other tools there (gitleaks in ~/go/bin, say) keep working. cleanup removes
// the shadows.
func StripPath(path string) (stripped string, cleanup func()) {
	var keep, shadows []string
	for _, dir := range filepath.SplitList(path) {
		if _, err := os.Stat(filepath.Join(dir, agenttmux.Bin)); err != nil {
			keep = append(keep, dir)
			continue
		}
		shadow, err := os.MkdirTemp(os.Getenv("GOTMPDIR"), "at-path-")
		if err != nil {
			continue // drop the whole entry rather than run agent-tmux
		}
		shadows = append(shadows, shadow)
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.Name() != agenttmux.Bin {
				_ = os.Symlink(filepath.Join(dir, e.Name()), filepath.Join(shadow, e.Name()))
			}
		}
		keep = append(keep, shadow)
	}
	return strings.Join(keep, string(os.PathListSeparator)), func() {
		for _, s := range shadows {
			os.RemoveAll(s)
		}
	}
}

// Hide takes the machine's agent-tmux off PATH for the whole test binary
// (call it from TestMain and run cleanup after the tests), so agent-tmux is
// missing unless a test fakes one. It also turns the release check off, so no
// test asks GitHub.
func Hide() (cleanup func()) {
	path, cleanup := StripPath(os.Getenv("PATH"))
	_ = os.Setenv("PATH", path)
	_ = os.Setenv(agenttmux.NoCheckEnv, "1")
	return cleanup
}

// Fake puts an agent-tmux on PATH for this test. body is shell code that runs
// after the arguments are recorded; "$1" is the verb. It returns the log the
// fake appends one line per call to: the arguments, each ended by a tab.
//
// The script lives under $GOTMPDIR when that is set, since /tmp may be
// mounted noexec.
func Fake(t *testing.T, body string) (log string) {
	t.Helper()
	dir, err := os.MkdirTemp(os.Getenv("GOTMPDIR"), "at-fake-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	log = filepath.Join(dir, "argv.log")
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do printf '%s\\t' \"$a\"; done >> '" + log + "'\n" +
		"printf '\\n' >> '" + log + "'\n" +
		body + "\n"
	if err := os.WriteFile(filepath.Join(dir, agenttmux.Bin), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	path, cleanup := StripPath(os.Getenv("PATH"))
	t.Cleanup(cleanup)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+path)
	return log
}

// Missing takes agent-tmux off PATH for this test.
func Missing(t *testing.T) {
	t.Helper()
	path, cleanup := StripPath(os.Getenv("PATH"))
	t.Cleanup(cleanup)
	t.Setenv("PATH", path)
}

// Calls reads a Fake's log: one entry per call, its arguments in order.
func Calls(t *testing.T, log string) [][]string {
	t.Helper()
	data, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		args := []string{}
		if line != "" {
			args = strings.Split(strings.TrimSuffix(line, "\t"), "\t")
		}
		out = append(out, args)
	}
	return out
}
