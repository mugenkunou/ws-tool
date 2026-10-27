package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitAvail(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

// gitOut runs git in dir and returns its trimmed output.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitFile(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	write(t, filepath.Join(dir, name), content)
	gitOut(t, dir, "add", "-A")
	gitOut(t, dir, "commit", "-m", msg)
}

// fixture is a bare remote with two clones on branch main: a (the repo under
// test, tracking origin/main) and other (a second machine that pushes).
type fixture struct {
	remote, a, other string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	gitAvail(t)
	root := t.TempDir()
	f := fixture{remote: filepath.Join(root, "remote.git"), a: filepath.Join(root, "ws", "a"), other: filepath.Join(root, "other")}
	gitOut(t, root, "init", "--bare", "-b", "main", f.remote)
	for _, dir := range []string{f.a, f.other} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gitOut(t, f.a, "init", "-b", "main")
	gitOut(t, f.a, "config", "user.name", "A")
	gitOut(t, f.a, "config", "user.email", "a@example.com")
	gitOut(t, f.a, "remote", "add", "origin", f.remote)
	commitFile(t, f.a, "base.txt", "base\n", "base")
	gitOut(t, f.a, "push", "-u", "origin", "main")

	gitOut(t, root, "clone", f.remote, f.other)
	gitOut(t, f.other, "config", "user.name", "Other")
	gitOut(t, f.other, "config", "user.email", "o@example.com")
	return f
}

func (f fixture) status(t *testing.T) RepoStatus {
	t.Helper()
	return Scan(filepath.Dir(f.a), []Repository{{Path: f.a}})[0]
}

func (f fixture) fetch(t *testing.T) {
	t.Helper()
	gitOut(t, f.a, "fetch", "--all", "--prune")
}

func never(t *testing.T) Chooser {
	return func(path string, local, remote int) (Strategy, string) {
		t.Helper()
		t.Fatalf("no choice expected for %s", path)
		return StrategySkip, ""
	}
}

func fixed(s Strategy, calls *[][2]int) Chooser {
	return func(path string, local, remote int) (Strategy, string) {
		if calls != nil {
			*calls = append(*calls, [2]int{local, remote})
		}
		return s, "diverged: skipped"
	}
}

func TestPlanPull(t *testing.T) {
	cases := []struct {
		name   string
		st     RepoStatus
		reason string // skip reason, "" when planned or nothing to do
		want   Strategy
	}{
		{"error", RepoStatus{Error: "boom"}, "error: boom", ""},
		{"detached", RepoStatus{Detached: true}, "detached HEAD", ""},
		{"no upstream", RepoStatus{}, "no upstream (ws repo configure sets one)", ""},
		{"up to date", RepoStatus{HasUpstream: true}, "", ""},
		{"ahead only", RepoStatus{HasUpstream: true, Ahead: 2}, "", ""},
		{"behind only", RepoStatus{HasUpstream: true, Behind: 3}, "", StrategyFF},
		{"diverged", RepoStatus{HasUpstream: true, Ahead: 1, Behind: 1}, "", StrategyRebase},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			item, skip := PlanPull(c.st, fixed(StrategyRebase, nil))
			if c.reason != "" {
				if skip == nil || skip.Reason != c.reason {
					t.Fatalf("want skip %q, got %+v", c.reason, skip)
				}
				return
			}
			if skip != nil {
				t.Fatalf("unexpected skip %+v", skip)
			}
			if c.want == "" {
				if item != nil {
					t.Fatalf("want nothing to do, got %+v", item)
				}
				return
			}
			if item == nil || item.Strategy != c.want {
				t.Fatalf("want strategy %s, got %+v", c.want, item)
			}
		})
	}
}

func TestPlanPullDivergedSkipAndNoChoiceForFastForward(t *testing.T) {
	_, skip := PlanPull(RepoStatus{Path: "/r", HasUpstream: true, Ahead: 2, Behind: 1}, fixed(StrategySkip, nil))
	if skip == nil || skip.Path != "/r" || skip.Reason != "diverged: skipped" {
		t.Fatalf("skip: %+v", skip)
	}
	// A fast-forward needs no choice.
	if item, _ := PlanPull(RepoStatus{HasUpstream: true, Behind: 1}, never(t)); item == nil || item.Strategy != StrategyFF {
		t.Fatalf("fast-forward: %+v", item)
	}
}

func TestPullItemDescribe(t *testing.T) {
	if got := (PullItem{Behind: 3, Strategy: StrategyFF}).Describe("/w/a"); got != "Pull /w/a (3 behind, fast-forward)" {
		t.Fatal(got)
	}
	if got := (PullItem{Behind: 1, Strategy: StrategyRebase}).Describe("/w/a"); got != "Pull /w/a (diverged, rebase)" {
		t.Fatal(got)
	}
}

func TestPushSkip(t *testing.T) {
	cases := []struct {
		st   RepoStatus
		want string
	}{
		{RepoStatus{Error: "x"}, "error: x"},
		{RepoStatus{Detached: true}, "detached HEAD"},
		{RepoStatus{}, "no upstream (ws repo configure sets one)"},
		{RepoStatus{HasUpstream: true, Behind: 1}, "behind or diverged: pull first (or ws repo save)"},
		{RepoStatus{HasUpstream: true, Ahead: 1, Behind: 1}, "behind or diverged: pull first (or ws repo save)"},
		{RepoStatus{HasUpstream: true}, "nothing to push"},
		{RepoStatus{HasUpstream: true, Ahead: 2}, ""},
	}
	for _, c := range cases {
		if got := PushSkip(c.st); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.st, got, c.want)
		}
	}
}

func TestPlanSave(t *testing.T) {
	up := RepoStatus{Path: "/r", HasUpstream: true}

	if item, skip := PlanSave(up, 0, never(t)); item != nil || skip != nil {
		t.Fatalf("clean and in sync: nothing to do, got %+v %+v", item, skip)
	}
	if _, skip := PlanSave(RepoStatus{Path: "/r"}, 2, never(t)); skip == nil || !strings.Contains(skip.Reason, "no upstream") {
		t.Fatalf("no upstream: %+v", skip)
	}
	if _, skip := PlanSave(RepoStatus{Path: "/r", Detached: true}, 2, never(t)); skip == nil || skip.Reason != "detached HEAD" {
		t.Fatalf("detached: %+v", skip)
	}

	item, _ := PlanSave(up, 3, never(t))
	if item == nil || item.Changes != 3 || item.Pull || !item.Push {
		t.Fatalf("dirty only: commit and push, got %+v", item)
	}
	if got := item.Describe("/w/a"); got != "Save /w/a: commit 3 changes, push" {
		t.Fatal(got)
	}

	ahead := up
	ahead.Ahead = 2
	if item, _ := PlanSave(ahead, 0, never(t)); item == nil || item.Changes != 0 || item.Pull || !item.Push {
		t.Fatalf("ahead only: push, got %+v", item)
	}

	behind := up
	behind.Behind = 1
	item, _ = PlanSave(behind, 0, never(t))
	if item == nil || !item.Pull || item.Push || item.Strategy != StrategyFF {
		t.Fatalf("behind only: fast-forward, no push: %+v", item)
	}
	if got := item.Describe("/w/a"); got != "Save /w/a: pull (fast-forward)" {
		t.Fatal(got)
	}

	// Dirty and behind: the commit makes the repo diverged, so a choice is
	// needed (1 local commit to be, 1 remote).
	var calls [][2]int
	item, _ = PlanSave(behind, 1, fixed(StrategyMerge, &calls))
	if len(calls) != 1 || calls[0] != [2]int{1, 1} {
		t.Fatalf("chooser calls: %v", calls)
	}
	if item == nil || item.Strategy != StrategyMerge || !item.Pull || !item.Push || item.Changes != 1 {
		t.Fatalf("dirty+behind: %+v", item)
	}
	if got := item.Describe("/w/a"); got != "Save /w/a: commit 1 change, pull (merge), push" {
		t.Fatal(got)
	}

	// Diverged and skipped.
	div := up
	div.Ahead, div.Behind = 2, 1
	if item, skip := PlanSave(div, 0, fixed(StrategySkip, nil)); item != nil || skip == nil || skip.Reason != "diverged: skipped" {
		t.Fatalf("diverged skip: %+v %+v", item, skip)
	}
}

func TestDefaultSaveMessage(t *testing.T) {
	at := time.Date(2026, 10, 22, 14, 5, 0, 0, time.Local)
	if got := DefaultSaveMessage(at, "laptop"); got != "ws save 2026-10-22 14:05 (laptop)" {
		t.Fatal(got)
	}
}

func TestPullFastForward(t *testing.T) {
	f := newFixture(t)
	commitFile(t, f.other, "new.txt", "n\n", "remote work")
	gitOut(t, f.other, "push")
	f.fetch(t)

	st := f.status(t)
	if st.Behind != 1 {
		t.Fatalf("behind %d", st.Behind)
	}
	if err := Pull(f.a, StrategyFF); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.a, "new.txt")); err != nil {
		t.Fatal("fast-forward should bring new.txt")
	}
}

// diverge makes a diverge with a conflict on base.txt.
func (f fixture) diverge(t *testing.T) {
	t.Helper()
	commitFile(t, f.other, "base.txt", "remote edit\n", "remote edit")
	gitOut(t, f.other, "push")
	commitFile(t, f.a, "base.txt", "local edit\n", "local edit")
	f.fetch(t)
}

func TestPullRebaseConflictIsAborted(t *testing.T) {
	f := newFixture(t)
	f.diverge(t)
	head := gitOut(t, f.a, "rev-parse", "HEAD")

	if err := Pull(f.a, StrategyRebase); err == nil {
		t.Fatal("a conflicting rebase must fail")
	}
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(f.a, ".git", d)); err == nil {
			t.Fatalf("repo left half-way: .git/%s exists", d)
		}
	}
	if got := gitOut(t, f.a, "rev-parse", "HEAD"); got != head {
		t.Fatalf("HEAD moved: %s -> %s", head, got)
	}
}

func TestPullMergeConflictIsAborted(t *testing.T) {
	f := newFixture(t)
	f.diverge(t)

	if err := Pull(f.a, StrategyMerge); err == nil {
		t.Fatal("a conflicting merge must fail")
	}
	if _, err := os.Stat(filepath.Join(f.a, ".git", "MERGE_HEAD")); err == nil {
		t.Fatal("repo left half-way: MERGE_HEAD exists")
	}
	if got := gitOut(t, f.a, "status", "--porcelain"); got != "" {
		t.Fatalf("working tree not clean after abort: %q", got)
	}
}

func TestPullRebaseAndMergeSucceedWithoutConflict(t *testing.T) {
	for _, s := range []Strategy{StrategyRebase, StrategyMerge} {
		t.Run(string(s), func(t *testing.T) {
			f := newFixture(t)
			commitFile(t, f.other, "r.txt", "r\n", "remote")
			gitOut(t, f.other, "push")
			commitFile(t, f.a, "l.txt", "l\n", "local")
			f.fetch(t)
			if err := Pull(f.a, s); err != nil {
				t.Fatal(err)
			}
			st := f.status(t)
			if st.Behind != 0 || st.Ahead < 1 {
				t.Fatalf("after pull: ahead %d behind %d", st.Ahead, st.Behind)
			}
		})
	}
}

func TestPushSendsCommits(t *testing.T) {
	f := newFixture(t)
	commitFile(t, f.a, "p.txt", "p\n", "local")
	if err := Push(f.a); err != nil {
		t.Fatal(err)
	}
	if got := gitOut(t, f.remote, "log", "--format=%s", "-1"); got != "local" {
		t.Fatalf("remote head: %q", got)
	}
}

func TestSaveCommitsUntrackedAndPushes(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.a, "untracked.txt"), "u\n")
	write(t, filepath.Join(f.a, "base.txt"), "changed\n")
	if n := UncommittedCount(f.a); n != 2 {
		t.Fatalf("uncommitted %d, want 2", n)
	}
	st := f.status(t)
	item, _ := PlanSave(st, UncommittedCount(f.a), never(t))
	if err := Save(*item, "my message"); err != nil {
		t.Fatal(err)
	}
	if got := gitOut(t, f.remote, "log", "--format=%s", "-1"); got != "my message" {
		t.Fatalf("remote head %q", got)
	}
	if got := gitOut(t, f.a, "status", "--porcelain"); got != "" {
		t.Fatalf("working tree not clean: %q", got)
	}
}

func TestSaveKeepsTheCommitWhenPullFails(t *testing.T) {
	f := newFixture(t)
	commitFile(t, f.other, "r.txt", "r\n", "remote")
	gitOut(t, f.other, "push")
	f.fetch(t)
	write(t, filepath.Join(f.a, "mine.txt"), "mine\n")

	// A fast-forward is impossible once the commit exists: the pull step fails.
	err := Save(SaveItem{Path: f.a, Changes: 1, Pull: true, Strategy: StrategyFF, Push: true}, "keep me")
	if err == nil || !strings.HasPrefix(err.Error(), "pull failed:") {
		t.Fatalf("want a pull failure, got %v", err)
	}
	if got := gitOut(t, f.a, "log", "--format=%s", "-1"); got != "keep me" {
		t.Fatalf("the commit must stay, HEAD is %q", got)
	}
}

func TestSavePushFailureNamesThePush(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.a, "mine.txt"), "mine\n")
	if err := os.Rename(f.remote, f.remote+".gone"); err != nil {
		t.Fatal(err)
	}
	err := Save(SaveItem{Path: f.a, Changes: 1, Push: true}, "m")
	if err == nil || !strings.HasPrefix(err.Error(), "push failed:") {
		t.Fatalf("want a push failure, got %v", err)
	}
	if got := gitOut(t, f.a, "log", "--format=%s", "-1"); got != "m" {
		t.Fatalf("the commit must stay, HEAD is %q", got)
	}
}

func TestRunOneCapturesBothStreamsAndExitCode(t *testing.T) {
	dir := t.TempDir()
	res := RunOne(dir, Repository{Path: dir}, []string{"sh", "-c", "echo out; echo err >&2; exit 3"})
	if res.Success || res.ExitCode != 3 || res.Error != "exit 3" {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(res.Output, "out") || !strings.Contains(res.Output, "err") {
		t.Fatalf("output must hold stdout and stderr: %q", res.Output)
	}
	if res := RunOne(dir, Repository{Path: dir}, []string{"/no/such/command"}); res.Success || res.ExitCode != -1 {
		t.Fatalf("a command that cannot start: %+v", res)
	}
	ok := RunOne(dir, Repository{Path: dir}, []string{"sh", "-c", "pwd; echo $GIT_TERMINAL_PROMPT"})
	if !ok.Success || !strings.HasSuffix(ok.Output, "\n0") && !strings.HasSuffix(ok.Output, "0") {
		t.Fatalf("GIT_TERMINAL_PROMPT must be 0: %+v", ok)
	}
	if !strings.HasPrefix(ok.Output, dir) {
		t.Fatalf("working directory should be the repo: %q", ok.Output)
	}
}

func TestGitNeverPrompts(t *testing.T) {
	if !strings.Contains(strings.Join(gitEnv(), "\n"), "GIT_TERMINAL_PROMPT=0") {
		t.Fatal("gitEnv must disable terminal prompts")
	}
}

func TestLastFetch(t *testing.T) {
	f := newFixture(t)
	if _, ok := LastFetch(f.a); ok {
		// the push -u in the fixture does not write FETCH_HEAD
		t.Log("FETCH_HEAD exists already")
	}
	f.fetch(t)
	at, ok := LastFetch(f.a)
	if !ok || time.Since(at) > time.Minute {
		t.Fatalf("LastFetch: %v %v", at, ok)
	}
}

func TestDiscoverNestedOutsideAndExcludes(t *testing.T) {
	gitAvail(t)
	root := t.TempDir()
	ws := filepath.Join(root, "Workspace")
	outside := filepath.Join(root, "elsewhere", "repo")
	for _, p := range []string{
		filepath.Join(ws, "a"),
		filepath.Join(ws, "a", "inner"), // nested in a
		filepath.Join(ws, "node_modules", "x"),
		filepath.Join(ws, "b", "node_modules", "y"),
		outside,
	} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		gitOut(t, p, "init")
	}

	repos, err := Discover(ws, []string{"."}, []string{"node_modules"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range repos {
		got = append(got, strings.TrimPrefix(r.Path, ws+"/"))
	}
	// Nested repos are listed; only <ws>/node_modules is excluded (a path, not a name).
	want := []string{"a", "a/inner", "b/node_modules/y"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}

	// A root outside the workspace finds nothing, even when it is absolute.
	repos, err = Discover(ws, []string{outside, filepath.Dir(outside)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 0 {
		t.Fatalf("outside the workspace is never listed: %v", repos)
	}

	// Roots are workspace-relative.
	repos, err = Discover(ws, []string{"b"}, nil)
	if err != nil || len(repos) != 1 || repos[0].Path != filepath.Join(ws, "b", "node_modules", "y") {
		t.Fatalf("relative root: %v %v", repos, err)
	}
}
