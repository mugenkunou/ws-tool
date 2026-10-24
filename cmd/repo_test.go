package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ── fixtures ───────────────────────────────────────────────────────

func runGitLocal(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func gitOutLocal(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
	return strings.TrimSpace(string(output))
}

// isolateGit gives the test its own global git config.
func isolateGit(t *testing.T, content string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	return cfg
}

// repoWorkspace is an initialized workspace with no repos.
func repoWorkspace(t *testing.T) string {
	t.Helper()
	isolateGit(t, "[core]\n")
	return initWorkspace(t)
}

// mkRepo creates a committed repo on branch main with a local identity.
func mkRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitLocal(t, dir, "init", "-b", "main")
	runGitLocal(t, dir, "config", "user.email", "ws@example.com")
	runGitLocal(t, dir, "config", "user.name", "ws")
	commit(t, dir, "f.txt", "x\n", "init")
	return dir
}

func commit(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitLocal(t, dir, "add", "-A")
	runGitLocal(t, dir, "commit", "-m", msg)
}

// mkRemote creates a bare remote and makes dir push to it, tracking origin/main.
func mkRemote(t *testing.T, dir string) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGitLocal(t, t.TempDir(), "init", "--bare", "-b", "main", remote)
	runGitLocal(t, dir, "remote", "add", "origin", remote)
	runGitLocal(t, dir, "push", "-u", "origin", "main")
	return remote
}

// cloneOf clones remote to a second machine, and returns its path.
func cloneOf(t *testing.T, remote string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "other")
	runGitLocal(t, t.TempDir(), "clone", remote, dir)
	runGitLocal(t, dir, "config", "user.email", "o@example.com")
	runGitLocal(t, dir, "config", "user.name", "other")
	return dir
}

type wsResult struct {
	code     int
	out, err string
}

// wsExec runs ws with the given stdin.
func wsExec(t *testing.T, stdin string, args ...string) wsResult {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Execute(args, strings.NewReader(stdin), &out, &errOut)
	return wsResult{code, out.String(), errOut.String()}
}

// forceTerminal makes the repo commands believe they run on a terminal.
func forceTerminal(t *testing.T) {
	t.Helper()
	saved := repoInteractive
	repoInteractive = func(io.Reader, io.Writer) bool { return true }
	t.Cleanup(func() { repoInteractive = saved })
}

func hasFetchHead(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git", "FETCH_HEAD"))
	return err == nil
}

// repoPair is a workspace with repo "a" tracking a remote, and a second clone.
type repoPair struct {
	ws, a, remote, other string
}

func newPair(t *testing.T) repoPair {
	t.Helper()
	ws := repoWorkspace(t)
	a := mkRepo(t, filepath.Join(ws, "a"))
	remote := mkRemote(t, a)
	return repoPair{ws: ws, a: a, remote: remote, other: cloneOf(t, remote)}
}

func (p repoPair) remoteCommit(t *testing.T, name, content string) {
	t.Helper()
	commit(t, p.other, name, content, "remote "+name)
	runGitLocal(t, p.other, "push")
}

// initScanFixture creates a workspace with one committed repo "r1".
// When identity is false, local user.name/user.email are removed after the commit.
func initScanFixture(t *testing.T, identity bool) string {
	t.Helper()
	workspace := repoWorkspace(t)
	repoPath := mkRepo(t, filepath.Join(workspace, "r1"))
	if !identity {
		runGitLocal(t, repoPath, "config", "--unset", "user.name")
		runGitLocal(t, repoPath, "config", "--unset", "user.email")
	}
	return workspace
}

func decode(t *testing.T, r wsResult, into any) {
	t.Helper()
	var env struct {
		Schema  int             `json:"schema"`
		Command string          `json:"command"`
		Data    json.RawMessage `json:"data"`
		DryRun  bool            `json:"dry_run"`
	}
	if err := json.Unmarshal([]byte(r.out), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s\nstderr: %s", err, r.out, r.err)
	}
	if env.Schema != 3 {
		t.Fatalf("schema %d", env.Schema)
	}
	if err := json.Unmarshal(env.Data, into); err != nil {
		t.Fatalf("data: %v\n%s", err, env.Data)
	}
}

func jsonCommand(t *testing.T, r wsResult) string {
	t.Helper()
	var env struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(r.out), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, r.out)
	}
	return env.Command
}

// ── basics ─────────────────────────────────────────────────────────

func TestRepoNoArgsShowsHelp(t *testing.T) {
	testSetXDG(t)
	r := wsExec(t, "", "repo")
	if r.code != 0 || !strings.Contains(r.out, "ls") || !strings.Contains(r.out, "root add") || r.err != "" {
		t.Fatalf("ws repo: code %d out %q err %q", r.code, r.out, r.err)
	}
	r = wsExec(t, "", "repo", "root")
	if r.code != 0 || !strings.Contains(r.out, "add") || !strings.Contains(r.out, "rm") {
		t.Fatalf("ws repo root: code %d out %q", r.code, r.out)
	}
	if r = wsExec(t, "", "repo", "frobnicate"); r.code != 1 || !strings.Contains(r.err, "unknown command: repo frobnicate") {
		t.Fatalf("unknown verb: %d %q", r.code, r.err)
	}
}

func TestRepoNeedsInitializedWorkspace(t *testing.T) {
	testSetXDG(t)
	r := wsExec(t, "", "--workspace", filepath.Join(t.TempDir(), "none"), "repo", "ls")
	if r.code != 1 || !strings.Contains(r.err, "not initialized") {
		t.Fatalf("%d %q", r.code, r.err)
	}
}

// ── ls ─────────────────────────────────────────────────────────────

func TestRepoLsTable(t *testing.T) {
	ws := repoWorkspace(t)
	clean := mkRepo(t, filepath.Join(ws, "data", "bruno"))
	mkRemote(t, clean)
	dirty := mkRepo(t, filepath.Join(ws, "notes", "brain"))
	if err := os.WriteFile(filepath.Join(dirty, "f.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkRepo(t, filepath.Join(ws, "experiments", "blog")) // no upstream

	r := wsExec(t, "", "--workspace", ws, "--no-color", "repo", "ls")
	if r.code != 0 || r.err != "" {
		t.Fatalf("ls: %d %q", r.code, r.err)
	}
	lines := strings.Split(strings.TrimSpace(r.out), "\n")
	if len(lines) != 4 {
		t.Fatalf("header and 3 rows expected:\n%s", r.out)
	}
	for _, col := range []string{"REPO", "STATE", "ISSUES", "BRANCH"} {
		if !strings.Contains(lines[0], col) {
			t.Errorf("header lacks %s: %s", col, lines[0])
		}
	}
	// Absolute paths, in path order.
	order := []string{filepath.Join(ws, "data", "bruno"), filepath.Join(ws, "experiments", "blog"), filepath.Join(ws, "notes", "brain")}
	states := []string{"clean", "no upstream", "dirty"}
	for i, p := range order {
		if !strings.HasPrefix(lines[i+1], p+" ") || !strings.Contains(lines[i+1], states[i]) {
			t.Errorf("row %d: want %s %s, got %s", i, p, states[i], lines[i+1])
		}
	}
	// Dirty repos and findings exit 0.
	if r.code != 0 {
		t.Fatal("ls must exit 0 when repos are dirty")
	}
}

func TestRepoLsNoRepos(t *testing.T) {
	ws := repoWorkspace(t)
	r := wsExec(t, "", "--workspace", ws, "repo", "ls")
	if r.code != 0 || strings.TrimSpace(r.out) != "No repositories found." {
		t.Fatalf("%d %q", r.code, r.out)
	}
}

func TestRepoLsDoesNotFetchByDefault(t *testing.T) {
	p := newPair(t)
	p.remoteCommit(t, "n.txt", "n\n")
	if hasFetchHead(p.a) {
		t.Fatal("fixture should not have fetched")
	}
	r := wsExec(t, "", "--workspace", p.ws, "--json", "repo", "ls")
	if r.code != 0 {
		t.Fatalf("%d %s", r.code, r.err)
	}
	if hasFetchHead(p.a) {
		t.Fatal("ls without --fetch must not touch the repo")
	}
	var data struct {
		Statuses []map[string]any `json:"statuses"`
	}
	decode(t, r, &data)
	if data.Statuses[0]["behind"].(float64) != 0 {
		t.Fatal("not fetched yet: behind should still be 0")
	}

	r = wsExec(t, "", "--workspace", p.ws, "--json", "repo", "ls", "--fetch")
	if r.code != 0 {
		t.Fatalf("%d %s", r.code, r.err)
	}
	decode(t, r, &data)
	if data.Statuses[0]["behind"].(float64) != 1 {
		t.Fatalf("--fetch should find the remote commit: %v", data.Statuses[0])
	}
	if !hasFetchHead(p.a) {
		t.Fatal("--fetch must fetch")
	}
}

func TestRepoLsFetchFailureExits3AndStillLists(t *testing.T) {
	ws := repoWorkspace(t)
	good := mkRepo(t, filepath.Join(ws, "good"))
	mkRemote(t, good)
	bad := mkRepo(t, filepath.Join(ws, "bad"))
	runGitLocal(t, bad, "remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git"))

	r := wsExec(t, "", "--workspace", ws, "--no-color", "repo", "ls", "--fetch")
	if r.code != 3 {
		t.Fatalf("exit %d, want 3\nstdout: %s\nstderr: %s", r.code, r.out, r.err)
	}
	if !strings.Contains(r.err, "ws: fetch failed: "+bad+": ") {
		t.Fatalf("stderr: %q", r.err)
	}
	if !strings.Contains(r.out, bad) || !strings.Contains(r.out, good) {
		t.Fatalf("both repos are listed:\n%s", r.out)
	}

	r = wsExec(t, "", "--workspace", ws, "--json", "repo", "ls", "--fetch")
	if r.code != 3 {
		t.Fatalf("json exit %d", r.code)
	}
	var data struct {
		Warnings []string `json:"fetch_warnings"`
	}
	decode(t, r, &data)
	if len(data.Warnings) != 1 || !strings.HasPrefix(data.Warnings[0], bad+": ") {
		t.Fatalf("fetch_warnings: %v", data.Warnings)
	}

	// --dry-run skips the fetch, and so the failure.
	if err := os.Remove(filepath.Join(good, ".git", "FETCH_HEAD")); err != nil {
		t.Fatal(err)
	}
	r = wsExec(t, "", "--workspace", ws, "--dry-run", "repo", "ls", "--fetch")
	if r.code != 0 || r.err != "" {
		t.Fatalf("dry-run: %d %q", r.code, r.err)
	}
	if hasFetchHead(good) {
		t.Fatal("--dry-run must not fetch")
	}
}

func TestRepoLsUnknownTargetFailsBeforeFetch(t *testing.T) {
	p := newPair(t)
	nowhere := filepath.Join(p.ws, "nowhere")
	r := wsExec(t, "", "--workspace", p.ws, "repo", "ls", "--fetch", p.a, nowhere)
	if r.code != 1 || strings.TrimSpace(r.err) != "ws: no repo at "+nowhere {
		t.Fatalf("%d %q", r.code, r.err)
	}
	if hasFetchHead(p.a) {
		t.Fatal("a bad target must stop everything before any fetch")
	}
}

func TestRepoTargetRules(t *testing.T) {
	ws := repoWorkspace(t)
	a := mkRepo(t, filepath.Join(ws, "g", "a"))
	b := mkRepo(t, filepath.Join(ws, "g", "b"))
	inner := filepath.Join(a, "sub")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}

	// A path inside a repo, and a directory that holds repos, are not targets.
	for _, bad := range []string{inner, filepath.Join(ws, "g")} {
		r := wsExec(t, "", "--workspace", ws, "repo", "ls", bad)
		if r.code != 1 || !strings.Contains(r.err, "ws: no repo at "+bad) {
			t.Errorf("%s: %d %q", bad, r.code, r.err)
		}
	}

	// Paths are cleaned, repeats count once, and output keeps path order.
	r := wsExec(t, "", "--workspace", ws, "--json", "repo", "ls", b+"/", a+"/../a", a, b)
	if r.code != 0 {
		t.Fatalf("%d %q", r.code, r.err)
	}
	var data struct {
		Statuses []struct {
			Path string `json:"path"`
		} `json:"statuses"`
	}
	decode(t, r, &data)
	if len(data.Statuses) != 2 || data.Statuses[0].Path != a || data.Statuses[1].Path != b {
		t.Fatalf("statuses: %+v", data.Statuses)
	}

	// A filter that removes a target leaves it out quietly.
	r = wsExec(t, "", "--workspace", ws, "--json", "repo", "ls", a, b, "--path", a)
	decode(t, r, &data)
	if r.code != 0 || len(data.Statuses) != 1 || data.Statuses[0].Path != a {
		t.Fatalf("--path narrows the targets: %d %+v %q", r.code, data.Statuses, r.err)
	}
}

func TestRepoTargetTildeAndRelative(t *testing.T) {
	ws := repoWorkspace(t)
	a := mkRepo(t, filepath.Join(ws, "a"))
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Symlink(a, filepath.Join(home, "link")); err != nil {
		t.Skip("symlinks unavailable")
	}
	// ~/link is not the discovered path (exact match), so it is no target.
	r := wsExec(t, "", "--workspace", ws, "repo", "ls", "~/link")
	if r.code != 1 || !strings.Contains(r.err, "ws: no repo at "+filepath.Join(home, "link")) {
		t.Fatalf("%d %q", r.code, r.err)
	}
	chdir(t, ws)
	if r := wsExec(t, "", "--workspace", ws, "repo", "ls", "./a"); r.code != 0 {
		t.Fatalf("cwd-relative target: %d %q", r.code, r.err)
	}
}

func TestRepoLsFilters(t *testing.T) {
	p := newPair(t)
	dirty := mkRepo(t, filepath.Join(p.ws, "dirty"))
	if err := os.WriteFile(filepath.Join(dirty, "f.txt"), []byte("d\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	detached := mkRepo(t, filepath.Join(p.ws, "detached"))
	runGitLocal(t, detached, "checkout", "--detach")
	commit(t, p.a, "ahead.txt", "x\n", "ahead")
	p.remoteCommit(t, "behind.txt", "x\n")
	runGitLocal(t, p.a, "fetch")

	list := func(args ...string) []string {
		r := wsExec(t, "", append([]string{"--workspace", p.ws, "--json", "repo", "ls"}, args...)...)
		if r.code != 0 {
			t.Fatalf("%v: %d %s", args, r.code, r.err)
		}
		var data struct {
			Statuses []struct {
				Path string `json:"path"`
			} `json:"statuses"`
		}
		decode(t, r, &data)
		var out []string
		for _, s := range data.Statuses {
			out = append(out, filepath.Base(s.Path))
		}
		return out
	}
	eq := func(got []string, want ...string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("got %v, want %v", got, want)
		}
	}
	eq(list("--dirty"), "dirty")
	eq(list("--detached"), "detached")
	eq(list("--ahead"), "a")
	eq(list("--behind"), "a")
	eq(list("--ahead", "--dirty"))                    // all must match
	eq(list("--path", filepath.Join(p.ws, "a")), "a") // whole path components
	eq(list("--path", filepath.Join(p.ws, "d")))
}

func TestRepoLsUnknownCheckAndField(t *testing.T) {
	ws := initScanFixture(t, true)
	r := wsExec(t, "", "--workspace", ws, "repo", "ls", "--check", "bogus")
	if r.code != 1 || !strings.Contains(r.err, `unknown check "bogus"`) || !strings.Contains(r.err, "credentials") {
		t.Fatalf("%d %q", r.code, r.err)
	}
	r = wsExec(t, "", "--workspace", ws, "repo", "ls", "--sort", "bogus")
	if r.code != 1 {
		t.Fatalf("unknown sort field: %d", r.code)
	}
	r = wsExec(t, "", "--workspace", ws, "repo", "ls", "--bogus-flag")
	if r.code != 1 {
		t.Fatalf("bad flag: %d", r.code)
	}
}

func TestRepoLsJSONShape(t *testing.T) {
	ws := initScanFixture(t, false) // no identity at all
	r := wsExec(t, "", "--workspace", ws, "--json", "repo", "ls", "--check", "identity")
	if r.code != 0 || jsonCommand(t, r) != "repo.ls" {
		t.Fatalf("%d %s", r.code, r.out)
	}
	var data struct {
		Statuses []map[string]any `json:"statuses"`
		Findings []struct {
			Repo, Check, Severity, Detail string
		} `json:"findings"`
		Warnings []string `json:"fetch_warnings"`
	}
	decode(t, r, &data)
	if len(data.Statuses) != 1 || len(data.Findings) != 2 || data.Warnings != nil {
		t.Fatalf("%+v", data)
	}
	for _, key := range []string{"path", "state", "ahead", "behind", "issues", "branch", "detached", "dirty", "has_upstream", "findings", "error"} {
		if _, ok := data.Statuses[0][key]; !ok {
			t.Errorf("row lacks field %q", key)
		}
	}
	if data.Statuses[0]["issues"].(float64) != 2 {
		t.Errorf("two warnings: issues %v", data.Statuses[0]["issues"])
	}
	for _, f := range data.Findings {
		if f.Severity != "warn" || f.Check != "identity" || !strings.HasSuffix(f.Detail, "ws repo configure "+filepath.Join(ws, "r1")+")") {
			t.Errorf("finding %+v", f)
		}
	}

	// findings is [] and not null when there are none.
	ws = initScanFixture(t, true)
	r = wsExec(t, "", "--workspace", ws, "--json", "repo", "ls", "--check", "identity")
	if !strings.Contains(r.out, `"findings": []`) {
		t.Fatalf("findings must be []: %s", r.out)
	}
}

// Info findings are listed but are not issues.
func TestRepoLsIssuesIgnoreInfo(t *testing.T) {
	isolateGit(t, "[user]\n\tname = G\n\temail = g@example.com\n")
	ws := initWorkspace(t)
	r1 := mkRepo(t, filepath.Join(ws, "r1"))
	runGitLocal(t, r1, "config", "--unset", "user.name")
	runGitLocal(t, r1, "config", "--unset", "user.email")

	r := wsExec(t, "", "--workspace", ws, "--json", "repo", "ls", "--check", "identity")
	var data struct {
		Statuses []map[string]any `json:"statuses"`
		Findings []struct{ Severity string }
	}
	decode(t, r, &data)
	if len(data.Findings) != 2 || data.Findings[0].Severity != "info" {
		t.Fatalf("identity set only globally is info: %+v", data.Findings)
	}
	if data.Statuses[0]["issues"].(float64) != 0 {
		t.Fatalf("issues must not count info: %v", data.Statuses[0]["issues"])
	}
	if ids := data.Statuses[0]["findings"].([]any); len(ids) != 2 {
		t.Fatalf("findings lists every check id: %v", ids)
	}
	text := wsExec(t, "", "--workspace", ws, "--no-color", "repo", "ls", "--check", "identity")
	row := strings.Fields(strings.Split(strings.TrimSpace(text.out), "\n")[1])
	// path state(2 words) ahead behind [issues blank] branch
	if row[len(row)-1] != "main" || row[len(row)-2] != "0" {
		t.Fatalf("blank issues cell expected: %v", row)
	}
}

func TestRepoLsSortFilterWide(t *testing.T) {
	ws := repoWorkspace(t)
	mkRepo(t, filepath.Join(ws, "b"))
	d := mkRepo(t, filepath.Join(ws, "a"))
	if err := os.WriteFile(filepath.Join(d, "f.txt"), []byte("d\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := wsExec(t, "", "--workspace", ws, "--no-color", "repo", "ls", "--sort", "-path")
	rows := strings.Split(strings.TrimSpace(r.out), "\n")
	if !strings.HasPrefix(rows[1], filepath.Join(ws, "b")) {
		t.Fatalf("descending sort:\n%s", r.out)
	}
	r = wsExec(t, "", "--workspace", ws, "--no-color", "repo", "ls", "--filter", "state=dirty")
	if strings.Count(strings.TrimSpace(r.out), "\n") != 1 || !strings.Contains(r.out, filepath.Join(ws, "a")) {
		t.Fatalf("filter:\n%s", r.out)
	}
	r = wsExec(t, "", "--workspace", ws, "--no-color", "repo", "ls", "--wide")
	for _, col := range []string{"DETACHED", "DIRTY", "FINDINGS", "ERROR"} {
		if !strings.Contains(r.out, col) {
			t.Errorf("--wide lacks %s:\n%s", col, r.out)
		}
	}
}

// ── aliases ────────────────────────────────────────────────────────

func TestRepoOldNamesAreAliases(t *testing.T) {
	p := newPair(t)
	p.remoteCommit(t, "n.txt", "n\n")

	// scan fetches; its hint names the new command.
	r := wsExec(t, "", "--workspace", p.ws, "repo", "scan")
	if r.code != 0 || !strings.Contains(r.err, `ws: "repo scan" is now "repo ls --fetch"`) {
		t.Fatalf("scan: %d %q", r.code, r.err)
	}
	if !hasFetchHead(p.a) || !strings.Contains(r.out, p.a) {
		t.Fatalf("repo scan must fetch and list:\n%s", r.out)
	}

	other := newPair(t)
	r = wsExec(t, "", "--workspace", other.ws, "repo", "scan", "--no-fetch")
	if r.code != 0 || hasFetchHead(other.a) {
		t.Fatalf("scan --no-fetch must not fetch: %d", r.code)
	}

	r = wsExec(t, "", "--workspace", p.ws, "repo", "doctor")
	if r.code != 0 || !strings.Contains(r.err, `"repo doctor" is now "repo ls"`) || !strings.Contains(r.out, p.a) {
		t.Fatalf("doctor: %d %q", r.code, r.err)
	}

	r = wsExec(t, "", "--workspace", p.ws, "repo", "ls-roots")
	if r.code != 0 || !strings.Contains(r.err, `"repo ls-roots" is now "repo root ls"`) || strings.TrimSpace(r.out) != p.ws {
		t.Fatalf("ls-roots: %d %q %q", r.code, r.err, r.out)
	}

	sub := filepath.Join(p.ws, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	r = wsExec(t, "", "--workspace", p.ws, "--dry-run", "repo", "add-root", sub)
	if r.code != 0 || !strings.Contains(r.err, `"repo add-root" is now "repo root add"`) || !strings.Contains(r.out, "Add repo root "+sub) {
		t.Fatalf("add-root: %d %q %q", r.code, r.err, r.out)
	}

	// sync is save: it commits, pulls and pushes.
	q := newPair(t)
	commit(t, q.a, "l.txt", "l\n", "local")
	r = wsExec(t, "", "--workspace", q.ws, "--quiet", "repo", "sync")
	if r.code != 0 {
		t.Fatalf("sync: %d %q %q", r.code, r.out, r.err)
	}
	if got := gitOutLocal(t, q.remote, "log", "--format=%s", "-1"); got != "local" {
		t.Fatalf("sync should push: remote head %q", got)
	}
}

// ── show ───────────────────────────────────────────────────────────

func TestRepoShow(t *testing.T) {
	ws := repoWorkspace(t)
	blog := mkRepo(t, filepath.Join(ws, "blog")) // no upstream
	r := wsExec(t, "", "--workspace", ws, "--no-color", "repo", "show", blog)
	if r.code != 0 {
		t.Fatalf("%d %q", r.code, r.err)
	}
	for _, want := range []string{"Path      " + blog, "Branch    main", "State     no upstream", "Upstream  none", "Hygiene", "warn  upstream"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q in:\n%s", want, r.out)
		}
	}
	if hasFetchHead(blog) {
		t.Fatal("show never fetches")
	}

	r = wsExec(t, "", "--workspace", ws, "--json", "repo", "show", blog)
	if jsonCommand(t, r) != "repo.show" {
		t.Fatal(r.out)
	}
	var data struct {
		Status   map[string]any   `json:"status"`
		Findings []map[string]any `json:"findings"`
	}
	decode(t, r, &data)
	if data.Status["path"] != blog || len(data.Findings) == 0 || data.Findings[0]["check"] == nil {
		t.Fatalf("%+v", data)
	}
}

func TestRepoShowArguments(t *testing.T) {
	ws := repoWorkspace(t)
	a := mkRepo(t, filepath.Join(ws, "a"))
	b := mkRepo(t, filepath.Join(ws, "b"))
	for _, args := range [][]string{{}, {a, b}, {filepath.Join(ws, "nope")}, {"--bogus", a}} {
		r := wsExec(t, "", append([]string{"--workspace", ws, "repo", "show"}, args...)...)
		if r.code != 1 || r.err == "" {
			t.Errorf("%v: %d %q", args, r.code, r.err)
		}
	}
}

func TestRepoShowCleanRepoHasNoHygiene(t *testing.T) {
	isolateGit(t, "[user]\n\tname = G\n\temail = g@example.com\n[init]\n\tdefaultBranch = main\n[credential]\n\thelper = store\n")
	ws := initWorkspace(t)
	a := mkRepo(t, filepath.Join(ws, "a"))
	mkRemote(t, a)
	runGitLocal(t, a, "fetch")
	r := wsExec(t, "", "--workspace", ws, "repo", "show", a)
	if !strings.Contains(r.out, "State     clean") || !strings.Contains(r.out, "Upstream  ↑0 ↓0") {
		t.Fatalf("%s", r.out)
	}
	// identity is local here, so only global-vs-local info may appear: none expected.
	if !strings.Contains(r.out, "Hygiene  none") {
		t.Fatalf("a clean repo has no findings:\n%s", r.out)
	}
}

// ── fetch ──────────────────────────────────────────────────────────

func TestRepoFetch(t *testing.T) {
	p := newPair(t)
	p.remoteCommit(t, "n.txt", "n\n")
	r := wsExec(t, "", "--workspace", p.ws, "--no-color", "repo", "fetch")
	if r.code != 0 || strings.TrimSpace(r.out) != "[ok] "+p.a+" fetched" {
		t.Fatalf("%d %q %q", r.code, r.out, r.err)
	}
	if got := gitOutLocal(t, p.a, "rev-list", "--count", "HEAD..origin/main"); got != "1" {
		t.Fatalf("remote-tracking ref not updated: %s", got)
	}
	// The branch and working tree stay.
	if _, err := os.Stat(filepath.Join(p.a, "n.txt")); err == nil {
		t.Fatal("fetch must not touch the working tree")
	}

	r = wsExec(t, "", "--workspace", p.ws, "--json", "repo", "fetch", p.a)
	var data []struct {
		Path    string `json:"path"`
		Success bool   `json:"success"`
	}
	decode(t, r, &data)
	if jsonCommand(t, r) != "repo.fetch" || len(data) != 1 || !data[0].Success || data[0].Path != p.a {
		t.Fatalf("%s", r.out)
	}
}

func TestRepoFetchDryRunDoesNothing(t *testing.T) {
	p := newPair(t)
	r := wsExec(t, "", "--workspace", p.ws, "--dry-run", "repo", "fetch")
	if r.code != 0 || strings.TrimSpace(r.out) != "Would fetch "+p.a {
		t.Fatalf("%d %q", r.code, r.out)
	}
	if hasFetchHead(p.a) {
		t.Fatal("--dry-run must not fetch")
	}
}

func TestRepoFetchExitCodes(t *testing.T) {
	ws := repoWorkspace(t)
	good := mkRepo(t, filepath.Join(ws, "good"))
	mkRemote(t, good)
	bad := mkRepo(t, filepath.Join(ws, "bad"))
	runGitLocal(t, bad, "remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git"))

	r := wsExec(t, "", "--workspace", ws, "--no-color", "repo", "fetch")
	if r.code != 3 || !strings.Contains(r.out, "[err] "+bad+" failed: ") || !strings.Contains(r.out, "[ok] "+good+" fetched") {
		t.Fatalf("some fail: %d\n%s", r.code, r.out)
	}
	if r = wsExec(t, "", "--workspace", ws, "repo", "fetch", bad); r.code != 1 {
		t.Fatalf("all fail: %d", r.code)
	}
	if r = wsExec(t, "", "--workspace", ws, "repo", "fetch", good); r.code != 0 {
		t.Fatalf("all succeed: %d", r.code)
	}
	r = wsExec(t, "", "--workspace", ws, "--json", "repo", "fetch")
	if r.code != 3 {
		t.Fatalf("json exit %d", r.code)
	}
}

func TestRepoFetchFilterReadsLastFetchState(t *testing.T) {
	p := newPair(t)
	other := mkRepo(t, filepath.Join(p.ws, "z"))
	mkRemote(t, other)
	if err := os.WriteFile(filepath.Join(p.a, "f.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := wsExec(t, "", "--workspace", p.ws, "repo", "fetch", "--dirty")
	if r.code != 0 || !strings.Contains(r.out, p.a) || strings.Contains(r.out, other) {
		t.Fatalf("%d\n%s", r.code, r.out)
	}
	if hasFetchHead(other) {
		t.Fatal("a repo the filter removes is not fetched")
	}
}

// ── pull ───────────────────────────────────────────────────────────

func TestRepoPullFastForward(t *testing.T) {
	p := newPair(t)
	p.remoteCommit(t, "n.txt", "n\n")

	r := wsExec(t, "", "--workspace", p.ws, "--no-color", "repo", "pull")
	// prompted: the default answer (Enter, EOF) is yes
	if r.code != 0 {
		t.Fatalf("%d %q %q", r.code, r.out, r.err)
	}
	if !strings.Contains(r.out, "Pull "+p.a+" (1 behind, fast-forward)") {
		t.Fatalf("plan line:\n%s", r.out)
	}
	if _, err := os.Stat(filepath.Join(p.a, "n.txt")); err != nil {
		t.Fatal("pull should bring n.txt")
	}
}

func TestRepoPullUpToDate(t *testing.T) {
	p := newPair(t)
	r := wsExec(t, "", "--workspace", p.ws, "repo", "pull")
	if r.code != 0 || !strings.Contains(r.out, "All repositories are up to date.") {
		t.Fatalf("%d %q", r.code, r.out)
	}
}

func TestRepoPullPromptsPerRepoAndDeclines(t *testing.T) {
	p := newPair(t)
	p.remoteCommit(t, "n.txt", "n\n")
	r := wsExec(t, "n\n", "--workspace", p.ws, "repo", "pull")
	if r.code != 0 {
		t.Fatalf("%d", r.code)
	}
	if _, err := os.Stat(filepath.Join(p.a, "n.txt")); err == nil {
		t.Fatal("declined: nothing may change")
	}
}

func TestRepoPullSkipsWithReasons(t *testing.T) {
	ws := repoWorkspace(t)
	noUp := mkRepo(t, filepath.Join(ws, "noup"))
	det := mkRepo(t, filepath.Join(ws, "det"))
	mkRemote(t, det)
	runGitLocal(t, det, "checkout", "--detach")
	bad := mkRepo(t, filepath.Join(ws, "bad"))
	mkRemote(t, bad)
	runGitLocal(t, bad, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))

	r := wsExec(t, "", "--workspace", ws, "--no-color", "repo", "pull")
	if r.code != 0 {
		t.Fatalf("skips are not failures: %d", r.code)
	}
	for _, want := range []string{
		"Skipped: " + noUp + " (no upstream (ws repo configure sets one))",
		"Skipped: " + det + " (detached HEAD)",
		"Skipped: " + bad + " (fetch failed: ",
	} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q in:\n%s", want, r.out)
		}
	}

	r = wsExec(t, "", "--workspace", ws, "--json", "repo", "pull")
	var data struct {
		Actions []any `json:"actions"`
		Skipped []struct{ Path, Reason string }
	}
	decode(t, r, &data)
	if jsonCommand(t, r) != "repo.pull" || len(data.Actions) != 0 || len(data.Skipped) != 3 {
		t.Fatalf("%s", r.out)
	}
}

func divergePair(t *testing.T) repoPair {
	t.Helper()
	p := newPair(t)
	p.remoteCommit(t, "remote.txt", "r\n")
	commit(t, p.a, "local.txt", "l\n", "local")
	return p
}

func TestRepoPullDivergedWithoutTerminalIsSkipped(t *testing.T) {
	p := divergePair(t)
	for _, args := range [][]string{{"repo", "pull"}, {"--quiet", "repo", "pull"}, {"--json", "repo", "pull"}} {
		r := wsExec(t, "r\n", append([]string{"--workspace", p.ws}, args...)...)
		if r.code != 0 {
			t.Fatalf("%v: %d %q", args, r.code, r.err)
		}
		if strings.Contains(strings.Join(args, " "), "--quiet") {
			continue // quiet prints nothing
		}
		if !strings.Contains(r.out, "diverged: choose --rebase or --merge") {
			t.Errorf("%v: reason missing:\n%s", args, r.out)
		}
	}
	if _, err := os.Stat(filepath.Join(p.a, "remote.txt")); err == nil {
		t.Fatal("a diverged repo is never pulled without a choice")
	}
}

func TestRepoPullRebaseAndMergeFlags(t *testing.T) {
	for _, flag := range []string{"--rebase", "--merge"} {
		t.Run(flag, func(t *testing.T) {
			p := divergePair(t)
			r := wsExec(t, "", "--workspace", p.ws, "--no-color", "--quiet", "repo", "pull", flag)
			if r.code != 0 {
				t.Fatalf("%d %q", r.code, r.err)
			}
			if _, err := os.Stat(filepath.Join(p.a, "remote.txt")); err != nil {
				t.Fatal("pulled")
			}
			merges := gitOutLocal(t, p.a, "rev-list", "--merges", "--count", "HEAD")
			if (flag == "--merge") != (merges == "1") {
				t.Fatalf("%s: merge commits %s", flag, merges)
			}
		})
	}
	p := divergePair(t)
	if r := wsExec(t, "", "--workspace", p.ws, "repo", "pull", "--rebase", "--merge"); r.code != 1 {
		t.Fatalf("both flags: %d", r.code)
	}
}

func TestRepoPullAsksRebaseMergeSkip(t *testing.T) {
	forceTerminal(t)
	for _, c := range []struct {
		answer string
		pulled bool
		merges string
		plan   string
	}{
		{"r\ny\n", true, "0", "(diverged, rebase)"},
		{"m\ny\n", true, "1", "(diverged, merge)"},
		{"s\n", false, "", "diverged: skipped"},
	} {
		p := divergePair(t)
		r := wsExec(t, c.answer, "--workspace", p.ws, "--no-color", "repo", "pull")
		if r.code != 0 {
			t.Fatalf("%q: %d %q", c.answer, r.code, r.err)
		}
		if !strings.Contains(r.out, p.a+" has diverged (1 local, 1 remote commit).") ||
			!strings.Contains(r.out, "Pull with [r]ebase, [m]erge, or [s]kip this repo?") ||
			!strings.Contains(r.out, c.plan) {
			t.Errorf("%q: output:\n%s", c.answer, r.out)
		}
		_, err := os.Stat(filepath.Join(p.a, "remote.txt"))
		if (err == nil) != c.pulled {
			t.Errorf("%q: pulled=%v", c.answer, err == nil)
		}
		if c.pulled && gitOutLocal(t, p.a, "rev-list", "--merges", "--count", "HEAD") != c.merges {
			t.Errorf("%q: wrong strategy used", c.answer)
		}
	}
}

func TestRepoPullDryRunPlansFromLastFetch(t *testing.T) {
	p := newPair(t)
	p.remoteCommit(t, "n.txt", "n\n")

	// Not fetched yet: a dry run fetches nothing, so there is nothing to pull.
	r := wsExec(t, "", "--workspace", p.ws, "--dry-run", "repo", "pull")
	if r.code != 0 || hasFetchHead(p.a) {
		t.Fatalf("%d fetched=%v", r.code, hasFetchHead(p.a))
	}
	if !strings.Contains(r.out, "Planned from the last fetch") {
		t.Fatalf("a dry run says how old its data is:\n%s", r.out)
	}
	if strings.Contains(r.out, "[dry-run] Pull") {
		t.Fatalf("stale state has nothing to pull:\n%s", r.out)
	}

	// Once fetched, the dry run plans the pull and still changes nothing.
	runGitLocal(t, p.a, "fetch")
	r = wsExec(t, "", "--workspace", p.ws, "--no-color", "--dry-run", "repo", "pull")
	if !strings.Contains(r.out, "[dry-run] Pull "+p.a+" (1 behind, fast-forward)") {
		t.Fatalf("%s", r.out)
	}
	if _, err := os.Stat(filepath.Join(p.a, "n.txt")); err == nil {
		t.Fatal("dry run must not pull")
	}
}

func TestRepoPullJSONExitFollowsActions(t *testing.T) {
	p := newPair(t)
	p.remoteCommit(t, "n.txt", "n\n")
	// A dirty file the pull would overwrite: git refuses, the action fails.
	if err := os.WriteFile(filepath.Join(p.a, "n.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := wsExec(t, "", "--workspace", p.ws, "--json", "repo", "pull")
	if r.code != 1 {
		t.Fatalf("every action failed: exit %d\n%s", r.code, r.out)
	}
	var data struct {
		Actions []struct{ ID, Status, Error string }
	}
	decode(t, r, &data)
	if len(data.Actions) != 1 || data.Actions[0].Status != "failed" || data.Actions[0].Error == "" {
		t.Fatalf("%+v", data)
	}
}

func TestRepoPullFailedRebaseIsAborted(t *testing.T) {
	p := newPair(t)
	p.remoteCommit(t, "base.txt", "remote edit\n")
	commit(t, p.a, "base.txt", "local edit\n", "local edit")
	r := wsExec(t, "", "--workspace", p.ws, "--quiet", "repo", "pull", "--rebase")
	if r.code != 1 {
		t.Fatalf("a conflicting rebase fails its action: %d", r.code)
	}
	if _, err := os.Stat(filepath.Join(p.a, ".git", "rebase-merge")); err == nil {
		t.Fatal("no repo is left mid-rebase")
	}
}

// ── push ───────────────────────────────────────────────────────────

func TestRepoPush(t *testing.T) {
	p := newPair(t)
	commit(t, p.a, "l.txt", "l\n", "local")
	commit(t, p.a, "m.txt", "m\n", "local 2")
	if err := os.WriteFile(filepath.Join(p.a, "f.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := wsExec(t, "y\n", "--workspace", p.ws, "--no-color", "repo", "push")
	if r.code != 0 {
		t.Fatalf("%d %q %q", r.code, r.out, r.err)
	}
	if !strings.Contains(r.out, "Push "+p.a+" (2 ahead; 1 uncommitted change stays local)") {
		t.Fatalf("plan line:\n%s", r.out)
	}
	if got := gitOutLocal(t, p.remote, "rev-list", "--count", "main"); got != "3" {
		t.Fatalf("remote has %s commits", got)
	}
	// It pushes only: the dirty file is still dirty, not committed.
	if got := gitOutLocal(t, p.a, "status", "--porcelain"); got == "" {
		t.Fatal("push must not commit")
	}
}

func TestRepoPushSkipsWithReasons(t *testing.T) {
	ws := repoWorkspace(t)
	noUp := mkRepo(t, filepath.Join(ws, "1-noup"))
	det := mkRepo(t, filepath.Join(ws, "2-det"))
	mkRemote(t, det)
	runGitLocal(t, det, "checkout", "--detach")
	same := mkRepo(t, filepath.Join(ws, "3-same"))
	remote := mkRemote(t, same)
	behind := mkRepo(t, filepath.Join(ws, "4-behind"))
	remote2 := mkRemote(t, behind)
	o := cloneOf(t, remote2)
	commit(t, o, "x.txt", "x\n", "x")
	runGitLocal(t, o, "push")
	runGitLocal(t, behind, "fetch")
	_ = remote

	r := wsExec(t, "", "--workspace", ws, "--no-color", "repo", "push")
	if r.code != 0 {
		t.Fatalf("%d", r.code)
	}
	for _, want := range []string{
		"Skipped: " + noUp + " (no upstream (ws repo configure sets one))",
		"Skipped: " + det + " (detached HEAD)",
		"Skipped: " + same + " (nothing to push)",
		"Skipped: " + behind + " (behind or diverged: pull first (or ws repo save))",
	} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q in:\n%s", want, r.out)
		}
	}
}

func TestRepoPushJSONAndFailure(t *testing.T) {
	p := newPair(t)
	commit(t, p.a, "l.txt", "l\n", "local")
	if err := os.Rename(p.remote, p.remote+".gone"); err != nil {
		t.Fatal(err)
	}
	r := wsExec(t, "", "--workspace", p.ws, "--json", "repo", "push")
	if r.code != 1 || jsonCommand(t, r) != "repo.push" {
		t.Fatalf("a failed push exits 1: %d\n%s", r.code, r.out)
	}
	if !strings.Contains(r.out, `"skipped": []`) {
		t.Fatalf("skipped is a list:\n%s", r.out)
	}
}

func TestRepoPushNeverFetchesOrForces(t *testing.T) {
	p := newPair(t)
	p.remoteCommit(t, "n.txt", "n\n") // remote moved, but we never fetched
	commit(t, p.a, "l.txt", "l\n", "local")
	r := wsExec(t, "", "--workspace", p.ws, "--quiet", "repo", "push")
	if hasFetchHead(p.a) {
		t.Fatal("push does not fetch")
	}
	// Stale counts say ahead, so it tries a plain push, which git rejects:
	// the action fails, nothing is forced.
	if r.code != 1 {
		t.Fatalf("rejected push: exit %d", r.code)
	}
	if got := gitOutLocal(t, p.remote, "log", "--format=%s", "-1", "main"); got != "remote n.txt" {
		t.Fatalf("remote must keep its commit: %q", got)
	}
}

// ── save ───────────────────────────────────────────────────────────

var saveMessage = regexp.MustCompile(`^ws save \d{4}-\d{2}-\d{2} \d{2}:\d{2} \(.+\)$`)

func TestRepoSaveCommitsAndPushes(t *testing.T) {
	p := newPair(t)
	if err := os.WriteFile(filepath.Join(p.a, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.a, "f.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := wsExec(t, "y\n", "--workspace", p.ws, "--no-color", "repo", "save")
	if r.code != 0 {
		t.Fatalf("%d %q %q", r.code, r.out, r.err)
	}
	if !strings.Contains(r.out, "Save "+p.a+": commit 2 changes, push") {
		t.Fatalf("plan line:\n%s", r.out)
	}
	if msg := gitOutLocal(t, p.remote, "log", "--format=%s", "-1", "main"); !saveMessage.MatchString(msg) {
		t.Fatalf("default message %q", msg)
	}
	if gitOutLocal(t, p.a, "status", "--porcelain") != "" {
		t.Fatal("untracked files are committed too")
	}
}

func TestRepoSaveMessageFlag(t *testing.T) {
	p := newPair(t)
	if err := os.WriteFile(filepath.Join(p.a, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := wsExec(t, "", "--workspace", p.ws, "--quiet", "repo", "save", "-m", "my words")
	if r.code != 0 {
		t.Fatalf("%d %q", r.code, r.err)
	}
	if msg := gitOutLocal(t, p.remote, "log", "--format=%s", "-1", "main"); msg != "my words" {
		t.Fatalf("message %q", msg)
	}
}

func TestRepoSaveNothingToDo(t *testing.T) {
	p := newPair(t)
	r := wsExec(t, "", "--workspace", p.ws, "repo", "save")
	if r.code != 0 || !strings.Contains(r.out, "Nothing to save.") {
		t.Fatalf("%d %q", r.code, r.out)
	}
}

func TestRepoSavePullsFastForward(t *testing.T) {
	p := newPair(t)
	p.remoteCommit(t, "n.txt", "n\n")
	r := wsExec(t, "", "--workspace", p.ws, "--no-color", "repo", "save")
	if r.code != 0 || !strings.Contains(r.out, "Save "+p.a+": pull (fast-forward)") {
		t.Fatalf("%d\n%s\n%s", r.code, r.out, r.err)
	}
	if _, err := os.Stat(filepath.Join(p.a, "n.txt")); err != nil {
		t.Fatal("saved repo is up to date")
	}
}

func TestRepoSaveDirtyAndBehindAsksForAStrategy(t *testing.T) {
	// Committing makes the repo diverged: no auto-merge.
	p := newPair(t)
	p.remoteCommit(t, "n.txt", "n\n")
	if err := os.WriteFile(filepath.Join(p.a, "mine.txt"), []byte("m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := wsExec(t, "", "--workspace", p.ws, "--quiet", "repo", "save")
	if r.code != 0 {
		t.Fatalf("%d", r.code)
	}
	if gitOutLocal(t, p.a, "status", "--porcelain") == "" {
		t.Fatal("skipped repos stay as they were: nothing may be committed")
	}
	r = wsExec(t, "", "--workspace", p.ws, "--json", "repo", "save")
	if !strings.Contains(r.out, "diverged: choose --rebase or --merge") {
		t.Fatalf("%s", r.out)
	}

	r = wsExec(t, "", "--workspace", p.ws, "--quiet", "repo", "save", "--rebase")
	if r.code != 0 {
		t.Fatalf("--rebase: %d %q", r.code, r.err)
	}
	if got := gitOutLocal(t, p.remote, "ls-tree", "--name-only", "main"); !strings.Contains(got, "mine.txt") || !strings.Contains(got, "n.txt") {
		t.Fatalf("remote tree:\n%s", got)
	}
}

func TestRepoSaveAsksWhenDiverged(t *testing.T) {
	forceTerminal(t)
	p := divergePair(t)
	r := wsExec(t, "m\ny\n", "--workspace", p.ws, "--no-color", "repo", "save")
	if r.code != 0 || !strings.Contains(r.out, "pull (merge), push") {
		t.Fatalf("%d\n%s\n%s", r.code, r.out, r.err)
	}
	if got := gitOutLocal(t, p.remote, "rev-list", "--merges", "--count", "main"); got != "1" {
		t.Fatalf("merge strategy chosen: %s merges", got)
	}
}

func TestRepoSaveSkipsAndFailures(t *testing.T) {
	ws := repoWorkspace(t)
	noUp := mkRepo(t, filepath.Join(ws, "noup"))
	det := mkRepo(t, filepath.Join(ws, "det"))
	mkRemote(t, det)
	runGitLocal(t, det, "checkout", "--detach")
	r := wsExec(t, "", "--workspace", ws, "--no-color", "repo", "save")
	if r.code != 0 ||
		!strings.Contains(r.out, "Skipped: "+noUp+" (no upstream (ws repo configure sets one))") ||
		!strings.Contains(r.out, "Skipped: "+det+" (detached HEAD)") {
		t.Fatalf("%d\n%s", r.code, r.out)
	}

	// A remote that cannot be fetched: the repo is skipped and nothing is committed.
	p := newPair(t)
	if err := os.WriteFile(filepath.Join(p.a, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(p.remote, p.remote+".gone"); err != nil {
		t.Fatal(err)
	}
	r = wsExec(t, "", "--workspace", p.ws, "--no-color", "repo", "save")
	if !strings.Contains(r.out, "Skipped: "+p.a+" (fetch failed: ") {
		t.Fatalf("a failed fetch skips the repo:\n%s", r.out)
	}
	if gitOutLocal(t, p.a, "status", "--porcelain") == "" {
		t.Fatal("nothing committed after a failed fetch")
	}
}

func TestRepoSaveDryRun(t *testing.T) {
	p := newPair(t)
	if err := os.WriteFile(filepath.Join(p.a, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := wsExec(t, "", "--workspace", p.ws, "--no-color", "--dry-run", "repo", "save")
	if r.code != 0 || !strings.Contains(r.out, "[dry-run] Save "+p.a+": commit 1 change, push") {
		t.Fatalf("%d\n%s", r.code, r.out)
	}
	if hasFetchHead(p.a) || gitOutLocal(t, p.a, "status", "--porcelain") == "" {
		t.Fatal("a dry run fetches and commits nothing")
	}
	r = wsExec(t, "", "--workspace", p.ws, "--json", "--dry-run", "repo", "save")
	if !strings.Contains(r.out, `"dry_run": true`) || jsonCommand(t, r) != "repo.save" {
		t.Fatalf("%s", r.out)
	}
}

// ── configure ──────────────────────────────────────────────────────

func TestRepoConfigureNeedsATerminal(t *testing.T) {
	ws := initScanFixture(t, false)
	for _, args := range [][]string{{"repo", "configure"}, {"--quiet", "repo", "configure"}, {"--json", "repo", "configure"}} {
		r := wsExec(t, "Me\n", append([]string{"--workspace", ws}, args...)...)
		if r.code != 1 || !strings.Contains(r.err, "ws repo configure needs a terminal") {
			t.Errorf("%v: %d %q", args, r.code, r.err)
		}
	}
	// Even on a terminal, --quiet and --json refuse.
	forceTerminal(t)
	r := wsExec(t, "", "--workspace", ws, "--json", "repo", "configure")
	if r.code != 1 || !strings.Contains(r.err, "needs a terminal") {
		t.Fatalf("%d %q", r.code, r.err)
	}
}

// A global config that already has the default branch and a credential
// helper, so only identity and upstream questions arise.
const gitConfigNoise = "[init]\n\tdefaultBranch = main\n[credential]\n\thelper = store\n"

func TestRepoConfigureIdentityPromptsAndPlan(t *testing.T) {
	forceTerminal(t)
	cfg := isolateGit(t, gitConfigNoise)
	ws := initWorkspace(t)
	r1 := mkRepo(t, filepath.Join(ws, "r1"))
	r2 := mkRepo(t, filepath.Join(ws, "r2"))
	for _, r := range []string{r1, r2} {
		runGitLocal(t, r, "config", "--unset", "user.name")
		runGitLocal(t, r, "config", "--unset", "user.email")
	}
	// r1: name, email, Enter (global is the default: no identity exists yet),
	// then the remote URL (blank). r2 gets the global identity unasked, so it
	// only asks for a remote URL (blank). Then the plan: accept all.
	in := "Ada\nada@example.com\n\n\n\na\n"
	r := wsExec(t, in, "--workspace", ws, "--no-color", "repo", "configure")
	if r.code != 0 {
		t.Fatalf("%d\n%s\n%s", r.code, r.out, r.err)
	}
	for _, want := range []string{"Name:", "Email:", "For all repos (global) or only this one? [g/l]", `Set user.name to "Ada" for all repos`} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q in:\n%s", want, r.out)
		}
	}
	// The global answer is asked once, for the first repo only.
	if strings.Count(r.out, "Name:") != 1 {
		t.Fatalf("global identity is asked once:\n%s", r.out)
	}
	if got := gitOutLocal(t, r2, "config", "user.name"); got != "Ada" {
		t.Fatalf("both repos get the global identity, r2 sees %q", got)
	}
	if got := gitOutLocal(t, r1, "config", "--file", cfg, "--get", "user.email"); got != "ada@example.com" {
		t.Fatalf("global user.email %q", got)
	}
	// No suggestions: the prompts carry no default.
	if strings.Contains(r.out, "Name: [") || strings.Contains(r.out, "Email: [") {
		t.Fatalf("name and email prompts suggest nothing:\n%s", r.out)
	}
}

func TestRepoConfigureIdentityLocal(t *testing.T) {
	forceTerminal(t)
	isolateGit(t, gitConfigNoise)
	ws := initWorkspace(t)
	r1 := mkRepo(t, filepath.Join(ws, "r1"))
	r2 := mkRepo(t, filepath.Join(ws, "r2"))
	for _, r := range []string{r1, r2} {
		runGitLocal(t, r, "config", "--unset", "user.name")
		runGitLocal(t, r, "config", "--unset", "user.email")
	}
	// Only r1 is a target. Local, then accept the plan.
	in := "Grace\ngrace@example.com\nl\n\ny\ny\n"
	r := wsExec(t, in, "--workspace", ws, "--no-color", "repo", "configure", r1)
	if r.code != 0 {
		t.Fatalf("%d\n%s\n%s", r.code, r.out, r.err)
	}
	if got := gitOutLocal(t, r1, "config", "--local", "user.name"); got != "Grace" {
		t.Fatalf("local user.name %q\n%s", got, r.out)
	}
	if out, err := exec.Command("git", "-C", r2, "config", "user.name").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		t.Fatalf("r2 was not a target, but has user.name %q", out)
	}
}

func TestRepoConfigureBlankSkipsAndEmptyPlan(t *testing.T) {
	forceTerminal(t)
	isolateGit(t, gitConfigNoise)
	ws := initWorkspace(t)
	r1 := mkRepo(t, filepath.Join(ws, "r1"))
	runGitLocal(t, r1, "config", "--unset", "user.name")
	runGitLocal(t, r1, "config", "--unset", "user.email")
	// Blank name, blank email, blank remote URL: nothing is set.
	r := wsExec(t, "\n\n\n", "--workspace", ws, "repo", "configure", r1)
	if r.code != 0 || !strings.Contains(r.out, "Nothing to configure.") {
		t.Fatalf("%d %q %q", r.code, r.out, r.err)
	}
	if out, err := exec.Command("git", "-C", r1, "config", "user.name").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		t.Fatalf("a blank answer sets nothing, got %q", out)
	}
}

func TestRepoConfigureUpstream(t *testing.T) {
	forceTerminal(t)
	isolateGit(t, "[user]\n\tname = G\n\temail = g@example.com\n"+gitConfigNoise)
	ws := initWorkspace(t)

	// 1. origin has the branch: Track origin/main? [Y/n]
	a := mkRepo(t, filepath.Join(ws, "1-track"))
	remote := mkRemote(t, a)
	runGitLocal(t, a, "branch", "--unset-upstream")
	// 2. origin exists but lacks the branch: Push and track? [y/N]
	b := mkRepo(t, filepath.Join(ws, "2-push"))
	runGitLocal(t, b, "remote", "add", "origin", remote)
	runGitLocal(t, b, "checkout", "-b", "feature")
	// 3. no remote: Remote URL
	c := mkRepo(t, filepath.Join(ws, "3-remote"))
	newRemote := filepath.Join(t.TempDir(), "new.git")
	runGitLocal(t, t.TempDir(), "init", "--bare", newRemote)
	// fetch-staleness info exists for all (never fetched) — decline those.
	in := strings.Join([]string{
		"",  // 1: Track origin/main? default yes
		"n", // 1: Fetch now?  (origin exists, never fetched? it was pushed: FETCH_HEAD absent)
		"y", // 2: Push feature to origin and track it?
		"n", // 2: Fetch now?
		newRemote,
		"a", // accept the plan
	}, "\n") + "\n"
	r := wsExec(t, in, "--workspace", ws, "--no-color", "repo", "configure")
	if r.code != 0 {
		t.Fatalf("%d\n%s\n%s", r.code, r.out, r.err)
	}
	for _, want := range []string{"Track origin/main? [Y/n]", "Push feature to origin and track it? [y/N]", "Remote URL (blank to skip):"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q in:\n%s", want, r.out)
		}
	}
	if got := gitOutLocal(t, a, "rev-parse", "--abbrev-ref", "@{u}"); got != "origin/main" {
		t.Errorf("1: upstream %q", got)
	}
	if got := gitOutLocal(t, b, "rev-parse", "--abbrev-ref", "@{u}"); got != "origin/feature" {
		t.Errorf("2: upstream %q", got)
	}
	if got := gitOutLocal(t, c, "remote", "get-url", "origin"); got != newRemote {
		t.Errorf("3: origin %q", got)
	}
}

func TestRepoConfigureDefaultBranchCredentialsAndFetch(t *testing.T) {
	forceTerminal(t)
	cfg := isolateGit(t, "[user]\n\tname = G\n\temail = g@example.com\n")
	ws := initWorkspace(t)
	a := mkRepo(t, filepath.Join(ws, "a"))
	mkRemote(t, a)
	runGitLocal(t, a, "remote", "set-url", "origin", "https://example.invalid/x.git")

	enabled := 0
	saved := credentialEnable
	credentialEnable = func(*repoEnv) error { enabled++; return nil }
	t.Cleanup(func() { credentialEnable = saved })

	// default branch: Enter accepts main; credentials: y; fetch: n.
	in := "\ny\nn\na\n"
	r := wsExec(t, in, "--workspace", ws, "--no-color", "repo", "configure", a)
	if r.code != 0 {
		t.Fatalf("%d\n%s\n%s", r.code, r.out, r.err)
	}
	for _, want := range []string{"Default branch for new repos [main]:", "Use ws (pass) as git's credential helper? [y/N]", "Fetch now? [Y/n]"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q in:\n%s", want, r.out)
		}
	}
	if got := gitOutLocal(t, a, "config", "--file", cfg, "--get", "init.defaultBranch"); got != "main" {
		t.Errorf("init.defaultBranch %q", got)
	}
	if enabled != 1 {
		t.Errorf("ws credential enable ran %d times", enabled)
	}
}

func TestRepoConfigureDryRunAsksAndChangesNothing(t *testing.T) {
	forceTerminal(t)
	isolateGit(t, "[core]\n")
	ws := initWorkspace(t)
	r1 := mkRepo(t, filepath.Join(ws, "r1"))
	runGitLocal(t, r1, "config", "--unset", "user.name")
	runGitLocal(t, r1, "config", "--unset", "user.email")
	r := wsExec(t, "Ada\nada@example.com\ng\n", "--workspace", ws, "--no-color", "--dry-run", "repo", "configure", r1)
	if r.code != 0 || !strings.Contains(r.out, "[dry-run] Set user.name") {
		t.Fatalf("%d\n%s\n%s", r.code, r.out, r.err)
	}
	if out, err := exec.Command("git", "-C", r1, "config", "user.name").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		t.Fatalf("dry run set %q", out)
	}
}

func TestRepoConfigureNothingToDo(t *testing.T) {
	forceTerminal(t)
	isolateGit(t, "[user]\n\tname = G\n\temail = g@example.com\n[init]\n\tdefaultBranch = main\n[credential]\n\thelper = store\n")
	ws := initWorkspace(t)
	a := mkRepo(t, filepath.Join(ws, "a"))
	mkRemote(t, a)
	runGitLocal(t, a, "fetch")
	r := wsExec(t, "", "--workspace", ws, "repo", "configure")
	if r.code != 0 || !strings.Contains(r.out, "Nothing to configure.") {
		t.Fatalf("%d\n%s", r.code, r.out)
	}
}

func TestRepoConfigureUnknownTarget(t *testing.T) {
	forceTerminal(t)
	ws := repoWorkspace(t)
	r := wsExec(t, "", "--workspace", ws, "repo", "configure", filepath.Join(ws, "nope"))
	if r.code != 1 || !strings.Contains(r.err, "ws: no repo at") {
		t.Fatalf("%d %q", r.code, r.err)
	}
}

// ── run ────────────────────────────────────────────────────────────

func TestRepoRunPrintsIndentedOutput(t *testing.T) {
	ws := repoWorkspace(t)
	a := mkRepo(t, filepath.Join(ws, "a"))
	if err := os.WriteFile(filepath.Join(a, "f.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkRepo(t, filepath.Join(ws, "b"))
	r := wsExec(t, "y\n", "--workspace", ws, "--no-color", "repo", "run", "--dirty", "--", "git", "status", "--short")
	if r.code != 0 {
		t.Fatalf("%d %q %q", r.code, r.out, r.err)
	}
	if !strings.Contains(r.out, "Run in "+a) || !strings.Contains(r.out, "      M f.txt") {
		t.Fatalf("output printed under its action, indented:\n%q", r.out)
	}
	if strings.Contains(r.out, filepath.Join(ws, "b")) {
		t.Fatalf("--dirty narrows the targets:\n%s", r.out)
	}
}

func TestRepoRunTargetsBeforeDashDash(t *testing.T) {
	ws := repoWorkspace(t)
	a := mkRepo(t, filepath.Join(ws, "a"))
	b := mkRepo(t, filepath.Join(ws, "b"))
	r := wsExec(t, "", "--workspace", ws, "--quiet", "--json", "repo", "run", b, "--", "pwd")
	var data struct {
		Command []string `json:"command"`
		Actions []struct{ ID, Status string }
		Results []struct {
			Path     string `json:"path"`
			ExitCode int    `json:"exit_code"`
			Output   string `json:"output"`
		}
	}
	decode(t, r, &data)
	if r.code != 0 || jsonCommand(t, r) != "repo.run" || len(data.Results) != 1 || data.Results[0].Path != b || data.Results[0].Output != b {
		t.Fatalf("%d %s", r.code, r.out)
	}
	if strings.Join(data.Command, " ") != "pwd" || len(data.Actions) != 1 || data.Actions[0].Status != "executed" {
		t.Fatalf("%+v", data)
	}
	_ = a

	// A command that has `--` of its own keeps it.
	r = wsExec(t, "", "--workspace", ws, "--json", "repo", "run", "--", "git", "log", "--oneline", "--", "f.txt")
	decode(t, r, &data)
	if strings.Join(data.Command, " ") != "git log --oneline -- f.txt" || r.code != 0 {
		t.Fatalf("%d %v", r.code, data.Command)
	}
}

func TestRepoRunWithoutDashDashEverythingIsTheCommand(t *testing.T) {
	ws := repoWorkspace(t)
	a := mkRepo(t, filepath.Join(ws, "a"))
	r := wsExec(t, "y\n", "--workspace", ws, "--no-color", "repo", "run", "pwd")
	if r.code != 0 || !strings.Contains(r.out, "      "+a) {
		t.Fatalf("%d %q", r.code, r.out)
	}
	// With no `--`, a path is part of the command, not a target.
	r = wsExec(t, "y\n", "--workspace", ws, "--no-color", "repo", "run", "echo", a)
	if r.code != 0 || !strings.Contains(r.out, "      "+a) {
		t.Fatalf("%d %q", r.code, r.out)
	}
}

func TestRepoRunUsage(t *testing.T) {
	ws := repoWorkspace(t)
	mkRepo(t, filepath.Join(ws, "a"))
	for _, args := range [][]string{{"repo", "run"}, {"repo", "run", "--"}, {"repo", "run", "--dirty"}} {
		r := wsExec(t, "", append([]string{"--workspace", ws}, args...)...)
		if r.code != 1 || strings.TrimSpace(r.err) != "usage: ws repo run [repo…] -- <cmd…>" {
			t.Errorf("%v: %d %q", args, r.code, r.err)
		}
	}
	if r := wsExec(t, "", "--workspace", ws, "repo", "run", filepath.Join(ws, "nope"), "--", "true"); r.code != 1 || !strings.Contains(r.err, "ws: no repo at") {
		t.Fatalf("%d %q", r.code, r.err)
	}
}

func TestRepoRunExitCodes(t *testing.T) {
	ws := repoWorkspace(t)
	a := mkRepo(t, filepath.Join(ws, "a"))
	mkRepo(t, filepath.Join(ws, "b"))
	// Exit 3 when only one repo's command fails; 1 when every one does.
	script := `test "$(basename "$PWD")" = a && echo failing >&2 && exit 4 || exit 0`
	r := wsExec(t, "", "--workspace", ws, "--no-color", "--quiet", "repo", "run", "--", "sh", "-c", script)
	if r.code != 3 {
		t.Fatalf("partial: %d", r.code)
	}
	r = wsExec(t, "y\ny\n", "--workspace", ws, "--no-color", "repo", "run", "--", "sh", "-c", script)
	if r.code != 3 || !strings.Contains(r.out, "Run in "+a+": exit 4") || !strings.Contains(r.out, "      failing") {
		t.Fatalf("a failing command shows its output too:\n%d %s", r.code, r.out)
	}
	r = wsExec(t, "", "--workspace", ws, "--quiet", "repo", "run", "--", "false")
	if r.code != 1 {
		t.Fatalf("all fail: %d", r.code)
	}
	r = wsExec(t, "", "--workspace", ws, "--json", "repo", "run", a, "--", "sh", "-c", "echo hi; exit 7")
	var data struct {
		Results []struct {
			ExitCode int    `json:"exit_code"`
			Output   string `json:"output"`
		}
	}
	decode(t, r, &data)
	if r.code != 1 || data.Results[0].ExitCode != 7 || data.Results[0].Output != "hi" {
		t.Fatalf("%d %+v", r.code, data)
	}
}

func TestRepoRunDryRunRunsNothing(t *testing.T) {
	ws := repoWorkspace(t)
	a := mkRepo(t, filepath.Join(ws, "a"))
	r := wsExec(t, "", "--workspace", ws, "--no-color", "--dry-run", "repo", "run", "--", "touch", "marker")
	if r.code != 0 || !strings.Contains(r.out, "[dry-run] Run in "+a) {
		t.Fatalf("%d %q", r.code, r.out)
	}
	if _, err := os.Stat(filepath.Join(a, "marker")); err == nil {
		t.Fatal("dry run executed the command")
	}
}

func TestRepoRunPromptsPerRepo(t *testing.T) {
	ws := repoWorkspace(t)
	a := mkRepo(t, filepath.Join(ws, "a"))
	b := mkRepo(t, filepath.Join(ws, "b"))
	r := wsExec(t, "n\ny\n", "--workspace", ws, "--no-color", "repo", "run", "--", "touch", "marker")
	if r.code != 0 {
		t.Fatalf("%d", r.code)
	}
	if _, err := os.Stat(filepath.Join(a, "marker")); err == nil {
		t.Fatal("declined repo ran")
	}
	if _, err := os.Stat(filepath.Join(b, "marker")); err != nil {
		t.Fatal("accepted repo did not run")
	}
}

// ── root ───────────────────────────────────────────────────────────

func rootsInConfig(t *testing.T, ws string) []string {
	t.Helper()
	r := wsExec(t, "", "--workspace", ws, "--json", "config", "view")
	var data struct {
		Repo struct {
			Roots []string `json:"roots"`
		} `json:"repo"`
	}
	decode(t, r, &data)
	return data.Repo.Roots
}

func TestRepoRootLs(t *testing.T) {
	ws := repoWorkspace(t)
	r := wsExec(t, "", "--workspace", ws, "repo", "root", "ls")
	if r.code != 0 || strings.TrimSpace(r.out) != ws {
		t.Fatalf("default root is the workspace: %d %q", r.code, r.out)
	}
	r = wsExec(t, "", "--workspace", ws, "--json", "repo", "root", "ls")
	var data struct {
		Roots []string `json:"roots"`
	}
	decode(t, r, &data)
	if jsonCommand(t, r) != "repo.root.ls" || len(data.Roots) != 1 || data.Roots[0] != ws {
		t.Fatalf("%s", r.out)
	}
}

func TestRepoRootAddStoresWorkspaceRelative(t *testing.T) {
	ws := repoWorkspace(t)
	exp := filepath.Join(ws, "experiments")
	if err := os.MkdirAll(exp, 0o755); err != nil {
		t.Fatal(err)
	}
	r := wsExec(t, "y\n", "--workspace", ws, "--no-color", "repo", "root", "add", exp)
	if r.code != 0 || !strings.Contains(r.out, "Add repo root "+exp) {
		t.Fatalf("%d %q %q", r.code, r.out, r.err)
	}
	raw, err := os.ReadFile(filepath.Join(ws, "ws.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Repo struct {
			Roots []string `json:"roots"`
		} `json:"repo"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repo.Roots) != 2 || cfg.Repo.Roots[1] != "experiments" {
		t.Fatalf("stored value must be workspace-relative: %v", cfg.Repo.Roots)
	}
	if r = wsExec(t, "", "--workspace", ws, "repo", "root", "ls"); r.out != ws+"\n"+exp+"\n" {
		t.Fatalf("ws.json order: %q", r.out)
	}

	// Duplicate (compared after resolving), however it is spelled.
	r = wsExec(t, "", "--workspace", ws, "repo", "root", "add", exp+"/../experiments/")
	if r.code != 1 || strings.TrimSpace(r.err) != "already a repo root: "+exp {
		t.Fatalf("%d %q", r.code, r.err)
	}
	if r = wsExec(t, "", "--workspace", ws, "repo", "root", "add", ws); r.code != 1 || !strings.Contains(r.err, "already a repo root: "+ws) {
		t.Fatalf("the default root counts: %d %q", r.code, r.err)
	}
}

func TestRepoRootAddRefusals(t *testing.T) {
	ws := repoWorkspace(t)
	outside := t.TempDir()
	file := filepath.Join(ws, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ arg, msg string }{
		{outside, "outside the workspace"},
		{filepath.Join(ws, "missing"), "no such directory"},
		{file, "not a directory"},
	} {
		r := wsExec(t, "", "--workspace", ws, "repo", "root", "add", c.arg)
		if r.code != 1 || !strings.Contains(r.err, c.msg) {
			t.Errorf("%s: %d %q", c.arg, r.code, r.err)
		}
	}
	for _, args := range [][]string{{}, {"a", "b"}} {
		r := wsExec(t, "", append([]string{"--workspace", ws, "repo", "root", "add"}, args...)...)
		if r.code != 1 || !strings.Contains(r.err, "usage: ws repo root add <dir>") {
			t.Errorf("%v: %d %q", args, r.code, r.err)
		}
	}
	if got := rootsInConfig(t, ws); len(got) != 1 {
		t.Fatalf("a refusal changes nothing: %v", got)
	}
}

func TestRepoRootAddDryRunAndJSON(t *testing.T) {
	ws := repoWorkspace(t)
	sub := filepath.Join(ws, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	r := wsExec(t, "", "--workspace", ws, "--no-color", "--dry-run", "repo", "root", "add", sub)
	if r.code != 0 || !strings.Contains(r.out, "[dry-run] Add repo root "+sub) {
		t.Fatalf("%d %q", r.code, r.out)
	}
	if got := rootsInConfig(t, ws); len(got) != 1 {
		t.Fatalf("a dry run changes nothing: %v", got)
	}
	r = wsExec(t, "", "--workspace", ws, "--json", "repo", "root", "add", sub)
	var data struct {
		Path    string `json:"path"`
		Actions []struct{ ID, Status string }
	}
	decode(t, r, &data)
	if r.code != 0 || jsonCommand(t, r) != "repo.root.add" || data.Path != sub || data.Actions[0].ID != "root-add-"+sub || data.Actions[0].Status != "executed" {
		t.Fatalf("%d %s", r.code, r.out)
	}
}

func TestRepoRootRm(t *testing.T) {
	ws := repoWorkspace(t)
	exp := filepath.Join(ws, "experiments")
	if err := os.MkdirAll(exp, 0o755); err != nil {
		t.Fatal(err)
	}
	if r := wsExec(t, "", "--workspace", ws, "--quiet", "repo", "root", "add", exp); r.code != 0 {
		t.Fatalf("%d %q", r.code, r.err)
	}

	r := wsExec(t, "y\n", "--workspace", ws, "--no-color", "repo", "root", "rm", exp)
	if r.code != 0 || !strings.Contains(r.out, "Remove repo root "+exp) {
		t.Fatalf("%d %q %q", r.code, r.out, r.err)
	}
	if r = wsExec(t, "", "--workspace", ws, "repo", "root", "ls"); strings.TrimSpace(r.out) != ws {
		t.Fatalf("roots after rm: %q", r.out)
	}
	if _, err := os.Stat(exp); err != nil {
		t.Fatal("rm touches no directory on disk")
	}

	// Not a root.
	r = wsExec(t, "", "--workspace", ws, "repo", "root", "rm", exp)
	if r.code != 1 || strings.TrimSpace(r.err) != "not a repo root: "+exp {
		t.Fatalf("%d %q", r.code, r.err)
	}
	// The last root stays: an empty list would widen discovery.
	r = wsExec(t, "", "--workspace", ws, "repo", "root", "rm", ws)
	if r.code != 1 || strings.TrimSpace(r.err) != "cannot remove the last repo root; add another first" {
		t.Fatalf("%d %q", r.code, r.err)
	}
	if got := rootsInConfig(t, ws); len(got) != 1 {
		t.Fatalf("%v", got)
	}
}

func TestRepoRootRmDeletesEveryMatchingEntryAndOldForms(t *testing.T) {
	ws := repoWorkspace(t)
	exp := filepath.Join(ws, "experiments")
	if err := os.MkdirAll(exp, 0o755); err != nil {
		t.Fatal(err)
	}
	// Older entries may be absolute: both spellings match.
	cfgPath := filepath.Join(ws, "ws.json")
	raw, _ := os.ReadFile(cfgPath)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["repo"].(map[string]any)["roots"] = []string{".", "experiments", exp}
	out, _ := json.Marshal(m)
	if err := os.WriteFile(cfgPath, out, 0o644); err != nil {
		t.Fatal(err)
	}
	r := wsExec(t, "", "--workspace", ws, "--json", "repo", "root", "rm", exp)
	if r.code != 0 || jsonCommand(t, r) != "repo.root.rm" {
		t.Fatalf("%d %s %s", r.code, r.out, r.err)
	}
	if got := rootsInConfig(t, ws); len(got) != 1 || got[0] != ws {
		t.Fatalf("every matching entry goes: %v", got)
	}
}

func TestRepoRootChangesDiscovery(t *testing.T) {
	ws := repoWorkspace(t)
	inRoot := mkRepo(t, filepath.Join(ws, "experiments", "blog"))
	other := mkRepo(t, filepath.Join(ws, "notes", "brain"))
	// Narrow discovery to experiments: add it, drop the workspace.
	if r := wsExec(t, "", "--workspace", ws, "--quiet", "repo", "root", "add", filepath.Join(ws, "experiments")); r.code != 0 {
		t.Fatalf("%d %q", r.code, r.err)
	}
	if r := wsExec(t, "", "--workspace", ws, "--quiet", "repo", "root", "rm", ws); r.code != 0 {
		t.Fatalf("%d %q", r.code, r.err)
	}
	r := wsExec(t, "", "--workspace", ws, "--no-color", "repo", "ls")
	if !strings.Contains(r.out, inRoot) || strings.Contains(r.out, other) {
		t.Fatalf("only the root is walked:\n%s", r.out)
	}
}

// ── discovery ──────────────────────────────────────────────────────

func TestRepoDiscoveryNestedAndExcluded(t *testing.T) {
	ws := repoWorkspace(t)
	outer := mkRepo(t, filepath.Join(ws, "outer"))
	inner := mkRepo(t, filepath.Join(outer, "vendor", "inner"))
	skipped := mkRepo(t, filepath.Join(ws, "skip", "me"))

	cfgPath := filepath.Join(ws, "ws.json")
	raw, _ := os.ReadFile(cfgPath)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["repo"].(map[string]any)["exclude_dirs"] = []string{"skip"}
	out, _ := json.Marshal(m)
	if err := os.WriteFile(cfgPath, out, 0o644); err != nil {
		t.Fatal(err)
	}

	r := wsExec(t, "", "--workspace", ws, "--no-color", "repo", "ls")
	if !strings.Contains(r.out, outer+" ") || !strings.Contains(r.out, inner+" ") {
		t.Fatalf("nested repos are listed separately:\n%s", r.out)
	}
	if strings.Contains(r.out, skipped) {
		t.Fatalf("exclude_dirs skips everything under the directory:\n%s", r.out)
	}
}

func TestRepoOutsideWorkspaceNeverListed(t *testing.T) {
	ws := repoWorkspace(t)
	outside := mkRepo(t, filepath.Join(t.TempDir(), "outside"))
	cfgPath := filepath.Join(ws, "ws.json")
	raw, _ := os.ReadFile(cfgPath)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	// An old absolute root outside the workspace finds nothing.
	m["repo"].(map[string]any)["roots"] = []string{filepath.Dir(outside)}
	out, _ := json.Marshal(m)
	if err := os.WriteFile(cfgPath, out, 0o644); err != nil {
		t.Fatal(err)
	}
	r := wsExec(t, "", "--workspace", ws, "repo", "ls")
	if strings.Contains(r.out, outside) {
		t.Fatalf("a repo outside the workspace is never listed:\n%s", r.out)
	}
	if r := wsExec(t, "", "--workspace", ws, "repo", "ls", outside); r.code != 1 {
		t.Fatalf("and is no target: %d", r.code)
	}
}

// ── json envelope ──────────────────────────────────────────────────

func TestRepoJSONCommandNames(t *testing.T) {
	p := newPair(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"repo", "ls"}, "repo.ls"},
		{[]string{"repo", "show", p.a}, "repo.show"},
		{[]string{"repo", "fetch"}, "repo.fetch"},
		{[]string{"repo", "pull"}, "repo.pull"},
		{[]string{"repo", "push"}, "repo.push"},
		{[]string{"repo", "save"}, "repo.save"},
		{[]string{"repo", "run", "--", "true"}, "repo.run"},
		{[]string{"repo", "root", "ls"}, "repo.root.ls"},
	} {
		r := wsExec(t, "", append([]string{"--workspace", p.ws, "--json"}, c.args...)...)
		if r.code != 0 || jsonCommand(t, r) != c.want {
			t.Errorf("%v: %d %q", c.args, r.code, jsonCommand(t, r))
		}
	}
}
