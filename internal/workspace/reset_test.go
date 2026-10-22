package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/provision"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// helper: bootstrap a minimal workspace so reset has something to tear down.
func setupWorkspace(t *testing.T) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "Workspace")

	res, err := Init(InitOptions{WorkspacePath: root})
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if len(res.Created) == 0 {
		t.Fatal("init created nothing")
	}
	cfgPath := wsconfig.ConfigPath(root)
	return root, cfgPath
}

func TestResetRemovesConfigsDir(t *testing.T) {
	root, _ := setupWorkspace(t)

	res, err := Reset(ResetOptions{WorkspacePath: root})
	if err != nil {
		t.Fatalf("reset failed: %v", err)
	}

	if !res.WSRemoved {
		t.Fatal("expected configs/ to be removed")
	}

	if _, err := os.Stat(filepath.Join(root, "ws.json")); !os.IsNotExist(err) {
		t.Fatal("ws.json should be gone")
	}

	if _, err := os.Stat(filepath.Join(root, "configs")); !os.IsNotExist(err) {
		t.Fatal("configs/ dir should be gone")
	}

	if len(res.Subsystems) == 0 {
		t.Fatal("expected subsystem results")
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
		t.Fatal("configs/ should not be removed in dry-run")
	}

	// configs/ should still exist.
	if _, err := os.Stat(filepath.Join(root, "configs")); err != nil {
		t.Fatal("configs/ should still exist after dry-run")
	}

	// config should still exist.
	if !ConfigExists(cfgPath) {
		t.Fatal("config should still exist after dry-run")
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

	// Record a link manually to test Provisions() helper.
	if err := wsconfig.AddLink(cfgPath, wsconfig.Link{
		System:   "/home/user/.bashrc",
		Original: "configs/dotfiles/bashrc",
	}); err != nil {
		t.Fatalf("AddLink: %v", err)
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
		{provision.Entry{Type: provision.TypeConfigLine, Path: "/foo/.bashrc"}, "remove ws line"},
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
