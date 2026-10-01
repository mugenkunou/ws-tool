package workspace

import (
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/config"
)

// PathOverrides carries explicit path choices (e.g. from --workspace,
// --config, --manifest). Empty fields fall back to the standard resolution.
type PathOverrides struct {
	Workspace string
	Config    string
	Manifest  string
}

// Paths are the resolved, absolute locations of a workspace's state.
type Paths struct {
	Workspace string
	Config    string
	Manifest  string
}

// ResolvePaths resolves the workspace, config, and manifest paths without
// side effects: override → env → config file → default, all absolute.
// It is shared by every front end (CLI, TUI) so they cannot drift.
func ResolvePaths(o PathOverrides) (Paths, error) {
	for _, v := range []*string{&o.Workspace, &o.Config, &o.Manifest} {
		if *v == "" {
			continue
		}
		abs, err := config.ExpandUserPath(*v)
		if err != nil {
			return Paths{}, err
		}
		*v = abs
	}

	configPath := o.Config
	if configPath == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return Paths{}, err
		}
		configPath = p
	}

	// Try to read workspace from config if not set via override/env.
	workspaceArg := o.Workspace
	if workspaceArg == "" {
		workspaceArg = os.Getenv("WS_WORKSPACE")
	}
	if workspaceArg == "" {
		if cfg, err := config.Load(configPath); err == nil && cfg.Workspace != "" {
			workspaceArg = cfg.Workspace
		}
	}
	if workspaceArg == "" {
		workspaceArg = "~/Workspace"
	}

	workspacePath, err := config.ExpandUserPath(workspaceArg)
	if err != nil {
		return Paths{}, err
	}

	manifestPath := o.Manifest
	if manifestPath == "" {
		manifestPath = filepath.Join(workspacePath, "ws", "manifest.json")
	}

	// Migration: if XDG config doesn't exist, fall back to old workspace-embedded config.
	if o.Config == "" && !ConfigExists(configPath) {
		oldPath := filepath.Join(workspacePath, "ws", "config.json")
		if ConfigExists(oldPath) {
			configPath = oldPath
		}
	}
	return Paths{Workspace: workspacePath, Config: configPath, Manifest: manifestPath}, nil
}
