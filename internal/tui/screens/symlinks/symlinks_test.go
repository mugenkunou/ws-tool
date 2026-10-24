package symlinks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/symlink"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// TestLoadReportsBrokenLinks runs the real loader against a temp workspace.
func TestLoadReportsBrokenLinks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Workspace")
	t.Setenv("HOME", t.TempDir()) // Detect reads $HOME
	for _, dir := range []string{"symlinks"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := wsconfig.ConfigPath(root)
	if err := wsconfig.Save(cfgPath, wsconfig.Default()); err != nil {
		t.Fatal(err)
	}

	sys := t.TempDir()
	for _, name := range []string{".healthy", ".broken"} {
		p := filepath.Join(sys, name)
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := symlink.Add(symlink.AddOptions{WorkspacePath: root, ConfigPath: cfgPath, Host: p}); err != nil {
			t.Fatal(err)
		}
	}
	// Break one link by deleting its workspace target.
	links, _ := symlink.List(cfgPath)
	for _, l := range links {
		if filepath.Base(l.System) == ".broken" {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(symlink.LinkOriginalPath(l)))); err != nil {
				t.Fatal(err)
			}
		}
	}

	e := env.Env{Paths: workspace.Paths{Workspace: root, Config: cfgPath}}
	msg := Load(e)().(LoadedMsg)
	if msg.Err != nil {
		t.Fatal(msg.Err)
	}
	if len(msg.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(msg.Entries))
	}
	for _, en := range msg.Entries {
		want := symlink.StatusOK
		if filepath.Base(en.System) == ".broken" {
			want = symlink.StatusBroken
		}
		if en.Status != want {
			t.Errorf("%s: status = %q, want %q", en.System, en.Status, want)
		}
	}
	if msg.Issues() != 1 {
		t.Errorf("Issues() = %d, want 1", msg.Issues())
	}
}

func TestLoadMissingConfig(t *testing.T) {
	e := env.Env{Paths: workspace.Paths{Workspace: t.TempDir(), Config: filepath.Join(t.TempDir(), "nope.json")}}
	if msg := Load(e)().(LoadedMsg); msg.Err == nil {
		t.Fatal("expected an error for a missing config")
	}
}

// TestListShowsTheWorkspaceCopy: the list shows where each copy lives (there is
// no class column: the location says what it is), and the copy's path is
// shown in full for a secret too.
func TestListShowsTheWorkspaceCopy(t *testing.T) {
	e := env.Env{Paths: workspace.Paths{Workspace: "/ws"}}
	c, _ := New(e).Update(nav.ResizeMsg{Width: 100, Height: 10})
	c, _ = c.Update(LoadedMsg{Entries: []Entry{
		{System: "/a", Original: "symlinks/system/a", WorkspacePath: "/ws/symlinks/system/a", Status: symlink.StatusOK, Registered: true},
		{System: "/b", Original: "agent/b", WorkspacePath: "/ws/agent/b", Status: symlink.StatusOK, Registered: true},
	}})
	v := ansi.Strip(c.View())
	if strings.Contains(v, "Class") {
		t.Errorf("the class column is gone:\n%s", v)
	}
	for _, want := range []string{"Workspace copy", "symlinks/system/a", "agent/b"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
}
