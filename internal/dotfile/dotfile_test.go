package dotfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/manifest"
)

func setupWorkspace(t *testing.T) (workspaceRoot, manifestPath string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Workspace")
	wsDir := filepath.Join(root, "ws")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatalf("mkdir ws/ failed: %v", err)
	}
	cfgPath := filepath.Join(wsDir, "config.json")
	if err := config.Save(cfgPath, config.Default()); err != nil {
		t.Fatalf("save config failed: %v", err)
	}
	mPath := filepath.Join(wsDir, "manifest.json")
	if err := manifest.Save(mPath, manifest.Default()); err != nil {
		t.Fatalf("save manifest failed: %v", err)
	}
	return root, mPath
}

func TestAddListRemoveRoundTrip(t *testing.T) {
	workspaceRoot, manifestPath := setupWorkspace(t)

	systemFile := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(systemFile, []byte("export TEST=1\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	addRes, err := Add(AddOptions{
		WorkspacePath: workspaceRoot,
		ManifestPath:  manifestPath,
		SystemPath:    systemFile,
	})
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if len(addRes.Messages) == 0 {
		t.Fatal("expected add messages")
	}

	records, err := List(manifestPath)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 managed dotfile, got %d", len(records))
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
		ManifestPath:  manifestPath,
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

	records, err = List(manifestPath)
	if err != nil {
		t.Fatalf("list after remove failed: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected no managed dotfiles, got %d", len(records))
	}
}

func TestScanAndFixBrokenSymlink(t *testing.T) {
	workspaceRoot, manifestPath := setupWorkspace(t)
	systemFile := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(systemFile, []byte("alias ll='ls -la'\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	if _, err := Add(AddOptions{WorkspacePath: workspaceRoot, ManifestPath: manifestPath, SystemPath: systemFile}); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	records, err := List(manifestPath)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one record, got %d", len(records))
	}

	target := filepath.Join(workspaceRoot, filepath.FromSlash(DotfilePath(records[0].Name)))
	if err := os.Remove(target); err != nil {
		t.Fatalf("remove target failed: %v", err)
	}

	issues, err := Scan(ScanOptions{WorkspacePath: workspaceRoot, ManifestPath: manifestPath})
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
	fixRes, err := Fix(FixOptions{WorkspacePath: workspaceRoot, ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("fix failed: %v", err)
	}
	if len(fixRes.Failed) != 1 {
		t.Fatalf("expected 1 failed dotfile, got %d", len(fixRes.Failed))
	}
}

func TestFixCreatesSymlink(t *testing.T) {
	workspaceRoot, manifestPath := setupWorkspace(t)
	systemFile := filepath.Join(t.TempDir(), ".profile")
	if err := os.WriteFile(systemFile, []byte("export PATH=$PATH\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	// Add the dotfile normally.
	if _, err := Add(AddOptions{WorkspacePath: workspaceRoot, ManifestPath: manifestPath, SystemPath: systemFile}); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	// Remove the system symlink to simulate a fresh machine.
	if err := os.Remove(systemFile); err != nil {
		t.Fatalf("remove symlink failed: %v", err)
	}

	// Fix should recreate the symlink.
	fixRes, err := Fix(FixOptions{WorkspacePath: workspaceRoot, ManifestPath: manifestPath})
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
	workspaceRoot, manifestPath := setupWorkspace(t)
	systemFile := filepath.Join(t.TempDir(), ".gitconfig")
	if err := os.WriteFile(systemFile, []byte("[user]\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	if _, err := Add(AddOptions{WorkspacePath: workspaceRoot, ManifestPath: manifestPath, SystemPath: systemFile}); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	// Fix when everything is already correct.
	fixRes, err := Fix(FixOptions{WorkspacePath: workspaceRoot, ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("fix failed: %v", err)
	}
	if len(fixRes.Unchanged) != 1 {
		t.Fatalf("expected 1 unchanged, got %d (fixed=%d, failed=%d)", len(fixRes.Unchanged), len(fixRes.Fixed), len(fixRes.Failed))
	}
}

func TestAddDryRun(t *testing.T) {
	workspaceRoot, manifestPath := setupWorkspace(t)
	systemFile := filepath.Join(t.TempDir(), ".tmux.conf")
	if err := os.WriteFile(systemFile, []byte("set -g mouse on\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	res, err := Add(AddOptions{
		WorkspacePath: workspaceRoot,
		ManifestPath:  manifestPath,
		SystemPath:    systemFile,
		DryRun:        true,
	})
	if err != nil {
		t.Fatalf("dry-run add failed: %v", err)
	}
	if len(res.Messages) == 0 {
		t.Fatal("expected dry-run messages")
	}

	cfgPath, _ := config.ResolvePath(workspaceRoot, "ws/config.json")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("workspace config should still exist: %v", err)
	}

	m, err := manifest.Load(manifestPath)
	if err != nil {
		t.Fatalf("manifest load failed: %v", err)
	}
	if len(m.Dotfiles) != 0 {
		t.Fatalf("dry-run should not modify manifest, got %d entries", len(m.Dotfiles))
	}
}

// TestRemovePrunesEmptyDirs verifies that removing a file-level dotfile whose
// workspace path lives inside a subdirectory (e.g. ws/dotfiles/ssh/config)
// also removes the now-empty parent directory hierarchy under ws/dotfiles/.
func TestRemovePrunesEmptyDirs(t *testing.T) {
	workspaceRoot, manifestPath := setupWorkspace(t)

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
		ManifestPath:  manifestPath,
		SystemPath:    systemFile,
	}); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	// Workspace should contain the nested directory.
	records, _ := List(manifestPath)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	wsFile := filepath.Join(workspaceRoot, filepath.FromSlash(DotfilePath(records[0].Name)))
	wsParent := filepath.Dir(wsFile)
	if _, err := os.Stat(wsParent); err != nil {
		t.Fatalf("workspace parent dir should exist before remove: %v", err)
	}

	// Remove the dotfile.
	if _, err := Remove(RemoveOptions{
		WorkspacePath: workspaceRoot,
		ManifestPath:  manifestPath,
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
	workspaceRoot, manifestPath := setupWorkspace(t)

	sshDir := filepath.Join(t.TempDir(), ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir .ssh failed: %v", err)
	}

	fileA := filepath.Join(sshDir, "config")
	fileB := filepath.Join(sshDir, "known_hosts")
	if err := os.WriteFile(fileA, []byte("Host *\n"), 0o600); err != nil {
		t.Fatalf("write fileA: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("github.com ...\n"), 0o600); err != nil {
		t.Fatalf("write fileB: %v", err)
	}

	for _, f := range []string{fileA, fileB} {
		if _, err := Add(AddOptions{
			WorkspacePath: workspaceRoot,
			ManifestPath:  manifestPath,
			SystemPath:    f,
		}); err != nil {
			t.Fatalf("add %s failed: %v", f, err)
		}
	}

	// Remove only fileA.
	if _, err := Remove(RemoveOptions{
		WorkspacePath: workspaceRoot,
		ManifestPath:  manifestPath,
		SystemPath:    fileA,
	}); err != nil {
		t.Fatalf("remove fileA failed: %v", err)
	}

	// The workspace subdirectory should still exist (fileB is still there).
	records, _ := List(manifestPath)
	if len(records) != 1 {
		t.Fatalf("expected 1 remaining record, got %d", len(records))
	}
	wsBFile := filepath.Join(workspaceRoot, filepath.FromSlash(DotfilePath(records[0].Name)))
	wsParent := filepath.Dir(wsBFile)
	if _, err := os.Stat(wsParent); err != nil {
		t.Fatalf("workspace parent dir should NOT be removed while sibling %s still exists: %v", wsBFile, err)
	}
}
