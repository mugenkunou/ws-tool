package trash

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupAndStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	res, err := Setup(SetupOptions{
		RootDir:      "", // default: XDG trash dir
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
	wantRoot := filepath.Join(home, ".local", "share", "Trash")
	if res.RootDir != wantRoot {
		t.Fatalf("root dir: want %s, got %s", wantRoot, res.RootDir)
	}
	if _, err := os.Stat(filepath.Join(wantRoot, "files")); err != nil {
		t.Fatalf("expected files/ dir to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wantRoot, "info")); err != nil {
		t.Fatalf("expected info/ dir to exist: %v", err)
	}

	status, err := GetStatus("")
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
		RootDir:      "",
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

	statePath := filepath.Join(home, ".local", "state", "ws-tool", "trash-setup.json")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("state file should not exist on dry-run, got err=%v", err)
	}
}

func TestDefaultRootIsXDGTrashDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")

	status, err := GetStatus("")
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	want := filepath.Join(home, ".local", "share", "Trash")
	if status.RootDir != want {
		t.Fatalf("root dir: want %s, got %s", want, status.RootDir)
	}
}

func TestSoftDeleteMovesIntoFilesWithTrashInfo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	src := filepath.Join(home, "doomed.txt")
	if err := os.WriteFile(src, []byte("bye"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := SoftDelete(src); err != nil {
		t.Fatalf("SoftDelete failed: %v", err)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected original path to be gone, err=%v", err)
	}

	trashedPath := filepath.Join(home, ".local", "share", "Trash", "files", "doomed.txt")
	data, err := os.ReadFile(trashedPath)
	if err != nil {
		t.Fatalf("expected trashed file at %s: %v", trashedPath, err)
	}
	if string(data) != "bye" {
		t.Fatalf("trashed content mismatch: %q", data)
	}

	infoPath := filepath.Join(home, ".local", "share", "Trash", "info", "doomed.txt.trashinfo")
	info, err := os.ReadFile(infoPath)
	if err != nil {
		t.Fatalf("expected .trashinfo at %s: %v", infoPath, err)
	}
	if !containsAll(string(info), "[Trash Info]", "Path=", "DeletionDate=", src) {
		t.Fatalf("trashinfo content missing expected fields: %s", info)
	}
}

func TestSoftDeleteCollisionGetsUniqueName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	filesDirPath := filepath.Join(home, ".local", "share", "Trash", "files")
	if err := os.MkdirAll(filesDirPath, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filesDirPath, "dup.txt"), []byte("first"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	src := filepath.Join(home, "dup.txt")
	if err := os.WriteFile(src, []byte("second"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := SoftDelete(src); err != nil {
		t.Fatalf("SoftDelete failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(filesDirPath, "dup.txt.2")); err != nil {
		t.Fatalf("expected collision-renamed entry dup.txt.2: %v", err)
	}
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func TestScanEmptyTrashDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	trashDir := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(filesDir(trashDir), 0o755); err != nil {
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
	if err := os.MkdirAll(filesDir(trashDir), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// Create a 2MB file to exceed a 1MB threshold.
	big := make([]byte, 2*1024*1024)
	if err := os.WriteFile(filepath.Join(filesDir(trashDir), "big.bin"), big, 0o644); err != nil {
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
	if err := os.MkdirAll(filesDir(trashDir), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(filesDir(trashDir), "small.txt"), []byte("hello"), 0o644); err != nil {
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

	// A custom root (different from the default XDG trash dir) still uses
	// the symlink-redirect mechanism.
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

func TestFileExplorerIntegrationAutomaticWithDefaultRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// The default root IS the XDG trash dir, so FileExplorer integration
	// needs no symlink — Setup's own mkdir of files/+info/ is sufficient.
	_, err := Setup(SetupOptions{
		RootDir:      "",
		FileExplorer: true,
	})
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	symlinkPath := filepath.Join(home, ".local", "share", "Trash")
	if info, err := os.Lstat(symlinkPath); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected a real directory (not a symlink) at %s", symlinkPath)
	}

	status, err := GetStatus("")
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if !status.FileExplorerConfigured {
		t.Fatal("expected FileExplorerConfigured=true with default root")
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
	err := ensureFileExplorerIntegration(rootDir)
	if !errors.Is(err, ErrXDGTrashDirExists) {
		t.Fatalf("expected ErrXDGTrashDirExists, got: %v", err)
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

func TestEmptyNonExistentTrashDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	res, err := Empty(EmptyOptions{RootDir: filepath.Join(home, "no-such-trash")})
	if err != nil {
		t.Fatalf("empty failed: %v", err)
	}
	if res.Exists {
		t.Fatal("expected Exists=false")
	}
	if res.EntryCount != 0 || res.FileCount != 0 || res.SizeBytes != 0 {
		t.Fatalf("expected zeroed result for missing trash dir, got %+v", res)
	}
}

func TestEmptyRemovesContentsButKeepsRootDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	trashDir := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(filepath.Join(filesDir(trashDir), "nested"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.MkdirAll(infoDir(trashDir), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filesDir(trashDir), "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filesDir(trashDir), "nested", "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(infoDir(trashDir), "a.txt.trashinfo"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	res, err := Empty(EmptyOptions{RootDir: trashDir})
	if err != nil {
		t.Fatalf("empty failed: %v", err)
	}
	if !res.Exists {
		t.Fatal("expected Exists=true")
	}
	if res.EntryCount != 2 {
		t.Fatalf("expected 2 top-level files/ entries, got %d", res.EntryCount)
	}
	if res.FileCount != 2 {
		t.Fatalf("expected 2 files removed, got %d", res.FileCount)
	}

	filesEntries, err := os.ReadDir(filesDir(trashDir))
	if err != nil {
		t.Fatalf("readdir failed: %v", err)
	}
	if len(filesEntries) != 0 {
		t.Fatalf("expected files/ to be empty, found %d entries", len(filesEntries))
	}
	infoEntries, err := os.ReadDir(infoDir(trashDir))
	if err != nil {
		t.Fatalf("readdir failed: %v", err)
	}
	if len(infoEntries) != 0 {
		t.Fatalf("expected info/ to be empty, found %d entries", len(infoEntries))
	}
	// Root dir (and files/, info/ subdirs) must still exist.
	if _, err := os.Stat(trashDir); err != nil {
		t.Fatalf("expected trash root to still exist: %v", err)
	}
}

func TestEmptyDryRunDoesNotRemoveFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	trashDir := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(filesDir(trashDir), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	filePath := filepath.Join(filesDir(trashDir), "keep.txt")
	if err := os.WriteFile(filePath, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	res, err := Empty(EmptyOptions{RootDir: trashDir, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run empty failed: %v", err)
	}
	if !res.DryRun {
		t.Fatal("expected dry-run result")
	}
	if res.EntryCount != 1 || res.FileCount != 1 {
		t.Fatalf("unexpected dry-run counts: %+v", res)
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("expected file to still exist after dry-run: %v", err)
	}
}
