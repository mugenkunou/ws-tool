package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrashStatusAndSetupFromCLI(t *testing.T) {
	testSetXDG(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "trash", "status"}, strings.NewReader("y\n"), &out, &errOut); code != 2 {
		t.Fatalf("expected trash status exit 2 before setup, got=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "WARNING") {
		t.Fatalf("unexpected trash status output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "trash", "enable"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("trash enable failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "trash", "status"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("expected trash status exit 0 after setup, got=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "OK") {
		t.Fatalf("unexpected trash status output: %s", out.String())
	}
}

func TestTrashEmptyFromCLI(t *testing.T) {
	testSetXDG(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	trashRoot := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(filepath.Join(trashRoot, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trashRoot, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trashRoot, "nested", "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "trash", "empty"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("trash empty failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	entries, err := os.ReadDir(trashRoot)
	if err != nil {
		t.Fatalf("readdir failed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty trash root, found %d entries", len(entries))
	}
}

func TestTrashEmptyDryRunFromCLI(t *testing.T) {
	testSetXDG(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	trashRoot := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(trashRoot, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	filePath := filepath.Join(trashRoot, "keep.txt")
	if err := os.WriteFile(filePath, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "trash", "empty", "--dry-run"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("trash empty --dry-run failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("expected file to remain after dry-run: %v", err)
	}
}
