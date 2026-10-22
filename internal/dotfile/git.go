package dotfile

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/backup"
	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// RepoPath returns the dotfile backup repository, which is configs/dotfiles/
// itself: the originals are the work tree.
func RepoPath(workspacePath string) string {
	return filepath.Join(workspacePath, "configs", "dotfiles")
}

// BackupBranch is the branch ws commits to and pushes.
func BackupBranch(git wsconfig.DotfileGit) string {
	if git.Branch != "" {
		return git.Branch
	}
	return backup.DefaultBranch
}

// EnsureGitRepo initializes the backup repository if needed, and sets the
// branch and, when remoteURL is not empty, origin.
func EnsureGitRepo(repoPath, remoteURL, branch string) error {
	if err := backup.Init(repoPath, branch); err != nil {
		return err
	}
	if remoteURL != "" && backup.RemoteURL(repoPath) != remoteURL {
		return backup.SetRemote(repoPath, remoteURL)
	}
	return nil
}

// BackupOptions configures CommitBackup and PushBackup.
type BackupOptions struct {
	WorkspacePath string
	ConfigPath    string // the resolved ws.json, which travels with the backup
	Git           wsconfig.DotfileGit
	Message       string
}

// CommitBackup commits every pending change in the backup repository, together
// with a fresh copy of ws.json (at configs/dotfiles/ws.json), so the remote
// alone holds the entry list. It reports whether a commit was made.
func CommitBackup(opts BackupOptions) (committed bool, err error) {
	repoPath := RepoPath(opts.WorkspacePath)
	if err := EnsureGitRepo(repoPath, opts.Git.RemoteURL, BackupBranch(opts.Git)); err != nil {
		return false, fmt.Errorf("git repo setup: %w", err)
	}
	// The resolved config, not <workspace>/ws.json: --config and $WS_CONFIG apply.
	if data, err := os.ReadFile(opts.ConfigPath); err == nil {
		_ = os.WriteFile(filepath.Join(repoPath, "ws.json"), data, 0o644)
	}
	msg := opts.Message
	if msg == "" {
		msg = "auto-sync dotfiles"
	}
	return backup.CommitAll(repoPath, msg)
}

// PushResult describes the outcome of PushBackup.
type PushResult struct {
	Committed  bool                  `json:"committed"`
	Pushed     bool                  `json:"pushed"`
	RemoteURL  string                `json:"remote_url"`
	Branch     string                `json:"branch"`
	Visibility repo.VisibilityResult `json:"visibility"`
}

// PushBackup commits pending changes and pushes the branch. The private-remote
// check runs first: a public repository, or an authenticated lookup that finds
// none, is refused and nothing is pushed (the commit stays local). A check that
// cannot be determined is reported in the result, and the push goes ahead. It
// ignores auto_commit and auto_push, because it is an explicit request.
func PushBackup(opts BackupOptions) (PushResult, error) {
	repoPath := RepoPath(opts.WorkspacePath)
	res := PushResult{RemoteURL: opts.Git.RemoteURL, Branch: BackupBranch(opts.Git)}
	if opts.Git.RemoteURL == "" {
		return res, fmt.Errorf("backup is not enabled; run ws dotfile backup enable --remote <url>")
	}

	committed, err := CommitBackup(opts)
	res.Committed = committed
	if err != nil {
		return res, err
	}

	res.Visibility = backup.CheckRemote(opts.Git.RemoteURL, opts.Git.PassEntry)
	if err := res.Visibility.Refusal(); err != nil {
		return res, err
	}
	if err := backup.Push(repoPath, res.Branch); err != nil {
		return res, fmt.Errorf("push failed (commit kept locally): %w", err)
	}
	res.Pushed = true
	return res, nil
}

// GitSyncResult describes the outcome of the auto-commit that follows add,
// link --to and unlink.
type GitSyncResult struct {
	Committed bool   `json:"committed"`
	Pushed    bool   `json:"pushed"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

// GitSyncOptions configures GitSync.
type GitSyncOptions struct {
	WorkspacePath string
	ConfigPath    string
	Git           wsconfig.DotfileGit
	CommitMessage string
}

// GitSync performs the auto-commit and the optional auto-push after a dotfile
// change (the settings auto_commit and auto_push). Errors are not fatal: they
// come back in the result, so the command that changed the dotfile still
// succeeds. A push is checked like backup push is, so a public remote is never
// pushed to.
func GitSync(opts GitSyncOptions) GitSyncResult {
	if !opts.Git.AutoCommit {
		return GitSyncResult{Message: "auto-commit disabled"}
	}
	committed, err := CommitBackup(BackupOptions{
		WorkspacePath: opts.WorkspacePath,
		ConfigPath:    opts.ConfigPath,
		Git:           opts.Git,
		Message:       opts.CommitMessage,
	})
	if err != nil {
		return GitSyncResult{Error: err.Error()}
	}
	result := GitSyncResult{Committed: committed}
	if !committed {
		result.Message = "nothing to commit"
	}
	if !opts.Git.AutoPush {
		return result
	}

	vis := backup.CheckRemote(opts.Git.RemoteURL, opts.Git.PassEntry)
	if err := vis.Refusal(); err != nil {
		result.Error = "push refused (commit kept locally): " + err.Error()
		return result
	}
	if err := backup.Push(RepoPath(opts.WorkspacePath), BackupBranch(opts.Git)); err != nil {
		result.Error = "push failed (commit kept locally): " + err.Error()
		return result
	}
	result.Pushed = true
	return result
}

// GitIsInitialized, GitRemoteURL and friends are the repository queries the
// TUI's git view still uses; they read the shared backup package.
func GitIsInitialized(repoPath string) bool { return backup.IsRepo(repoPath) }

// GitHasRemote reports whether the repository has an origin.
func GitHasRemote(repoPath string) bool { return backup.RemoteURL(repoPath) != "" }

// GitRemoteURL returns origin's url, or "".
func GitRemoteURL(repoPath string) string { return backup.RemoteURL(repoPath) }

// GitBranch returns the checked-out branch.
func GitBranch(repoPath string) string { return backup.CurrentBranch(repoPath) }

// GitAheadBehind returns ahead/behind relative to origin/<branch>.
func GitAheadBehind(repoPath, branch string) (ahead, behind int) {
	return backup.AheadBehind(repoPath, branch)
}

// GitAddRemote adds or updates origin.
func GitAddRemote(repoPath, url string) error { return backup.SetRemote(repoPath, url) }

// GitPush pushes the branch to origin.
func GitPush(repoPath, branch string) error { return backup.Push(repoPath, branch) }

// GitStatus returns the porcelain output.
func GitStatus(repoPath string) (string, error) {
	lines, err := backup.Porcelain(repoPath)
	if err != nil {
		return "", err
	}
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out, nil
}

// GitLastCommit returns the last commit's date and message.
func GitLastCommit(repoPath string) (string, error) { return backup.LastCommit(repoPath), nil }

// GitLog returns the last count commits, one per line.
func GitLog(repoPath string, count int) (string, error) {
	commits, err := backup.Log(repoPath, count)
	if err != nil {
		return "", err
	}
	out := ""
	for _, c := range commits {
		out += c.Hash + " " + c.Subject + "\n"
	}
	return out, nil
}
