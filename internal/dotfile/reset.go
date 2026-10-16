package dotfile

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// ResetOptions configures a dotfile reset operation.
type ResetOptions struct {
	WorkspacePath string
	ConfigPath    string
	DryRun        bool
}

// ResetEntry describes the outcome of resetting a single dotfile.
type ResetEntry struct {
	Link    wsconfig.Link `json:"link"`
	Action  string        `json:"action"` // "restored", "would-restore", "skipped", "failed"
	Message string        `json:"message"`
}

// ResetResult describes the outcome of a full dotfile reset.
type ResetResult struct {
	Entries  []ResetEntry `json:"entries"`
	Messages []string     `json:"messages"`
	DryRun   bool         `json:"dry_run"`
}

// Reset undoes all managed dotfile symlinks: removes symlinks at system
// paths, restores the original files from the workspace, and clears dotfile
// link records from ws.json.
func Reset(opts ResetOptions) (ResetResult, error) {
	result := ResetResult{
		Entries:  []ResetEntry{},
		Messages: []string{},
		DryRun:   opts.DryRun,
	}

	cfg, err := wsconfig.Load(opts.ConfigPath)
	if err != nil {
		return result, err
	}

	if len(cfg.Links) == 0 {
		result.Messages = append(result.Messages, "No managed dotfiles.")
		return result, nil
	}

	for _, link := range cfg.Links {
		entry := ResetEntry{Link: link}

		workspaceAbs := filepath.Join(opts.WorkspacePath, filepath.FromSlash(DotfilePath(link.Original, link.Class)))

		if opts.DryRun {
			entry.Action = "would-restore"
			entry.Message = fmt.Sprintf("would remove symlink %s and restore from %s", link.System, workspaceAbs)
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
		if _, err := os.Lstat(link.System); err == nil {
			var rmErr error
			if link.Sudo {
				rmErr = sudoRemoveAll(link.System)
			} else {
				rmErr = os.RemoveAll(link.System)
			}
			if rmErr != nil {
				entry.Action = "failed"
				entry.Message = fmt.Sprintf("failed to remove %s: %s", link.System, rmErr)
				result.Entries = append(result.Entries, entry)
				continue
			}
		}

		// Restore original file from workspace → system path.
		if link.Sudo {
			if mkErr := sudoMkdirAll(filepath.Dir(link.System)); mkErr != nil {
				entry.Action = "failed"
				entry.Message = mkErr.Error()
				result.Entries = append(result.Entries, entry)
				continue
			}
			if mvErr := sudoRename(workspaceAbs, link.System); mvErr != nil {
				entry.Action = "failed"
				entry.Message = fmt.Sprintf("failed to restore %s: %s", link.System, mvErr)
				result.Entries = append(result.Entries, entry)
				continue
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(link.System), 0o755); err != nil {
				entry.Action = "failed"
				entry.Message = err.Error()
				result.Entries = append(result.Entries, entry)
				continue
			}
			if err := movePath(workspaceAbs, link.System); err != nil {
				entry.Action = "failed"
				entry.Message = fmt.Sprintf("failed to restore %s: %s", link.System, err)
				result.Entries = append(result.Entries, entry)
				continue
			}
		}

		entry.Action = "restored"
		entry.Message = fmt.Sprintf("restored %s", link.System)
		result.Entries = append(result.Entries, entry)
	}

	// Clear link records from config (keep only failed ones).
	if !opts.DryRun {
		reloadedCfg, err := wsconfig.Load(opts.ConfigPath)
		if err != nil {
			return result, fmt.Errorf("reloading config: %w", err)
		}
		failedSystems := make(map[string]struct{})
		for _, e := range result.Entries {
			if e.Action == "failed" {
				failedSystems[e.Link.System] = struct{}{}
			}
		}
		remaining := reloadedCfg.Links[:0:0]
		for _, l := range reloadedCfg.Links {
			if _, isFailed := failedSystems[l.System]; isFailed {
				remaining = append(remaining, l)
			}
		}
		if remaining == nil {
			remaining = []wsconfig.Link{}
		}
		reloadedCfg.Links = remaining
		if err := wsconfig.Save(opts.ConfigPath, reloadedCfg); err != nil {
			return result, fmt.Errorf("saving config: %w", err)
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
