package repo_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/repo"
)

// makeGitRepo creates a bare git repository in a temp dir and returns
// the absolute path. The caller may configure it further with git commands.
func makeGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init")
	return dir
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v failed: %v\n%s", args, err, string(out))
	}
}

// gitAvailable skips the test if git is not in PATH.
func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func TestDoctorCleanRepo(t *testing.T) {
	gitAvailable(t)
	repoDir := makeGitRepo(t)
	run(t, repoDir, "git", "config", "user.name", "Test User")
	run(t, repoDir, "git", "config", "user.email", "test@example.com")

	// Commit something so the branch exists.
	if err := os.WriteFile(filepath.Join(repoDir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "init")

	// Only run checks that don't require an upstream or external fetch.
	findings := repo.Doctor(repoDir, []repo.Repository{{Path: "."}}, repo.DoctorOptions{
		Checks: []string{"identity", "dirty"},
	})
	for _, f := range findings {
		if f.Severity >= repo.SeverityWarn {
			t.Errorf("unexpected warning for clean repo: check=%s detail=%s", f.Check, f.Detail)
		}
	}
}

func TestDoctorMissingEmailLocallyGlobalPresent(t *testing.T) {
	gitAvailable(t)
	// Set a global email via GIT_CONFIG_GLOBAL so it doesn't touch the real ~/.gitconfig.
	globalCfgDir := t.TempDir()
	globalCfg := filepath.Join(globalCfgDir, "gitconfig")
	if err := os.WriteFile(globalCfg, []byte("[user]\n\temail = global@example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalCfg)

	repoDir := makeGitRepo(t)
	run(t, repoDir, "git", "config", "user.name", "Test User")
	// No local user.email — but global is set.

	findings := repo.Doctor(repoDir, []repo.Repository{{Path: "."}}, repo.DoctorOptions{
		Checks: []string{"identity"},
	})
	found := false
	for _, f := range findings {
		if f.Check == "identity" && f.Severity == repo.SeverityInfo {
			found = true
		}
		if f.Check == "identity" && f.Severity == repo.SeverityWarn {
			t.Errorf("should be info not warn when global email is set: detail=%s", f.Detail)
		}
	}
	if !found {
		t.Error("expected SeverityInfo finding for missing local email when global is set")
	}
}

func TestDoctorMissingIdentityNoGlobal(t *testing.T) {
	gitAvailable(t)
	// Point GIT_CONFIG_GLOBAL to an empty file so global identity is absent.
	globalCfgDir := t.TempDir()
	globalCfg := filepath.Join(globalCfgDir, "gitconfig")
	if err := os.WriteFile(globalCfg, []byte("[core]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalCfg)

	repoDir := makeGitRepo(t)
	// No local user.email and no global user.email.

	findings := repo.Doctor(repoDir, []repo.Repository{{Path: "."}}, repo.DoctorOptions{
		Checks: []string{"identity"},
	})
	warnCount := 0
	for _, f := range findings {
		if f.Check == "identity" && f.Severity == repo.SeverityWarn {
			warnCount++
		}
	}
	if warnCount == 0 {
		t.Error("expected SeverityWarn when both local and global identity are missing")
	}
}

func TestDoctorNoUpstream(t *testing.T) {
	gitAvailable(t)
	repoDir := makeGitRepo(t)
	run(t, repoDir, "git", "config", "user.name", "Test User")
	run(t, repoDir, "git", "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(repoDir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "init")
	// No remote — no upstream tracking.

	findings := repo.Doctor(repoDir, []repo.Repository{{Path: "."}}, repo.DoctorOptions{
		Checks: []string{"upstream"},
	})
	found := false
	for _, f := range findings {
		if f.Check == "upstream" && f.Severity == repo.SeverityWarn {
			found = true
		}
	}
	if !found {
		t.Error("expected SeverityWarn for branch with no upstream")
	}
}

func TestDoctorDirtyTree(t *testing.T) {
	gitAvailable(t)
	repoDir := makeGitRepo(t)
	run(t, repoDir, "git", "config", "user.name", "Test User")
	run(t, repoDir, "git", "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(repoDir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "init")

	// Make a dirty change.
	if err := os.WriteFile(filepath.Join(repoDir, "f.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}

	findings := repo.Doctor(repoDir, []repo.Repository{{Path: "."}}, repo.DoctorOptions{
		Checks: []string{"dirty"},
	})
	found := false
	for _, f := range findings {
		if f.Check == "dirty" && f.Severity == repo.SeverityWarn {
			found = true
		}
	}
	if !found {
		t.Error("expected SeverityWarn for dirty tree")
	}
}
