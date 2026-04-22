package trash

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/provision"
)

// ResetOptions configures a trash reset operation.
type ResetOptions struct {
	WorkspacePath string
	DryRun        bool
}

// ResetEntry describes the outcome of resetting a single trash provision.
type ResetEntry struct {
	Path    string `json:"path"`
	Action  string `json:"action"` // "removed", "would-remove", "skipped", "failed"
	Message string `json:"message"`
}

// ResetResult describes the outcome of a full trash reset.
type ResetResult struct {
	Entries  []ResetEntry `json:"entries"`
	Messages []string     `json:"messages"`
	DryRun   bool         `json:"dry_run"`
}

// Reset undoes all trash setup side-effects: removes the ws-trash-rm script
// and removes alias lines from shell rc files. It reads the provision ledger
// to find what was created by "trash enable" (or legacy "trash setup").
func Reset(opts ResetOptions) (ResetResult, error) {
	result := ResetResult{
		Entries:  []ResetEntry{},
		Messages: []string{},
		DryRun:   opts.DryRun,
	}

	provPath := provision.LedgerPath(opts.WorkspacePath)
	ledger, err := provision.Load(provPath)
	if err != nil {
		return result, fmt.Errorf("loading provisions: %w", err)
	}

	// Collect all trash-related provisions.
	var trashEntries []provision.Entry
	for _, e := range ledger.Entries {
		if e.Command == "trash enable" || e.Command == "trash setup" {
			trashEntries = append(trashEntries, e)
		}
	}

	if len(trashEntries) == 0 {
		result.Messages = append(result.Messages, "No trash provisions found.")
		return result, nil
	}

	for _, e := range trashEntries {
		entry := ResetEntry{Path: e.Path}

		if opts.DryRun {
			entry.Action = "would-remove"
			switch e.Type {
			case provision.TypeFile:
				entry.Message = "would delete " + e.Path
			case provision.TypeConfigLine:
				entry.Message = fmt.Sprintf("would remove %q from %s", e.Line, filepath.Base(e.Path))
			default:
				entry.Message = "would undo " + string(e.Type)
			}
			result.Entries = append(result.Entries, entry)
			continue
		}

		ur := provision.Undo(e)
		_ = provision.Remove(provPath, e.Type, e.Path)

		switch ur.Action {
		case "removed":
			entry.Action = "removed"
			entry.Message = ur.Message
		case "skipped":
			entry.Action = "skipped"
			entry.Message = ur.Message
		default:
			entry.Action = ur.Action
			entry.Message = ur.Message
		}
		result.Entries = append(result.Entries, entry)
	}

	// Remove the trash state file.
	if !opts.DryRun {
		if sp, err := stateFilePath(); err == nil {
			if _, err := os.Stat(sp); err == nil {
				os.Remove(sp)
			}
		}
	}

	removed := 0
	for _, e := range result.Entries {
		if e.Action == "removed" || e.Action == "would-remove" {
			removed++
		}
	}

	if opts.DryRun {
		result.Messages = append(result.Messages, fmt.Sprintf("Would undo %d trash provision(s).", removed))
	} else if removed > 0 {
		result.Messages = append(result.Messages, fmt.Sprintf("Undid %d trash provision(s).", removed))
	}

	return result, nil
}
