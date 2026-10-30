package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/skill"
)

// fakeProvider puts a shell script named bin on a temp PATH. `bin skill` prints
// body; with fail set it exits 1. It returns the script's folder.
func fakeProvider(t *testing.T, bin, body string, fail bool) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" != skill ]; then exit 2; fi\n"
	if fail {
		script += "echo boom >&2\nexit 1\n"
	} else {
		script += "cat <<'EOF'\n" + body + "EOF\n"
	}
	if err := os.WriteFile(filepath.Join(dir, bin), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	old := skill.Providers
	skill.Providers = []string{bin}
	t.Cleanup(func() { skill.Providers = old })
	return dir
}

const fakeSkill = "---\nname: fake-tool\ndescription: drives fake-tool\n---\nUse fake-tool.\n"

func TestSkillSyncCopiesAndLinks(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	fakeProvider(t, "fake-bin", fakeSkill, false)

	code, out, errOut := e.run("a\n", "skill", "install", "--providers", "--tool", "claude")
	if code != 0 {
		t.Fatalf("sync: %d\n%s%s", code, out, errOut)
	}
	dest := filepath.Join(e.ws, "agent", "skills", "fake-tool")
	got, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	if err != nil || string(got) != fakeSkill {
		t.Fatalf("copy = %q, %v", got, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, skill.ProviderFile)); strings.TrimSpace(string(b)) != "fake-bin" {
		t.Fatalf("marker = %q", b)
	}
	link, err := os.Readlink(filepath.Join(e.home, ".claude", "skills", "fake-tool"))
	if err != nil || link != dest {
		t.Fatalf("link = %q, %v", link, err)
	}
	// A second run has nothing to do, and check is clean.
	if code, out, _ := e.run("", "skill", "install", "--providers"); code != 0 || !strings.Contains(out, "up to date") {
		t.Fatalf("second sync: %d %s", code, out)
	}
	if code, out, _ := e.run("", "skill", "check"); code != 0 || !strings.Contains(out, "No drift") {
		t.Fatalf("check: %d %s", code, out)
	}
}

func TestSkillCheckProviderStaleAndGone(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	dir := fakeProvider(t, "fake-bin", fakeSkill, false)
	if code, out, errOut := e.run("a\n", "skill", "install", "--providers"); code != 0 {
		t.Fatalf("sync: %d %s%s", code, out, errOut)
	}

	// The binary now prints a newer skill: stale, with the fix.
	// --dry-run keeps the run from refreshing the copy first, so check sees it stale.
	fakeProvider(t, "fake-bin", fakeSkill+"More.\n", false)
	code, out, _ := e.run("", "--dry-run", "skill", "check")
	if code != 0 || !strings.Contains(out, "differs from `fake-bin skill`") || !strings.Contains(out, "ws skill install --providers") {
		t.Fatalf("stale: %d\n%s", code, out)
	}
	if code, _, _ := e.run("a\n", "skill", "install", "--providers"); code != 0 {
		t.Fatal("resync failed")
	}
	if _, out, _ := e.run("", "skill", "check"); !strings.Contains(out, "No drift") {
		t.Fatalf("after resync:\n%s", out)
	}

	// The binary is gone.
	if err := os.Remove(filepath.Join(dir, "fake-bin")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir()+":/usr/bin:/bin")
	_, out, _ = e.run("", "skill", "check")
	if !strings.Contains(out, "fake-bin, which is not on PATH") || !strings.Contains(out, "remove ") {
		t.Fatalf("gone:\n%s", out)
	}
}

func TestSkillProviderFailureIsNotFatal(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	fakeProvider(t, "fake-bin", "", true)

	code, out, _ := e.run("", "skill", "check")
	if code != 0 || !strings.Contains(out, "fake-bin") || !strings.Contains(out, "boom") {
		t.Fatalf("check: %d\n%s", code, out)
	}
	code, _, errOut := e.run("", "skill", "install", "--providers")
	if code != 1 || !strings.Contains(errOut, "boom") {
		t.Fatalf("sync: %d %s", code, errOut)
	}
	// Other skills are still listed.
	if _, out, _ := e.run("", "skill", "scan"); !strings.Contains(out, "review-pr") {
		t.Fatalf("scan:\n%s", out)
	}
}

func TestSkillSyncNeverOverwritesYours(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	body := "---\nname: review-pr\n---\nx\n" // review-pr is a skill of yours
	fakeProvider(t, "fake-bin", body, false)
	code, _, errOut := e.run("a\n", "skill", "install", "--providers")
	if code != 1 || !strings.Contains(errOut, "was not copied from fake-bin") {
		t.Fatalf("sync: %d %s", code, errOut)
	}
	got, _ := os.ReadFile(filepath.Join(e.ws, "agent", "skills", "review-pr", "SKILL.md"))
	if strings.Contains(string(got), "\nx\n") {
		t.Fatal("your skill was overwritten")
	}
}

func TestSkillProviderMissingBinaryIsQuiet(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	old := skill.Providers
	skill.Providers = []string{"no-such-bin-xyz"}
	t.Cleanup(func() { skill.Providers = old })
	if code, out, _ := e.run("", "skill", "check"); code != 0 || !strings.Contains(out, "No drift") {
		t.Fatalf("check: %d\n%s", code, out)
	}
}

func TestSkillScanOffersManagedBeforeCopy(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	fakeProvider(t, "fake-bin", fakeSkill, false)

	code, out, _ := e.run("", "skill", "scan")
	if code != 0 || !strings.Contains(out, "Managed (") || !strings.Contains(out, "ready: ws skill install fake-tool") {
		t.Fatalf("scan: %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(e.ws, "agent", "skills", "fake-tool")); err == nil {
		t.Fatal("scan copied the skill")
	}
	if code, out, _ := e.run("", "skill", "show", "fake-tool"); code != 0 || !strings.Contains(out, "managed") || !strings.Contains(out, "ready:") {
		t.Fatalf("show: %d\n%s", code, out)
	}

	// Installing it by name copies it, then links it.
	code, out, errOut := e.run("a\n", "skill", "install", "fake-tool", "--tool", "claude")
	if code != 0 {
		t.Fatalf("install: %d\n%s%s", code, out, errOut)
	}
	dest := filepath.Join(e.ws, "agent", "skills", "fake-tool")
	if got, _ := os.ReadFile(filepath.Join(dest, "SKILL.md")); string(got) != fakeSkill {
		t.Fatalf("copy = %q", got)
	}
	if link, err := os.Readlink(filepath.Join(e.home, ".claude", "skills", "fake-tool")); err != nil || link != dest {
		t.Fatalf("link = %q, %v", link, err)
	}
	if _, out, _ := e.run("", "skill", "scan"); strings.Contains(out, "ready:") || !strings.Contains(out, "Managed (") {
		t.Fatalf("scan after install:\n%s", out)
	}
	if code, out, _ := e.run("", "skill", "check"); code != 0 || !strings.Contains(out, "No drift") {
		t.Fatalf("check: %d %s", code, out)
	}
}

func TestSkillOfferWithoutBinary(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	old := skill.Providers
	skill.Providers = []string{"no-such-bin-xyz"}
	t.Cleanup(func() { skill.Providers = old })

	_, out, _ := e.run("", "skill", "scan")
	if !strings.Contains(out, "no-such-bin-xyz") || !strings.Contains(out, "install no-such-bin-xyz on this machine") {
		t.Fatalf("scan:\n%s", out)
	}
	code, _, errOut := e.run("a\n", "skill", "install", "no-such-bin-xyz", "--tool", "claude")
	if code != 1 || !strings.Contains(errOut, "install no-such-bin-xyz on this machine") {
		t.Fatalf("install: %d %s", code, errOut)
	}
	// The picklist leaves it out.
	_, out, _ = e.run("\n", "skill", "install", "--tool", "claude")
	if strings.Contains(out, "no-such-bin-xyz") {
		t.Fatalf("picklist offers it:\n%s", out)
	}
}
