package trash

import (
	"fmt"
	"os"
)

// ResetOptions configures a trash reset operation.
type ResetOptions struct {
	WorkspacePath string
	ConfigPath    string
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

// Reset undoes trash setup side-effects: removes the ws-trash-rm script,
// removes alias lines from shell rc files, and removes the trash state file.
// In the new layout, provision tracking is not done — this reads the state
// file to know whether trash was set up (via GetStatus) and cleans up.
func Reset(opts ResetOptions) (ResetResult, error) {
	result := ResetResult{
		Entries:  []ResetEntry{},
		Messages: []string{},
		DryRun:   opts.DryRun,
	}

	// Remove the trash state file.
	sp, err := stateFilePath()
	if err != nil {
		result.Messages = append(result.Messages, "No trash state found.")
		return result, nil
	}

	if _, statErr := os.Stat(sp); os.IsNotExist(statErr) {
		result.Messages = append(result.Messages, "No trash provisions found.")
		return result, nil
	}

	if opts.DryRun {
		result.Entries = append(result.Entries, ResetEntry{
			Path:    sp,
			Action:  "would-remove",
			Message: "would delete " + sp,
		})
		result.Messages = append(result.Messages, "Would undo 1 trash provision(s).")
		return result, nil
	}

	if err := os.Remove(sp); err != nil && !os.IsNotExist(err) {
		result.Entries = append(result.Entries, ResetEntry{
			Path:    sp,
			Action:  "failed",
			Message: fmt.Sprintf("failed to remove %s: %v", sp, err),
		})
		return result, nil
	}

	result.Entries = append(result.Entries, ResetEntry{
		Path:    sp,
		Action:  "removed",
		Message: "removed " + sp,
	})
	result.Messages = append(result.Messages, "Undid 1 trash provision(s).")
	return result, nil
}
