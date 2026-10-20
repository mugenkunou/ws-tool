package scratch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewListDelete(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Scratch")

	res, err := New(NewOptions{RootDir: root, Name: "proxy-auth", SuffixMode: "auto"})
	if err != nil {
		t.Fatalf("new failed: %v", err)
	}
	if !res.Created {
		t.Fatal("expected created=true")
	}

	if err := os.WriteFile(filepath.Join(res.Path, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	list, err := List(ListOptions{RootDir: root, SortBy: "name"})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one scratch entry, got %d", len(list))
	}

	del, err := Delete(DeleteOptions{RootDir: root, Name: "proxy-auth"})
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if len(del.Removed) != 1 {
		t.Fatalf("expected one removed, got %+v", del)
	}
}

func TestNewDryRun(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Scratch")
	res, err := New(NewOptions{RootDir: root, Name: "x", SuffixMode: "auto", DryRun: true})
	if err != nil {
		t.Fatalf("new dry-run failed: %v", err)
	}
	if !res.DryRun {
		t.Fatal("expected dry-run result")
	}
	if _, err := os.Stat(res.Path); err == nil {
		t.Fatal("dry-run should not create directory")
	}
}
