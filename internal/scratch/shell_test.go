package scratch

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

func TestShellCommandUsesShellEnvInTheDirectory(t *testing.T) {
	c := shellCommand("/some/dir", "/bin/zsh", func(string) (string, error) { return "", errors.New("unused") })
	if filepath.Base(c.Path) != "zsh" && c.Args[0] != "/bin/zsh" {
		t.Fatalf("argv = %v", c.Args)
	}
	if c.Dir != "/some/dir" {
		t.Fatalf("dir = %q", c.Dir)
	}
	if !slices.Contains(c.Env, ShellEnvVar+"=/some/dir") {
		t.Fatal("the shell gets " + ShellEnvVar)
	}
}

func TestShellCommandFallsBackToBashThenSh(t *testing.T) {
	has := func(string) (string, error) { return "/usr/bin/bash", nil }
	if c := shellCommand("/d", "", has); c.Args[0] != "bash" {
		t.Fatalf("with bash: %v", c.Args)
	}
	none := func(string) (string, error) { return "", errors.New("no") }
	if c := shellCommand("/d", "", none); c.Args[0] != "sh" {
		t.Fatalf("without bash: %v", c.Args)
	}
}

func TestInShellReadsTheMarker(t *testing.T) {
	t.Setenv(ShellEnvVar, "")
	if InShell() != "" {
		t.Fatal("not in a scratch shell")
	}
	t.Setenv(ShellEnvVar, "/x")
	if InShell() != "/x" {
		t.Fatal("in a scratch shell")
	}
}
