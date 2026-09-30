package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigSchema(t *testing.T) {
	cfg := Default()
	if cfg.ConfigSchema != CurrentSchema {
		t.Fatalf("expected schema %d, got %d", CurrentSchema, cfg.ConfigSchema)
	}
	if cfg.Scratch.RootDir == "" {
		t.Fatal("expected default scratch root to be set")
	}
	if cfg.Search.MaxResults != 0 {
		t.Fatalf("expected search.max_results default 0, got %d", cfg.Search.MaxResults)
	}
	if cfg.Trash.WarnSizeMB != 1024 {
		t.Fatalf("expected trash.warn_size_mb default 1024, got %d", cfg.Trash.WarnSizeMB)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ws", "config.json")

	in := Default()
	in.Dotfile.Git.Enabled = true

	if err := Save(path, in); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	out, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if !out.Dotfile.Git.Enabled {
		t.Fatal("expected dotfile.git.enabled to persist")
	}
}

func TestLoadRejectsHigherSchema(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.json")
	content := `{"config_schema":999}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture failed: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for unsupported schema")
	}
	if !strings.Contains(err.Error(), "unsupported config schema") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExpandUserPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home dir failed: %v", err)
	}

	got, err := ExpandUserPath("~/Workspace")
	if err != nil {
		t.Fatalf("expand failed: %v", err)
	}

	want := filepath.Join(home, "Workspace")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestResolvePath(t *testing.T) {
	tmp := t.TempDir()

	got, err := ResolvePath(tmp, "ws/config.json")
	if err != nil {
		t.Fatalf("resolve relative failed: %v", err)
	}
	if got != filepath.Join(tmp, "ws", "config.json") {
		t.Fatalf("unexpected resolved path: %s", got)
	}

	abs := filepath.Join(tmp, "absolute.json")
	got, err = ResolvePath(tmp, abs)
	if err != nil {
		t.Fatalf("resolve absolute failed: %v", err)
	}
	if got != abs {
		t.Fatalf("expected absolute path unchanged, got %s", got)
	}
}

func TestWithAbsPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	ws := "/w/Workspace"
	cfg := Default()
	cfg.Workspace = ws
	cfg.Secret.SkipDirs = []string{"vendor/"}
	cfg.Capture.Locations = map[string]string{"notes": "Notes/brain", "abs": "/srv/x"}

	got := WithAbsPaths(cfg, ws)

	if got.Scratch.RootDir != filepath.Join(home, "Scratch") {
		t.Errorf("scratch root = %q", got.Scratch.RootDir)
	}
	if got.Trash.RootDir != filepath.Join(home, ".Trash") {
		t.Errorf("trash root = %q", got.Trash.RootDir)
	}
	if len(got.Repo.Roots) != 1 || got.Repo.Roots[0] != ws {
		t.Errorf("repo roots = %v", got.Repo.Roots)
	}
	if got.Repo.ExcludeDirs[0] != "/w/Workspace/ws" {
		t.Errorf("exclude dirs = %v", got.Repo.ExcludeDirs)
	}
	if got.Secret.SkipDirs[0] != "/w/Workspace/vendor" {
		t.Errorf("skip dirs = %v", got.Secret.SkipDirs)
	}
	if got.Capture.Locations["notes"] != "/w/Workspace/Notes/brain" || got.Capture.Locations["abs"] != "/srv/x" {
		t.Errorf("capture locations = %v", got.Capture.Locations)
	}
	// Original must be untouched.
	if cfg.Repo.Roots[0] != "." || cfg.Capture.Locations["notes"] != "Notes/brain" {
		t.Errorf("WithAbsPaths mutated its input: %+v", cfg)
	}
}

func TestWorkspaceRel(t *testing.T) {
	ws := "/w/Workspace"
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"node_modules", "node_modules", true},
		{"./a//b/", "a/b", true},
		{"/w/Workspace/a/b", "a/b", true},
		{"/w/Workspace", ".", true},
		{"/elsewhere/x", "", false},
		{"../x", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := WorkspaceRel(ws, c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("WorkspaceRel(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
