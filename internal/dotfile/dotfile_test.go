package dotfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

func setupWorkspace(t *testing.T) (workspaceRoot, configPath string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Workspace")
	configsDir := filepath.Join(root, "configs", "dotfiles")
	if err := os.MkdirAll(configsDir, 0o755); err != nil {
		t.Fatalf("mkdir configs/dotfiles/ failed: %v", err)
	}
	secretsDir := filepath.Join(root, "secrets", "dotfiles")
	if err := os.MkdirAll(secretsDir, 0o755); err != nil {
		t.Fatalf("mkdir secrets/dotfiles/ failed: %v", err)
	}
	cfgPath := wsconfig.ConfigPath(root)
	if err := wsconfig.Save(cfgPath, wsconfig.Default()); err != nil {
		t.Fatalf("save wsconfig failed: %v", err)
	}
	return root, cfgPath
}

func TestAddListRemoveRoundTrip(t *testing.T) {
	workspaceRoot, configPath := setupWorkspace(t)

	systemFile := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(systemFile, []byte("export TEST=1\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	addRes, err := Add(AddOptions{
		WorkspacePath: workspaceRoot,
		ConfigPath:    configPath,
		SystemPath:    systemFile,
	})
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if len(addRes.Messages) == 0 {
		t.Fatal("expected add messages")
	}

	links, err := List(configPath)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 managed dotfile, got %d", len(links))
	}

	entry, err := os.Lstat(systemFile)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if entry.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected system file to be symlink")
	}

	removeRes, err := Remove(RemoveOptions{
		WorkspacePath: workspaceRoot,
		ConfigPath:    configPath,
		SystemPath:    systemFile,
	})
	if err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if len(removeRes.Messages) == 0 {
		t.Fatal("expected remove messages")
	}

	entry, err = os.Lstat(systemFile)
	if err != nil {
		t.Fatalf("stat after remove failed: %v", err)
	}
	if entry.Mode()&os.ModeSymlink != 0 {
		t.Fatal("expected restored system file to be regular file")
	}

	links, err = List(configPath)
	if err != nil {
		t.Fatalf("list after remove failed: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("expected no managed dotfiles, got %d", len(links))
	}
}

func TestScanAndFixBrokenSymlink(t *testing.T) {
	workspaceRoot, configPath := setupWorkspace(t)
	systemFile := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(systemFile, []byte("alias ll='ls -la'\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	if _, err := Add(AddOptions{WorkspacePath: workspaceRoot, ConfigPath: configPath, SystemPath: systemFile}); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	links, err := List(configPath)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected one link, got %d", len(links))
	}

	target := filepath.Join(workspaceRoot, filepath.FromSlash(DotfilePath(links[0].Original, links[0].Class)))
	if err := os.Remove(target); err != nil {
		t.Fatalf("remove target failed: %v", err)
	}

	issues, err := Scan(ScanOptions{WorkspacePath: workspaceRoot, ConfigPath: configPath})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected one issue, got %d", len(issues))
	}
	if issues[0].Status != StatusBroken {
		t.Fatalf("expected BROKEN status, got %s", issues[0].Status)
	}

	// Fix cannot fix this — workspace target is missing, so it should fail the dotfile.
	fixRes, err := Fix(FixOptions{WorkspacePath: workspaceRoot, ConfigPath: configPath})
	if err != nil {
		t.Fatalf("fix failed: %v", err)
	}
	if len(fixRes.Failed) != 1 {
		t.Fatalf("expected 1 failed dotfile, got %d", len(fixRes.Failed))
	}
}

func TestFixCreatesSymlink(t *testing.T) {
	workspaceRoot, configPath := setupWorkspace(t)
	systemFile := filepath.Join(t.TempDir(), ".profile")
	if err := os.WriteFile(systemFile, []byte("export PATH=$PATH\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	// Add the dotfile normally.
	if _, err := Add(AddOptions{WorkspacePath: workspaceRoot, ConfigPath: configPath, SystemPath: systemFile}); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	// Remove the system symlink to simulate a fresh machine.
	if err := os.Remove(systemFile); err != nil {
		t.Fatalf("remove symlink failed: %v", err)
	}

	// Fix should recreate the symlink.
	fixRes, err := Fix(FixOptions{WorkspacePath: workspaceRoot, ConfigPath: configPath})
	if err != nil {
		t.Fatalf("fix failed: %v", err)
	}
	if len(fixRes.Fixed) != 1 {
		t.Fatalf("expected 1 fixed dotfile, got %d (unchanged=%d, failed=%d)", len(fixRes.Fixed), len(fixRes.Unchanged), len(fixRes.Failed))
	}

	// Verify symlink exists.
	entry, err := os.Lstat(systemFile)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if entry.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected system path to be a symlink after fix")
	}
}

func TestFixUnchanged(t *testing.T) {
	workspaceRoot, configPath := setupWorkspace(t)
	systemFile := filepath.Join(t.TempDir(), ".gitconfig")
	if err := os.WriteFile(systemFile, []byte("[user]\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	if _, err := Add(AddOptions{WorkspacePath: workspaceRoot, ConfigPath: configPath, SystemPath: systemFile}); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	// Fix when everything is already correct.
	fixRes, err := Fix(FixOptions{WorkspacePath: workspaceRoot, ConfigPath: configPath})
	if err != nil {
		t.Fatalf("fix failed: %v", err)
	}
	if len(fixRes.Unchanged) != 1 {
		t.Fatalf("expected 1 unchanged, got %d (fixed=%d, failed=%d)", len(fixRes.Unchanged), len(fixRes.Fixed), len(fixRes.Failed))
	}
}

func TestAddDryRun(t *testing.T) {
	workspaceRoot, configPath := setupWorkspace(t)
	systemFile := filepath.Join(t.TempDir(), ".tmux.conf")
	if err := os.WriteFile(systemFile, []byte("set -g mouse on\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	res, err := Add(AddOptions{
		WorkspacePath: workspaceRoot,
		ConfigPath:    configPath,
		SystemPath:    systemFile,
		DryRun:        true,
	})
	if err != nil {
		t.Fatalf("dry-run add failed: %v", err)
	}
	if len(res.Messages) == 0 {
		t.Fatal("expected dry-run messages")
	}

	// ws.json should still exist
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("workspace config should still exist: %v", err)
	}

	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		t.Fatalf("config load failed: %v", err)
	}
	if len(cfg.Links) != 0 {
		t.Fatalf("dry-run should not modify config, got %d entries", len(cfg.Links))
	}
}

// TestRemovePrunesEmptyDirs verifies that removing a file-level dotfile whose
// workspace path lives inside a subdirectory (e.g. configs/dotfiles/ssh/config)
// also removes the now-empty parent directory hierarchy under the dotfiles root.
func TestRemovePrunesEmptyDirs(t *testing.T) {
	workspaceRoot, configPath := setupWorkspace(t)

	// Simulate ~/.ssh/config: create a file inside a subdirectory.
	sshDir := filepath.Join(t.TempDir(), ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir .ssh failed: %v", err)
	}
	systemFile := filepath.Join(sshDir, "config")
	if err := os.WriteFile(systemFile, []byte("Host *\n"), 0o600); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	if _, err := Add(AddOptions{
		WorkspacePath: workspaceRoot,
		ConfigPath:    configPath,
		SystemPath:    systemFile,
	}); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	// Workspace should contain the nested directory.
	links, _ := List(configPath)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	wsFile := filepath.Join(workspaceRoot, filepath.FromSlash(DotfilePath(links[0].Original, links[0].Class)))
	wsParent := filepath.Dir(wsFile)
	if _, err := os.Stat(wsParent); err != nil {
		t.Fatalf("workspace parent dir should exist before remove: %v", err)
	}

	// Remove the dotfile.
	if _, err := Remove(RemoveOptions{
		WorkspacePath: workspaceRoot,
		ConfigPath:    configPath,
		SystemPath:    systemFile,
	}); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	// The workspace subdirectory should be gone.
	if _, err := os.Stat(wsParent); err == nil {
		t.Fatalf("expected workspace parent dir %s to be removed after dotfile rm, but it still exists", wsParent)
	}

	// The system file should be restored as a real file.
	info, err := os.Lstat(systemFile)
	if err != nil {
		t.Fatalf("system file should be restored: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("expected restored system file to be a regular file, not a symlink")
	}
}

// TestRemovePrunesOnlyEmptyDirs verifies that non-empty directories are not
// removed when one sibling file is removed but others remain.
func TestRemovePrunesOnlyEmptyDirs(t *testing.T) {
	workspaceRoot, configPath := setupWorkspace(t)

	sshDir := filepath.Join(t.TempDir(), ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir .ssh failed: %v", err)
	}

	fileA := filepath.Join(sshDir, "config")
	fileB := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(fileA, []byte("Host *\n"), 0o600); err != nil {
		t.Fatalf("write fileA: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("ssh-rsa AAAA...\n"), 0o600); err != nil {
		t.Fatalf("write fileB: %v", err)
	}

	for _, f := range []string{fileA, fileB} {
		if _, err := Add(AddOptions{
			WorkspacePath: workspaceRoot,
			ConfigPath:    configPath,
			SystemPath:    f,
		}); err != nil {
			t.Fatalf("add %s failed: %v", f, err)
		}
	}

	// Remove only fileA.
	if _, err := Remove(RemoveOptions{
		WorkspacePath: workspaceRoot,
		ConfigPath:    configPath,
		SystemPath:    fileA,
	}); err != nil {
		t.Fatalf("remove fileA failed: %v", err)
	}

	// The workspace subdirectory should still exist (fileB is still there).
	links, _ := List(configPath)
	if len(links) != 1 {
		t.Fatalf("expected 1 remaining link, got %d", len(links))
	}
	wsBFile := filepath.Join(workspaceRoot, filepath.FromSlash(DotfilePath(links[0].Original, links[0].Class)))
	wsParent := filepath.Dir(wsBFile)
	if _, err := os.Stat(wsParent); err != nil {
		t.Fatalf("workspace parent dir should NOT be removed while sibling %s still exists: %v", wsBFile, err)
	}
}
