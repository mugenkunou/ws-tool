package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func target(t *testing.T, goos, goarch string) {
	t.Helper()
	oldOS, oldArch := hintOS, hintArch
	hintOS, hintArch = goos, goarch
	t.Cleanup(func() { hintOS, hintArch = oldOS, oldArch })
}

func TestInstallCommands(t *testing.T) {
	target(t, "linux", "amd64")
	got := strings.Join(InstallCommands("install", "mugenkunou/agent-tmux", "agent-tmux", "/home/u/.local/bin/agent-tmux"), "\n")
	want := `install (linux amd64):
  curl -fLO https://github.com/mugenkunou/agent-tmux/releases/latest/download/agent-tmux-linux-amd64
  chmod +x agent-tmux-linux-amd64
  mv agent-tmux-linux-amd64 /home/u/.local/bin/agent-tmux`
	if got != want {
		t.Errorf("commands:\n%s\nwant:\n%s", got, want)
	}

	target(t, "darwin", "arm64")
	got = strings.Join(InstallCommands("install", "mugenkunou/agent-tmux", "agent-tmux", "/x"), "\n")
	if strings.Contains(got, "curl") || !strings.Contains(got, "github.com/mugenkunou/agent-tmux/releases/latest") {
		t.Errorf("other systems get the releases page only: %q", got)
	}
}

func TestWSUpgradeLines(t *testing.T) {
	target(t, "linux", "amd64")
	dir := t.TempDir()
	exe := filepath.Join(dir, "ws")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}

	got := strings.Join(WSUpgradeLines(exe), "\n")
	for _, w := range []string{
		"curl -fLO https://github.com/mugenkunou/ws-tool/releases/latest/download/ws-linux-amd64",
		"chmod +x ws-linux-amd64",
		"mv ws-linux-amd64 " + resolved,
	} {
		if !strings.Contains(got, w) {
			t.Errorf("release copy lacks %q:\n%s", w, got)
		}
	}
	if strings.Contains(got, "make install") {
		t.Errorf("release copy offers make install:\n%s", got)
	}

	if err := os.WriteFile(filepath.Join(dir, ".ws-source"), []byte("make\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = strings.Join(WSUpgradeLines(exe), "\n")
	if !strings.Contains(got, "git pull && make install") || !strings.Contains(got, "curl -fLO") {
		t.Errorf("make install copy offers both:\n%s", got)
	}

	link := filepath.Join(t.TempDir(), "ws")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(WSUpgradeLines(link), "\n"); !strings.Contains(got, "make install") || !strings.Contains(got, "mv ws-linux-amd64 "+resolved) {
		t.Errorf("a symlink resolves to the real binary:\n%s", got)
	}
}
