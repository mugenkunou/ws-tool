package dotfile

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/provision"
)

// ResetOptions configures a dotfile reset operation.
type ResetOptions struct {
	WorkspacePath string
	ManifestPath  string
	DryRun        bool
}

// ResetEntry describes the outcome of resetting a single dotfile.
type ResetEntry struct {
	Record  manifest.DotfileRecord `json:"record"`
	Action  string                 `json:"action"`  // "restored", "would-restore", "skipped", "failed"
	Message string                 `json:"message"`
}

// ResetResult describes the outcome of a full dotfile reset.
type ResetResult struct {
	Entries  []ResetEntry `json:"entries"`
	Messages []string     `json:"messages"`
	DryRun   bool         `json:"dry_run"`
}

// Reset undoes all managed dotfile symlinks: removes symlinks at system
// paths, restores the original files from the workspace, and clears dotfile
// records from the manifest. It also removes the matching symlink provisions
// from the ledger.
func Reset(opts ResetOptions) (ResetResult, error) {
	result := ResetResult{
		Entries:  []ResetEntry{},
		Messages: []string{},
		DryRun:   opts.DryRun,
	}

	m, err := manifest.Load(opts.ManifestPath)
	if err != nil {
		return result, err
	}

	if len(m.Dotfiles) == 0 {
		result.Messages = append(result.Messages, "No managed dotfiles.")
		return result, nil
	}

	provPath := provision.LedgerPath(opts.WorkspacePath)

	for _, record := range m.Dotfiles {
		entry := ResetEntry{Record: record}

		workspaceAbs := filepath.Join(opts.WorkspacePath, filepath.FromSlash(DotfilePath(record.Name)))

		if opts.DryRun {
			entry.Action = "would-restore"
			entry.Message = fmt.Sprintf("would remove symlink %s and restore from %s", record.System, workspaceAbs)
			result.Entries = append(result.Entries, entry)
			continue
		}

		// Check if workspace copy exists to restore from.
		if _, err := os.Stat(workspaceAbs); err != nil {
			entry.Action = "failed"
			entry.Message = fmt.Sprintf("workspace copy missing: %s", workspaceAbs)
			result.Entries = append(result.Entries, entry)
			continue
		}

		// Remove the symlink (or whatever is at the system path now).
		if _, err := os.Lstat(record.System); err == nil {
			if err := os.RemoveAll(record.System); err != nil {
				entry.Action = "failed"
				entry.Message = fmt.Sprintf("failed to remove %s: %s", record.System, err)
				result.Entries = append(result.Entries, entry)
				continue
			}
		}

		// Restore original file from workspace → system path.
		if err := os.MkdirAll(filepath.Dir(record.System), 0o755); err != nil {
			entry.Action = "failed"
			entry.Message = err.Error()
			result.Entries = append(result.Entries, entry)
			continue
		}

		if err := movePath(workspaceAbs, record.System); err != nil {
			entry.Action = "failed"
			entry.Message = fmt.Sprintf("failed to restore %s: %s", record.System, err)
			result.Entries = append(result.Entries, entry)
			continue
		}

		// Remove symlink provision.
		_ = provision.Remove(provPath, provision.TypeSymlink, record.System)

		entry.Action = "restored"
		entry.Message = fmt.Sprintf("restored %s", record.System)
		result.Entries = append(result.Entries, entry)
	}

	// Clear dotfile records from manifest (keep only failed ones).
	if !opts.DryRun {
		var remaining []manifest.DotfileRecord
		for _, entry := range result.Entries {
			if entry.Action == "failed" {
				remaining = append(remaining, entry.Record)
			}
		}
		if remaining == nil {
			remaining = []manifest.DotfileRecord{}
		}
		m.Dotfiles = remaining
		if err := manifest.Save(opts.ManifestPath, m); err != nil {
			return result, fmt.Errorf("saving manifest: %w", err)
		}
	}

	restored := 0
	failed := 0
	for _, e := range result.Entries {
		switch e.Action {
		case "restored":
			restored++
		case "would-restore":
			restored++
		case "failed":
			failed++
		}
	}

	if opts.DryRun {
		result.Messages = append(result.Messages, fmt.Sprintf("Would restore %d dotfile(s).", restored))
	} else {
		if restored > 0 {
			result.Messages = append(result.Messages, fmt.Sprintf("Restored %d dotfile(s).", restored))
		}
		if failed > 0 {
			result.Messages = append(result.Messages, fmt.Sprintf("%d dotfile(s) failed to restore.", failed))
		}
	}

	return result, nil
}
