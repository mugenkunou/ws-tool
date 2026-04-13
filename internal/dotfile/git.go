package dotfile

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitSyncOptions configures an auto-commit/push operation on the dotfiles-git repo.
type GitSyncOptions struct {
	WorkspacePath string
	RepoPath      string // absolute path to ws/dotfiles-git
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

// GitSync performs auto-commit and optional auto-push on the dotfiles-git repo.
// It copies the current dotfiles/ content into the git repo, commits changes,
// and pushes if configured. Errors are non-fatal — the operation returns a result
// rather than failing the parent command.
func GitSync(opts GitSyncOptions) GitSyncResult {
	if !opts.AutoCommit {
		return GitSyncResult{Message: "auto-commit disabled"}
	}

	// Ensure the repo is initialized.
	if err := ensureGitRepo(opts.RepoPath, opts.RemoteURL, opts.Branch); err != nil {
		return GitSyncResult{Error: "git repo setup: " + err.Error()}
	}

	// Sync dotfiles/ content into the git repo working tree.
	dotfilesDir := filepath.Join(opts.WorkspacePath, "ws", "dotfiles")
	if _, err := os.Stat(dotfilesDir); err != nil {
		return GitSyncResult{Error: "dotfiles dir not found"}
	}
	destDir := filepath.Join(opts.RepoPath, "dotfiles")
	if err := syncDir(dotfilesDir, destDir); err != nil {
		return GitSyncResult{Error: "sync dotfiles: " + err.Error()}
	}

	// Also copy manifest.json for portable restore.
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

// ensureGitRepo initializes the git repo if needed, sets remote and branch.
func ensureGitRepo(repoPath, remoteURL, branch string) error {
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

// syncDir copies all files from src to dest, creating dest if needed.
// It uses a simple recursive copy: create dirs, copy files.
func syncDir(src, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return nil
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files
		}
		return os.WriteFile(target, data, 0o644)
	})
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
