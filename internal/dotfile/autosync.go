package dotfile

import (
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// AutoSync runs the configured auto-commit/push after a dotfile change.
// It does nothing (enabled=false) unless a backup remote is configured, because
// backup is enabled exactly while dotfile.git.remote_url is set.
func AutoSync(workspacePath, configPath, commitMessage string) (res GitSyncResult, enabled bool) {
	cfg, err := wsconfig.Load(configPath)
	if err != nil || cfg.Dotfile.Git.RemoteURL == "" {
		return GitSyncResult{}, false
	}
	return GitSync(GitSyncOptions{
		WorkspacePath: workspacePath,
		ConfigPath:    configPath,
		Git:           cfg.Dotfile.Git,
		CommitMessage: commitMessage,
	}), true
}
