package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	trashRoot := filepath.Join(home, ".local", "share", "Trash")
	filesDir := filepath.Join(trashRoot, "files")
	if err := os.MkdirAll(filepath.Join(filesDir, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filesDir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filesDir, "nested", "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "trash", "empty"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("trash empty failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	entries, err := os.ReadDir(filesDir)
	if err != nil {
		t.Fatalf("readdir failed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty trash files dir, found %d entries", len(entries))
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

	trashRoot := filepath.Join(home, ".local", "share", "Trash")
	filesDir := filepath.Join(trashRoot, "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	filePath := filepath.Join(filesDir, "keep.txt")
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

func TestTrashNoArgsShowsHelp(t *testing.T) {
	testSetXDG(t)
	var out, errOut bytes.Buffer
	code := Execute([]string{"trash"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("ws trash (no args): expected exit 0, got %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "enable") {
		t.Fatalf("expected help with 'enable' subcommand, got: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got: %s", errOut.String())
	}
}

func TestTrashPurgeOlderThanFromCLI(t *testing.T) {
	testSetXDG(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer
	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	trashRoot := filepath.Join(home, ".local", "share", "Trash")
	seed := func(name string, age time.Duration) {
		for _, d := range []string{"files", "info"} {
			if err := os.MkdirAll(filepath.Join(trashRoot, d), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(trashRoot, "files", name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		info := "[Trash Info]\nPath=/o/" + name + "\nDeletionDate=" + time.Now().Add(-age).Format("2006-01-02T15:04:05") + "\n"
		if err := os.WriteFile(filepath.Join(trashRoot, "info", name+".trashinfo"), []byte(info), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	seed("old.txt", 40*24*time.Hour)
	seed("new.txt", time.Hour)

	args := func(extra ...string) []string {
		return append([]string{"--workspace", workspace, "trash", "purge"}, extra...)
	}
	if code := Execute(args(), strings.NewReader(""), &out, &errOut); code == 0 {
		t.Fatal("purge without --older-than should fail")
	}
	if code := Execute(args("--older-than", "30", "--dry-run"), strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("dry run failed: %s", errOut.String())
	}
	if _, err := os.Stat(filepath.Join(trashRoot, "files", "old.txt")); err != nil {
		t.Fatalf("dry run removed old.txt: %v", err)
	}
	if code := Execute(args("--older-than", "30"), strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("purge failed: %s", errOut.String())
	}
	if _, err := os.Stat(filepath.Join(trashRoot, "files", "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("old.txt should be purged, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(trashRoot, "files", "new.txt")); err != nil {
		t.Fatalf("new.txt should remain: %v", err)
	}
}
