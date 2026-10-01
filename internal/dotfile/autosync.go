package dotfile

import (
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/manifest"
)

// AutoSync runs the configured auto-commit/push after a dotfile change. It
// does nothing (enabled=false) unless dotfile git is enabled in config.
func AutoSync(workspacePath, configPath, manifestPath, commitMessage string) (res GitSyncResult, enabled bool) {
	cfg, err := config.Load(configPath)
	if err != nil || !cfg.Dotfile.Git.Enabled {
		return GitSyncResult{}, false
	}
	mani, err := manifest.Load(manifestPath)
	if err != nil {
		return GitSyncResult{}, false
	}
	return GitSync(GitSyncOptions{
		WorkspacePath: workspacePath,
		RepoPath:      filepath.Join(workspacePath, "ws", "dotfiles"),
		RemoteURL:     mani.DotfileGit.RemoteURL,
		Branch:        mani.DotfileGit.Branch,
		AutoCommit:    mani.DotfileGit.AutoCommit,
		AutoPush:      mani.DotfileGit.AutoPush,
		CommitMessage: commitMessage,
	}), true
}
