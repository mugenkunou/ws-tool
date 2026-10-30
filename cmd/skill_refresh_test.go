package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Any ws command refreshes the built-ins, not only `ws skill` and `ws init`.
func TestRunRefreshesBuiltins(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	if code, out, errOut := e.run("y\n", "skill", "install", "workspace-write", "--tool", "claude"); code != 0 {
		t.Fatalf("install: %d %s%s", code, out, errOut)
	}
	managedFile := filepath.Join(e.managed("workspace-write"), "SKILL.md")
	if err := os.Remove(managedFile); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := e.run("", "layout"); code != 0 {
		t.Fatalf("layout: %d %s%s", code, out, errOut)
	}
	if _, err := os.Stat(managedFile); err != nil {
		t.Fatalf("a plain run did not restore the built-in: %v", err)
	}
}

// A stale copy of a tool binary's skill is rewritten on the next run.
func TestRunUpdatesStaleProviderCopy(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	fakeProvider(t, "fake-bin", fakeSkill, false)
	if code, out, errOut := e.run("a\n", "skill", "install", "--providers"); code != 0 {
		t.Fatalf("sync: %d %s%s", code, out, errOut)
	}

	// The binary now ships a newer skill; an ordinary command brings the copy in step.
	fakeProvider(t, "fake-bin", fakeSkill+"More.\n", false)
	code, _, errOut := e.run("", "layout")
	if code != 0 || !strings.Contains(errOut, "updated skill fake-tool from fake-bin") {
		t.Fatalf("run: %d %s", code, errOut)
	}
	got, _ := os.ReadFile(filepath.Join(e.ws, "agent", "skills", "fake-tool", "SKILL.md"))
	if string(got) != fakeSkill+"More.\n" {
		t.Fatalf("copy not updated: %q", got)
	}
}

// A run never adds a provider copy: that stays `ws skill install --providers`.
func TestRunDoesNotAddProviderCopy(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	fakeProvider(t, "fake-bin", fakeSkill, false)
	if code, out, errOut := e.run("", "layout"); code != 0 {
		t.Fatalf("layout: %d %s%s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(e.ws, "agent", "skills", "fake-tool")); err == nil {
		t.Fatal("a run added a provider copy")
	}
}

// --dry-run writes nothing, even a stale copy.
func TestRunDryRunKeepsProviderCopy(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	fakeProvider(t, "fake-bin", fakeSkill, false)
	if code, out, errOut := e.run("a\n", "skill", "install", "--providers"); code != 0 {
		t.Fatalf("sync: %d %s%s", code, out, errOut)
	}
	fakeProvider(t, "fake-bin", fakeSkill+"More.\n", false)
	if code, out, errOut := e.run("", "--dry-run", "layout"); code != 0 {
		t.Fatalf("dry-run: %d %s%s", code, out, errOut)
	}
	got, _ := os.ReadFile(filepath.Join(e.ws, "agent", "skills", "fake-tool", "SKILL.md"))
	if string(got) != fakeSkill {
		t.Fatalf("dry run rewrote the copy: %q", got)
	}
}

// A built-in dev-linked to a checkout gets a note on each run, not with --json.
func TestRunNotesDevLink(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	src := t.TempDir()
	checkout := filepath.Join(src, "skills", "workspace-write")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "SKILL.md"), []byte("live"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := e.run("y\n", "skill", "install", "--dev="+src, "workspace-write"); code != 0 {
		t.Fatalf("install --dev: %d %s%s", code, out, errOut)
	}

	code, _, errOut := e.run("", "layout")
	if code != 0 || !strings.Contains(errOut, "workspace-write runs from "+checkout) || !strings.Contains(errOut, "dev link") {
		t.Fatalf("no dev-link note: %d %s", code, errOut)
	}
	if _, _, errOut := e.run("", "--json", "layout"); strings.Contains(errOut, "dev link") {
		t.Fatalf("--json printed the note: %s", errOut)
	}
}
