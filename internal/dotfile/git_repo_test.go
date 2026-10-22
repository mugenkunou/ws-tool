package dotfile

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// The dotfile repo is configs/dotfiles/ itself, so an auto-sync commits the
// dotfiles that ws add moved there.
func TestAutoSyncCommitsInConfigsDotfiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	workspaceRoot, configPath := setupWorkspace(t)
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	cfg.Dotfile.Git = wsconfig.DotfileGit{
		RemoteURL:  filepath.Join(t.TempDir(), "remote.git"),
		Branch:     "main",
		AutoCommit: true,
		AutoPush:   false,
	}
	if err := wsconfig.Save(configPath, cfg); err != nil {
		t.Fatalf("save config failed: %v", err)
	}

	systemFile := filepath.Join(t.TempDir(), ".vimrc")
	if err := os.WriteFile(systemFile, []byte("set nu\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	res, err := Add(AddOptions{WorkspacePath: workspaceRoot, ConfigPath: configPath, SystemPath: systemFile})
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	sync, enabled := AutoSync(workspaceRoot, configPath, "add vimrc")
	if !enabled || sync.Error != "" || !sync.Committed {
		t.Fatalf("expected a commit, got enabled=%v result=%+v", enabled, sync)
	}

	repo := RepoPath(workspaceRoot)
	if repo != filepath.Join(workspaceRoot, "configs", "dotfiles") {
		t.Fatalf("expected repo at configs/dotfiles, got %s", repo)
	}
	out, err := exec.Command("git", "-C", repo, "ls-files").Output()
	if err != nil {
		t.Fatalf("git ls-files failed: %v", err)
	}
	name := strings.TrimPrefix(res.Link.Original, "configs/dotfiles/")
	if !strings.Contains(string(out), name) {
		t.Fatalf("expected %s tracked in %s, got:\n%s", name, repo, out)
	}
}
