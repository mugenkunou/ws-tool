package scratch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewListPrune(t *testing.T) {
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

	prune, err := Prune(PruneOptions{RootDir: root, All: true})
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}
	if len(prune.Removed) != 1 {
		t.Fatalf("expected one removed, got %+v", prune)
	}
}

func TestParseOlderThanAndDryRun(t *testing.T) {
	d, err := ParseOlderThan("30d")
	if err != nil {
		t.Fatalf("parse older-than failed: %v", err)
	}
	if d < 30*24*time.Hour {
		t.Fatalf("unexpected duration: %s", d)
	}

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
