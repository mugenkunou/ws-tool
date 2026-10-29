package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// machine puts three known configs in the home directory: two ordinary (git), one
// secret with its .pub (ssh).
func (e dfEnv) machine(t *testing.T) {
	t.Helper()
	e.file(t, ".gitconfig", "[user]\n")
	e.file(t, ".gitignore_global", "*.swp\n")
	e.file(t, ".ssh/id_rsa", "-----BEGIN OPENSSH PRIVATE KEY-----\n")
	e.file(t, ".ssh/id_rsa.pub", "ssh-rsa AAAA\n")
}

// ── ls: statuses, nothing suggested ────────────────────────────────

// TestKnownConfigsAreNeverSuggested: known configs on the machine change nothing
// in `ls`, `add` with no path takes none, and the removed pieces are gone.
func TestKnownConfigsAreNeverSuggested(t *testing.T) {
	e := newDfEnv(t)
	e.machine(t)
	out := e.mustRun(t, "", "symlink", "ls")
	if !strings.Contains(out, "No managed symlinks.") || strings.Contains(out, ".gitconfig") || strings.Contains(out, "known config") {
		t.Fatalf("ls:\n%s", out)
	}
	if recs := listRecords(t, "--workspace", e.workspace, "symlink", "ls", "--filter", "status=found"); len(recs) != 0 {
		t.Fatalf("no record is ever found: %v", recs)
	}
	if code, _, errOut := e.run("", "symlink", "add"); code != 1 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("add with no path: exit %d stderr=%s", code, errOut)
	}
	if code, _, _ := e.run("", "symlink", "add", "--app", "git"); code != 1 {
		t.Fatalf("--app is gone: exit %d", code)
	}
	if isLink(filepath.Join(e.home, ".gitconfig")) {
		t.Fatal("nothing was added")
	}
}

// TestLsJSONFields: the documented fields of a record.
func TestLsJSONFields(t *testing.T) {
	e := newDfEnv(t)
	e.mustRun(t, "y\n", "symlink", "add", e.file(t, ".a", "a"))
	recs := e.ls(t)
	if len(recs) != 1 {
		t.Fatalf("records = %d", len(recs))
	}
	keys := make([]string, 0, len(recs[0]))
	for k := range recs[0] {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if want := []string{"app", "dir", "message", "original", "status", "sudo", "system", "workspace_path"}; !slices.Equal(keys, want) {
		t.Fatalf("fields = %v, want %v", keys, want)
	}
}

func TestLsShowsEveryStatus(t *testing.T) {
	e := newDfEnv(t)
	ok := e.file(t, ".a", "a")
	missing := e.file(t, ".b", "b")
	broken := e.file(t, ".c", "c")
	drifted := e.file(t, ".d", "d")
	for _, p := range []string{ok, missing, broken, drifted} {
		e.mustRun(t, "y\n", "symlink", "add", p)
	}
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(e.workspace, "symlinks", "home", ".c")); err != nil {
		t.Fatal(err)
	}
	replaceWithFile(t, drifted, "live")
	// untracked: a link into the workspace that ws.json does not know.
	target := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(e.home, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	// orphan: content under symlinks/ with no entry.
	orphan := filepath.Join(e.workspace, "symlinks", "home", ".zshrc")
	if err := os.WriteFile(orphan, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := map[string]string{}
	for _, r := range e.ls(t) {
		got[filepath.Base(r["system"].(string))] = r["status"].(string)
	}
	want := map[string]string{".a": "ok", ".b": "missing", ".c": "broken", ".d": "drifted", "AGENTS.md": "untracked", ".zshrc": "orphan"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: status %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
}

// TestSymlinkHelpFitsEightyColumns: the noun's help and each verb's help (with the
// examples) fit a narrow terminal.
func TestSymlinkHelpFitsEightyColumns(t *testing.T) {
	for _, args := range [][]string{{"symlink"}, {"symlink", "ls"}, {"symlink", "add"}, {"symlink", "link"}, {"symlink", "unlink"}} {
		code, out, errOut := (dfEnv{}).run("", append(args, "--help")...)
		if code != 0 {
			t.Fatalf("%v: exit %d %s", args, code, errOut)
		}
		for _, line := range strings.Split(out, "\n") {
			if n := len([]rune(line)); n > 80 {
				t.Errorf("ws %s --help: %d columns: %q", strings.Join(args, " "), n, line)
			}
		}
	}
	_, out, _ := (dfEnv{}).run("", "symlink", "add", "--help")
	for _, want := range []string{"Examples:", "--files"} {
		if !strings.Contains(out, want) {
			t.Errorf("add help lacks %q:\n%s", want, out)
		}
	}
}
