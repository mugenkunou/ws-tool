package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDotfileCommandAddAndScan(t *testing.T) {
	testSetXDG(t)
	workspace := filepath.Join(t.TempDir(), "Workspace")

	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	systemFile := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(systemFile, []byte("export PATH=$PATH\n"), 0o644); err != nil {
		t.Fatalf("write system file failed: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "dotfile", "add", systemFile}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("dotfile add failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Add dotfile") {
		t.Fatalf("unexpected add output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "dotfile", "scan"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("dotfile scan failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Dotfile scan") || !strings.Contains(out.String(), "OK") {
		t.Fatalf("unexpected scan output: %s", out.String())
	}
}

func TestScanExitCodeOnViolation(t *testing.T) {
	testSetXDG(t)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	systemFile := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(systemFile, []byte("alias k=kubectl\n"), 0o644); err != nil {
		t.Fatalf("write system file failed: %v", err)
	}

	if code := Execute([]string{"--workspace", workspace, "dotfile", "add", systemFile}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("dotfile add failed: code=%d stderr=%s", code, errOut.String())
	}

	target, err := os.Readlink(systemFile)
	if err != nil {
		t.Fatalf("readlink failed: %v", err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatalf("remove target failed: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "dotfile", "scan"}, strings.NewReader("y\n"), &out, &errOut); code != 2 {
		t.Fatalf("expected scan exit 2, got %d (stdout=%s stderr=%s)", code, out.String(), errOut.String())
	}
}

func TestDotfileSyncCommand(t *testing.T) {
	testSetXDG(t)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	// Create and add the dotfile with initial content.
	systemFile := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(systemFile, []byte("export PATH=$PATH\n"), 0o644); err != nil {
		t.Fatalf("write system file: %v", err)
	}
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "dotfile", "add", systemFile}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("dotfile add failed: code=%d stderr=%s", code, errOut.String())
	}

	// Simulate 0Helper: remove the symlink, write a real file with updated content.
	if err := os.Remove(systemFile); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}
	updatedContent := "export PATH=$PATH\nexport UPDATED=1\n"
	if err := os.WriteFile(systemFile, []byte(updatedContent), 0o644); err != nil {
		t.Fatalf("write updated file: %v", err)
	}

	// Run sync: live file should be pulled into ws copy and symlink re-created.
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "dotfile", "sync", systemFile}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("dotfile sync failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	// system path must now be a symlink.
	target, err := os.Readlink(systemFile)
	if err != nil {
		t.Fatalf("expected symlink after sync, readlink failed: %v", err)
	}
	// ws copy must contain the updated content (live file was pulled in).
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read ws copy: %v", err)
	}
	if string(got) != updatedContent {
		t.Fatalf("ws copy content mismatch: got %q, want %q", string(got), updatedContent)
	}
}

func TestDotfileNoArgsShowsHelp(t *testing.T) {
	testSetXDG(t)
	var out, errOut bytes.Buffer
	code := Execute([]string{"dotfile"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("ws dotfile (no args): expected exit 0, got %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "add") {
		t.Fatalf("expected help with 'add' subcommand, got: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got: %s", errOut.String())
	}
}
