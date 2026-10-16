package dotfile

import (
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// AutoSync runs the configured auto-commit/push after a dotfile change.
// It does nothing (enabled=false) unless a dotfile git remote is configured.
func AutoSync(workspacePath, configPath, _ /* manifestPath compat */, commitMessage string) (res GitSyncResult, enabled bool) {
	cfg, err := wsconfig.Load(configPath)
	if err != nil || cfg.Dotfile.Git.RemoteURL == "" {
		return GitSyncResult{}, false
	}
	repoPath := RepoPath(workspacePath)
	return GitSync(GitSyncOptions{
		WorkspacePath: workspacePath,
		RepoPath:      repoPath,
		RemoteURL:     cfg.Dotfile.Git.RemoteURL,
		Branch:        cfg.Dotfile.Git.Branch,
		AutoCommit:    cfg.Dotfile.Git.AutoCommit,
		AutoPush:      cfg.Dotfile.Git.AutoPush,
		CommitMessage: commitMessage,
	}), true
}
