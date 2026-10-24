package workspace

import (
	"os"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// PathOverrides carries explicit path choices (e.g. from --workspace).
// Empty fields fall back to the standard resolution.
type PathOverrides struct {
	Workspace string
}

// Paths holds the resolved, absolute locations of a workspace's key files.
type Paths struct {
	Workspace string // absolute path to the workspace root
	Config    string // absolute path to ws.json
}

// ResolvePaths resolves the workspace and config paths without side effects:
// override → env → default, all absolute.
//
// Resolution:
//   - Workspace: --workspace flag → $WS_WORKSPACE → ~/Workspace
//   - Config: always <workspace>/ws.json
//
// This is a clean-break implementation: there is no fallback to the old
// XDG config (~/.config/ws-tool/config.json) or the old ws/manifest.json.
// The workspace root is only ever changed via the --workspace flag or the
// $WS_WORKSPACE environment variable; no config file stores it.
func ResolvePaths(o PathOverrides) (Paths, error) {
	// Expand any provided override paths.
	if o.Workspace != "" {
		abs, err := wsconfig.ExpandUserPath(o.Workspace)
		if err != nil {
			return Paths{}, err
		}
		o.Workspace = abs
	}

	// Resolve workspace root.
	workspacePath := o.Workspace
	if workspacePath == "" {
		workspacePath = os.Getenv("WS_WORKSPACE")
	}
	if workspacePath == "" {
		def, err := wsconfig.DefaultWorkspace()
		if err != nil {
			return Paths{}, err
		}
		workspacePath = def
	} else {
		abs, err := wsconfig.ExpandUserPath(workspacePath)
		if err != nil {
			return Paths{}, err
		}
		workspacePath = abs
	}

	// Resolve config path.
	configPath := wsconfig.ConfigPath(workspacePath)

	return Paths{Workspace: workspacePath, Config: configPath}, nil
}

// ConfigExists reports whether the config file exists at path.
func ConfigExists(configPath string) bool {
	info, err := os.Stat(configPath)
	return err == nil && !info.IsDir()
}
