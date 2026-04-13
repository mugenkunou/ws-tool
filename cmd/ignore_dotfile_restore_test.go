package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIgnoreExtraCommands(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	if err := os.WriteFile(filepath.Join(workspace, "trace.bin"), []byte{1, 2, 3}, 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "ignore", "generate", "--merge", "--scan"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("ignore generate failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "ignore", "ls"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("ignore ls failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "ignore", "tree", "--depth", "1"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("ignore tree failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
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

func TestDotfileGitConnectAndStatus(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "dotfile", "git", "enable", "--remote-url", "https://example.com/private.git", "--username", "user", "--branch", "main", "--auto-push"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("dotfile git enable failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "enabled") {
		t.Fatalf("unexpected connect output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "dotfile", "git", "status"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("dotfile git status failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "Git versioning") {
		t.Fatalf("unexpected status output: %s", out.String())
	}
}

func TestRestoreCommand(t *testing.T) {
	t.Run("fails on uninitialized workspace", func(t *testing.T) {
		workspace := filepath.Join(t.TempDir(), "Workspace")
		var out bytes.Buffer
		var errOut bytes.Buffer

		code := Execute([]string{"--workspace", workspace, "restore"}, strings.NewReader("y\n"), &out, &errOut)
		if code != 1 {
			t.Fatalf("expected exit code 1, got %d stdout=%s stderr=%s", code, out.String(), errOut.String())
		}
		if !strings.Contains(errOut.String(), "not initialized") {
			t.Fatalf("expected 'not initialized' error, got stderr=%s", errOut.String())
		}
		if !strings.Contains(errOut.String(), "ws init") {
			t.Fatalf("expected hint to run 'ws init', got stderr=%s", errOut.String())
		}
	})

	t.Run("succeeds on initialized workspace", func(t *testing.T) {
		workspace := filepath.Join(t.TempDir(), "Workspace")
		var out bytes.Buffer
		var errOut bytes.Buffer

		// Initialize workspace first
		if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
			t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
		}

		out.Reset()
		errOut.Reset()
		if code := Execute([]string{"--workspace", workspace, "restore"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
			t.Fatalf("restore failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
		}
		if !strings.Contains(out.String(), "WORKSPACE RESTORE") {
			t.Fatalf("unexpected restore output: %s", out.String())
		}
	})
}
