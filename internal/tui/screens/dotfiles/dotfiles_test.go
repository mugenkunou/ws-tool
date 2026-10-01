package dotfiles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/workspace"
)

// TestLoadReportsBrokenLinks runs the real loader against a temp workspace.
func TestLoadReportsBrokenLinks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Workspace")
	if err := os.MkdirAll(filepath.Join(root, "ws"), 0o755); err != nil {
		t.Fatal(err)
	}
	mPath := filepath.Join(root, "ws", "manifest.json")
	if err := manifest.Save(mPath, manifest.Default()); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(filepath.Join(root, "ws", "config.json"), config.Default()); err != nil {
		t.Fatal(err)
	}

	sys := t.TempDir()
	for _, name := range []string{".healthy", ".broken"} {
		p := filepath.Join(sys, name)
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := dotfile.Add(dotfile.AddOptions{WorkspacePath: root, ManifestPath: mPath, SystemPath: p}); err != nil {
			t.Fatal(err)
		}
	}
	// Break one link by deleting its workspace target.
	records, _ := dotfile.List(mPath)
	for _, r := range records {
		if filepath.Base(r.System) == ".broken" {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(dotfile.DotfilePath(r.Name)))); err != nil {
				t.Fatal(err)
			}
		}
	}

	e := env.Env{Paths: workspace.Paths{Workspace: root, Manifest: mPath}}
	msg := Load(e)().(LoadedMsg)
	if msg.Err != nil {
		t.Fatal(msg.Err)
	}
	if len(msg.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(msg.Entries))
	}
	for _, en := range msg.Entries {
		want := ""
		if filepath.Base(en.Record.System) == ".broken" {
			want = dotfile.StatusBroken
		}
		if en.Status != want {
			t.Errorf("%s: status = %q, want %q", en.Record.System, en.Status, want)
		}
	}
	if msg.Issues() != 1 {
		t.Errorf("Issues() = %d, want 1", msg.Issues())
	}
}

func TestLoadMissingManifest(t *testing.T) {
	e := env.Env{Paths: workspace.Paths{Workspace: t.TempDir(), Manifest: filepath.Join(t.TempDir(), "nope.json")}}
	if msg := Load(e)().(LoadedMsg); msg.Err == nil {
		t.Fatal("expected an error for a missing manifest")
	}
}
