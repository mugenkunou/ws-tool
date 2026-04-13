package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultManifestSchema(t *testing.T) {
	m := Default()
	if m.ManifestSchema != CurrentSchema {
		t.Fatalf("expected schema %d, got %d", CurrentSchema, m.ManifestSchema)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ws", "manifest.json")

	in := Default()
	in.Dotfiles = append(in.Dotfiles, DotfileRecord{
		System: "~/.bashrc",
		Name:   "bashrc",
	})

	if err := Save(path, in); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	out, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if len(out.Dotfiles) != 1 {
		t.Fatalf("expected 1 dotfile, got %d", len(out.Dotfiles))
	}
}

func TestLoadRejectsHigherSchema(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "manifest.json")
	content := `{"manifest_schema":999}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture failed: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for unsupported schema")
	}
	if !strings.Contains(err.Error(), "unsupported manifest schema") {
		t.Fatalf("unexpected error: %v", err)
	}
}
