package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitCreatesWorkspaceScaffold(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "Workspace")

	res, err := Init(InitOptions{WorkspacePath: root})
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if len(res.Created) == 0 {
		t.Fatal("expected created files")
	}

	mustExist(t, filepath.Join(root, "ws", "config.json"))
	mustExist(t, filepath.Join(root, "ws", "manifest.json"))
	mustExist(t, filepath.Join(root, ".megaignore"))
}

func TestInitDryRunDoesNotWrite(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "Workspace")

	res, err := Init(InitOptions{WorkspacePath: root, DryRun: true})
	if err != nil {
		t.Fatalf("init dry-run failed: %v", err)
	}

	if !res.DryRun {
		t.Fatal("expected dry_run=true in result")
	}

	if _, err := os.Stat(filepath.Join(root, "ws")); err == nil {
		t.Fatal("workspace should not be created in dry run")
	}
}

func TestConfigExists(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ws", "config.json")

	if ConfigExists(path) {
		t.Fatal("config should not exist")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if !ConfigExists(path) {
		t.Fatal("config should exist")
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}
