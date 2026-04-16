package dotfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitSyncOptions configures an auto-commit/push operation on the dotfiles repo.
type GitSyncOptions struct {
	WorkspacePath string
	RepoPath      string // absolute path to ws/dotfiles (the git repo itself)
	RemoteURL     string
	Branch        string
	AutoCommit    bool
	AutoPush      bool
	CommitMessage string
}

// GitSyncResult describes the outcome of a dotfile git sync operation.
type GitSyncResult struct {
	Committed bool   `json:"committed"`
	Pushed    bool   `json:"pushed"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

// GitSync performs auto-commit and optional auto-push on the dotfiles repo.
// The git repo lives directly inside ws/dotfiles/ (like pass uses ~/.password-store/).
// Errors are non-fatal — the operation returns a result
// rather than failing the parent command.
func GitSync(opts GitSyncOptions) GitSyncResult {
	if !opts.AutoCommit {
		return GitSyncResult{Message: "auto-commit disabled"}
	}

	// Ensure the repo is initialized.
	if err := EnsureGitRepo(opts.RepoPath, opts.RemoteURL, opts.Branch); err != nil {
		return GitSyncResult{Error: "git repo setup: " + err.Error()}
	}

	// Copy manifest.json into ws/dotfiles/ for portable restore.
	manifestSrc := filepath.Join(opts.WorkspacePath, "ws", "manifest.json")
	manifestDest := filepath.Join(opts.RepoPath, "manifest.json")
	if _, err := os.Stat(manifestSrc); err == nil {
		data, err := os.ReadFile(manifestSrc)
		if err == nil {
			_ = os.WriteFile(manifestDest, data, 0o644)
		}
	}

	// Stage all changes.
	if _, err := runDotfileGit(opts.RepoPath, "add", "-A"); err != nil {
		return GitSyncResult{Error: "git add: " + err.Error()}
	}

	// Check if there are changes to commit.
	porcelain, _ := runDotfileGit(opts.RepoPath, "status", "--porcelain")
	if strings.TrimSpace(porcelain) == "" {
		return GitSyncResult{Message: "nothing to commit"}
	}

	// Commit.
	msg := opts.CommitMessage
	if msg == "" {
		msg = "auto-sync dotfiles"
	}
	if _, err := runDotfileGit(opts.RepoPath, "commit", "-m", msg); err != nil {
		return GitSyncResult{Error: "git commit: " + err.Error()}
	}
	result := GitSyncResult{Committed: true}

	// Push if configured.
	if opts.AutoPush {
		if _, err := runDotfileGit(opts.RepoPath, "push", "-u", "origin", opts.Branch); err != nil {
			result.Error = "push failed (commit preserved locally): " + err.Error()
			return result
		}
		result.Pushed = true
	}

	return result
}

// EnsureGitRepo initializes the git repo if needed, sets remote and branch.
func EnsureGitRepo(repoPath, remoteURL, branch string) error {
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		return err
	}

	gitDir := filepath.Join(repoPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		if _, err := runDotfileGit(repoPath, "init"); err != nil {
			return err
		}
		if _, err := runDotfileGit(repoPath, "config", "user.email", "ws@localhost"); err != nil {
			return err
		}
		if _, err := runDotfileGit(repoPath, "config", "user.name", "ws"); err != nil {
			return err
		}
	}

	// Ensure branch.
	if branch != "" {
		currentBranch, _ := runDotfileGit(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
		if strings.TrimSpace(currentBranch) != branch {
			// Try to checkout or create the branch.
			if _, err := runDotfileGit(repoPath, "checkout", "-B", branch); err != nil {
				return err
			}
		}
	}

	// Ensure remote.
	if remoteURL != "" {
		existing, _ := runDotfileGit(repoPath, "remote", "get-url", "origin")
		if strings.TrimSpace(existing) != remoteURL {
			if strings.TrimSpace(existing) != "" {
				_, _ = runDotfileGit(repoPath, "remote", "set-url", "origin", remoteURL)
			} else {
				_, _ = runDotfileGit(repoPath, "remote", "add", "origin", remoteURL)
			}
		}
	}

	return nil
}

func runDotfileGit(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errText := strings.TrimSpace(stderr.String())
		if errText == "" {
			errText = err.Error()
		}
		return "", errors.New(errText)
	}
	return stdout.String(), nil
}

// GitIsInitialized returns true if ws/dotfiles/ has a .git directory.
func GitIsInitialized(repoPath string) bool {
	info, err := os.Stat(filepath.Join(repoPath, ".git"))
	return err == nil && info.IsDir()
}

// GitHasRemote returns true if the repo has a remote named "origin".
func GitHasRemote(repoPath string) bool {
	out, err := runDotfileGit(repoPath, "remote", "get-url", "origin")
	return err == nil && strings.TrimSpace(out) != ""
}

// GitRemoteURL returns the origin remote URL, or empty string.
func GitRemoteURL(repoPath string) string {
	out, err := runDotfileGit(repoPath, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// GitBranch returns the current branch name.
func GitBranch(repoPath string) string {
	out, err := runDotfileGit(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// GitPush pushes the current branch to origin.
func GitPush(repoPath, branch string) error {
	if branch == "" {
		branch = GitBranch(repoPath)
	}
	if branch == "" {
		branch = "main"
	}
	_, err := runDotfileGit(repoPath, "push", "-u", "origin", branch)
	return err
}

// GitLog returns the last N commit log entries as a string.
func GitLog(repoPath string, count int) (string, error) {
	countStr := fmt.Sprintf("-%d", count)
	return runDotfileGit(repoPath, "log", countStr, "--oneline", "--no-decorate")
}

// GitStatus returns the porcelain status output.
func GitStatus(repoPath string) (string, error) {
	return runDotfileGit(repoPath, "status", "--porcelain")
}

// GitLastCommit returns the last commit's date and message.
func GitLastCommit(repoPath string) (string, error) {
	return runDotfileGit(repoPath, "log", "-1", "--format=%ci  %s")
}

// GitAheadBehind returns ahead/behind counts relative to origin/branch.
func GitAheadBehind(repoPath, branch string) (ahead, behind int) {
	if branch == "" {
		branch = "main"
	}
	ref := "origin/" + branch
	out, err := runDotfileGit(repoPath, "rev-list", "--left-right", "--count", "HEAD..."+ref)
	if err != nil {
		return 0, 0
	}
	parts := strings.Fields(strings.TrimSpace(out))
	if len(parts) == 2 {
		fmt.Sscanf(parts[0], "%d", &ahead)
		fmt.Sscanf(parts[1], "%d", &behind)
	}
	return
}

// GitAddRemote adds or updates the origin remote.
func GitAddRemote(repoPath, url string) error {
	existing, _ := runDotfileGit(repoPath, "remote", "get-url", "origin")
	if strings.TrimSpace(existing) != "" {
		_, err := runDotfileGit(repoPath, "remote", "set-url", "origin", url)
		return err
	}
	_, err := runDotfileGit(repoPath, "remote", "add", "origin", url)
	return err
}
