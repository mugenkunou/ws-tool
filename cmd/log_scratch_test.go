package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogCommandsFlow(t *testing.T) {
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
	if code := Execute([]string{"--workspace", workspace, "log", "start", "--tag", "demo"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("log start failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Start recording") {
		t.Fatalf("unexpected start output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "log", "ls"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("log ls failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "demo") {
		t.Fatalf("unexpected ls output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "log", "stop"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("log stop failed: code=%d stderr=%s", code, errOut.String())
	}
}

func TestScratchCommandsFlow(t *testing.T) {
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
	if code := Execute([]string{"--workspace", workspace, "scratch", "new", "demo-scratch", "--no-open"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("scratch new failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Create scratch") {
		t.Fatalf("unexpected scratch new output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "scratch", "ls"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("scratch ls failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "demo-scratch") {
		t.Fatalf("unexpected scratch ls output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "scratch", "prune", "--all"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("scratch prune failed: code=%d stderr=%s", code, errOut.String())
	}

	scratchRoot := filepath.Join(home, "Scratch")
	entries, _ := os.ReadDir(scratchRoot)
	if len(entries) != 0 {
		t.Fatalf("expected scratch root to be pruned, found %d entries", len(entries))
	}
}

func TestScratchDeleteAndAgeFormatting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	if code := Execute([]string{"--workspace", workspace, "scratch", "new", "age-case", "--no-open"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("scratch new failed: code=%d stderr=%s", code, errOut.String())
	}

	scratchRoot := filepath.Join(home, "Scratch")
	entries, err := os.ReadDir(scratchRoot)
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected scratch entry, err=%v", err)
	}
	entryPath := filepath.Join(scratchRoot, entries[0].Name())
	old := time.Now().Add(-96*time.Hour - 41*time.Minute)
	if err := os.Chtimes(entryPath, old, old); err != nil {
		t.Fatalf("chtimes failed: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "scratch", "ls"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("scratch ls failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "age=4d") {
		t.Fatalf("expected day-format age output, got: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "scratch", "rm", "age-case"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("scratch rm failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
}
