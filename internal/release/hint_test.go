package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWSUpgradeLines(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ws")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}

	got := strings.Join(WSUpgradeLines(exe), "\n")
	if !strings.Contains(got, WSReleasesPage) || strings.Contains(got, "make install") {
		t.Errorf("release copy: %q", got)
	}

	if err := os.WriteFile(filepath.Join(dir, ".ws-source"), []byte("make\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = strings.Join(WSUpgradeLines(exe), "\n")
	if !strings.Contains(got, "git pull && make install") || strings.Contains(got, WSReleasesPage) {
		t.Errorf("make install copy: %q", got)
	}

	link := filepath.Join(t.TempDir(), "ws")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(WSUpgradeLines(link), "\n"); !strings.Contains(got, "make install") {
		t.Errorf("symlink to make install copy: %q", got)
	}
}
