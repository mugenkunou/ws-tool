package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDotfileCommandAddAndScan(t *testing.T) {
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
