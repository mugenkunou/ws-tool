package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dfEnv is one isolated machine for the symlink commands: a home directory, an
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
	return listRecords(t, "--workspace", e.workspace, "symlink", "ls")
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

func TestSymlinkAddPlanLineNamesBothPaths(t *testing.T) {
	e := newDfEnv(t)
	a := e.file(t, ".a", "a")
	out := e.mustRun(t, "y\n", "symlink", "add", a)
	want := "Add " + a + " → " + filepath.Join(e.workspace, "symlinks", "home", ".a")
	if !strings.Contains(out, want) {
		t.Fatalf("the plan line names both paths (%q):\n%s", want, out)
	}
	if !isLink(a) || len(e.ls(t)) != 1 {
		t.Fatal("the file must be managed")
	}
	if code, _, errOut := e.run("", "symlink", "add", a, "x", "y"); code != 1 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("three paths: exit %d stderr=%s", code, errOut)
	}
}

// TestSymlinkAddTwoPathForm: the second argument is where the copy goes, relative
// to the workspace or absolute (it replaces --to).
func TestSymlinkAddTwoPathForm(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".notes", "n\n")
	out := e.mustRun(t, "y\n", "symlink", "add", p, "agent/notes")
	dest := filepath.Join(e.workspace, "agent", "notes")
	if !strings.Contains(out, "Add "+p+" → "+dest) || read(t, dest) != "n\n" || !isLink(p) {
		t.Fatalf("two-path add:\n%s", out)
	}
	recs := e.ls(t)
	if len(recs) != 1 || recs[0]["original"] != "agent/notes" {
		t.Fatalf("records = %v", recs)
	}
	// The destination must exist nowhere else, and lie inside the workspace.
	q := e.file(t, ".other", "o")
	if code, _, errOut := e.run("y\n", "symlink", "add", q, filepath.Join(t.TempDir(), "x")); code != 1 || !strings.Contains(errOut, "inside the workspace") {
		t.Fatalf("outside the workspace: exit %d stderr=%s", code, errOut)
	}
	if code, _, errOut := e.run("y\n", "symlink", "add", q, "agent/notes"); code != 1 || !strings.Contains(errOut, "already the original") {
		t.Fatalf("an entry's original: exit %d stderr=%s", code, errOut)
	}
	if isLink(q) {
		t.Fatal("a refused add changes nothing")
	}
}

func TestSymlinkAddDryRunChangesNothing(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".a", "a")
	out := e.mustRun(t, "", "--dry-run", "symlink", "add", p)
	if !strings.Contains(out, "[dry-run]") || isLink(p) || len(e.ls(t)) != 0 {
		t.Fatalf("dry-run:\n%s", out)
	}
	if _, err := os.Lstat(filepath.Join(e.workspace, "symlinks", "home", ".a")); err == nil {
		t.Fatal("nothing may move")
	}
}

// TestSymlinkAddFolderRules: a catalog folder is one entry, --files is one entry
// per file, an unknown folder asks, and --dir forces one entry.
func TestSymlinkAddFolderRules(t *testing.T) {
	e := newDfEnv(t)
	first := e.file(t, ".claude/commands/a.md", "a")
	e.file(t, ".claude/commands/b.md", "b")
	dir := filepath.Dir(first)
	out := e.mustRun(t, "y\n", "symlink", "add", dir)
	if strings.Count(out, "[y/n/a/q]") != 1 || !isLink(dir) {
		t.Fatalf("a catalog folder is one entry:\n%s", out)
	}
	recs := e.ls(t)
	if len(recs) != 1 || recs[0]["dir"] != true || recs[0]["original"] != "symlinks/home/.claude/commands" {
		t.Fatalf("records = %v", recs)
	}

	e.file(t, "stuff/one.txt", "1")
	e.file(t, "stuff/two.txt", "2")
	stuff := filepath.Join(e.home, "stuff")
	out = e.mustRun(t, "y\ny\n", "symlink", "add", stuff, "--files")
	if strings.Count(out, "[y/n/a/q]") != 2 || isLink(stuff) || !isLink(filepath.Join(stuff, "one.txt")) {
		t.Fatalf("--files is one entry per file:\n%s", out)
	}

	// An unknown folder asks; a catalog secret starts unchecked.
	e.file(t, "mix/plain.txt", "p")
	e.file(t, ".ssh/id_rsa", "k")
	ssh := filepath.Join(e.home, ".ssh")
	out = e.mustRun(t, "\ny\n", "symlink", "add", ssh)
	if !strings.Contains(out, "secret") || isLink(filepath.Join(ssh, "id_rsa")) {
		t.Fatalf("the pick list leaves a secret unchecked by default:\n%s", out)
	}
	if code, _, errOut := e.run("", "symlink", "add", filepath.Join(e.home, "mix"), "agent/mix"); code != 1 || !strings.Contains(errOut, "--dir") {
		t.Fatalf("an unknown folder with a ws-path needs --dir: exit %d stderr=%s", code, errOut)
	}
}

func TestSymlinkAddFlagAfterPaths(t *testing.T) {
	e := newDfEnv(t)
	dir := filepath.Join(e.home, ".password-store")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.gpg"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(e.workspace, "symlinks", "home", ".password-store")
	out := e.mustRun(t, "y\n", "symlink", "add", dir, "--dir")
	if !strings.Contains(out, "Add directory "+dir+" → "+to+" (whole)") {
		t.Fatalf("plan line:\n%s", out)
	}
	if !isLink(dir) {
		t.Fatal("the directory must be linked")
	}
}

// TestSymlinkClassAndToAreRemoved: --class and --to exit 1 with the one-line
// pointer to the two-path form (plan 0003 §3). Nothing moves.
func TestSymlinkClassAndToAreRemoved(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".x", "x")
	want := "--class/--to were removed: pass the workspace path as a second argument"
	for _, args := range [][]string{
		{"symlink", "add", "--class", "secret", p},
		{"symlink", "add", p, "--to", filepath.Join(e.workspace, "agent", "x")},
		{"symlink", "link", "--class=config", p},
		{"symlink", "link", p, "--to", filepath.Join(e.workspace, "agent", "x")},
	} {
		if code, _, errOut := e.run("", args...); code != 1 || !strings.Contains(errOut, want) {
			t.Fatalf("ws %v: exit %d stderr=%s", args, code, errOut)
		}
	}
	if isLink(p) || len(e.ls(t)) != 0 {
		t.Fatal("a rejected flag must change nothing")
	}
}

func TestSymlinkAddRefusals(t *testing.T) {
	e := newDfEnv(t)
	real := e.file(t, ".real", "x")
	alias := filepath.Join(e.home, ".alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := e.run("y\n", "symlink", "add", alias)
	if code != 1 || !strings.Contains(out+errOut, "link to somewhere else") {
		t.Fatalf("a symlink: exit %d out=%s err=%s", code, out, errOut)
	}

	inside := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(inside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = e.run("y\n", "symlink", "add", inside)
	if code != 1 || !strings.Contains(out+errOut, "inside the workspace") {
		t.Fatalf("a path inside the workspace: exit %d out=%s err=%s", code, out, errOut)
	}

	code, out, errOut = e.run("y\n", "symlink", "add", filepath.Join(e.home, ".missing"))
	if code != 1 || !strings.Contains(out+errOut, "nothing to add") {
		t.Fatalf("a missing path: exit %d out=%s err=%s", code, out, errOut)
	}

	if code, _, errOut = e.run("", "symlink", "add", real, "--dir"); code != 1 || !strings.Contains(errOut, "--dir needs a directory") {
		t.Fatalf("--dir on a file: exit %d stderr=%s", code, errOut)
	}
	if code, _, errOut = e.run(""); code != 0 {
		t.Fatalf("ws with only the workspace flag prints help: %d %s", code, errOut)
	}
	// With no path, add asks (or, without a terminal, lists what was found): exit 0.
	asTerminal(t, false)
	if code, out, errOut = e.run("", "symlink", "add"); code != 0 {
		t.Fatalf("no path: exit %d out=%s stderr=%s", code, out, errOut)
	}
	if code, _, errOut = e.run("", "symlink", "add", real, "agent/x", "extra"); code != 1 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("three paths: exit %d stderr=%s", code, errOut)
	}
}

func TestSymlinkAddReportsManagedAndBrokenEntries(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".bashrc", "x\n")
	e.mustRun(t, "y\n", "symlink", "add", p)

	out := e.mustRun(t, "", "symlink", "add", p)
	if !strings.Contains(out, "already managed") || strings.Contains(out, "[y/n/a/q]") {
		t.Fatalf("an ok entry gets no action:\n%s", out)
	}

	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	out = e.mustRun(t, "", "symlink", "add", p)
	if !strings.Contains(out, "ws symlink link "+p) || strings.Contains(out, "[y/n/a/q]") {
		t.Fatalf("a broken entry gets the link hint, no action:\n%s", out)
	}
}

// TestSymlinkAddTakesTheLiveFileOfAnOverwrittenEntry: a repeated `symlink add` re-takes it.
func TestSymlinkAddTakesTheLiveFileOfAnOverwrittenEntry(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".bashrc", "old\n")
	e.mustRun(t, "y\n", "symlink", "add", p)
	replaceWithFile(t, p, "live\n")

	out := e.mustRun(t, "y\n", "symlink", "add", p)
	if !strings.Contains(out, "Add "+p+" again (the live file replaces") || !strings.Contains(out, "the old copy goes to the trash") {
		t.Fatalf("plan line:\n%s", out)
	}
	if read(t, p) != "live\n" || !isLink(p) {
		t.Fatal("the live file becomes the workspace copy, linked back")
	}
	copyPath := filepath.Join(e.workspace, "symlinks", "home", ".bashrc")
	if read(t, copyPath) != "live\n" {
		t.Fatal("the workspace copy must hold the live content")
	}
	trashed, _ := os.ReadDir(filepath.Join(os.Getenv("XDG_DATA_HOME"), "Trash", "files"))
	if len(trashed) != 1 {
		t.Fatalf("the old copy goes to the trash, got %d items", len(trashed))
	}
}

// TestSymlinkAddOnAHealthyLinkKeepsTheCopy: `symlink add` on a healthy link
// must not delete the only copy.
func TestSymlinkAddOnAHealthyLinkKeepsTheCopy(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".bashrc", "precious\n")
	e.mustRun(t, "y\n", "symlink", "add", p)

	code, out, errOut := e.run("y\n", "symlink", "add", p)
	if code != 0 || !strings.Contains(out, "already managed") {
		t.Fatalf("exit %d out=%s err=%s", code, out, errOut)
	}
	if read(t, p) != "precious\n" || !isLink(p) {
		t.Fatal("the only copy must survive")
	}
}

// TestSymlinkAddAsksNoSecretQuestion: there is no secret detection at add time (ADR
// 0003): a private key is stored beside everything else, with no warning.
func TestSymlinkAddAsksNoSecretQuestion(t *testing.T) {
	e := newDfEnv(t)
	key := e.file(t, ".ssh/id_ed25519", "-----BEGIN OPENSSH PRIVATE KEY-----\n")
	out := e.mustRun(t, "y\n", "symlink", "add", key)
	if strings.Contains(out, "secret") || strings.Contains(out, "Track anyway?") {
		t.Fatalf("no secret prompt:\n%s", out)
	}
	if !strings.Contains(out, filepath.Join("symlinks", "home", ".ssh", "id_ed25519")) || !isLink(key) {
		t.Fatalf("stored at the mirror path:\n%s", out)
	}
}

func TestSymlinkAddJSONIsOnePlanWithActions(t *testing.T) {
	e := newDfEnv(t)
	a := e.file(t, ".a", "a")
	code, out, errOut := e.run("", "--json", "symlink", "add", a, "agent/a")
	if code != 0 {
		t.Fatalf("exit %d\nstdout=%s stderr=%s", code, out, errOut)
	}
	code, out, errOut = e.run("", "--json", "symlink", "add", filepath.Join(e.home, ".gone"))
	if code != 1 { // the only action failed
		t.Fatalf("exit %d, want 1\nstdout=%s stderr=%s", code, out, errOut)
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
	if env.Command != "symlink.add" || len(env.Data.Actions) != 1 || env.Data.Actions[0].Status != "failed" {
		t.Fatalf("envelope = %+v", env)
	}
}

// ── link ───────────────────────────────────────────────────────────

func TestSymlinkLinkRepairsAndSkipsDrifted(t *testing.T) {
	e := newDfEnv(t)
	a := e.file(t, ".a", "a\n")
	b := e.file(t, ".b", "b\n")
	c := e.file(t, ".c", "c\n")
	d := e.file(t, ".d", "d\n")
	for _, p := range []string{a, b, c, d} {
		e.mustRun(t, "y\n", "symlink", "add", p)
	}
	if err := os.Remove(a); err != nil { // missing
		t.Fatal(err)
	}
	replaceWithFile(t, b, "live\n") // drifted
	other := filepath.Join(e.home, "elsewhere")
	if err := os.WriteFile(other, []byte("o\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(d); err != nil { // broken: a link to somewhere else
		t.Fatal(err)
	}
	if err := os.Symlink(other, d); err != nil {
		t.Fatal(err)
	}

	out := e.mustRun(t, "y\ny\n", "symlink", "link")
	if strings.Count(out, "[y/n/a/q]") != 2 {
		t.Fatalf("one action per repairable entry:\n%s", out)
	}
	if !strings.Contains(out, "drifted: ws symlink add "+b+" keeps the live file") {
		t.Fatalf("a drifted entry is skipped with a hint:\n%s", out)
	}
	if !strings.Contains(out, "(what is there moves to "+d+".ws-backup)") {
		t.Fatalf("a displaced link is named in the plan line:\n%s", out)
	}
	if !isLink(a) || !isLink(d) || read(t, d) != "d\n" {
		t.Fatal("missing and broken links repaired")
	}
	if isLink(b) || read(t, b) != "live\n" {
		t.Fatal("link never overwrites a real file")
	}

	e.mustRun(t, "y\n", "symlink", "add", b) // adopt the live file; now all is ok
	out = e.mustRun(t, "", "symlink", "link")
	if strings.TrimSpace(out) != "Nothing to link." {
		t.Fatalf("nothing to repair:\n%s", out)
	}
}

// TestSymlinkLinkRegistersAnOrphan: content under symlinks/ with no entry is
// registered and linked by `link <host-path>`.
func TestSymlinkLinkRegistersAnOrphan(t *testing.T) {
	e := newDfEnv(t)
	copyPath := filepath.Join(e.workspace, "symlinks", "home", ".zshrc")
	if err := os.MkdirAll(filepath.Dir(copyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyPath, []byte("z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(e.home, ".zshrc")
	out := e.mustRun(t, "y\n", "symlink", "link", host)
	if !strings.Contains(out, "Register and link "+host) || !isLink(host) || len(e.ls(t)) != 1 {
		t.Fatalf("orphan:\n%s", out)
	}
}

func TestSymlinkLinkNamedEntries(t *testing.T) {
	e := newDfEnv(t)
	a := e.file(t, ".a", "a\n")
	b := e.file(t, ".b", "b\n")
	e.mustRun(t, "y\n", "symlink", "add", a)
	e.mustRun(t, "y\n", "symlink", "add", b)
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	e.mustRun(t, "y\n", "symlink", "link", a)
	if !isLink(a) || isLink(b) {
		t.Fatal("only the named entry is repaired")
	}
	if out := e.mustRun(t, "", "symlink", "link", a); !strings.Contains(out, "already linked") {
		t.Fatalf("an ok target is reported as already linked:\n%s", out)
	}
	if code, _, errOut := e.run("", "symlink", "link", filepath.Join(e.home, ".unmanaged")); code != 1 || !strings.Contains(errOut, "not a managed symlink") {
		t.Fatalf("an unknown target: exit %d stderr=%s", code, errOut)
	}
}

func TestSymlinkLinkMissingCopyExitCodes(t *testing.T) {
	e := newDfEnv(t)
	good := e.file(t, ".good", "g\n")
	bad := e.file(t, ".bad", "b\n")
	e.mustRun(t, "y\n", "symlink", "add", good)
	e.mustRun(t, "y\n", "symlink", "add", bad)
	if err := os.Remove(good); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(bad); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(e.workspace, "symlinks", "home", ".bad")); err != nil {
		t.Fatal(err)
	}

	// A missing copy is reported, not offered as an action, and counts as failed.
	code, out, _ := e.run("y\n", "symlink", "link")
	if code != 3 || !strings.Contains(out, bad+": workspace copy") || !strings.Contains(out, "is missing") {
		t.Fatalf("a good entry and a missing copy exit 3, got %d:\n%s", code, out)
	}
	if strings.Count(out, "[y/n/a/q]") != 1 {
		t.Fatalf("the missing copy is not an action:\n%s", out)
	}

	code, out, _ = e.run("y\n", "symlink", "link", bad)
	if code != 1 {
		t.Fatalf("only a missing copy exits 1, got %d:\n%s", code, out)
	}

	code, out, _ = e.run("", "--json", "symlink", "link", bad)
	if code != 1 || !strings.Contains(out, `"status": "failed"`) {
		t.Fatalf("--json reports the failure and exits 1, got %d:\n%s", code, out)
	}
}

// ── unlink ─────────────────────────────────────────────────────────

func TestSymlinkUnlinkNeedsPathsOrAll(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".a", "a")
	for _, args := range [][]string{{"symlink", "unlink"}, {"symlink", "unlink", "--all", p}} {
		if code, _, errOut := e.run("", args...); code != 1 || !strings.Contains(errOut, "usage:") {
			t.Fatalf("ws %v: exit %d stderr=%s", args, code, errOut)
		}
	}
	if code, _, errOut := e.run("", "symlink", "unlink", p); code != 1 || !strings.Contains(errOut, "not a managed symlink") {
		t.Fatalf("an unknown target: exit %d stderr=%s", code, errOut)
	}
	if out := e.mustRun(t, "", "symlink", "unlink", "--all"); !strings.Contains(out, "No managed symlinks.") {
		t.Fatalf("nothing managed:\n%s", out)
	}
}

func TestSymlinkUnlinkPlanLines(t *testing.T) {
	e := newDfEnv(t)
	a := e.file(t, ".bashrc", "a\n")
	e.mustRun(t, "y\n", "symlink", "add", a)
	agent := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(agent, []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(e.home, "CLAUDE.md")
	// A shared copy: an entry whose original lies outside the owned roots.
	e.mustRun(t, "y\n", "symlink", "add", b, agent)
	replaceWithFile(t, a, "live\n")

	out := e.mustRun(t, "y\ny\n", "symlink", "unlink", a, b)
	if !strings.Contains(out, "Unlink "+a+" (move "+filepath.Join(e.workspace, "symlinks", "home", ".bashrc")+" back; the live file moves to "+a+".ws-backup)") {
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

func TestSymlinkUnlinkAllIsOneActionPerEntry(t *testing.T) {
	e := newDfEnv(t)
	for _, n := range []string{".a", ".b", ".c"} {
		e.mustRun(t, "y\n", "symlink", "add", e.file(t, n, "x"))
	}
	out := e.mustRun(t, "y\ny\ny\n", "symlink", "unlink", "--all")
	if strings.Count(out, "[y/n/a/q]") != 3 || len(e.ls(t)) != 0 {
		t.Fatalf("--all builds one action per entry (it used to be a single action):\n%s", out)
	}
}

// TestSymlinkUnlinkPartialJSONExit3: with --json the exit code follows the plan.
func TestSymlinkUnlinkPartialJSONExit3(t *testing.T) {
	e := newDfEnv(t)
	a := e.file(t, ".a", "a")
	b := e.file(t, ".b", "b")
	e.mustRun(t, "y\n", "symlink", "add", a)
	e.mustRun(t, "y\n", "symlink", "add", b)
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
	code, out, _ := e.run("", "--json", "symlink", "unlink", "--all")
	if code != 3 && code != 1 {
		t.Fatalf("a failed action must show in the exit code, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, `"status": "failed"`) {
		t.Fatalf("the failed action is reported:\n%s", out)
	}
}

// ── ls ─────────────────────────────────────────────────────────────

func TestSymlinkLsColumnsAndNoTargets(t *testing.T) {
	e := newDfEnv(t)
	p := e.file(t, ".bashrc", "x\n")
	e.mustRun(t, "y\n", "symlink", "add", p)

	out := e.mustRun(t, "", "symlink", "ls")
	head := strings.Fields(strings.SplitN(out, "\n", 2)[0])
	if strings.Join(head, " ") != "HOST PATH WORKSPACE COPY APP STATUS SUDO" {
		t.Fatalf("default columns: %v\n%s", head, out)
	}
	if !strings.Contains(out, filepath.Join(e.workspace, "symlinks", "home", ".bashrc")) {
		t.Fatalf("the workspace copy is printed absolute:\n%s", out)
	}
	if code, _, errOut := e.run("", "symlink", "ls", p); code != 1 || !strings.Contains(errOut, "no targets") {
		t.Fatalf("ls takes no targets: exit %d stderr=%s", code, errOut)
	}
	// No class anywhere.
	recs := e.ls(t)
	if _, has := recs[0]["class"]; has {
		t.Fatalf("class is removed from the record: %v", recs[0])
	}
}

func TestSymlinkUnknownVerb(t *testing.T) {
	e := newDfEnv(t)
	code, _, errOut := e.run("", "symlink", "frobnicate")
	if code != 1 || !strings.Contains(errOut, "unknown command: symlink frobnicate") {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
}
