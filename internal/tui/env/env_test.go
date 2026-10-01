package env

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/workspace"
)

// initWorkspace writes a minimal initialized workspace and returns overrides
// pointing at it, so the test never touches the real XDG config.
func initWorkspace(t *testing.T) workspace.PathOverrides {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Workspace")
	if err := os.MkdirAll(filepath.Join(root, "ws"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(cfgPath, config.Default()); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(filepath.Join(root, "ws", "manifest.json"), manifest.Default()); err != nil {
		t.Fatal(err)
	}
	return workspace.PathOverrides{Workspace: root, Config: cfgPath}
}

func TestLoadNotInitialized(t *testing.T) {
	root := t.TempDir()
	e, err := Load(workspace.PathOverrides{Workspace: root, Config: filepath.Join(root, "missing.json")})
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
	if want := filepath.Join(o.Workspace, "ws", "manifest.json"); e.Paths.Manifest != want {
		t.Errorf("manifest = %q, want %q", e.Paths.Manifest, want)
	}
	if want := filepath.Join(o.Workspace, "ws", "ws-log"); e.LogDir != want {
		t.Errorf("log dir = %q, want %q", e.LogDir, want)
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
		"/home/tester/Workspace":              "~/Workspace",
	} {
		if got := e.Rel(in); got != want {
			t.Errorf("Rel(%q) = %q, want %q", in, got, want)
		}
	}
}
