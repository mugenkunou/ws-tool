package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// dfEnv is one isolated machine for the dotfile commands: a home directory, an
// XDG trash (TakeLive sends the old copy there, and it must never be the real
// one), and an initialized workspace.
type dfEnv struct {
	home, workspace string
}

func newDfEnv(t *testing.T) dfEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	return dfEnv{home: home, workspace: initWorkspace(t)}
}

func (e dfEnv) file(t *testing.T, rel, content string) string {
	t.Helper()
	p := filepath.Join(e.home, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// run executes ws on the workspace and returns exit code, stdout and stderr.
func (e dfEnv) run(stdin string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	full := append([]string{"--workspace", e.workspace}, args...)
	code := Execute(full, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func (e dfEnv) mustRun(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	code, out, errOut := e.run(stdin, args...)
	if code != 0 {
		t.Fatalf("ws %s: exit %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), code, out, errOut)
	}
	return out
}

func (e dfEnv) ls(t *testing.T) []map[string]any {
	t.Helper()
	return listRecords(t, "--workspace", e.workspace, "dotfile", "ls")
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func isLink(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

// replaceWithFile swaps a symlink for a real file: the "overwritten" status.
func replaceWithFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ── add ────────────────────────────────────────────────────────────

func TestDotfileAddSeveralPathsIsOnePlan(t *testing.T) {
	e := newDfEnv(t)
	a := e.file(t, ".a", "a")
	b := e.file(t, ".b", "b")
	out := e.mustRun(t, "y\ny\n", "dotfile", "add", a, b)
	if strings.Count(out, "[y/n/a/q]") != 2 {
		t.Fatalf("one action per file:\n%s", out)
	}
	want := "Add " + a + " → " + filepath.Join(e.workspace, "configs", "dotfiles", "a")
	if !strings.Contains(out, want) {
		t.Fatalf("the plan line names the storage path (%q):\n%s", want, out)
	}
	if !isLink(a) || !isLink(b) || len(e.ls(t)) != 2 {
		t.Fatal("both files must be managed")
	}
}

func TestDotfileAddFlagAfterPaths(t *testing.T) {
	e := newDfEnv(t)
	dir := filepath.Join(e.home, ".password-store")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.gpg"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(e.workspace, "secrets", "dotfiles", "password-store")
	out := e.mustRun(t, "y\n", "dotfile", "add", dir, "--dir", "--to", to)
	if !strings.Contains(out, "Add directory "+dir+" → "+to+" (whole)") {
		t.Fatalf("plan line:\n%s", out)
	}
	if !isLink(dir) {
		t.Fatal("the directory must be linked")
	}
}

func TestDotfileAddTo(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".gitconfig", "[user]\n")
	to := filepath.Join(e.workspace, "agent", "gitconfig")
	e.mustRun(t, "y\n", "dotfile", "add", p, "--to", to)
	if read(t, to) != "[user]\n" || !isLink(p) {
		t.Fatal("--to must store the copy at the given path")
	}
	// exactly one path
	q := e.file(t, ".q", "q")
	r := e.file(t, ".r", "r")
	if code, _, errOut := e.run("", "dotfile", "add", q, r, "--to", filepath.Join(e.workspace, "x")); code != 1 || !strings.Contains(errOut, "exactly one path") {
		t.Fatalf("--to with two paths: exit %d stderr=%s", code, errOut)
	}
}

func TestDotfileAddClassIsRemoved(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".x", "x")
	code, _, errOut := e.run("", "dotfile", "add", "--class", "secret", p)
	if code != 1 || !strings.Contains(errOut, "--to") {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
	if code, _, errOut = e.run("", "dotfile", "link", "--class=config", p); code != 1 || !strings.Contains(errOut, "--to") {
		t.Fatalf("link --class: exit %d stderr=%s", code, errOut)
	}
}

func TestDotfileAddRefusals(t *testing.T) {
	e := newDfEnv(t)
	real := e.file(t, ".real", "x")
	alias := filepath.Join(e.home, ".alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := e.run("y\n", "dotfile", "add", alias)
	if code != 1 || !strings.Contains(out+errOut, "symlink") {
		t.Fatalf("a symlink: exit %d out=%s err=%s", code, out, errOut)
	}

	inside := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(inside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = e.run("y\n", "dotfile", "add", inside)
	if code != 1 || !strings.Contains(out+errOut, "ws dotfile link") {
		t.Fatalf("a path inside the workspace: exit %d out=%s err=%s", code, out, errOut)
	}

	code, out, errOut = e.run("y\n", "dotfile", "add", filepath.Join(e.home, ".missing"))
	if code != 1 || !strings.Contains(out+errOut, "not found") {
		t.Fatalf("a missing path: exit %d out=%s err=%s", code, out, errOut)
	}

	if code, _, errOut = e.run("", "dotfile", "add", real, "--dir"); code != 1 || !strings.Contains(errOut, "--dir needs a directory") {
		t.Fatalf("--dir on a file: exit %d stderr=%s", code, errOut)
	}
	if code, _, errOut = e.run(""); code != 0 {
		t.Fatalf("ws with only the workspace flag prints help: %d %s", code, errOut)
	}
	if code, _, errOut = e.run("", "dotfile", "add"); code != 1 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("no path: exit %d stderr=%s", code, errOut)
	}
}

func TestDotfileAddReportsManagedAndBrokenEntries(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".bashrc", "x\n")
	e.mustRun(t, "y\n", "dotfile", "add", p)

	out := e.mustRun(t, "", "dotfile", "add", p)
	if !strings.Contains(out, "already managed") || strings.Contains(out, "[y/n/a/q]") {
		t.Fatalf("an ok entry gets no action:\n%s", out)
	}

	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	out = e.mustRun(t, "", "dotfile", "add", p)
	if !strings.Contains(out, "ws dotfile link "+p) || strings.Contains(out, "[y/n/a/q]") {
		t.Fatalf("a broken entry gets the link hint, no action:\n%s", out)
	}
}

// TestDotfileAddTakesTheLiveFileOfAnOverwrittenEntry: the old `dotfile sync`.
func TestDotfileAddTakesTheLiveFileOfAnOverwrittenEntry(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".bashrc", "old\n")
	e.mustRun(t, "y\n", "dotfile", "add", p)
	replaceWithFile(t, p, "live\n")

	out := e.mustRun(t, "y\n", "dotfile", "add", p)
	if !strings.Contains(out, "Add "+p+" again (the live file replaces") || !strings.Contains(out, "the old copy goes to the trash") {
		t.Fatalf("plan line:\n%s", out)
	}
	if read(t, p) != "live\n" || !isLink(p) {
		t.Fatal("the live file becomes the workspace copy, linked back")
	}
	copyPath := filepath.Join(e.workspace, "configs", "dotfiles", "bashrc")
	if read(t, copyPath) != "live\n" {
		t.Fatal("the workspace copy must hold the live content")
	}
	trashed, _ := os.ReadDir(filepath.Join(os.Getenv("XDG_DATA_HOME"), "Trash", "files"))
	if len(trashed) != 1 {
		t.Fatalf("the old copy goes to the trash, got %d items", len(trashed))
	}
}

// TestDotfileSyncAliasOnAHealthyLinkKeepsTheCopy is plan 0002 bug B1: the old
// `dotfile sync` on a healthy link deleted the only copy.
func TestDotfileSyncAliasOnAHealthyLinkKeepsTheCopy(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".bashrc", "precious\n")
	e.mustRun(t, "y\n", "dotfile", "add", p)

	code, out, errOut := e.run("y\n", "dotfile", "sync", p)
	if code != 0 || !strings.Contains(out, "already managed") {
		t.Fatalf("exit %d out=%s err=%s", code, out, errOut)
	}
	if !strings.Contains(errOut, `"dotfile sync" is now "dotfile add"`) {
		t.Fatalf("a typed alias prints the rename: %q", errOut)
	}
	if read(t, p) != "precious\n" || !isLink(p) {
		t.Fatal("the only copy must survive")
	}
}

func TestDotfileAddSecretWarning(t *testing.T) {
	e := newDfEnv(t)
	key := e.file(t, ".ssh/id_ed25519", "-----BEGIN OPENSSH PRIVATE KEY-----\n")
	cfgFile := e.file(t, ".bashrc", "x\n")
	out := e.mustRun(t, "y\ny\ny\n", "dotfile", "add", cfgFile, key)
	if !strings.Contains(out, "1 file(s) look like secrets") || !strings.Contains(out, "Track anyway?") {
		t.Fatalf("secret warning:\n%s", out)
	}
	if !strings.Contains(out, "(secret)") || !strings.Contains(out, filepath.Join("secrets", "dotfiles", "ssh", "id_ed25519")) {
		t.Fatalf("the secret's plan line names secrets/dotfiles:\n%s", out)
	}

	// Answering no drops the secret from the plan.
	key2 := e.file(t, ".ssh/id_rsa", "-----BEGIN RSA PRIVATE KEY-----\n")
	out = e.mustRun(t, "n\n", "dotfile", "add", key2)
	if strings.Contains(out, "[y/n/a/q]") || isLink(key2) {
		t.Fatalf("declined secrets must not be added:\n%s", out)
	}
}

func TestDotfileAddJSONIsOnePlanWithActions(t *testing.T) {
	e := newDfEnv(t)
	a := e.file(t, ".a", "a")
	code, out, errOut := e.run("", "--json", "dotfile", "add", a, filepath.Join(e.home, ".gone"))
	if code != 3 { // one executed, one failed
		t.Fatalf("exit %d, want 3 (partial)\nstdout=%s stderr=%s", code, out, errOut)
	}
	var env struct {
		Command string `json:"command"`
		Data    struct {
			Actions []struct{ Status string } `json:"actions"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if env.Command != "dotfile.add" || len(env.Data.Actions) != 2 {
		t.Fatalf("envelope = %+v", env)
	}
}

// ── link ───────────────────────────────────────────────────────────

func TestDotfileLinkRepairsBrokenAndOverwritten(t *testing.T) {
	e := newDfEnv(t)
	a := e.file(t, ".a", "a\n")
	b := e.file(t, ".b", "b\n")
	c := e.file(t, ".c", "c\n")
	for _, p := range []string{a, b, c} {
		e.mustRun(t, "y\n", "dotfile", "add", p)
	}
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	replaceWithFile(t, b, "live\n")

	out := e.mustRun(t, "y\ny\n", "dotfile", "link")
	if strings.Count(out, "[y/n/a/q]") != 2 {
		t.Fatalf("one action per entry that is not ok:\n%s", out)
	}
	if !strings.Contains(out, "(the live file moves to "+b+".ws-backup)") {
		t.Fatalf("a displaced live file is named in the plan line:\n%s", out)
	}
	if !isLink(a) || !isLink(b) || read(t, b+".ws-backup") != "live\n" {
		t.Fatal("both links repaired, the live file kept as .ws-backup")
	}

	out = e.mustRun(t, "", "dotfile", "link")
	if strings.TrimSpace(out) != "All dotfile links are ok." {
		t.Fatalf("nothing to repair:\n%s", out)
	}
}

func TestDotfileLinkNamedEntries(t *testing.T) {
	e := newDfEnv(t)
	a := e.file(t, ".a", "a\n")
	b := e.file(t, ".b", "b\n")
	e.mustRun(t, "y\n", "dotfile", "add", a)
	e.mustRun(t, "y\n", "dotfile", "add", b)
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	e.mustRun(t, "y\n", "dotfile", "link", a)
	if !isLink(a) || isLink(b) {
		t.Fatal("only the named entry is repaired")
	}
	if out := e.mustRun(t, "", "dotfile", "link", a); !strings.Contains(out, "already linked") {
		t.Fatalf("an ok target is reported as already linked:\n%s", out)
	}
	if code, _, errOut := e.run("", "dotfile", "link", filepath.Join(e.home, ".unmanaged")); code != 1 || !strings.Contains(errOut, "not a managed dotfile") {
		t.Fatalf("an unknown target: exit %d stderr=%s", code, errOut)
	}
}

func TestDotfileLinkMissingCopyExitCodes(t *testing.T) {
	e := newDfEnv(t)
	good := e.file(t, ".good", "g\n")
	bad := e.file(t, ".bad", "b\n")
	e.mustRun(t, "y\n", "dotfile", "add", good)
	e.mustRun(t, "y\n", "dotfile", "add", bad)
	if err := os.Remove(good); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(bad); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(e.workspace, "configs", "dotfiles", "bad")); err != nil {
		t.Fatal(err)
	}

	// A missing copy is reported, not offered as an action, and counts as failed.
	code, out, _ := e.run("y\n", "dotfile", "link")
	if code != 3 || !strings.Contains(out, bad+": workspace copy") || !strings.Contains(out, "is missing") {
		t.Fatalf("a good entry and a missing copy exit 3, got %d:\n%s", code, out)
	}
	if strings.Count(out, "[y/n/a/q]") != 1 {
		t.Fatalf("the missing copy is not an action:\n%s", out)
	}

	code, out, _ = e.run("y\n", "dotfile", "link", bad)
	if code != 1 {
		t.Fatalf("only a missing copy exits 1, got %d:\n%s", code, out)
	}

	code, out, _ = e.run("", "--json", "dotfile", "link", bad)
	if code != 1 || !strings.Contains(out, `"status": "failed"`) {
		t.Fatalf("--json reports the failure and exits 1, got %d:\n%s", code, out)
	}
}

// TestDotfileLinkToDryRunJSONHasActions is plan 0002 bug B10: link --dry-run ran
// the plan and then called Link again, and --json had no actions.
func TestDotfileLinkToDryRunJSONHasActions(t *testing.T) {
	e := newDfEnv(t)
	agent := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(agent, []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sys := filepath.Join(e.home, ".claude", "CLAUDE.md")
	code, out, errOut := e.run("", "--json", "--dry-run", "dotfile", "link", sys, "--to", agent)
	if code != 0 {
		t.Fatalf("exit %d out=%s err=%s", code, out, errOut)
	}
	var env struct {
		DryRun bool `json:"dry_run"`
		Data   struct {
			System  string                    `json:"system"`
			Path    string                    `json:"workspace_path"`
			Actions []struct{ Status string } `json:"actions"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if !env.DryRun || env.Data.System != sys || env.Data.Path != agent || len(env.Data.Actions) != 1 || env.Data.Actions[0].Status != "dry-run" {
		t.Fatalf("envelope = %+v", env)
	}
	if _, err := os.Lstat(sys); err == nil {
		t.Fatal("--dry-run must change nothing")
	}
}

func TestDotfileLinkToManagesAWorkspaceFile(t *testing.T) {
	e := newDfEnv(t)
	agent := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(agent, []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sys := e.file(t, ".claude/CLAUDE.md", "mine\n")

	out := e.mustRun(t, "y\n", "dotfile", "link", sys, "--to", agent)
	if !strings.Contains(out, "Link "+sys+" → "+agent+" (new entry; the live file moves to "+sys+".ws-backup)") {
		t.Fatalf("plan line:\n%s", out)
	}
	if read(t, sys) != "rules\n" || read(t, sys+".ws-backup") != "mine\n" || read(t, agent) != "rules\n" {
		t.Fatal("link --to renames what was there, and the workspace file stays")
	}
	recs := e.ls(t)
	if len(recs) != 1 || recs[0]["original"] != "agent/AGENTS.md" {
		t.Fatalf("records = %v", recs)
	}
}

// TestDotfileLinkToResolvesAgainstTheCwd: <ws-path> follows the input rule
// (relative to the current directory), not the workspace root.
func TestDotfileLinkToResolvesAgainstTheCwd(t *testing.T) {
	e := newDfEnv(t)
	agent := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(agent, []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sys := filepath.Join(e.home, "CLAUDE.md")

	// From the workspace root, agent/AGENTS.md is the file.
	t.Chdir(e.workspace)
	e.mustRun(t, "y\n", "dotfile", "link", sys, "--to", "agent/AGENTS.md")
	if !isLink(sys) {
		t.Fatal("a cwd-relative path inside the workspace must work")
	}

	// From anywhere else it is a different path, and is refused.
	other := filepath.Join(e.home, ".other")
	t.Chdir(t.TempDir())
	code, _, errOut := e.run("", "dotfile", "link", other, "--to", "agent/AGENTS.md")
	if code != 1 || !strings.Contains(errOut, "inside the workspace") {
		t.Fatalf("outside the workspace: exit %d stderr=%s", code, errOut)
	}
}

func TestDotfileLinkTwoPathAliasStillWorks(t *testing.T) {
	e := newDfEnv(t)
	agent := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(agent, []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sys := filepath.Join(e.home, "CLAUDE.md")
	code, _, errOut := e.run("y\n", "dotfile", "link", sys, agent)
	if code != 0 || !isLink(sys) {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, `is now "dotfile link <path> --to <ws-path>"`) {
		t.Fatalf("the rename hint: %q", errOut)
	}
}

// ── unlink ─────────────────────────────────────────────────────────

func TestDotfileUnlinkNeedsPathsOrAll(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".a", "a")
	for _, args := range [][]string{{"dotfile", "unlink"}, {"dotfile", "unlink", "--all", p}} {
		if code, _, errOut := e.run("", args...); code != 1 || !strings.Contains(errOut, "usage:") {
			t.Fatalf("ws %v: exit %d stderr=%s", args, code, errOut)
		}
	}
	if code, _, errOut := e.run("", "dotfile", "unlink", p); code != 1 || !strings.Contains(errOut, "not a managed dotfile") {
		t.Fatalf("an unknown target: exit %d stderr=%s", code, errOut)
	}
	if out := e.mustRun(t, "", "dotfile", "unlink", "--all"); !strings.Contains(out, "No managed dotfiles.") {
		t.Fatalf("nothing managed:\n%s", out)
	}
}

func TestDotfileUnlinkPlanLines(t *testing.T) {
	e := newDfEnv(t)
	a := e.file(t, ".bashrc", "a\n")
	e.mustRun(t, "y\n", "dotfile", "add", a)
	agent := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(agent, []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(e.home, "CLAUDE.md")
	e.mustRun(t, "y\n", "dotfile", "link", b, "--to", agent)
	replaceWithFile(t, a, "live\n")

	out := e.mustRun(t, "y\ny\n", "dotfile", "unlink", a, b)
	if !strings.Contains(out, "Unlink "+a+" (move "+filepath.Join(e.workspace, "configs", "dotfiles", "bashrc")+" back; the live file moves to "+a+".ws-backup)") {
		t.Fatalf("owned copy, overwritten:\n%s", out)
	}
	if !strings.Contains(out, "Unlink "+b+" (copy "+agent+" there; the original stays)") {
		t.Fatalf("shared copy:\n%s", out)
	}
	if read(t, agent) != "rules\n" || read(t, b) != "rules\n" || read(t, a) != "a\n" || read(t, a+".ws-backup") != "live\n" {
		t.Fatal("unlink results")
	}
	if len(e.ls(t)) != 0 {
		t.Fatal("both entries removed")
	}
}

func TestDotfileUnlinkAllIsOneActionPerEntry(t *testing.T) {
	e := newDfEnv(t)
	for _, n := range []string{".a", ".b", ".c"} {
		e.mustRun(t, "y\n", "dotfile", "add", e.file(t, n, "x"))
	}
	out := e.mustRun(t, "y\ny\ny\n", "dotfile", "unlink", "--all")
	if strings.Count(out, "[y/n/a/q]") != 3 || len(e.ls(t)) != 0 {
		t.Fatalf("--all builds one action per entry (it used to be a single action):\n%s", out)
	}
}

// TestDotfileUnlinkPartialJSONExit3: with --json the exit code follows the plan.
func TestDotfileUnlinkPartialJSONExit3(t *testing.T) {
	e := newDfEnv(t)
	a := e.file(t, ".a", "a")
	b := e.file(t, ".b", "b")
	e.mustRun(t, "y\n", "dotfile", "add", a)
	e.mustRun(t, "y\n", "dotfile", "add", b)
	// Make b's unlink fail: its system parent is read-only and the live file is
	// in the way, so the backup rename fails.
	replaceWithFile(t, b, "live")
	if err := os.Chmod(e.home, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(e.home, 0o700)
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	code, out, _ := e.run("", "--json", "dotfile", "unlink", "--all")
	if code != 3 && code != 1 {
		t.Fatalf("a failed action must show in the exit code, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, `"status": "failed"`) {
		t.Fatalf("the failed action is reported:\n%s", out)
	}
}

// ── edit ───────────────────────────────────────────────────────────

// terminalEditor puts a `vim` with the given exit code on PATH. vim is a terminal
// editor, so ws waits for it and its exit code is the command's.
func terminalEditor(t *testing.T, exit int) {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nexit %d\n", exit)
	if err := os.WriteFile(filepath.Join(dir, "vim"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VISUAL", "vim")
}

func TestDotfileEdit(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".bashrc", "x\n")
	e.mustRun(t, "y\n", "dotfile", "add", p)

	terminalEditor(t, 0)
	if code, out, errOut := e.run("", "dotfile", "edit", p); code != 0 {
		t.Fatalf("the editor exits 0: exit %d out=%s err=%s", code, out, errOut)
	}
	terminalEditor(t, 3)
	if code, _, _ := e.run("", "dotfile", "edit", p); code != 1 {
		t.Fatalf("a failing editor exits 1, got %d", code)
	}
	terminalEditor(t, 0)
	if code, _, errOut := e.run("", "dotfile", "edit", filepath.Join(e.home, ".unmanaged")); code != 1 || !strings.Contains(errOut, "not a managed dotfile") {
		t.Fatalf("an unmanaged path: exit %d stderr=%s", code, errOut)
	}

	// An overwritten entry warns that edits reach the system only after link.
	replaceWithFile(t, p, "live\n")
	code, _, errOut := e.run("", "dotfile", "edit", p)
	if code != 0 || !strings.Contains(errOut, "ws dotfile link "+p) {
		t.Fatalf("overwritten warning: exit %d stderr=%s", code, errOut)
	}

	// A missing workspace copy is an error.
	if err := os.Remove(filepath.Join(e.workspace, "configs", "dotfiles", "bashrc")); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut = e.run("", "dotfile", "edit", p); code != 1 || !strings.Contains(errOut, "missing") {
		t.Fatalf("a missing copy: exit %d stderr=%s", code, errOut)
	}
}

func TestDotfileEditJSON(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".bashrc", "x\n")
	e.mustRun(t, "y\n", "dotfile", "add", p)
	terminalEditor(t, 0)
	code, out, errOut := e.run("", "--json", "dotfile", "edit", p)
	if code != 0 {
		t.Fatalf("exit %d err=%s", code, errOut)
	}
	var env struct {
		Command string `json:"command"`
		Data    struct{ System, WorkspacePath string }
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if env.Command != "dotfile.edit" {
		t.Fatalf("command = %q", env.Command)
	}
}

// ── ls ─────────────────────────────────────────────────────────────

func TestDotfileLsColumnsAndNoTargets(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".bashrc", "x\n")
	e.mustRun(t, "y\n", "dotfile", "add", p)

	out := e.mustRun(t, "", "dotfile", "ls")
	head := strings.Fields(strings.SplitN(out, "\n", 2)[0])
	if strings.Join(head, " ") != "SYSTEM PATH WORKSPACE COPY STATUS SUDO" {
		t.Fatalf("default columns: %v\n%s", head, out)
	}
	if !strings.Contains(out, filepath.Join(e.workspace, "configs", "dotfiles", "bashrc")) {
		t.Fatalf("the workspace copy is printed absolute:\n%s", out)
	}
	if code, _, errOut := e.run("", "dotfile", "ls", p); code != 1 || !strings.Contains(errOut, "no targets") {
		t.Fatalf("ls takes no targets: exit %d stderr=%s", code, errOut)
	}
	// No class anywhere.
	recs := e.ls(t)
	if _, has := recs[0]["class"]; has {
		t.Fatalf("class is removed from the record: %v", recs[0])
	}
}

// ── backup ─────────────────────────────────────────────────────────

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func bareRemote(t *testing.T) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	if o, err := exec.Command("git", "init", "--bare", "--initial-branch=main", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v %s", err, o)
	}
	return remote
}

func TestDotfileBackupPushAndStatusJSON(t *testing.T) {
	needGit(t)
	e := newDfEnv(t)
	remote := bareRemote(t)
	e.mustRun(t, "y\ny\ny\n", "dotfile", "backup", "enable", "--remote", remote)
	e.mustRun(t, "y\n", "dotfile", "add", e.file(t, ".vimrc", "set nu\n"))
	// A change the auto-commit did not see, so the explicit push has something to commit.
	if err := os.WriteFile(filepath.Join(e.workspace, "configs", "dotfiles", "extra"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := e.mustRun(t, "y\n", "dotfile", "backup", "push", "-m", "my message")
	if !strings.Contains(out, "Commit pending changes and push main to "+remote) {
		t.Fatalf("plan line:\n%s", out)
	}
	if o, err := exec.Command("git", "-C", remote, "log", "--format=%s").Output(); err != nil || !strings.Contains(string(o), "my message") {
		t.Fatalf("the remote has the commit: %v %s", err, o)
	}

	_, js, _ := e.run("", "--json", "dotfile", "backup", "status")
	var env struct {
		Command string         `json:"command"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(js), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, js)
	}
	if env.Command != "dotfile.backup.status" {
		t.Fatalf("command = %q", env.Command)
	}
	for _, k := range []string{"enabled", "initialized", "repo_path", "remote_url", "branch", "ahead", "behind", "auto_commit", "auto_push", "pass_entry", "uncommitted", "last_commit", "commits"} {
		if _, ok := env.Data[k]; !ok {
			t.Errorf("status JSON lacks %q: %v", k, env.Data)
		}
	}
	if env.Data["enabled"] != true || env.Data["ahead"].(float64) != 0 {
		t.Fatalf("status = %v", env.Data)
	}

	pushJSON := func() map[string]any {
		_, js, _ := e.run("", "--json", "dotfile", "backup", "push")
		var env struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(js), &env); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, js)
		}
		return env.Data
	}
	data := pushJSON()
	for _, k := range []string{"actions", "committed", "pushed", "remote_url", "branch"} {
		if _, ok := data[k]; !ok {
			t.Errorf("push JSON lacks %q: %v", k, data)
		}
	}
}

func TestDotfileBackupPushNeedsAnEnabledBackup(t *testing.T) {
	e := newDfEnv(t)
	code, _, errOut := e.run("", "dotfile", "backup", "push")
	if code != 1 || !strings.Contains(errOut, "not enabled") {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
}

func TestDotfileBackupDisableClearsTheRemoteAndTheCronJob(t *testing.T) {
	needGit(t)
	e := newDfEnv(t)
	remote := bareRemote(t)
	e.mustRun(t, "y\ny\ny\n", "dotfile", "backup", "enable", "--remote", remote)

	// A cron job from an old crontab line counts, under its old name.
	crontab := os.Getenv("WS_CRONTAB_FILE")
	block := "# >>> ws:dotfile-sync >>>\n*/30 * * * * /bin/ws cron run dotfile-sync\n# <<< ws:dotfile-sync <<<\n"
	if err := os.WriteFile(crontab, []byte("MAILTO=me\n"+block), 0o600); err != nil {
		t.Fatal(err)
	}
	// The same block is declared in configs/crontab, which `cron apply` reads.
	declared := filepath.Join(e.workspace, "configs", "crontab")
	if err := os.WriteFile(declared, []byte(block), 0o644); err != nil {
		t.Fatal(err)
	}

	out := e.mustRun(t, "y\ny\n", "dotfile", "backup", "disable")
	if !strings.Contains(out, "Remove the backup remote "+remote) || !strings.Contains(out, "Remove dotfile-backup from the crontab and "+declared) {
		t.Fatalf("plan lines:\n%s", out)
	}
	if got := read(t, crontab); strings.Contains(got, "dotfile-sync") || !strings.Contains(got, "MAILTO=me") {
		t.Fatalf("the ws block goes, other lines stay:\n%s", got)
	}
	if got := read(t, declared); strings.Contains(got, "dotfile-sync") {
		t.Fatalf("the declared block goes too, or cron apply brings the job back:\n%s", got)
	}
	if o, _ := exec.Command("git", "-C", filepath.Join(e.workspace, "configs", "dotfiles"), "remote").Output(); strings.TrimSpace(string(o)) != "" {
		t.Fatalf("origin must be removed, got %q", o)
	}
	// The history stays, and the auto settings keep their values.
	if !strings.Contains(e.mustRun(t, "", "dotfile", "backup", "status"), "disabled") {
		t.Fatal("status reads disabled")
	}
	out = e.mustRun(t, "", "dotfile", "backup", "disable")
	if !strings.Contains(out, "not enabled") {
		t.Fatalf("disabling twice is a no-op:\n%s", out)
	}
}

func TestDotfileBackupEnableRefusesAPublicRepository(t *testing.T) {
	// A fake API cannot be injected, so this pins the cmd side of Refusal: a
	// remote that cannot be determined (a path url) warns and goes on.
	needGit(t)
	e := newDfEnv(t)
	out := e.mustRun(t, "y\ny\ny\n", "dotfile", "backup", "enable", "--remote", bareRemote(t))
	if !strings.Contains(out, "cannot verify repository visibility") {
		t.Fatalf("an undetermined check warns and continues:\n%s", out)
	}
}

func TestDotfileGitAliasesReachBackup(t *testing.T) {
	needGit(t)
	e := newDfEnv(t)
	remote := bareRemote(t)
	code, out, errOut := e.run("y\ny\ny\n", "dotfile", "git", "remote", remote)
	if code != 0 {
		t.Fatalf("exit %d out=%s err=%s", code, out, errOut)
	}
	if !strings.Contains(errOut, `"dotfile git remote" is now "dotfile backup enable --remote <url>"`) {
		t.Fatalf("hint: %q", errOut)
	}
	code, out, errOut = e.run("", "dotfile", "git", "status")
	if code != 0 || !strings.Contains(out, "enabled") || !strings.Contains(errOut, `"dotfile git status" is now "dotfile backup status"`) {
		t.Fatalf("exit %d out=%s err=%s", code, out, errOut)
	}
}

func TestDotfileMigrateStaysHidden(t *testing.T) {
	e := newDfEnv(t)
	out := e.mustRun(t, "", "dotfile", "migrate")
	if !strings.Contains(out, "No directory entries") {
		t.Fatalf("migrate still runs:\n%s", out)
	}
	var help bytes.Buffer
	Execute([]string{"dotfile", "--help"}, strings.NewReader(""), &help, &bytes.Buffer{})
	if strings.Contains(help.String(), "migrate") {
		t.Fatal("migrate is hidden from help")
	}
}

func TestDotfileUnknownVerb(t *testing.T) {
	e := newDfEnv(t)
	code, _, errOut := e.run("", "dotfile", "frobnicate")
	if code != 1 || !strings.Contains(errOut, "unknown command: dotfile frobnicate") {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
}
