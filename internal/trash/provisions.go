package trash

import (
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/provision"
)

// RecordShellProvisions records the rm wrapper script and rc alias lines
// written by Setup with ShellRM, so `ws reset` can undo them.
func RecordShellProvisions(manifestPath string) error {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	_ = manifest.RecordProvision(manifestPath, provision.Entry{
		Type:    provision.TypeFile,
		Path:    filepath.Join(home, ".local", "bin", "ws-trash-rm"),
		Command: "trash enable",
	})
	for _, rc := range []string{filepath.Join(home, ".bashrc"), filepath.Join(home, ".zshrc")} {
		_ = manifest.RecordProvision(manifestPath, provision.Entry{
			Type:    provision.TypeConfigLine,
			Path:    rc,
			Line:    "alias rm='ws-trash-rm'",
			Command: "trash enable",
		})
	}
	return nil
}

// RecordExplorerProvision records the file-explorer trash symlink written by
// Setup with FileExplorer.
func RecordExplorerProvision(manifestPath, rootDir string) error {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	_ = manifest.RecordProvision(manifestPath, provision.Entry{
		Type:    provision.TypeSymlink,
		Path:    filepath.Join(home, ".local", "share", "Trash"),
		Target:  rootDir,
		Command: "trash enable",
	})
	return nil
}
