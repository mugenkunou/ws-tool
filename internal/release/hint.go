package release

import (
	"os"
	"path/filepath"
	"strings"
)

// WSReleasesPage is where release binaries of ws are published.
const WSReleasesPage = "https://github.com/mugenkunou/ws-tool/releases/latest"

// platform is the release asset suffix for the machine the commands run on,
// worked out by the shell: linux/darwin from uname -s, amd64/arm64 from uname -m.
const platform = `$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')`

// shellQuote leaves ordinary paths alone and single-quotes the rest.
func shellQuote(s string) string {
	if !strings.ContainsAny(s, " \t'\"$`\\&;|<>()*?[]{}!#") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// InstallSteps are the exact shell commands that put the latest release of a
// project at dest. Every release ships raw binaries named <asset>-<os>-<arch>
// without a version, so one URL pattern serves the newest, and the shell fills
// in the platform: the same three commands work on any distro or CPU the
// project builds for. The download keeps a fixed local name so chmod and mv
// need no substitution. Lines starting with "#" are notes, not commands.
func InstallSteps(repo, asset, dest string) []string {
	tmp := asset + ".download"
	return []string{
		"curl -fL -o " + tmp + ` "https://github.com/` + repo + "/releases/latest/download/" + asset + "-" + platform + `"`,
		"chmod +x " + tmp,
		"mv " + tmp + " " + shellQuote(dest),
	}
}

// InstallCommands is InstallSteps as plain indented lines under a heading, for
// messages that are not styled (the session error, the TUI).
func InstallCommands(verb, repo, asset, dest string) []string {
	lines := []string{verb + ":"}
	for _, l := range InstallSteps(repo, asset, dest) {
		lines = append(lines, "  "+strings.TrimPrefix(l, "# "))
	}
	return lines
}

// WSUpgradeSteps says how to upgrade the ws binary at exe. `make install`
// leaves a .ws-source file beside the binary; its presence means a checkout
// owns the binary, so rebuilding there is offered as the alternative.
func WSUpgradeSteps(exe string) []string {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	steps := InstallSteps("mugenkunou/ws-tool", "ws", exe)
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), ".ws-source")); err == nil {
		steps = append(steps, "# or, from your ws-tool checkout: git pull && make install")
	}
	return steps
}

// WSUpgradeLines is WSUpgradeSteps as plain lines for the TUI.
func WSUpgradeLines(exe string) []string {
	lines := []string{"upgrade:"}
	for _, l := range WSUpgradeSteps(exe) {
		lines = append(lines, "  "+strings.TrimPrefix(l, "# "))
	}
	return lines
}
