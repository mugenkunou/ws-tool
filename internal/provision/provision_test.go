package provision

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provisions.json")

	l, err := Load(path)
	if err != nil {
		t.Fatalf("Load non-existent: %v", err)
	}
	if len(l.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(l.Entries))
	}
	if l.Schema != CurrentSchema {
		t.Fatalf("expected schema %d, got %d", CurrentSchema, l.Schema)
	}
}

func TestRecordAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ws", "provisions.json")

	e := Entry{Type: TypeFile, Path: "/tmp/test.txt", Command: "init"}
	if err := Record(path, e); err != nil {
		t.Fatalf("Record: %v", err)
	}

	l, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(l.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(l.Entries))
	}
	if l.Entries[0].Path != "/tmp/test.txt" {
		t.Fatalf("unexpected path: %s", l.Entries[0].Path)
	}
	if l.Entries[0].Time == "" {
		t.Fatal("expected timestamp to be set")
	}
}

func TestRecordDedup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provisions.json")

	e1 := Entry{Type: TypeFile, Path: "/a", Command: "init"}
	e2 := Entry{Type: TypeFile, Path: "/a", Command: "generate"}

	if err := Record(path, e1); err != nil {
		t.Fatal(err)
	}
	if err := Record(path, e2); err != nil {
		t.Fatal(err)
	}

	l, _ := Load(path)
	if len(l.Entries) != 1 {
		t.Fatalf("expected dedup to 1, got %d", len(l.Entries))
	}
	if l.Entries[0].Command != "generate" {
		t.Fatalf("expected updated command, got %s", l.Entries[0].Command)
	}
}

func TestRecordDifferentTypes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provisions.json")

	e1 := Entry{Type: TypeFile, Path: "/a", Command: "init"}
	e2 := Entry{Type: TypeDir, Path: "/a", Command: "init"}

	if err := Record(path, e1); err != nil {
		t.Fatal(err)
	}
	if err := Record(path, e2); err != nil {
		t.Fatal(err)
	}

	l, _ := Load(path)
	if len(l.Entries) != 2 {
		t.Fatalf("different types should not dedup, got %d entries", len(l.Entries))
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provisions.json")

	Record(path, Entry{Type: TypeSymlink, Path: "/a", Command: "dotfile add"})
	Record(path, Entry{Type: TypeSymlink, Path: "/b", Command: "dotfile add"})

	if err := Remove(path, TypeSymlink, "/a"); err != nil {
		t.Fatal(err)
	}

	l, _ := Load(path)
	if len(l.Entries) != 1 {
		t.Fatalf("expected 1 after remove, got %d", len(l.Entries))
	}
	if l.Entries[0].Path != "/b" {
		t.Fatalf("wrong entry survived: %s", l.Entries[0].Path)
	}
}

func TestRecordAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provisions.json")

	entries := []Entry{
		{Type: TypeFile, Path: "/a", Command: "init"},
		{Type: TypeDir, Path: "/b", Command: "init"},
		{Type: TypeFile, Path: "/c", Command: "init"},
	}
	if err := RecordAll(path, entries); err != nil {
		t.Fatal(err)
	}

	l, _ := Load(path)
	if len(l.Entries) != 3 {
		t.Fatalf("expected 3, got %d", len(l.Entries))
	}
}

func TestReversed(t *testing.T) {
	entries := []Entry{
		{Path: "/a"},
		{Path: "/b"},
		{Path: "/c"},
	}
	rev := Reversed(entries)
	if rev[0].Path != "/c" || rev[1].Path != "/b" || rev[2].Path != "/a" {
		t.Fatalf("unexpected order: %v", rev)
	}
	// Original unchanged.
	if entries[0].Path != "/a" {
		t.Fatal("original mutated")
	}
}

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provisions.json")

	l := Ledger{
		Schema: CurrentSchema,
		Entries: []Entry{
			{Type: TypeConfigLine, Path: "/home/u/.bashrc", Line: "alias rm='ws-trash-rm'", Command: "trash setup"},
		},
	}
	if err := Save(path, l); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1, got %d", len(loaded.Entries))
	}
	if loaded.Entries[0].Line != "alias rm='ws-trash-rm'" {
		t.Fatalf("line mismatch: %s", loaded.Entries[0].Line)
	}
}

func TestLedgerPath(t *testing.T) {
	got := LedgerPath("/home/u/Workspace")
	want := filepath.Join("/home/u/Workspace", "ws", "provisions.json")
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestRemoveNonExistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provisions.json")

	// Remove from non-existent file should not error (creates empty ledger).
	if err := Remove(path, TypeFile, "/x"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatal("expected file to be created")
	}
}
