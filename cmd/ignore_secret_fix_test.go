package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretFixAllowlistBatch(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer

	// Initialize workspace.
	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\ny\ny\ny\ny\ny\ny\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	// Write a file with a secret.
	secretFile := filepath.Join(workspace, "creds.env")
	if err := os.WriteFile(secretFile, []byte("password=hunter2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run secret fix in batch allowlist mode.
	out.Reset()
	errOut.Reset()
	code := Execute(
		[]string{"--workspace", workspace, "secret", "fix", "--secret-mode", "allowlist", "--quiet"},
		strings.NewReader("y\n"),
		&out, &errOut,
	)
	if code != 0 {
		t.Fatalf("secret fix allowlist: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	// Verify the allowlist was updated in manifest.
	manifestBytes, err := os.ReadFile(filepath.Join(workspace, "ws", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifestBytes), "creds.env:1") {
		t.Fatalf("expected creds.env:1 in manifest allowlist, got: %s", string(manifestBytes))
	}

	// Re-scan should find no violations (allowlisted).
	out.Reset()
	errOut.Reset()
	code = Execute(
		[]string{"--workspace", workspace, "secret", "scan"},
		strings.NewReader(""),
		&out, &errOut,
	)
	if code != 0 {
		t.Fatalf("expected clean scan after allowlist, got code=%d stdout=%s", code, out.String())
	}
}

func TestSecretFixExcludeBatch(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\ny\ny\ny\ny\ny\ny\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	secretFile := filepath.Join(workspace, "secrets.txt")
	if err := os.WriteFile(secretFile, []byte("api_key=abc123\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	errOut.Reset()
	code := Execute(
		[]string{"--workspace", workspace, "secret", "fix", "--secret-mode", "exclude", "--quiet"},
		strings.NewReader("y\n"),
		&out, &errOut,
	)
	if code != 0 {
		t.Fatalf("secret fix exclude: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	// Verify .megaignore was updated.
	megaignore, err := os.ReadFile(filepath.Join(workspace, ".megaignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(megaignore), "-:secrets.txt") {
		t.Fatalf("expected -:secrets.txt in .megaignore, got: %s", string(megaignore))
	}
}

func TestSecretFixDryRun(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\ny\ny\ny\ny\ny\ny\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	if err := os.WriteFile(filepath.Join(workspace, "app.env"), []byte("password=test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	errOut.Reset()
	code := Execute(
		[]string{"--workspace", workspace, "secret", "fix", "--dry-run"},
		strings.NewReader(""),
		&out, &errOut,
	)
	if code != 0 {
		t.Fatalf("dry-run: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "dry-run") {
		t.Fatalf("expected dry-run in output, got: %s", out.String())
	}
}

func TestSecretFixNoViolations(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\ny\ny\ny\ny\ny\ny\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code := Execute(
		[]string{"--workspace", workspace, "secret", "fix"},
		strings.NewReader(""),
		&out, &errOut,
	)
	if code != 0 {
		t.Fatalf("expected code 0 for clean workspace, got %d", code)
	}
	if !strings.Contains(out.String(), "No secret violations") {
		t.Fatalf("expected no violations message, got: %s", out.String())
	}
}

func TestSecretFixJSON(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\ny\ny\ny\ny\ny\ny\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	if err := os.WriteFile(filepath.Join(workspace, "creds.env"), []byte("password=hunter2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	errOut.Reset()
	code := Execute(
		[]string{"--workspace", workspace, "--json", "secret", "fix", "--secret-mode", "allowlist"},
		strings.NewReader("y\n"),
		&out, &errOut,
	)
	if code != 0 {
		t.Fatalf("json fix: code=%d stderr=%s", code, errOut.String())
	}

	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %s", out.String())
	}
	if envelope["command"] != "secret.fix" {
		t.Fatalf("expected command=secret.fix, got %v", envelope["command"])
	}
}

func TestSecretFixInteractiveAllowlist(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\ny\ny\ny\ny\ny\ny\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	if err := os.WriteFile(filepath.Join(workspace, "app.env"), []byte("password=hunter2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Send "l" for allowlist, then let EOF close remaining prompts.
	out.Reset()
	errOut.Reset()
	code := Execute(
		[]string{"--workspace", workspace, "secret", "fix"},
		strings.NewReader("l\n"),
		&out, &errOut,
	)
	if code != 0 {
		t.Fatalf("interactive fix: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "Allowlisted") {
		t.Fatalf("expected Allowlisted in output, got: %s", out.String())
	}
}

func TestSecretFixInteractiveExclude(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\ny\ny\ny\ny\ny\ny\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	if err := os.WriteFile(filepath.Join(workspace, "app.env"), []byte("password=hunter2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Send "a" for add .megaignore.
	out.Reset()
	errOut.Reset()
	code := Execute(
		[]string{"--workspace", workspace, "secret", "fix"},
		strings.NewReader("a\n"),
		&out, &errOut,
	)
	if code != 0 {
		t.Fatalf("interactive fix: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), ".megaignore") {
		t.Fatalf("expected .megaignore in output, got: %s", out.String())
	}
}

func TestSecretFixInteractiveViewThenSkip(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\ny\ny\ny\ny\ny\ny\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	if err := os.WriteFile(filepath.Join(workspace, "app.env"), []byte("foo=bar\npassword=hunter2\nbaz=qux\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Send "v" to view context, then "s" to skip.
	out.Reset()
	errOut.Reset()
	code := Execute(
		[]string{"--workspace", workspace, "secret", "fix"},
		strings.NewReader("v\ns\n"),
		&out, &errOut,
	)
	if code != 2 {
		t.Fatalf("interactive fix: expected code=2 (skipped), got code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	// Should show context lines.
	if !strings.Contains(out.String(), "foo=bar") {
		t.Fatalf("expected context in output, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "1 skipped") {
		t.Fatalf("expected skipped summary, got: %s", out.String())
	}
}

func TestSecretFixInteractiveQuit(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\ny\ny\ny\ny\ny\ny\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	// Two secret files.
	if err := os.WriteFile(filepath.Join(workspace, "a.env"), []byte("password=one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "b.env"), []byte("password=two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Send "q" to quit after first violation.
	out.Reset()
	errOut.Reset()
	code := Execute(
		[]string{"--workspace", workspace, "secret", "fix"},
		strings.NewReader("q\n"),
		&out, &errOut,
	)
	if code != 2 {
		t.Fatalf("interactive fix quit: expected code=2 (skipped), got code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
}

func TestSecretSetupAlreadySetUp(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\ny\ny\ny\ny\ny\ny\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	// Setup may fail if gpg/pass aren't installed, or succeed if already set up.
	// We just verify the command doesn't panic.
	out.Reset()
	errOut.Reset()
	Execute(
		[]string{"--workspace", workspace, "secret", "setup"},
		strings.NewReader("y\n"),
		&out, &errOut,
	)
	// No assertion on exit code — depends on system state.
}

func TestSecretFixInvalidMode(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\ny\ny\ny\ny\ny\ny\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	if err := os.WriteFile(filepath.Join(workspace, "app.env"), []byte("password=test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	errOut.Reset()
	code := Execute(
		[]string{"--workspace", workspace, "secret", "fix", "--secret-mode", "invalid"},
		strings.NewReader("y\n"),
		&out, &errOut,
	)
	if code != 1 {
		t.Fatalf("expected code 1 for invalid mode, got %d", code)
	}
}
