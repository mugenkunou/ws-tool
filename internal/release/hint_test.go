package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallSteps(t *testing.T) {
	got := strings.Join(InstallSteps("mugenkunou/agent-tmux", "agent-tmux", "/home/u/.local/bin/agent-tmux"), "\n")
	want := `curl -fL -o agent-tmux.download "https://github.com/mugenkunou/agent-tmux/releases/latest/download/agent-tmux-$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
chmod +x agent-tmux.download
mv agent-tmux.download /home/u/.local/bin/agent-tmux`
	if got != want {
		t.Errorf("steps:\n%s\nwant:\n%s", got, want)
	}

	if got := InstallSteps("o/r", "r", "/opt/my tools/r"); !strings.HasSuffix(got[2], "mv r.download '/opt/my tools/r'") {
		t.Errorf("a path with a space is quoted: %q", got[2])
	}
}

func TestInstallCommandsAreIndentedUnderAHeading(t *testing.T) {
	lines := InstallCommands("install", "o/r", "r", "/x/r")
	if lines[0] != "install:" || !strings.HasPrefix(lines[1], "  curl -fL -o r.download ") || lines[3] != "  mv r.download /x/r" {
		t.Errorf("lines: %q", lines)
	}
}

func TestWSUpgradeSteps(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ws")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}

	got := strings.Join(WSUpgradeSteps(exe), "\n")
	for _, w := range []string{
		"releases/latest/download/ws-$(uname -s",
		"chmod +x ws.download",
		"mv ws.download " + resolved,
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
	got = strings.Join(WSUpgradeSteps(exe), "\n")
	if !strings.Contains(got, "# or, from your ws-tool checkout: git pull && make install") || !strings.Contains(got, "curl -fL") {
		t.Errorf("make install copy offers both:\n%s", got)
	}

	link := filepath.Join(t.TempDir(), "ws")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(WSUpgradeSteps(link), "\n"); !strings.Contains(got, "mv ws.download "+resolved) {
		t.Errorf("a symlink resolves to the real binary:\n%s", got)
	}
}
