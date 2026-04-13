package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverAndScan(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	ws := filepath.Join(t.TempDir(), "Workspace")
	repoPath := filepath.Join(ws, "Experiments", "demo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	runGitCmd(t, repoPath, "init")
	runGitCmd(t, repoPath, "config", "user.email", "ws@example.com")
	runGitCmd(t, repoPath, "config", "user.name", "ws")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("demo\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	runGitCmd(t, repoPath, "add", "README.md")
	runGitCmd(t, repoPath, "commit", "-m", "init")

	repos, err := Discover(ws, []string{"."}, nil)
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected one repo, got %d", len(repos))
	}
	if repos[0].Path != "Experiments/demo" {
		t.Fatalf("unexpected repo path: %s", repos[0].Path)
	}

	statuses := Scan(ws, repos)
	if len(statuses) != 1 {
		t.Fatalf("expected one status, got %d", len(statuses))
	}
	if statuses[0].Error != "" {
		t.Fatalf("unexpected status error: %s", statuses[0].Error)
	}
	if statuses[0].Dirty {
		t.Fatal("repo should be clean after commit")
	}

	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatalf("rewrite file failed: %v", err)
	}
	statuses = Scan(ws, repos)
	if !statuses[0].Dirty {
		t.Fatal("expected dirty=true after modification")
	}
}

func TestFetchAllNoRemotePartial(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	ws := filepath.Join(t.TempDir(), "Workspace")
	repoPath := filepath.Join(ws, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	runGitCmd(t, repoPath, "init")
	runGitCmd(t, repoPath, "remote", "add", "origin", "https://127.0.0.1:9/non-existent/repo.git")

	repos := []Repository{{Path: "repo"}}
	results := FetchAll(ws, repos)
	if len(results) != 1 {
		t.Fatalf("expected one fetch result, got %d", len(results))
	}
	if results[0].Success {
		t.Fatal("expected fetch to fail with unreachable remote")
	}
	if strings.TrimSpace(results[0].Error) == "" {
		t.Fatal("expected fetch error message")
	}
}

func TestRunAll(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	ws := filepath.Join(t.TempDir(), "Workspace")
	repoPath := filepath.Join(ws, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	runGitCmd(t, repoPath, "init")

	results := RunAll(ws, []Repository{{Path: "repo"}}, []string{"git", "status", "--short"})
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if !results[0].Success {
		t.Fatalf("expected run success, got error: %s", results[0].Error)
	}
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func TestDiscoverExcludeDirs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	ws := filepath.Join(t.TempDir(), "Workspace")

	// Create two repos: one at ws/ (should be excluded) and one outside it.
	wsRepo := filepath.Join(ws, "ws", "dotfiles-git")
	userRepo := filepath.Join(ws, "Experiments", "blog")
	for _, p := range []string{wsRepo, userRepo} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", p, err)
		}
		runGitCmd(t, p, "init")
	}

	// Without exclusion — both should be found.
	all, err := Discover(ws, []string{"."}, nil)
	if err != nil {
		t.Fatalf("discover (no exclude) failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 repos without exclusion, got %d", len(all))
	}

	// With ws/ excluded — only the user repo should appear.
	filtered, err := Discover(ws, []string{"."}, []string{"ws"})
	if err != nil {
		t.Fatalf("discover (exclude ws) failed: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected 1 repo with ws excluded, got %d", len(filtered))
	}
	if filtered[0].Path != "Experiments/blog" {
		t.Fatalf("expected Experiments/blog, got %s", filtered[0].Path)
	}
}

func TestReconcile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	ws := filepath.Join(t.TempDir(), "Workspace")
	repoPath := filepath.Join(ws, "notes", "brain")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	runGitCmd(t, repoPath, "init")

	tracked := []TrackedRepo{
		{Path: "notes/brain", Branch: "main", Remote: "origin"},
		{Path: "data/missing", Branch: "main", Remote: "origin"},
	}

	summary, repos, err := Reconcile(ws, []string{"."}, nil, tracked)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if summary.Found != 1 {
		t.Fatalf("expected 1 found, got %d", summary.Found)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
	if repos[0].Path != "notes/brain" {
		t.Fatalf("expected notes/brain, got %s", repos[0].Path)
	}
}

func TestSyncOne(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	ws := filepath.Join(t.TempDir(), "Workspace")

	// Create a bare remote.
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGitCmd(t, t.TempDir(), "init", "--bare", remote)

	// Create a repo with upstream.
	repoPath := filepath.Join(ws, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	runGitCmd(t, repoPath, "init")
	runGitCmd(t, repoPath, "config", "user.email", "ws@test.com")
	runGitCmd(t, repoPath, "config", "user.name", "ws")
	if err := os.WriteFile(filepath.Join(repoPath, "f.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	runGitCmd(t, repoPath, "add", "f.txt")
	runGitCmd(t, repoPath, "commit", "-m", "init")
	runGitCmd(t, repoPath, "remote", "add", "origin", remote)
	runGitCmd(t, repoPath, "push", "-u", "origin", "master")

	// Make a local commit to be ahead.
	if err := os.WriteFile(filepath.Join(repoPath, "g.txt"), []byte("world\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	runGitCmd(t, repoPath, "add", "g.txt")
	runGitCmd(t, repoPath, "commit", "-m", "local")

	// Scan to get status.
	statuses := Scan(ws, []Repository{{Path: "repo"}})
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}

	sp := PlanSync(statuses[0])
	if sp.Strategy != SyncPush {
		t.Fatalf("expected push strategy, got %s", sp.Strategy)
	}

	result := SyncOne(ws, sp, SyncOptions{})
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
}

func TestPlanSyncSkips(t *testing.T) {
	// Detached HEAD
	sp := PlanSync(RepoStatus{Path: "a", Branch: "HEAD", Detached: true})
	if sp.Strategy != SyncSkip || sp.Warning == "" {
		t.Fatalf("expected skip with warning for detached, got strategy=%s warning=%q", sp.Strategy, sp.Warning)
	}

	// Error status
	sp = PlanSync(RepoStatus{Path: "b", Error: "broken"})
	if sp.Strategy != SyncSkip {
		t.Fatalf("expected skip for error status, got %s", sp.Strategy)
	}

	// Up to date with upstream (0 ahead, 0 behind)
	sp = PlanSync(RepoStatus{Path: "c", Branch: "main", HasUpstream: true})
	if sp.Strategy != SyncSkip {
		t.Fatalf("expected skip for up-to-date, got %s", sp.Strategy)
	}
	if sp.Warning != "" {
		t.Fatalf("expected no warning for up-to-date, got %q", sp.Warning)
	}

	// No upstream configured
	sp = PlanSync(RepoStatus{Path: "d", Branch: "main", HasUpstream: false})
	if sp.Strategy != SyncSkip || sp.Warning == "" {
		t.Fatalf("expected skip with warning for no upstream, got strategy=%s warning=%q", sp.Strategy, sp.Warning)
	}
}

func TestFilter(t *testing.T) {
	statuses := []RepoStatus{
		{Path: "a", Dirty: true, Ahead: 0, Behind: 0},
		{Path: "b", Dirty: false, Ahead: 2, Behind: 0},
		{Path: "c", Dirty: false, Ahead: 0, Behind: 1},
		{Path: "d", Dirty: true, Ahead: 1, Behind: 0, Detached: true},
	}

	dirty := Filter(statuses, FilterOptions{Dirty: true})
	if len(dirty) != 2 {
		t.Fatalf("dirty filter: expected 2, got %d", len(dirty))
	}

	ahead := Filter(statuses, FilterOptions{Ahead: true})
	if len(ahead) != 2 {
		t.Fatalf("ahead filter: expected 2, got %d", len(ahead))
	}

	detached := Filter(statuses, FilterOptions{Detached: true})
	if len(detached) != 1 {
		t.Fatalf("detached filter: expected 1, got %d", len(detached))
	}

	noFilter := Filter(statuses, FilterOptions{})
	if len(noFilter) != 4 {
		t.Fatalf("no filter: expected 4, got %d", len(noFilter))
	}
}
