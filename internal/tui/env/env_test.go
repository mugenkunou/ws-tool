package env

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// initWorkspace writes a minimal initialized workspace and returns overrides
// pointing at it, so the test never touches the real workspace.
func initWorkspace(t *testing.T) workspace.PathOverrides {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, "ws.json")
	if err := wsconfig.Save(cfgPath, wsconfig.Default()); err != nil {
		t.Fatal(err)
	}
	return workspace.PathOverrides{Workspace: root}
}

func TestLoadNotInitialized(t *testing.T) {
	root := t.TempDir()
	e, err := Load(workspace.PathOverrides{Workspace: root})
	if !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("err = %v, want ErrNotInitialized", err)
	}
	if e.Paths.Workspace != root {
		t.Fatalf("workspace = %q, want %q (needed for the error screen)", e.Paths.Workspace, root)
	}
}

func TestLoadResolvesDirs(t *testing.T) {
	o := initWorkspace(t)
	e, err := Load(o)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(e.LogDir) || e.LogDir == filepath.Join(o.Workspace, "data", "sessions") {
		t.Errorf("log dir %q is not an absolute session.root_dir", e.LogDir)
	}
	if !filepath.IsAbs(e.ScratchDir) {
		t.Errorf("scratch dir %q is not absolute", e.ScratchDir)
	}
}

func TestRel(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	e := Env{Paths: workspace.Paths{Workspace: "/home/tester/Workspace"}}
	for in, want := range map[string]string{
		"/home/tester/Workspace/Projects/api": "Projects/api",
		"/home/tester/.password-store":        "~/.password-store",
		"/etc/hosts":                          "/etc/hosts",
	} {
		if got := e.Rel(in); got != want {
			t.Errorf("Rel(%q) = %q, want %q", in, got, want)
		}
	}
}
