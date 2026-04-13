package provision

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUndoFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("data"), 0o644)

	r := Undo(Entry{Type: TypeFile, Path: path})
	if r.Action != "removed" {
		t.Fatalf("expected removed, got %s: %s", r.Action, r.Message)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file should be deleted")
	}
}

func TestUndoFileAlreadyAbsent(t *testing.T) {
	r := Undo(Entry{Type: TypeFile, Path: "/nonexistent/path"})
	if r.Action != "skipped" {
		t.Fatalf("expected skipped, got %s", r.Action)
	}
}

func TestUndoDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "mydir")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "inner.txt"), []byte("x"), 0o644)

	r := Undo(Entry{Type: TypeDir, Path: sub})
	if r.Action != "removed" {
		t.Fatalf("expected removed, got %s: %s", r.Action, r.Message)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatal("dir should be deleted")
	}
}

func TestUndoSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	os.WriteFile(target, []byte("data"), 0o644)

	link := filepath.Join(dir, "link")
	os.Symlink(target, link)

	r := Undo(Entry{Type: TypeSymlink, Path: link})
	if r.Action != "removed" {
		t.Fatalf("expected removed, got %s: %s", r.Action, r.Message)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatal("symlink should be deleted")
	}
	// Target should still exist.
	if _, err := os.Stat(target); err != nil {
		t.Fatal("target should still exist")
	}
}

func TestUndoSymlinkOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	os.WriteFile(path, []byte("real file"), 0o644)

	r := Undo(Entry{Type: TypeSymlink, Path: path})
	if r.Action != "skipped" {
		t.Fatalf("expected skipped for non-symlink, got %s", r.Action)
	}
	// File should NOT be deleted — it's a real file, not our symlink.
	if _, err := os.Stat(path); err != nil {
		t.Fatal("real file should be preserved")
	}
}

func TestUndoConfigLine(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".bashrc")
	os.WriteFile(rc, []byte("# header\nalias rm='ws-trash-rm'\nexport PATH=$PATH\n"), 0o644)

	r := Undo(Entry{Type: TypeConfigLine, Path: rc, Line: "alias rm='ws-trash-rm'"})
	if r.Action != "removed" {
		t.Fatalf("expected removed, got %s: %s", r.Action, r.Message)
	}

	content, _ := os.ReadFile(rc)
	if got := string(content); got != "# header\nexport PATH=$PATH\n" {
		t.Fatalf("unexpected content after line removal:\n%s", got)
	}
}

func TestUndoConfigLineNotFound(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".bashrc")
	os.WriteFile(rc, []byte("# header\nexport PATH=$PATH\n"), 0o644)

	r := Undo(Entry{Type: TypeConfigLine, Path: rc, Line: "alias rm='ws-trash-rm'"})
	if r.Action != "skipped" {
		t.Fatalf("expected skipped, got %s", r.Action)
	}
}

func TestUndoGitExclude(t *testing.T) {
	dir := t.TempDir()
	exclude := filepath.Join(dir, "exclude")
	os.WriteFile(exclude, []byte("# local excludes\n.ws-context/\n"), 0o644)

	r := Undo(Entry{Type: TypeGitExclude, Path: exclude, Line: ".ws-context/"})
	if r.Action != "removed" {
		t.Fatalf("expected removed, got %s: %s", r.Action, r.Message)
	}

	content, _ := os.ReadFile(exclude)
	if got := string(content); got != "# local excludes\n" {
		t.Fatalf("unexpected content:\n%s", got)
	}
}
