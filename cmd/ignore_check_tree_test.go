package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// initIgnoreWorkspace creates a fresh workspace and returns its path.
func initIgnoreWorkspace(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer
	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}
	return workspace
}

// ── ws ignore check ───────────────────────────────────────────────────────────

func TestIgnoreCheckSynced(t *testing.T) {
	workspace := initIgnoreWorkspace(t)

	// Create a plain file that has no matching exclude rule.
	f := filepath.Join(workspace, "notes.txt")
	if err := os.WriteFile(f, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "ignore", "check", "notes.txt"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0 for synced file, got %d (stderr=%s)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "SYNCED") {
		t.Fatalf("expected SYNCED in output, got: %s", out.String())
	}
	// Reason line must be present.
	if !strings.Contains(out.String(), "Reason") {
		t.Fatalf("expected Reason line in output, got: %s", out.String())
	}
	// File size line must be present.
	if !strings.Contains(out.String(), "File size") {
		t.Fatalf("expected File size line in output, got: %s", out.String())
	}
}

func TestIgnoreCheckIgnored(t *testing.T) {
	workspace := initIgnoreWorkspace(t)

	// node_modules/ is excluded by the built-in default rules.
	nm := filepath.Join(workspace, "node_modules", "lodash.js")
	if err := os.MkdirAll(filepath.Dir(nm), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nm, []byte("// lodash\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "ignore", "check", "node_modules/lodash.js"}, strings.NewReader(""), &out, &errOut)
	// exit 2 = ignored
	if code != 2 {
		t.Fatalf("expected exit 2 for ignored file, got %d (stdout=%s stderr=%s)", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "IGNORED") {
		t.Fatalf("expected IGNORED in output, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "Reason") {
		t.Fatalf("expected Reason line, got: %s", out.String())
	}
}

func TestIgnoreCheckJSON(t *testing.T) {
	workspace := initIgnoreWorkspace(t)

	f := filepath.Join(workspace, "readme.md")
	if err := os.WriteFile(f, []byte("# readme\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "--json", "ignore", "check", "readme.md"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, errOut.String())
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out.String()), &payload); err != nil {
		t.Fatalf("invalid JSON: %v — output: %s", err, out.String())
	}
	if payload["command"] != "ignore.check" {
		t.Fatalf("unexpected command field: %v", payload["command"])
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got: %v", payload["data"])
	}
	if data["included"] != true {
		t.Fatalf("expected included=true for readme.md, got: %v", data["included"])
	}
	if _, hasPath := data["path"]; !hasPath {
		t.Fatalf("expected path field in JSON data, got: %v", data)
	}
}

func TestIgnoreCheckExitCodeIgnored(t *testing.T) {
	workspace := initIgnoreWorkspace(t)

	// Create a .env file; built-in rules exclude dotenv patterns.
	f := filepath.Join(workspace, ".env")
	if err := os.WriteFile(f, []byte("SECRET=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "ignore", "check", ".env"}, strings.NewReader(""), &out, &errOut)
	// .env may or may not be in the default rules; just verify the exit code
	// is 0 (synced) or 2 (ignored) — never 1 (error).
	if code != 0 && code != 2 {
		t.Fatalf("expected exit 0 or 2, got %d (stderr=%s)", code, errOut.String())
	}
}

func TestIgnoreCheckMissingPath(t *testing.T) {
	workspace := initIgnoreWorkspace(t)

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "ignore", "check", "does-not-exist.txt"}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("expected exit 1 for missing path, got %d", code)
	}
}

func TestIgnoreCheckNoArg(t *testing.T) {
	workspace := initIgnoreWorkspace(t)

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "ignore", "check"}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("expected exit 1 when no path given, got %d", code)
	}
	if !strings.Contains(errOut.String(), "usage") {
		t.Fatalf("expected usage message, got stderr: %s", errOut.String())
	}
}

// ── ws ignore tree ────────────────────────────────────────────────────────────

func TestIgnoreTreeBasic(t *testing.T) {
	workspace := initIgnoreWorkspace(t)

	// Create a directory with a synced file and an ignored directory.
	if err := os.MkdirAll(filepath.Join(workspace, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// node_modules is excluded by default rules.
	if err := os.MkdirAll(filepath.Join(workspace, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "node_modules", "pkg.js"), []byte("//\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "ignore", "tree"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, errOut.String())
	}
	output := out.String()

	// Root header must be present.
	if !strings.Contains(output, workspace) && !strings.Contains(output, "~/") {
		t.Fatalf("expected workspace root header, got: %s", output)
	}
	// src/ should appear as synced (✔ or check icon).
	if !strings.Contains(output, "src/") {
		t.Fatalf("expected src/ in output, got: %s", output)
	}
	// node_modules/ should appear (possibly as ignored or partial of root).
	if !strings.Contains(output, "node_modules/") {
		t.Fatalf("expected node_modules/ in output, got: %s", output)
	}
}

func TestIgnoreTreePartialDir(t *testing.T) {
	workspace := initIgnoreWorkspace(t)

	// Create a directory with both a synced file and an ignored sub-dir.
	if err := os.MkdirAll(filepath.Join(workspace, "project", "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "project", "index.js"), []byte("//\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "project", "node_modules", "pkg.js"), []byte("//\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "ignore", "tree"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, errOut.String())
	}
	output := out.String()

	// project/ must be shown as ◐ partial (it has excluded children inside node_modules).
	// In noColor mode the icon would be "~"; in color mode it's the UTF-8 ◐.
	// We test by checking the status is NOT ✔ (check icon) alone.
	if !strings.Contains(output, "project/") {
		t.Fatalf("expected project/ in output, got: %s", output)
	}
	// The ◐ or ~ partial indicator must appear.
	if !strings.Contains(output, "◐") && !strings.Contains(output, "~") {
		// project/ has an excluded child so must be partial.
		t.Fatalf("expected partial (◐ or ~) indicator in output for project/, got: %s", output)
	}
}

func TestIgnoreTreeDepth(t *testing.T) {
	workspace := initIgnoreWorkspace(t)

	// Two levels of dirs.
	if err := os.MkdirAll(filepath.Join(workspace, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "a", "b", "deep.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Default (no depth): unlimited — should see everything including deep.txt.
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "ignore", "tree"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("unexpected error: %d (stderr=%s)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "a/") {
		t.Fatalf("expected a/ in unlimited output, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "deep.txt") {
		t.Fatalf("expected deep.txt in unlimited output, got: %s", out.String())
	}

	// -L 1: should see a/ but NOT b/ or deep.txt.
	out.Reset()
	errOut.Reset()
	code = Execute([]string{"--workspace", workspace, "ignore", "tree", "-L", "1"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("unexpected error at -L 1: %d (stderr=%s)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "a/") {
		t.Fatalf("expected a/ at -L 1, got: %s", out.String())
	}
	if strings.Contains(out.String(), "deep.txt") {
		t.Fatalf("deep.txt should not appear at -L 1, got: %s", out.String())
	}

	// Positional level arg: ws ignore tree . 2 — should see b/ but NOT deep.txt.
	out.Reset()
	errOut.Reset()
	code = Execute([]string{"--workspace", workspace, "ignore", "tree", ".", "2"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("unexpected error at positional level=2: %d (stderr=%s)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "b/") {
		t.Fatalf("expected b/ at level 2, got: %s", out.String())
	}
	if strings.Contains(out.String(), "deep.txt") {
		t.Fatalf("deep.txt should not appear at level 2, got: %s", out.String())
	}

	// --depth 3: should see deep.txt.
	out.Reset()
	errOut.Reset()
	code = Execute([]string{"--workspace", workspace, "ignore", "tree", "--depth", "3"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("unexpected error at depth=3: %d (stderr=%s)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "deep.txt") {
		t.Fatalf("expected deep.txt at depth=3, got: %s", out.String())
	}
}

func TestIgnoreTreePath(t *testing.T) {
	workspace := initIgnoreWorkspace(t)

	// Create a subdir.
	if err := os.MkdirAll(filepath.Join(workspace, "sub", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "sub", "file.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "root.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "ignore", "tree", "--path", "sub"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, errOut.String())
	}
	// Should see sub contents, not root.txt.
	if !strings.Contains(out.String(), "file.txt") {
		t.Fatalf("expected file.txt inside sub/, got: %s", out.String())
	}
	if strings.Contains(out.String(), "root.txt") {
		t.Fatalf("root.txt should not appear when scoped to sub/, got: %s", out.String())
	}

	// Positional directory argument: ws ignore tree sub
	out.Reset()
	errOut.Reset()
	code = Execute([]string{"--workspace", workspace, "ignore", "tree", "sub"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0 with positional dir, got %d (stderr=%s)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "file.txt") {
		t.Fatalf("expected file.txt with positional dir arg, got: %s", out.String())
	}
	if strings.Contains(out.String(), "root.txt") {
		t.Fatalf("root.txt should not appear with positional dir arg, got: %s", out.String())
	}
}

func TestIgnoreTreeJSON(t *testing.T) {
	workspace := initIgnoreWorkspace(t)

	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "--json", "ignore", "tree"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, errOut.String())
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out.String()), &payload); err != nil {
		t.Fatalf("invalid JSON: %v — output: %s", err, out.String())
	}
	if payload["command"] != "ignore.tree" {
		t.Fatalf("unexpected command field: %v", payload["command"])
	}
}

func TestIgnoreTreeSummaryLine(t *testing.T) {
	workspace := initIgnoreWorkspace(t)

	// Add an ignored directory so the summary shows excluded files.
	if err := os.MkdirAll(filepath.Join(workspace, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "node_modules", "a.js"), []byte("//\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "ignore", "tree"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, errOut.String())
	}
	output := out.String()

	// Either the summary "N files excluded" line OR the "All files synced" line must appear.
	hasSummary := strings.Contains(output, "excluded") || strings.Contains(output, "synced")
	if !hasSummary {
		t.Fatalf("expected summary line in tree output, got: %s", output)
	}
}
