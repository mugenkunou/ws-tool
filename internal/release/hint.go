package release

import (
	"os"
	"path/filepath"
	"runtime"
)

// WSReleasesPage is where release binaries of ws are published.
const WSReleasesPage = "https://github.com/mugenkunou/ws-tool/releases/latest"

// hintOS and hintArch are what the install commands target; tests replace them.
var (
	hintOS   = runtime.GOOS
	hintArch = runtime.GOARCH
)

// InstallCommands are the exact commands that put the latest release of a
// project at dest. Every release ships raw binaries named <asset>-<os>-<arch>
// without a version, so one URL always serves the newest. Only linux/amd64 is
// spelled out; any other system gets the releases page.
func InstallCommands(verb, repo, asset, dest string) []string {
	page := "https://github.com/" + repo + "/releases/latest"
	if hintOS != "linux" || hintArch != "amd64" {
		return []string{verb + ": download the binary for your system from " + page}
	}
	name := asset + "-linux-amd64"
	return []string{
		verb + " (linux amd64):",
		"  curl -fLO " + page + "/download/" + name,
		"  chmod +x " + name,
		"  mv " + name + " " + dest,
	}
}

// WSUpgradeLines says how to upgrade the ws binary at exe. `make install`
// leaves a .ws-source file beside the binary; its presence means a checkout
// owns the binary, so rebuilding there is offered as the alternative.
func WSUpgradeLines(exe string) []string {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	lines := InstallCommands("upgrade", "mugenkunou/ws-tool", "ws", exe)
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), ".ws-source")); err == nil {
		lines = append(lines, "or, installed by make install: in your ws-tool checkout run", "  git pull && make install")
	}
	return lines
}
