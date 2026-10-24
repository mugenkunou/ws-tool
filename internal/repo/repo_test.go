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
	if repos[0].Path != repoPath {
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
	if filtered[0].Path != userRepo {
		t.Fatalf("expected %s, got %s", userRepo, filtered[0].Path)
	}

	// Absolute exclude values (config convention allows them) match too.
	absFiltered, err := Discover(ws, []string{"."}, []string{filepath.Join(ws, "ws")})
	if err != nil {
		t.Fatalf("discover (absolute exclude) failed: %v", err)
	}
	if len(absFiltered) != 1 || absFiltered[0].Path != userRepo {
		t.Fatalf("expected only %s with absolute exclude, got %+v", userRepo, absFiltered)
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

func TestFilterPathMatchesWholeComponents(t *testing.T) {
	statuses := []RepoStatus{
		{Path: "/w/Data/bruno"},
		{Path: "/w/Dat/x"},
		{Path: "/w/Data"},
	}
	got := Filter(statuses, FilterOptions{Path: "/w/Data"})
	if len(got) != 2 || got[0].Path != "/w/Data/bruno" || got[1].Path != "/w/Data" {
		t.Fatalf("expected /w/Data and /w/Data/bruno, got %+v", got)
	}
	if got := Filter(statuses, FilterOptions{Path: "/w/Da"}); len(got) != 0 {
		t.Fatalf("partial component must not match, got %+v", got)
	}
}
