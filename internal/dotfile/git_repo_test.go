package dotfile

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/backup"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

// bareRemote makes a bare repository to push to. A path url is an unknown
// provider, so the visibility check warns and the push goes ahead.
func bareRemote(t *testing.T) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "--initial-branch=main", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v %s", err, out)
	}
	return remote
}

func enableBackup(t *testing.T, e env, remote string, autoCommit, autoPush bool) wsconfig.DotfileGit {
	t.Helper()
	cfg, err := wsconfig.Load(e.config)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Dotfile.Git = wsconfig.DotfileGit{RemoteURL: remote, Branch: "main", AutoCommit: autoCommit, AutoPush: autoPush}
	if err := wsconfig.Save(e.config, cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.Dotfile.Git
}

// The dotfile repo is configs/dotfiles/ itself, so an auto-sync commits the
// dotfiles that ws add moved there.
func TestAutoSyncCommitsInConfigsDotfiles(t *testing.T) {
	needGit(t)
	e := setup(t)
	enableBackup(t, e, bareRemote(t), true, false)

	res, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: e.write(t, ".vimrc", "set nu\n")})
	if err != nil {
		t.Fatal(err)
	}

	sync, enabled := AutoSync(e.workspace, e.config, "add vimrc")
	if !enabled || sync.Error != "" || !sync.Committed {
		t.Fatalf("expected a commit, got enabled=%v result=%+v", enabled, sync)
	}
	if sync.Pushed {
		t.Fatal("auto_push is off")
	}

	repo := RepoPath(e.workspace)
	out, err := exec.Command("git", "-C", repo, "ls-files").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	name := strings.TrimPrefix(res.Link.Original, "configs/dotfiles/")
	if !strings.Contains(string(out), name) || !strings.Contains(string(out), "ws.json") {
		t.Fatalf("expected %s and ws.json tracked in %s, got:\n%s", name, repo, out)
	}
}

func TestAutoSyncDoesNothingWithoutARemote(t *testing.T) {
	e := setup(t)
	if _, enabled := AutoSync(e.workspace, e.config, "x"); enabled {
		t.Fatal("backup is enabled exactly while remote_url is set")
	}
}

func TestAutoSyncPushesWhenAutoPushIsOn(t *testing.T) {
	needGit(t)
	e := setup(t)
	remote := bareRemote(t)
	enableBackup(t, e, remote, true, true)
	e.add(t, e.write(t, ".vimrc", "set nu\n"))

	res, _ := AutoSync(e.workspace, e.config, "dotfile add: vimrc")
	if res.Error != "" || !res.Committed || !res.Pushed {
		t.Fatalf("result = %+v", res)
	}
	if out, err := exec.Command("git", "-C", remote, "log", "--oneline").Output(); err != nil || !strings.Contains(string(out), "dotfile add: vimrc") {
		t.Fatalf("the remote must have the commit: %v %s", err, out)
	}
}

// TestBackupCopiesTheResolvedConfig is plan 0002 bug B9: GitSync copied
// <workspace>/ws.json, so --config and $WS_CONFIG were ignored.
func TestBackupCopiesTheResolvedConfig(t *testing.T) {
	needGit(t)
	e := setup(t)
	custom := filepath.Join(t.TempDir(), "custom.json")
	cfg, _ := wsconfig.Load(e.config)
	cfg.Editor = "from-the-custom-config"
	cfg.Dotfile.Git = wsconfig.DotfileGit{RemoteURL: bareRemote(t), Branch: "main", AutoCommit: true}
	if err := wsconfig.Save(custom, cfg); err != nil {
		t.Fatal(err)
	}

	committed, err := CommitBackup(BackupOptions{WorkspacePath: e.workspace, ConfigPath: custom, Git: cfg.Dotfile.Git, Message: "m"})
	if err != nil || !committed {
		t.Fatalf("CommitBackup = %v, %v", committed, err)
	}
	copied, err := os.ReadFile(filepath.Join(RepoPath(e.workspace), "ws.json"))
	if err != nil || !strings.Contains(string(copied), "from-the-custom-config") {
		t.Fatalf("the backup must carry the config that was used, got %q %v", copied, err)
	}
}

func TestPushBackupCommitsAndPushesIgnoringAutoSettings(t *testing.T) {
	needGit(t)
	e := setup(t)
	remote := bareRemote(t)
	git := enableBackup(t, e, remote, false, false) // auto_commit and auto_push are off
	e.add(t, e.write(t, ".vimrc", "set nu\n"))

	res, err := PushBackup(BackupOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Git: git, Message: "ws dotfile backup push"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Committed || !res.Pushed || res.RemoteURL != remote || res.Branch != "main" {
		t.Fatalf("result = %+v", res)
	}
	if st := backup.Inspect(RepoPath(e.workspace), "main", 5); st.Ahead != 0 || len(st.Uncommitted) != 0 {
		t.Fatalf("state after a push: %+v", st)
	}

	// Nothing new: no commit, the push is still fine.
	res, err = PushBackup(BackupOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Git: git})
	if err != nil || res.Committed {
		t.Fatalf("a second push has nothing to commit: %+v %v", res, err)
	}
}

func TestPushBackupNeedsABackup(t *testing.T) {
	e := setup(t)
	_, err := PushBackup(BackupOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Git: wsconfig.DotfileGit{}})
	if err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("got %v", err)
	}
}
