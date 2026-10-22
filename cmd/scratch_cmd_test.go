package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scEnv is one isolated machine for the scratch commands: a home (the default
// scratch root is ~/Scratch), an XDG trash, and an initialized workspace.
type scEnv struct {
	home, workspace, root, trash string
}

func newScEnv(t *testing.T) scEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	data := filepath.Join(t.TempDir(), "data")
	t.Setenv("XDG_DATA_HOME", data)
	ws := initWorkspace(t)
	return scEnv{home: home, workspace: ws, root: filepath.Join(home, "Scratch"), trash: filepath.Join(data, "Trash", "files")}
}

func (e scEnv) run(stdin string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := Execute(append([]string{"--workspace", e.workspace}, args...), strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func (e scEnv) mustRun(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	code, out, errOut := e.run(stdin, args...)
	if code != 0 {
		t.Fatalf("ws %s: exit %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), code, out, errOut)
	}
	return out
}

func (e scEnv) mk(t *testing.T, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(e.root, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func exists2(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// ── new ────────────────────────────────────────────────────────────

func TestScratchNewNamesTheFinalPath(t *testing.T) {
	e := newScEnv(t)
	out := e.mustRun(t, "y\n", "scratch", "new", "proxy-auth", "--no-open", "--no-date")
	want := filepath.Join(e.root, "proxy-auth")
	if !strings.Contains(out, "Create "+want+" ") || !strings.Contains(out, "✔ Create "+want) {
		t.Fatalf("the plan line names the final path:\n%s", out)
	}
	if !exists2(filepath.Join(e.root, "proxy-auth", ".ws-meta.json")) {
		t.Fatal("the directory gets its metadata")
	}

	// A name that is taken gets -2, and the plan says so before it runs.
	out = e.mustRun(t, "y\n", "scratch", "new", "proxy-auth", "--no-open", "--no-date")
	if !strings.Contains(out, "Create "+want+"-2 ") {
		t.Fatalf("collision:\n%s", out)
	}

	// With the date suffix (the default).
	out = e.mustRun(t, "y\n", "scratch", "new", "dated", "--no-open")
	if !strings.Contains(out, filepath.Join(e.root, "dated.")) {
		t.Fatalf("date suffix:\n%s", out)
	}
}

func TestScratchNewTakesOneName(t *testing.T) {
	e := newScEnv(t)
	if code, _, errOut := e.run("", "scratch", "new", "two", "words", "--no-open"); code != 1 || !strings.Contains(errOut, "takes one name") {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
	// A quoted name with spaces is one name.
	e.mustRun(t, "y\n", "scratch", "new", "two words", "--no-open", "--no-date")
	if !exists2(filepath.Join(e.root, "two words")) {
		t.Fatal("a quoted name is one name")
	}
	// No name and no terminal generates scratch-<n>.
	out := e.mustRun(t, "y\n", "scratch", "new", "--no-open", "--no-date")
	if !strings.Contains(out, filepath.Join(e.root, "scratch-")) {
		t.Fatalf("a generated name:\n%s", out)
	}
}

func TestScratchNewOpensTheEditor(t *testing.T) {
	e := newScEnv(t)
	log := filepath.Join(t.TempDir(), "editor.log")
	// A terminal editor (vim) that records its arguments.
	dir := t.TempDir()
	script := "#!/bin/sh\necho \"$@\" > " + log + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "vim"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	e.mustRun(t, "y\n", "scratch", "new", "demo", "--no-date")
	got, err := os.ReadFile(log)
	if err != nil || strings.TrimSpace(string(got)) != filepath.Join(e.root, "demo") {
		t.Fatalf("vim (the default editor) is started on the directory: %q %v", got, err)
	}

	// A failing editor never fails the command: ws warns and prints the path.
	if err := os.WriteFile(filepath.Join(dir, "vim"), []byte("#!/bin/sh\nexit 4\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, _ := e.run("y\n", "scratch", "new", "demo2", "--no-date")
	if code != 0 || !strings.Contains(out, "Editor launch skipped") {
		t.Fatalf("exit %d:\n%s", code, out)
	}

	// --json starts no editor.
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}
	_, js, _ := e.run("", "--json", "scratch", "new", "demo3", "--no-date")
	if exists2(log) {
		t.Fatal("--json must not start the editor")
	}
	var env struct {
		Command string `json:"command"`
		Data    struct {
			Result  struct{ Path string }     `json:"result"`
			Actions []struct{ Status string } `json:"actions"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(js), &env); err != nil || env.Command != "scratch.new" || env.Data.Result.Path != filepath.Join(e.root, "demo3") || len(env.Data.Actions) != 1 {
		t.Fatalf("envelope = %+v err=%v\n%s", env, err, js)
	}
}

// ── add ────────────────────────────────────────────────────────────

func TestScratchAdd(t *testing.T) {
	e := newScEnv(t)
	dir := filepath.Join(e.workspace, "experiments", "net")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pcap := filepath.Join(dir, "capture.pcap")
	if err := os.WriteFile(pcap, []byte("packets"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := e.mustRun(t, "y\ny\n", "scratch", "add", pcap, dir)
	if !strings.Contains(out, "Move "+pcap+" → "+filepath.Join(e.root, "capture", "capture.pcap")) ||
		!strings.Contains(out, "Move "+dir+" → "+filepath.Join(e.root, "net")) {
		t.Fatalf("plan lines:\n%s", out)
	}
	if exists2(pcap) || exists2(dir) {
		t.Fatal("the sources must leave the workspace")
	}
	if b, _ := os.ReadFile(filepath.Join(e.root, "capture", "capture.pcap")); string(b) != "packets" {
		t.Fatal("the file must arrive")
	}
	if recs := listRecords(t, "--workspace", e.workspace, "scratch", "ls"); len(recs) != 2 {
		t.Fatalf("both are now scratch rows: %v", recs)
	}
}

func TestScratchAddRefusesBeforeMovingAnything(t *testing.T) {
	e := newScEnv(t)
	ok := filepath.Join(e.workspace, "ok.txt")
	if err := os.WriteFile(ok, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		filepath.Join(e.workspace, "missing.txt"),
		filepath.Join(e.workspace, "ws.json"),
		e.workspace,
	} {
		code, out, errOut := e.run("y\n", "scratch", "add", ok, bad)
		if code != 1 || strings.Contains(out, "[y/n/a/q]") || errOut == "" {
			t.Errorf("%s: exit %d out=%q err=%q", bad, code, out, errOut)
		}
		if !exists2(ok) {
			t.Fatalf("nothing may move when a path is refused (%s)", bad)
		}
	}
	if code, _, errOut := e.run("", "scratch", "add"); code != 1 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("no path: exit %d stderr=%s", code, errOut)
	}
}

func TestScratchAddJSON(t *testing.T) {
	e := newScEnv(t)
	f := filepath.Join(e.workspace, "dump.bin")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, js, _ := e.run("", "--json", "scratch", "add", f)
	var env struct {
		Command string `json:"command"`
		Data    struct {
			Moved   []struct{ Source, Path string } `json:"moved"`
			Actions []struct{ Status string }       `json:"actions"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(js), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, js)
	}
	if env.Command != "scratch.add" || len(env.Data.Moved) != 1 || env.Data.Moved[0].Source != f ||
		env.Data.Moved[0].Path != filepath.Join(e.root, "dump", "dump.bin") || env.Data.Actions[0].Status != "executed" {
		t.Fatalf("envelope = %+v", env)
	}
}

// ── open ───────────────────────────────────────────────────────────

func TestScratchOpenResolvesNames(t *testing.T) {
	e := newScEnv(t)
	e.mk(t, "dns-debug.2026-09", "foo", "foo-2", "myfoo")
	t.Setenv("VISUAL", "true") // a background editor that exits at once

	// A unique substring.
	code, out, _ := e.run("", "scratch", "open", "dns")
	if code != 0 || !strings.Contains(out, filepath.Join(e.root, "dns-debug.2026-09")) {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	// An exact name wins over longer names.
	if code, out, _ = e.run("", "scratch", "open", "foo"); code != 0 || !strings.Contains(out, filepath.Join(e.root, "foo")+"\n") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	// Ambiguous or missing: exit 1, with the candidates.
	code, _, errOut := e.run("", "scratch", "open", "fo")
	if code != 1 || !strings.Contains(errOut, "matches several") || !strings.Contains(errOut, "myfoo") {
		t.Fatalf("ambiguous: exit %d stderr=%s", code, errOut)
	}
	if code, _, errOut = e.run("", "scratch", "open", "nothing"); code != 1 || !strings.Contains(errOut, "not found") {
		t.Fatalf("missing: exit %d stderr=%s", code, errOut)
	}
	// No name and no terminal is a usage error.
	if code, _, errOut = e.run("", "scratch", "open"); code != 1 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("no name: exit %d stderr=%s", code, errOut)
	}
}

// ── tag ────────────────────────────────────────────────────────────

func TestScratchTag(t *testing.T) {
	e := newScEnv(t)
	e.mk(t, "dns-debug.2026-09")

	out := e.mustRun(t, "y\n", "scratch", "tag", "dns", "DNS, network", "k8s")
	if !strings.Contains(out, "Tag dns-debug.2026-09: +dns +network +k8s") {
		t.Fatalf("plan line:\n%s", out)
	}
	recs := listRecords(t, "--workspace", e.workspace, "scratch", "ls")
	if tags, _ := recs[0]["tags"].([]any); len(tags) != 3 {
		t.Fatalf("tags = %v", recs[0]["tags"])
	}

	// --rm removes; both in one command.
	out = e.mustRun(t, "y\n", "scratch", "tag", "dns", "extra", "--rm", "network", "--rm", "absent")
	if !strings.Contains(out, "+extra −network") {
		t.Fatalf("plan line:\n%s", out)
	}
	// A flag may follow the tags.
	if out := e.mustRun(t, "", "scratch", "tag", "dns", "extra"); strings.TrimSpace(out) != "No change." {
		t.Fatalf("nothing to change:\n%s", out)
	}

	// Every tag ever added is the vocabulary completion offers, and it only grows.
	cfg := read(t, filepath.Join(e.workspace, "ws.json"))
	for _, want := range []string{`"dns"`, `"network"`, `"k8s"`, `"extra"`} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("scratch.tags lacks %s:\n%s", want, cfg)
		}
	}
}

func TestScratchTagWithoutTagsNeedsATerminal(t *testing.T) {
	e := newScEnv(t)
	e.mk(t, "dns-debug.2026-09")
	// Nothing is suggested from the contents any more: no tags is a usage error.
	if err := os.WriteFile(filepath.Join(e.root, "dns-debug.2026-09", "deploy.sh"), []byte("kubectl get pods\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := e.run("", "scratch", "tag", "dns")
	if code != 1 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
	// --auto is accepted and ignored.
	if code, _, errOut = e.run("", "scratch", "tag", "dns", "--auto"); code != 1 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("--auto is a no-op: exit %d stderr=%s", code, errOut)
	}
	recs := listRecords(t, "--workspace", e.workspace, "scratch", "ls")
	if tags, _ := recs[0]["tags"].([]any); len(tags) != 0 {
		t.Fatalf("no tag may be invented from the contents: %v", tags)
	}
}

func TestScratchTagJSON(t *testing.T) {
	e := newScEnv(t)
	e.mk(t, "dns-debug.2026-09")
	_, js, _ := e.run("", "--json", "scratch", "tag", "dns", "a", "b")
	var env struct {
		Command string `json:"command"`
		Data    struct {
			Name, Path string
			Tags       []string
			Added      []string
			Removed    []string
			Actions    []struct{ Status string }
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(js), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, js)
	}
	d := env.Data
	if env.Command != "scratch.tag" || d.Name != "dns-debug.2026-09" || d.Path != filepath.Join(e.root, "dns-debug.2026-09") ||
		len(d.Tags) != 2 || len(d.Added) != 2 || d.Removed == nil || len(d.Actions) != 1 {
		t.Fatalf("envelope = %+v", env)
	}
}

// ── rm ─────────────────────────────────────────────────────────────

// TestScratchRmDeletesOnlyTheNamedDirectory is the scratch data-loss bug: rm
// removed every directory whose name contained the argument.
func TestScratchRmDeletesOnlyTheNamedDirectory(t *testing.T) {
	e := newScEnv(t)
	e.mk(t, "foo", "foo-2", "myfoo", "bar")

	out := e.mustRun(t, "y\n", "scratch", "rm", "foo")
	if strings.Count(out, "[y/n/a/q]") != 1 || !strings.Contains(out, "Delete "+filepath.Join(e.root, "foo")+" (") {
		t.Fatalf("one action for the one directory:\n%s", out)
	}
	for _, keep := range []string{"foo-2", "myfoo", "bar"} {
		if !exists2(filepath.Join(e.root, keep)) {
			t.Fatalf("%s must survive `scratch rm foo`", keep)
		}
	}
	if !exists2(filepath.Join(e.trash, "foo")) {
		t.Fatal("the directory goes to the trash")
	}

	// An ambiguous substring deletes nothing.
	code, out, errOut := e.run("y\n", "scratch", "rm", "fo")
	if code != 1 || !strings.Contains(errOut, "matches several") || strings.Contains(out, "[y/n/a/q]") {
		t.Fatalf("exit %d out=%s err=%s", code, out, errOut)
	}
	for _, keep := range []string{"foo-2", "myfoo"} {
		if !exists2(filepath.Join(e.root, keep)) {
			t.Fatalf("%s must survive an ambiguous rm", keep)
		}
	}
}

func TestScratchRmSeveralNamesIsAllOrNothing(t *testing.T) {
	e := newScEnv(t)
	e.mk(t, "alpha", "beta", "gamma")
	out := e.mustRun(t, "y\ny\n", "scratch", "rm", "alpha", "beta", "alpha")
	if strings.Count(out, "[y/n/a/q]") != 2 {
		t.Fatalf("one action per directory, a repeat counts once:\n%s", out)
	}
	if exists2(filepath.Join(e.root, "alpha")) || exists2(filepath.Join(e.root, "beta")) || !exists2(filepath.Join(e.root, "gamma")) {
		t.Fatal("alpha and beta go, gamma stays")
	}

	e.mk(t, "delta", "epsilon")
	if code, _, errOut := e.run("y\n", "scratch", "rm", "delta", "no-such"); code != 1 || !strings.Contains(errOut, "not found") {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
	if !exists2(filepath.Join(e.root, "delta")) {
		t.Fatal("nothing is deleted when one name is wrong")
	}
	if code, _, errOut := e.run("", "scratch", "rm"); code != 1 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("no name and no terminal: exit %d stderr=%s", code, errOut)
	}
	_, js, _ := e.run("", "--json", "scratch", "rm", "epsilon")
	if !strings.Contains(js, `"command": "scratch.rm"`) || !strings.Contains(js, `"status": "executed"`) {
		t.Fatalf("json:\n%s", js)
	}
}

// ── aliases and removed commands ───────────────────────────────────

func TestScratchSearchAliasIsLsFilter(t *testing.T) {
	e := newScEnv(t)
	e.mk(t, "dns-debug.2026-09", "proxy-auth.2026-10")
	code, out, errOut := e.run("", "scratch", "search", "dns")
	if code != 0 || !strings.Contains(out, "dns-debug") || strings.Contains(out, "proxy-auth") {
		t.Fatalf("exit %d out=%s", code, out)
	}
	if !strings.Contains(errOut, `"scratch search" is now "scratch ls --filter"`) {
		t.Fatalf("hint: %q", errOut)
	}
}

func TestScratchGrepIsGone(t *testing.T) {
	e := newScEnv(t)
	code, _, errOut := e.run("", "scratch", "grep", "x")
	if code != 1 || !strings.Contains(errOut, "unknown command: scratch grep") {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
}
