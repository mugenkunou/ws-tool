package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestIgnoreExtraCommands(t *testing.T) {
	testSetXDG(t)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "ignore", "ls"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("ignore ls failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "ignore", "ls", "--tree", "--depth", "1"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("ignore ls --tree failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "├──") && !strings.Contains(out.String(), "└──") {
		t.Fatalf("expected tree-like formatting, got: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "ignore", "edit", "--editor", "true"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("ignore edit failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
}

func TestDotfileGitSetupAndStatus(t *testing.T) {
	testSetXDG(t)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "dotfile", "git", "setup"}, strings.NewReader("\n"), &out, &errOut); code != 0 {
		t.Fatalf("dotfile git setup failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "No git remote") {
		t.Fatalf("unexpected setup output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "dotfile", "git", "status"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("dotfile git status failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "Git") {
		t.Fatalf("unexpected status output: %s", out.String())
	}
}
