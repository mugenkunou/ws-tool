package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

func TestRunOneNeverWaitsForAnEditor(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.a, "n.txt"), "n\n")
	gitOut(t, f.a, "add", "n.txt")
	// No -m: the editor accepts the empty message, so git aborts at once.
	res := RunOne(filepath.Dir(f.a), Repository{Path: f.a}, []string{"git", "commit"})
	if res.Success || !strings.Contains(res.Output, "empty commit message") {
		t.Fatalf("%+v", res)
	}
}

func TestRunAttachedPassesTheTerminalThrough(t *testing.T) {
	dir := t.TempDir()
	var out, errOut strings.Builder
	res := RunAttached(dir, Repository{Path: dir}, []string{"sh", "-c", "read x; echo got $x; echo e >&2; exit 2"},
		strings.NewReader("hi\n"), &out, &errOut)
	if res.Success || res.ExitCode != 2 || res.Output != "" {
		t.Fatalf("%+v", res)
	}
	if out.String() != "got hi\n" || errOut.String() != "e\n" {
		t.Fatalf("stdin, stdout and stderr pass through: %q %q", out.String(), errOut.String())
	}
	t.Setenv("GIT_TERMINAL_PROMPT", "")
	ok := RunAttached(dir, Repository{Path: dir}, []string{"sh", "-c", "echo \"[$GIT_TERMINAL_PROMPT]\""}, nil, &out, &errOut)
	if !ok.Success || !strings.HasSuffix(out.String(), "[]\n") {
		t.Fatalf("the environment is the user's own, so git may ask: %q", out.String())
	}
}

func TestCompletionWords(t *testing.T) {
	f := newFixture(t)
	gitOut(t, f.a, "branch", "feature")
	gitOut(t, f.a, "tag", "v1")
	got := strings.Join(CompletionWords([]string{f.a, f.a}), " ")
	if got != "feature main origin origin/main v1" {
		t.Fatalf("branches, remote branches, tags and remotes, sorted, once each: %q", got)
	}
}

func TestGitNeverPrompts(t *testing.T) {
	if !strings.Contains(strings.Join(gitEnv(), "\n"), "GIT_TERMINAL_PROMPT=0") {
		t.Fatal("gitEnv must disable terminal prompts")
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
