package context

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCreateCreatesContextAndUpdatesExclude(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	runGit(t, project, "init")

	res, err := Create(CreateOptions{ProjectPath: project, Task: "auth-redesign"})
	if err != nil {
		t.Fatalf("context create failed: %v", err)
	}
	if !res.Created {
		t.Fatal("expected created=true")
	}
	if !res.GitRepo {
		t.Fatal("expected git repo detection")
	}
	if !res.GitExcludeUpdated {
		t.Fatal("expected exclude file update")
	}

	ctxPath := filepath.Join(project, ".ws-context", "auth-redesign")
	if _, err := os.Stat(ctxPath); err != nil {
		t.Fatalf("context dir missing: %v", err)
	}
}

func TestCreateDryRun(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	res, err := Create(CreateOptions{ProjectPath: project, Task: "dry", DryRun: true})
	if err != nil {
		t.Fatalf("dry-run create failed: %v", err)
	}
	if !res.Created {
		t.Fatal("expected dry-run to report context creation")
	}
	if _, err := os.Stat(filepath.Join(project, ".ws-context", "dry")); err == nil {
		t.Fatal("dry-run should not create directory")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
