package trash

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetupAndStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	rootDir := "~/.Trash"
	res, err := Setup(SetupOptions{
		RootDir:      rootDir,
		ShellRM:      true,
		VSCodeDelete: true,
		FileExplorer: false,
	})
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if res.DryRun {
		t.Fatal("expected non-dry-run result")
	}
	if _, err := os.Stat(filepath.Join(home, ".Trash")); err != nil {
		t.Fatalf("expected trash dir to exist: %v", err)
	}

	status, err := GetStatus(rootDir)
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if !status.ShellRMConfigured || !status.VSCodeConfigured || status.FileExplorerConfigured {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.WarningCount() != 1 {
		t.Fatalf("expected warning count 1, got %d", status.WarningCount())
	}
}

func TestSetupDryRunDoesNotWriteState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	res, err := Setup(SetupOptions{
		RootDir:      "~/.Trash",
		ShellRM:      true,
		VSCodeDelete: true,
		FileExplorer: true,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("dry-run setup failed: %v", err)
	}
	if !res.DryRun {
		t.Fatal("expected dry-run result")
	}

	statePath := filepath.Join(home, ".config", "ws-tool", "trash-setup.json")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("state file should not exist on dry-run, got err=%v", err)
	}
}

func TestScanEmptyTrashDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	trashDir := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	res, err := Scan(ScanOptions{RootDir: trashDir, WarnSizeMB: 100})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if !res.Exists {
		t.Fatal("expected Exists=true")
	}
	if res.FileCount != 0 || res.SizeBytes != 0 {
		t.Fatalf("expected empty trash, got files=%d size=%d", res.FileCount, res.SizeBytes)
	}
	if res.OverLimit {
		t.Fatal("expected OverLimit=false for empty trash")
	}
}

func TestScanNonExistentTrashDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	res, err := Scan(ScanOptions{RootDir: filepath.Join(home, "no-such-trash"), WarnSizeMB: 100})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if res.Exists {
		t.Fatal("expected Exists=false")
	}
}

func TestScanOverLimit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	trashDir := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// Create a 2MB file to exceed a 1MB threshold.
	big := make([]byte, 2*1024*1024)
	if err := os.WriteFile(filepath.Join(trashDir, "big.bin"), big, 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	res, err := Scan(ScanOptions{RootDir: trashDir, WarnSizeMB: 1})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if !res.OverLimit {
		t.Fatal("expected OverLimit=true")
	}
	if res.FileCount != 1 {
		t.Fatalf("expected 1 file, got %d", res.FileCount)
	}
}

func TestScanUnderLimit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	trashDir := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(trashDir, "small.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	res, err := Scan(ScanOptions{RootDir: trashDir, WarnSizeMB: 100})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if res.OverLimit {
		t.Fatal("expected OverLimit=false")
	}
	if res.FileCount != 1 {
		t.Fatalf("expected 1 file, got %d", res.FileCount)
	}
}

func TestFileExplorerIntegrationSetupAndStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	rootDir := filepath.Join(home, ".Trash")

	_, err := Setup(SetupOptions{
		RootDir:      rootDir,
		ShellRM:      false,
		VSCodeDelete: false,
		FileExplorer: true,
		DryRun:       false,
	})
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// XDG symlink should point to the trash root.
	symlinkPath := filepath.Join(home, ".local", "share", "Trash")
	info, err := os.Lstat(symlinkPath)
	if err != nil {
		t.Fatalf("expected symlink at %s: %v", symlinkPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink, got %v", info.Mode())
	}
	target, err := os.Readlink(symlinkPath)
	if err != nil {
		t.Fatalf("readlink failed: %v", err)
	}
	if target != rootDir {
		t.Fatalf("symlink target: want %s, got %s", rootDir, target)
	}

	// Status should report file explorer as configured.
	status, err := GetStatus(rootDir)
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if !status.FileExplorerConfigured {
		t.Fatal("expected FileExplorerConfigured=true")
	}
}

func TestFileExplorerIntegrationIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	rootDir := filepath.Join(home, ".Trash")

	for i := 0; i < 2; i++ {
		if err := ensureFileExplorerIntegration(rootDir); err != nil {
			t.Fatalf("call %d: ensureFileExplorerIntegration failed: %v", i+1, err)
		}
	}

	if !fileExplorerConfigured(rootDir) {
		t.Fatal("expected fileExplorerConfigured=true after two calls")
	}
}

func TestFileExplorerIntegrationRealDirNotOverwritten(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a real directory at the XDG trash path before setup.
	symlinkPath := filepath.Join(home, ".local", "share", "Trash")
	if err := os.MkdirAll(symlinkPath, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	existingFile := filepath.Join(symlinkPath, "existing.txt")
	if err := os.WriteFile(existingFile, []byte("data"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	rootDir := filepath.Join(home, ".Trash")
	if err := ensureFileExplorerIntegration(rootDir); err != nil {
		t.Fatalf("ensureFileExplorerIntegration failed: %v", err)
	}

	// Real directory must not have been replaced.
	if _, err := os.Stat(existingFile); err != nil {
		t.Fatalf("existing file was removed: %v", err)
	}
}

func TestFileExplorerNotConfiguredWhenSymlinkAbsent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	rootDir := filepath.Join(home, ".Trash")
	if fileExplorerConfigured(rootDir) {
		t.Fatal("expected fileExplorerConfigured=false when no symlink exists")
	}
}
