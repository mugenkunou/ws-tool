package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/symlink"
)

// asTerminal makes `ws symlink add` think a person is at the keyboard.
func asTerminal(t *testing.T, yes bool) {
	t.Helper()
	old := addHasTerminal
	addHasTerminal = func() bool { return yes }
	t.Cleanup(func() { addHasTerminal = old })
}

func found(app, path string, secret, sudo bool) symlink.Entry {
	return symlink.Entry{Status: symlink.StatusFound, App: app, System: path, Secret: secret, Sudo: sudo, Group: path}
}

func TestChecklistRowsGroupByAppAndPairKeys(t *testing.T) {
	key := found("ssh", "/h/.ssh/id_rsa", true, false)
	pub := found("ssh", "/h/.ssh/id_rsa.pub", false, false)
	pub.Group = key.System
	rows := symlink.ChecklistRows([]symlink.Entry{
		found("git", "/h/.gitconfig", false, false),
		found("git", "/h/.gitignore_global", false, false),
		found("hosts", "/etc/hosts", false, true),
		found("ssh", "/h/.ssh/config", false, false),
		key, pub,
		found("aws", "/h/.aws/credentials", true, false),
	})
	type want struct {
		app     string
		n       int
		checked bool
	}
	got := make([]want, len(rows))
	for i, r := range rows {
		got[i] = want{r.App, len(r.Items), r.Checked}
	}
	// One row per app for its ordinary items (checked); every secret or sudo item on
	// its own (unchecked); a key and its .pub on one row.
	exp := []want{{"git", 2, true}, {"hosts", 1, false}, {"ssh", 1, true}, {"ssh", 2, false}, {"aws", 1, false}}
	if !slices.Equal(got, exp) {
		t.Fatalf("rows = %+v\nwant %+v", got, exp)
	}
	if l := rows[3].Label("/h"); l != "~/.ssh/id_rsa + .pub" {
		t.Fatalf("pair label = %q", l)
	}
	if !rows[1].Sudo || !rows[3].Secret || !rows[4].Secret {
		t.Fatal("tags must follow the items")
	}
}

func TestChecklistAnswerGrammar(t *testing.T) {
	rows := []symlink.CheckRow{{Checked: true}, {}, {Checked: true}, {}}
	cases := []struct {
		in     string
		ok     bool
		quit   bool
		chosen []int
	}{
		{"", true, false, []int{0, 2}},
		{"  ", true, false, []int{0, 2}},
		{"all", true, false, []int{0, 1, 2, 3}},
		{"ALL", true, false, []int{0, 1, 2, 3}},
		{"none", true, false, nil},
		{"2", true, false, []int{1}},
		{"1,3", true, false, []int{0, 2}},
		{"3, 1", true, false, []int{0, 2}},
		{"2,2", true, false, []int{1}},
		{"q", true, true, nil},
		{"0", false, false, nil},
		{"5", false, false, nil},
		{"x", false, false, nil},
		{"1,,2", false, false, nil},
		{"1;2", false, false, nil},
		{"yes", false, false, nil},
	}
	for _, c := range cases {
		chosen, ok, quit := parseChecklistAnswer(c.in, rows)
		if ok != c.ok || quit != c.quit || !slices.Equal(chosen, c.chosen) {
			t.Errorf("%q: chosen=%v ok=%v quit=%v, want %v %v %v", c.in, chosen, ok, quit, c.chosen, c.ok, c.quit)
		}
	}
}

// machine puts three known configs in the home directory: two ordinary (git), one
// secret with its .pub (ssh).
func (e dfEnv) machine(t *testing.T) {
	t.Helper()
	e.file(t, ".gitconfig", "[user]\n")
	e.file(t, ".gitignore_global", "*.swp\n")
	e.file(t, ".ssh/id_rsa", "-----BEGIN OPENSSH PRIVATE KEY-----\n")
	e.file(t, ".ssh/id_rsa.pub", "ssh-rsa AAAA\n")
}

func TestAddChecklistEnterTakesTheCheckedRows(t *testing.T) {
	e := newDfEnv(t)
	e.machine(t)
	asTerminal(t, true)
	// A bad answer asks again; Enter takes the checked rows only.
	code, out, errOut := e.run("what?\n\n", "symlink", "add")
	if code != 0 {
		t.Fatalf("exit %d\nout=%s\nerr=%s", code, out, errOut)
	}
	for _, want := range []string{
		"Found on this machine, not symlinked yet:",
		"[x] 1  git",
		"[ ] 2  ssh",
		"~/.ssh/id_rsa + .pub",
		"🔑 secret",
		"Stored under symlinks/ at the same path as on this machine.",
		`Not understood: "what?"`,
		"Add " + filepath.Join(e.home, ".gitconfig") + " → " + filepath.Join(e.workspace, "symlinks", "home", ".gitconfig"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if !isLink(filepath.Join(e.home, ".gitconfig")) || !isLink(filepath.Join(e.home, ".gitignore_global")) {
		t.Fatal("the checked row is added")
	}
	if isLink(filepath.Join(e.home, ".ssh", "id_rsa")) || isLink(filepath.Join(e.home, ".ssh", "id_rsa.pub")) {
		t.Fatal("a secret row starts unchecked")
	}
}

func TestAddChecklistAllAndNumbers(t *testing.T) {
	e := newDfEnv(t)
	e.machine(t)
	asTerminal(t, true)
	e.mustRun(t, "2\n", "symlink", "add") // only the ssh pair
	if isLink(filepath.Join(e.home, ".gitconfig")) || !isLink(filepath.Join(e.home, ".ssh", "id_rsa")) || !isLink(filepath.Join(e.home, ".ssh", "id_rsa.pub")) {
		t.Fatal("2 is the key and its .pub, and nothing else")
	}
	e.mustRun(t, "all\n", "symlink", "add")
	if !isLink(filepath.Join(e.home, ".gitconfig")) {
		t.Fatal("all takes the rest")
	}
	if out := e.mustRun(t, "", "symlink", "add"); !strings.Contains(out, "Nothing found") {
		t.Fatalf("nothing left:\n%s", out)
	}
}

func TestAddChecklistNoneAndCancel(t *testing.T) {
	e := newDfEnv(t)
	e.machine(t)
	asTerminal(t, true)
	if out := e.mustRun(t, "none\n", "symlink", "add"); !strings.Contains(out, "Nothing selected.") || isLink(filepath.Join(e.home, ".gitconfig")) {
		t.Fatalf("none:\n%s", out)
	}
	for _, in := range []string{"q\n", ""} { // q, and the input ending
		if code, _, _ := e.run(in, "symlink", "add"); code != 130 {
			t.Fatalf("input %q: exit %d, want 130", in, code)
		}
	}
	if isLink(filepath.Join(e.home, ".gitconfig")) || len(e.ls(t)) != 0 {
		t.Fatal("a cancelled checklist changes nothing")
	}
}

func TestAddWithoutATerminalPrintsWhatWasFound(t *testing.T) {
	e := newDfEnv(t)
	e.machine(t)
	asTerminal(t, false)
	out := e.mustRun(t, "", "symlink", "add")
	if !strings.Contains(out, ".gitconfig") || !strings.Contains(out, "found") || strings.Contains(out, "[y/n/a/q]") {
		t.Fatalf("no terminal: the found list, no questions:\n%s", out)
	}
	if isLink(filepath.Join(e.home, ".gitconfig")) {
		t.Fatal("no terminal: nothing changes")
	}
}

func TestAddApp(t *testing.T) {
	e := newDfEnv(t)
	e.machine(t)
	e.file(t, ".git-credentials", "https://u:p@example.com\n") // a secret git item
	out := e.mustRun(t, "y\ny\n", "symlink", "add", "--app", "git")
	if strings.Count(out, "[y/n/a/q]") != 2 || !isLink(filepath.Join(e.home, ".gitconfig")) || !isLink(filepath.Join(e.home, ".gitignore_global")) {
		t.Fatalf("--app adds the ordinary items:\n%s", out)
	}
	if isLink(filepath.Join(e.home, ".git-credentials")) {
		t.Fatal("--app never adds a secret")
	}

	// Nothing found for the app: exit 1. Only a secret found counts as nothing.
	if code, _, errOut := e.run("", "symlink", "add", "--app", "git"); code != 1 || !strings.Contains(errOut, "nothing found for git") || !strings.Contains(errOut, "1 secret") {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
	if code, _, errOut := e.run("", "symlink", "add", "--app", "ssh"); code != 1 || !strings.Contains(errOut, "nothing found for ssh") {
		t.Fatalf("ssh has only a key: exit %d stderr=%s", code, errOut)
	}
	if code, _, errOut := e.run("", "symlink", "add", "--app", "nosuchapp"); code != 1 || !strings.Contains(errOut, "unknown app") {
		t.Fatalf("unknown app: exit %d stderr=%s", code, errOut)
	}
	if code, _, errOut := e.run("", "symlink", "add", "--app", "git", filepath.Join(e.home, ".x")); code != 1 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("--app with a path: exit %d stderr=%s", code, errOut)
	}
}

// ── ls: statuses, default rows, footer ─────────────────────────────

func TestLsFoundIsHiddenWithAFooter(t *testing.T) {
	e := newDfEnv(t)
	e.machine(t)
	out := e.mustRun(t, "", "symlink", "ls")
	if !strings.Contains(out, "No managed symlinks.") || strings.Contains(out, ".gitconfig") {
		t.Fatalf("found rows are not shown by default:\n%s", out)
	}
	if !strings.Contains(out, "4 known configs found, not symlinked: ws symlink add") {
		t.Fatalf("footer:\n%s", out)
	}
	out = e.mustRun(t, "", "symlink", "ls", "--filter", "status=found")
	if !strings.Contains(out, ".gitconfig") || strings.Contains(out, "known configs found") {
		t.Fatalf("--filter status=found shows them, with no footer:\n%s", out)
	}
	if out = e.mustRun(t, "", "--quiet", "symlink", "ls"); strings.Contains(out, "known configs") {
		t.Fatalf("--quiet drops the footer:\n%s", out)
	}
	if out = e.mustRun(t, "", "--json", "symlink", "ls"); strings.Contains(out, "known configs") {
		t.Fatalf("--json carries no footer:\n%s", out)
	}
	// Found rows in JSON, with the documented fields and no class or secret.
	var recs []map[string]any
	recs = listRecords(t, "--workspace", e.workspace, "symlink", "ls", "--filter", "status=found")
	if len(recs) != 4 {
		t.Fatalf("found records = %d", len(recs))
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

func TestCompleteSymlinkAppValues(t *testing.T) {
	comps, dir := resolveCompletions([]string{"symlink", "add", "--app", "g"}, globalFlags{})
	if dir != compDirectiveNoFileComp || !slices.Contains(comps, "git") || !slices.Contains(comps, "gnupg") || slices.Contains(comps, "vim") {
		t.Fatalf("--app completes catalog labels: %v %d", comps, dir)
	}
	comps, _ = resolveCompletions([]string{"symlink", "add", "--"}, globalFlags{})
	for _, f := range []string{"--app", "--dir", "--files"} {
		if !slices.Contains(comps, f) {
			t.Errorf("flag %s not offered: %v", f, comps)
		}
	}
	comps, _ = resolveCompletions([]string{"symlink", ""}, globalFlags{})
	for _, v := range []string{"ls", "add", "link", "unlink"} {
		if !slices.Contains(comps, v) {
			t.Errorf("verb %s not offered: %v", v, comps)
		}
	}
	for _, v := range []string{"edit", "backup", "git", "migrate"} {
		if slices.Contains(comps, v) {
			t.Errorf("removed verb %s still offered", v)
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
	for _, want := range []string{"Examples:", "ws symlink add --app git", "--files", "--app <name>"} {
		if !strings.Contains(out, want) {
			t.Errorf("add help lacks %q:\n%s", want, out)
		}
	}
}
