package dotfile

import (
	"fmt"

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
	Action  string        `json:"action"` // "restored", "would-restore", "failed"
	Message string        `json:"message"`
}

// ResetResult describes the outcome of a full dotfile reset.
type ResetResult struct {
	Entries  []ResetEntry `json:"entries"`
	Messages []string     `json:"messages"`
	DryRun   bool         `json:"dry_run"`
}

// Reset unlinks every managed dotfile. It is `ws dotfile unlink --all` as one
// call, kept for the workspace reset and the dashboard until they go; each
// entry goes through Unlink, so a shared copy is copied rather than moved and a
// live file that replaced a link is kept as .ws-backup.
func Reset(opts ResetOptions) (ResetResult, error) {
	result := ResetResult{Entries: []ResetEntry{}, Messages: []string{}, DryRun: opts.DryRun}

	cfg, err := wsconfig.Load(opts.ConfigPath)
	if err != nil {
		return result, err
	}
	if len(cfg.Links) == 0 {
		result.Messages = append(result.Messages, "No managed dotfiles.")
		return result, nil
	}

	restored, failed := 0, 0
	for _, link := range cfg.Links {
		entry := ResetEntry{Link: link}
		switch {
		case opts.DryRun:
			entry.Action = "would-restore"
			entry.Message = fmt.Sprintf("would unlink %s", link.System)
			restored++
		default:
			if err := Unlink(UnlinkOptions{WorkspacePath: opts.WorkspacePath, ConfigPath: opts.ConfigPath, SystemPath: link.System}); err != nil {
				entry.Action = "failed"
				entry.Message = err.Error()
				failed++
			} else {
				entry.Action = "restored"
				entry.Message = fmt.Sprintf("restored %s", link.System)
				restored++
			}
		}
		result.Entries = append(result.Entries, entry)
	}

	if opts.DryRun {
		result.Messages = append(result.Messages, fmt.Sprintf("Would restore %d dotfile(s).", restored))
		return result, nil
	}
	if restored > 0 {
		result.Messages = append(result.Messages, fmt.Sprintf("Restored %d dotfile(s).", restored))
	}
	if failed > 0 {
		result.Messages = append(result.Messages, fmt.Sprintf("%d dotfile(s) failed to restore.", failed))
	}
	return result, nil
}
