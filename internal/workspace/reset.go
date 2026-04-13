package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/context"
	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/provision"
	"github.com/mugenkunou/ws-tool/internal/trash"
)

// ResetOptions configures a workspace reset operation.
type ResetOptions struct {
	WorkspacePath string
	ManifestPath  string // optional; defaults to <workspace>/ws/manifest.json
	DryRun        bool
}

// SubResetResult summarises one subsystem reset (dotfile, context, trash).
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
// (dotfile, context, trash) and then removing the ws/ directory.
//
// Pre-condition: the ws/ directory must exist; callers should verify this
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

	wsDir := filepath.Join(opts.WorkspacePath, "ws")

	if _, err := os.Stat(wsDir); err != nil {
		return result, fmt.Errorf("workspace not initialized (no ws/ at %s)", opts.WorkspacePath)
	}

	manifestPath := opts.ManifestPath
	if manifestPath == "" {
		manifestPath = filepath.Join(wsDir, "manifest.json")
	}

	// --- Phase 1: dotfile reset ---
	dfResult, err := dotfile.Reset(dotfile.ResetOptions{
		WorkspacePath: opts.WorkspacePath,
		ManifestPath:  manifestPath,
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

	// --- Phase 2: context remove all ---
	ctxResult, err := context.Remove(context.RemoveOptions{
		WorkspacePath: opts.WorkspacePath,
		All:           true,
		DryRun:        opts.DryRun,
	})
	sub = SubResetResult{Subsystem: "context"}
	if err != nil {
		sub.Errors = append(sub.Errors, err.Error())
		result.Errors = append(result.Errors, "context: "+err.Error())
	} else {
		for _, e := range ctxResult.Entries {
			if e.Action == "removed" || e.Action == "would-remove" {
				sub.Messages = append(sub.Messages, e.Action+": "+e.Path)
			}
		}
	}
	result.Subsystems = append(result.Subsystems, sub)

	// --- Phase 3: trash reset ---
	trashResult, err := trash.Reset(trash.ResetOptions{
		WorkspacePath: opts.WorkspacePath,
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

	// --- Phase 4: remove ws/ directory ---
	if opts.DryRun {
		result.WSRemoved = false
		return result, nil
	}

	if _, err := os.Stat(wsDir); err != nil {
		// Already gone (deleted by provision undo).
		result.WSRemoved = true
	} else if err := os.RemoveAll(wsDir); err != nil {
		result.WSRemoved = false
		result.Errors = append(result.Errors, fmt.Sprintf("failed to remove %s: %s", wsDir, err))
	} else {
		result.WSRemoved = true
	}

	return result, nil
}

// Provisions returns the loaded ledger entries for a workspace. This allows
// the CLI layer to render a pre-reset summary without duplicating the
// ledger-loading logic.
func Provisions(workspacePath string) ([]provision.Entry, error) {
	provPath := provision.LedgerPath(workspacePath)
	ledger, err := provision.Load(provPath)
	if err != nil {
		return nil, err
	}
	return ledger.Entries, nil
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
		return "remove line from " + filepath.Base(e.Path)
	case provision.TypeGitExclude:
		return "remove exclude entry"
	default:
		return "unknown"
	}
}
