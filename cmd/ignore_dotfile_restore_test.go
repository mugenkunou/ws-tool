package cmd

import (
	"bytes"
	"os/exec"
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

func TestDotfileBackupEnableAndStatus(t *testing.T) {
	testSetXDG(t)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	// With no remote at all, enable needs one: there is no local-only mode.
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "dotfile", "backup", "enable"}, strings.NewReader("\n"), &out, &errOut); code != 1 {
		t.Fatalf("a blank remote must exit 1, got %d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "remote is required") {
		t.Fatalf("expected the remote-required error, got %q", errOut.String())
	}

	remote := filepath.Join(t.TempDir(), "remote.git")
	if o, err := exec.Command("git", "init", "--bare", "--initial-branch=main", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v %s", err, o)
	}
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "dotfile", "backup", "enable", "--remote", remote}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("backup enable failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	for _, want := range []string{"Initialize git in", "Set the backup remote to " + remote, "ws dotfile backup push", "ws cron add dotfile-backup"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("enable output lacks %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "dotfile", "backup", "status"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("backup status failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	for _, want := range []string{"Backup", "enabled", "Repository", remote, "Auto-commit   on", "Auto-push     on"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("status lacks %q:\n%s", want, out.String())
		}
	}
}
