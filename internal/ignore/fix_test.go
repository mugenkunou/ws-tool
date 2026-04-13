package ignore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddRulesInsertsBeforeSentinel(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, ".megaignore")
	orig := "# header\n-:.*\n-s:*\n"
	if err := os.WriteFile(file, []byte(orig), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	added, err := AddRules(file, []string{"-:foo/bar", "-:baz"})
	if err != nil {
		t.Fatalf("add rules failed: %v", err)
	}
	if len(added) != 2 {
		t.Fatalf("expected 2 added rules, got %d", len(added))
	}

	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "-:foo/bar\n-:baz\n-s:*") && !strings.Contains(text, "-:baz\n-:foo/bar\n-s:*") {
		t.Fatalf("rules not inserted before sentinel:\n%s", text)
	}
}

func TestFixDryRun(t *testing.T) {
	res, err := Fix(FixOptions{
		MegaignorePath: "/tmp/non-existent-not-used",
		Violations:     []Violation{{Type: "bloat", Path: "big.bin"}},
		DryRun:         true,
	})
	if err != nil {
		t.Fatalf("fix dry-run failed: %v", err)
	}
	if len(res.AddedRules) != 1 {
		t.Fatalf("expected 1 rule in dry-run, got %d", len(res.AddedRules))
	}
	if res.AddedRules[0] != "-p:big.bin" {
		t.Fatalf("expected rule '-p:big.bin', got %q", res.AddedRules[0])
	}
}

func TestFixAddsRules(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, ".megaignore")
	if err := os.WriteFile(file, []byte("# header\n-s:*\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	violations := []Violation{
		{Type: "bloat", Path: "artifacts/big.zip"},
		{Type: "bloat", Path: "build/output.tar"},
	}
	res, err := Fix(FixOptions{MegaignorePath: file, Violations: violations})
	if err != nil {
		t.Fatalf("fix failed: %v", err)
	}
	if len(res.AddedRules) != 2 {
		t.Fatalf("expected 2 added rules, got %d", len(res.AddedRules))
	}

	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "-p:artifacts/big.zip") {
		t.Fatalf("expected rule in file, got:\n%s", text)
	}
	if !strings.Contains(text, "-p:build/output.tar") {
		t.Fatalf("expected rule in file, got:\n%s", text)
	}
}

func TestFixNoViolations(t *testing.T) {
	res, err := Fix(FixOptions{MegaignorePath: "/unused", Violations: nil})
	if err != nil {
		t.Fatalf("fix failed: %v", err)
	}
	if len(res.AddedRules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(res.AddedRules))
	}
}
