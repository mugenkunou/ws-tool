package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoLsAndScan(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	repoPath := filepath.Join(workspace, "Experiments", "r1")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	runGitLocal(t, repoPath, "init")
	runGitLocal(t, repoPath, "config", "user.email", "ws@example.com")
	runGitLocal(t, repoPath, "config", "user.name", "ws")
	if err := os.WriteFile(filepath.Join(repoPath, "x.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	runGitLocal(t, repoPath, "add", "x.txt")
	runGitLocal(t, repoPath, "commit", "-m", "init")

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "repo", "ls"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("repo ls failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Experiments/r1") {
		t.Fatalf("unexpected repo ls output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "repo", "scan"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("repo scan expected clean exit 0, got=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	if err := os.WriteFile(filepath.Join(repoPath, "x.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatalf("rewrite failed: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "repo", "scan"}, strings.NewReader("y\n"), &out, &errOut); code != 2 {
		t.Fatalf("repo scan expected violation exit 2, got=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "--dry-run", "repo", "run", "--", "git", "status", "--short"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("repo run dry-run expected exit 0, got=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "Would run command") {
		t.Fatalf("unexpected repo run dry-run output: %s", out.String())
	}
}

func runGitLocal(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func TestRepoSync(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	// Create a bare remote.
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGitLocal(t, t.TempDir(), "init", "--bare", remote)

	// Create a repo with an upstream.
	repoPath := filepath.Join(workspace, "Experiments", "r1")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	runGitLocal(t, repoPath, "init")
	runGitLocal(t, repoPath, "config", "user.email", "ws@example.com")
	runGitLocal(t, repoPath, "config", "user.name", "ws")
	if err := os.WriteFile(filepath.Join(repoPath, "x.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	runGitLocal(t, repoPath, "add", "x.txt")
	runGitLocal(t, repoPath, "commit", "-m", "init")
	runGitLocal(t, repoPath, "remote", "add", "origin", remote)
	runGitLocal(t, repoPath, "push", "-u", "origin", "master")

	// Make a local commit to be ahead.
	if err := os.WriteFile(filepath.Join(repoPath, "y.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	runGitLocal(t, repoPath, "add", "y.txt")
	runGitLocal(t, repoPath, "commit", "-m", "local change")

	// Dry-run sync.
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "--dry-run", "repo", "sync"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("repo sync dry-run expected 0, got %d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "dry-run") {
		t.Fatalf("expected dry-run in output, got: %s", out.String())
	}

	// Real sync (should push the ahead commit).
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "repo", "sync"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("repo sync expected 0, got %d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
}

func TestRepoExcludesWsDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	// Create a git repo inside ws/ (like dotfiles-git).
	wsRepo := filepath.Join(workspace, "ws", "dotfiles-git")
	if err := os.MkdirAll(wsRepo, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	runGitLocal(t, wsRepo, "init")

	// Create a user repo.
	userRepo := filepath.Join(workspace, "projects", "app")
	if err := os.MkdirAll(userRepo, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	runGitLocal(t, userRepo, "init")
	runGitLocal(t, userRepo, "config", "user.email", "ws@test.com")
	runGitLocal(t, userRepo, "config", "user.name", "ws")
	if err := os.WriteFile(filepath.Join(userRepo, "f.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	runGitLocal(t, userRepo, "add", "f.txt")
	runGitLocal(t, userRepo, "commit", "-m", "init")

	// ws repo ls should only show the user repo, not ws/dotfiles-git.
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "repo", "ls"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("repo ls expected 0, got %d stderr=%s", code, errOut.String())
	}
	output := out.String()
	if strings.Contains(output, "dotfiles-git") {
		t.Fatalf("ws/ repo should be excluded from repo ls, got: %s", output)
	}
	if !strings.Contains(output, "projects/app") {
		t.Fatalf("expected projects/app in repo ls output, got: %s", output)
	}
}

func TestRepoScanNoFetch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	repoPath := filepath.Join(workspace, "r1")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	runGitLocal(t, repoPath, "init")
	runGitLocal(t, repoPath, "config", "user.email", "ws@example.com")
	runGitLocal(t, repoPath, "config", "user.name", "ws")
	if err := os.WriteFile(filepath.Join(repoPath, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	runGitLocal(t, repoPath, "add", "f.txt")
	runGitLocal(t, repoPath, "commit", "-m", "init")

	// --no-fetch should work without network.
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "repo", "scan", "--no-fetch"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("repo scan --no-fetch expected 0, got %d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "r1") {
		t.Fatalf("expected r1 in scan output, got: %s", out.String())
	}
}
