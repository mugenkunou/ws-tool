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
	if !strings.Contains(out.String(), "demo-scratch") {
		t.Fatalf("expected path containing demo-scratch in scratch new output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "scratch", "ls"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("scratch ls failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "demo-scratch") {
		t.Fatalf("unexpected scratch ls output: %s", out.String())
	}
	// ls must show a path line (starts with whitespace and contains the scratch root)
	if !strings.Contains(out.String(), filepath.Join(home, "Scratch")) {
		t.Fatalf("expected path line in scratch ls output: %s", out.String())
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

func TestScratchOpenJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	// Create a scratch directory first.
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "scratch", "new", "open-test", "--no-open"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("scratch new failed: code=%d stderr=%s", code, errOut.String())
	}

	// Open with --json to avoid launching an editor.
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "--json", "scratch", "open", "open-test"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("scratch open --json failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "open-test") {
		t.Fatalf("expected open-test in json output, got: %s", out.String())
	}
}

func TestScratchOpenNotFound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	// Open a non-existent scratch dir.
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "--json", "scratch", "open", "does-not-exist"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("expected exit 1 for missing scratch, got code=%d", code)
	}
	if !strings.Contains(errOut.String(), "scratch entry not found") {
		t.Fatalf("expected not-found error, got stderr=%s", errOut.String())
	}
}

func TestScratchOpenSubstringMatch(t *testing.T) {
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
	if code := Execute([]string{"--workspace", workspace, "scratch", "new", "my-debug-session", "--no-open", "--no-date"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("scratch new failed: code=%d stderr=%s", code, errOut.String())
	}

	// Substring match should find "my-debug-session".
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "--json", "scratch", "open", "debug"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("scratch open substring failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "my-debug-session") {
		t.Fatalf("expected my-debug-session in output, got: %s", out.String())
	}
}

func TestScratchOpenPrintPath(t *testing.T) {
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
	if code := Execute([]string{"--workspace", workspace, "scratch", "new", "--no-open", "--no-date", "print-path-test"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("scratch new failed: code=%d stderr=%s", code, errOut.String())
	}

	// --print-path: stdout must be only the directory path (no decoration).
	// We fake the editor with a no-op so exec.Command.Start() succeeds.
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "scratch", "open", "--print-path", "--editor", "true", "print-path-test"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("scratch open --print-path failed: code=%d stderr=%s", code, errOut.String())
	}
	gotPath := strings.TrimSpace(out.String())
	if !strings.HasSuffix(gotPath, "print-path-test") {
		t.Fatalf("expected path ending in print-path-test on stdout, got: %q", gotPath)
	}
	if strings.Contains(gotPath, "\n") {
		t.Fatalf("stdout should be a single line path, got: %q", gotPath)
	}
	// Status/decoration must go to stderr, not stdout.
	if strings.Contains(out.String(), "Opening") {
		t.Fatalf("status line should go to stderr, not stdout")
	}
}
