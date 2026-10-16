package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/provision"
	"github.com/mugenkunou/ws-tool/internal/trash"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// ResetOptions configures a workspace reset operation.
type ResetOptions struct {
	WorkspacePath string
	DryRun        bool
	// ManifestPath is accepted for backward-compat callers; ignored in the new layout.
	ManifestPath string
}

// SubResetResult summarises one subsystem reset (dotfile, trash).
type SubResetResult struct {
	Subsystem string   `json:"subsystem"`
	Messages  []string `json:"messages"`
	Errors    []string `json:"errors,omitempty"`
}

// ResetResult describes the outcome of a full workspace reset.
type ResetResult struct {
	WorkspacePath string           `json:"workspace_path"`
	DryRun        bool             `json:"dry_run"`
	Subsystems    []SubResetResult `json:"subsystems"`
	WSRemoved     bool             `json:"ws_dir_removed"`
	Errors        []string         `json:"errors,omitempty"`
}

// Reset tears down a workspace by delegating to subsystem resets
// (dotfile, trash) and then removing the configs/ directory.
//
// Pre-condition: configs/ws.json must exist; callers should verify this
// before calling Reset so they can render an appropriate error.
func Reset(opts ResetOptions) (ResetResult, error) {
	result := ResetResult{
		WorkspacePath: opts.WorkspacePath,
		DryRun:        opts.DryRun,
		Subsystems:    []SubResetResult{},
	}

	if opts.WorkspacePath == "" {
		return result, fmt.Errorf("workspace path is required")
	}

	configPath := wsconfig.ConfigPath(opts.WorkspacePath)
	configsDir := filepath.Join(opts.WorkspacePath, "configs")

	if _, err := os.Stat(configsDir); err != nil {
		return result, fmt.Errorf("workspace not initialized (no configs/ at %s)", opts.WorkspacePath)
	}

	// --- Phase 1: dotfile reset ---
	dfResult, err := dotfile.Reset(dotfile.ResetOptions{
		WorkspacePath: opts.WorkspacePath,
		ConfigPath:    configPath,
		DryRun:        opts.DryRun,
	})
	sub := SubResetResult{Subsystem: "dotfile", Messages: dfResult.Messages}
	if err != nil {
		sub.Errors = append(sub.Errors, err.Error())
		result.Errors = append(result.Errors, "dotfile: "+err.Error())
	} else {
		for _, e := range dfResult.Entries {
			if e.Action == "failed" {
				sub.Errors = append(sub.Errors, e.Message)
				result.Errors = append(result.Errors, "dotfile: "+e.Message)
			}
		}
	}
	result.Subsystems = append(result.Subsystems, sub)

	// --- Phase 2: trash reset ---
	trashResult, err := trash.Reset(trash.ResetOptions{
		WorkspacePath: opts.WorkspacePath,
		ConfigPath:    configPath,
		DryRun:        opts.DryRun,
	})
	sub = SubResetResult{Subsystem: "trash", Messages: trashResult.Messages}
	if err != nil {
		sub.Errors = append(sub.Errors, err.Error())
		result.Errors = append(result.Errors, "trash: "+err.Error())
	} else {
		for _, e := range trashResult.Entries {
			if e.Action == "failed" {
				sub.Errors = append(sub.Errors, e.Message)
				result.Errors = append(result.Errors, "trash: "+e.Message)
			}
		}
	}
	result.Subsystems = append(result.Subsystems, sub)

	// --- Phase 3: remove configs/ directory ---
	if opts.DryRun {
		result.WSRemoved = false
		return result, nil
	}

	if _, err := os.Stat(configsDir); err != nil {
		result.WSRemoved = true
	} else if err := os.RemoveAll(configsDir); err != nil {
		result.WSRemoved = false
		result.Errors = append(result.Errors, fmt.Sprintf("failed to remove %s: %s", configsDir, err))
	} else {
		result.WSRemoved = true
	}

	return result, nil
}

// Provisions returns any symlink entries recorded in the link registry.
// This allows the CLI layer to render a pre-reset summary.
func Provisions(workspacePath string) ([]provision.Entry, error) {
	configPath := wsconfig.ConfigPath(workspacePath)
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []provision.Entry{}, nil
		}
		return nil, err
	}
	entries := make([]provision.Entry, 0, len(cfg.Links))
	for _, l := range cfg.Links {
		t := provision.TypeSymlink
		entries = append(entries, provision.Entry{
			Type:    t,
			Path:    l.System,
			Command: "dotfile add",
		})
	}
	return entries, nil
}

// UndoActionLabel returns a human-readable description of what undoing
// an entry will do. Exported so the cmd layer can use it for summaries.
func UndoActionLabel(e provision.Entry) string {
	return undoActionLabel(e)
}

func undoActionLabel(e provision.Entry) string {
	switch e.Type {
	case provision.TypeFile:
		return "delete file"
	case provision.TypeDir:
		return "delete directory"
	case provision.TypeSymlink:
		return "remove symlink"
	case provision.TypeConfigLine:
		return "remove ws line" // e.Path (absolute) is shown alongside
	case provision.TypeGitExclude:
		return "remove exclude entry"
	default:
		return "unknown"
	}
}
