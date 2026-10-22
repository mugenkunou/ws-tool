package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveFileDirAndSymlink(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Move(file, filepath.Join(root, "b.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("the source must be gone")
	}

	dir := filepath.Join(root, "d")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "f"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Move(dir, filepath.Join(root, "e")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "e", "sub", "f")); string(b) != "y" {
		t.Fatal("the tree must move whole")
	}

	// A symlink moves as the link, not as its target.
	link := filepath.Join(root, "l")
	if err := os.Symlink("/nonexistent/target", link); err != nil {
		t.Fatal(err)
	}
	if err := Move(link, filepath.Join(root, "l2")); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(filepath.Join(root, "l2")); err != nil || got != "/nonexistent/target" {
		t.Fatalf("readlink = %q, %v", got, err)
	}
}

func TestCopyKeepsModesAndNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(filepath.Join(src, "bin"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "bin", "run"), []byte("#!/bin/sh\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("run", filepath.Join(src, "bin", "alias")); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "dst")
	if err := Copy(src, dst); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dst, "bin", "run"))
	if err != nil || fi.Mode().Perm() != 0o750 {
		t.Fatalf("mode = %v, %v", fi, err)
	}
	if got, _ := os.Readlink(filepath.Join(dst, "bin", "alias")); got != "run" {
		t.Fatalf("symlink copied as %q", got)
	}
	// An existing file is never overwritten.
	if err := Copy(filepath.Join(src, "bin", "run"), filepath.Join(dst, "bin", "run")); err == nil {
		t.Fatal("Copy must refuse to overwrite")
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "bin", "run")); string(b) != "#!/bin/sh\n" {
		t.Fatal("the existing file was changed")
	}
}
