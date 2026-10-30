package release

import (
	"os"
	"path/filepath"
)

// WSReleasesPage is where release binaries of ws are published.
const WSReleasesPage = "https://github.com/mugenkunou/ws-tool/releases/latest"

// WSUpgradeLines says how to upgrade the ws binary at exe. `make install`
// leaves a .ws-source file beside the binary; its presence means a checkout
// owns the binary and rebuilding there replaces it. Any other copy (a release
// download) is left to the user.
func WSUpgradeLines(exe string) []string {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), ".ws-source")); err == nil {
		return []string{"installed by make install: in your ws-tool checkout run", "  git pull && make install"}
	}
	return []string{"download: " + WSReleasesPage}
}
