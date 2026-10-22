package dotfiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// TestLoadReportsBrokenLinks runs the real loader against a temp workspace.
func TestLoadReportsBrokenLinks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Workspace")
	for _, dir := range []string{"configs/dotfiles", "secrets/dotfiles"} {
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
		if _, err := dotfile.Add(dotfile.AddOptions{WorkspacePath: root, ConfigPath: cfgPath, SystemPath: p}); err != nil {
			t.Fatal(err)
		}
	}
	// Break one link by deleting its workspace target.
	links, _ := dotfile.List(cfgPath)
	for _, l := range links {
		if filepath.Base(l.System) == ".broken" {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(dotfile.LinkDotfilePath(l)))); err != nil {
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
		want := ""
		if filepath.Base(en.Link.System) == ".broken" {
			want = dotfile.StatusBroken
		}
		if en.Status != want {
			t.Errorf("%s: status = %q, want %q", en.Link.System, en.Status, want)
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

func TestListShowsClass(t *testing.T) {
	e := env.Env{Paths: workspace.Paths{Workspace: "/ws"}}
	c, _ := New(e).Update(nav.ResizeMsg{Width: 100, Height: 10})
	c, _ = c.Update(LoadedMsg{Entries: []Entry{
		{Link: wsconfig.Link{System: "/a", Class: dotfile.ClassSecret}},
		{Link: wsconfig.Link{System: "/b"}},
	}})
	v := ansi.Strip(c.View())
	for _, want := range []string{"Class", "secret", "config"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
}
