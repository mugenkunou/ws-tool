package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/provision"
)

// helper: bootstrap a minimal workspace so reset has something to tear down.
func setupWorkspace(t *testing.T) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "Workspace")
	cfgPath := filepath.Join(tmp, "config", "ws-tool", "config.json")

	res, err := Init(InitOptions{WorkspacePath: root, ConfigPath: cfgPath})
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if len(res.Created) == 0 {
		t.Fatal("init created nothing")
	}
	return root, cfgPath
}

func TestResetRemovesWS(t *testing.T) {
	root, _ := setupWorkspace(t)

	res, err := Reset(ResetOptions{WorkspacePath: root})
	if err != nil {
		t.Fatalf("reset failed: %v", err)
	}

	if !res.WSRemoved {
		t.Fatal("expected ws/ to be removed")
	}

	if _, err := os.Stat(filepath.Join(root, "ws")); !os.IsNotExist(err) {
		t.Fatal("ws/ dir should be gone")
	}

	if len(res.Subsystems) == 0 {
		t.Fatal("expected subsystem results")
	}
}

func TestResetUndoesTrashProvisions(t *testing.T) {
	root, _ := setupWorkspace(t)

	// Record a trash-related file provision.
	ext := filepath.Join(t.TempDir(), "ws-trash-rm")
	os.WriteFile(ext, []byte("#!/bin/bash"), 0o755)
	manifestPath := filepath.Join(root, "ws", "manifest.json")
	if err := manifest.RecordProvision(manifestPath, provision.Entry{
		Type:    provision.TypeFile,
		Path:    ext,
		Command: "trash setup",
	}); err != nil {
		t.Fatalf("record provision: %v", err)
	}

	res, err := Reset(ResetOptions{WorkspacePath: root})
	if err != nil {
		t.Fatalf("reset failed: %v", err)
	}

	if !res.WSRemoved {
		t.Fatal("expected ws/ to be removed")
	}

	if _, err := os.Stat(ext); !os.IsNotExist(err) {
		t.Fatal("trash script should be deleted by reset")
	}
}

func TestResetDryRunPreservesEverything(t *testing.T) {
	root, cfgPath := setupWorkspace(t)

	res, err := Reset(ResetOptions{WorkspacePath: root, DryRun: true})
	if err != nil {
		t.Fatalf("reset dry-run failed: %v", err)
	}

	if !res.DryRun {
		t.Fatal("expected dry_run=true")
	}
	if res.WSRemoved {
		t.Fatal("ws/ should not be removed in dry-run")
	}

	// ws/ should still exist.
	if _, err := os.Stat(filepath.Join(root, "ws")); err != nil {
		t.Fatal("ws/ should still exist after dry-run")
	}

	// config should still exist.
	if !ConfigExists(cfgPath) {
		t.Fatal("config should still exist after dry-run")
	}
}

func TestResetNoProvisionsStillRemovesWS(t *testing.T) {
	// Create workspace manually without provisions ledger.
	root := filepath.Join(t.TempDir(), "Workspace")
	wsDir := filepath.Join(root, "ws")
	os.MkdirAll(wsDir, 0o755)
	config.Save(filepath.Join(wsDir, "config.json"), config.Default())
	manifest.Save(filepath.Join(wsDir, "manifest.json"), manifest.Default())

	res, err := Reset(ResetOptions{WorkspacePath: root})
	if err != nil {
		t.Fatalf("reset failed: %v", err)
	}

	if !res.WSRemoved {
		t.Fatal("expected ws/ to be removed even with no provisions")
	}
}

func TestResetFailsOnUninitializedWorkspace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Workspace")

	_, err := Reset(ResetOptions{WorkspacePath: root})
	if err == nil {
		t.Fatal("expected error for uninitialized workspace")
	}
}

func TestResetRequiresWorkspacePath(t *testing.T) {
	_, err := Reset(ResetOptions{})
	if err == nil {
		t.Fatal("expected error for empty workspace path")
	}
}

func TestProvisions(t *testing.T) {
	root, cfgPath := setupWorkspace(t)

	// workspace.Init doesn't record provisions (that's done by the cmd layer),
	// so record one manually to test the Provisions() helper.
	manifestPath := filepath.Join(root, "ws", "manifest.json")
	if err := manifest.RecordProvision(manifestPath, provision.Entry{
		Type:    provision.TypeFile,
		Path:    cfgPath,
		Command: "init",
	}); err != nil {
		t.Fatalf("record provision: %v", err)
	}

	entries, err := Provisions(root)
	if err != nil {
		t.Fatalf("Provisions() failed: %v", err)
	}

	if len(entries) == 0 {
		t.Fatal("expected provisions after recording")
	}
}

func TestUndoActionLabel(t *testing.T) {
	cases := []struct {
		entry provision.Entry
		want  string
	}{
		{provision.Entry{Type: provision.TypeFile}, "delete file"},
		{provision.Entry{Type: provision.TypeDir}, "delete directory"},
		{provision.Entry{Type: provision.TypeSymlink}, "remove symlink"},
		{provision.Entry{Type: provision.TypeConfigLine, Path: "/foo/.bashrc"}, "remove line from .bashrc"},
		{provision.Entry{Type: provision.TypeGitExclude}, "remove exclude entry"},
		{provision.Entry{Type: "other"}, "unknown"},
	}
	for _, tc := range cases {
		got := UndoActionLabel(tc.entry)
		if got != tc.want {
			t.Errorf("UndoActionLabel(%v) = %q, want %q", tc.entry.Type, got, tc.want)
		}
	}
}
