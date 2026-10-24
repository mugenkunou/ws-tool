package secret

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// Plan builders for the write commands of `ws secret`, shared by the CLI and
// the TUI so both show the same checklist.

// AllowActions is one action per finding, then, when the allowlist holds
// stale anchors, one that drops them (spec §5 secret allow).
func AllowActions(configPath, workspacePath string, findings []Violation, stale []string) []plan.Action {
	var actions []plan.Action
	for _, v := range findings {
		anchor := Anchor(v)
		actions = append(actions, plan.Action{
			ID:          "allow-" + AbsAnchor(workspacePath, v),
			Description: fmt.Sprintf("Allow %s (false positive)", AbsAnchor(workspacePath, v)),
			Execute:     func() error { return wsconfig.AddToAllowlist(configPath, anchor) },
		})
	}
	if len(stale) > 0 {
		stale := append([]string(nil), stale...)
		noun := "anchors"
		if len(stale) == 1 {
			noun = "anchor"
		}
		actions = append(actions, plan.Action{
			ID:          "drop-stale-anchors",
			Description: fmt.Sprintf("Drop %d stale %s from secret.allowlist", len(stale), noun),
			Execute:     func() error { return DropAnchors(configPath, stale) },
		})
	}
	return actions
}

// DropAnchors removes anchors from secret.allowlist.
func DropAnchors(configPath string, anchors []string) error {
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		return err
	}
	drop := make(map[string]bool, len(anchors))
	for _, a := range anchors {
		drop[a] = true
	}
	kept := make([]string, 0, len(cfg.Secret.Allowlist))
	for _, a := range cfg.Secret.Allowlist {
		if !drop[a] {
			kept = append(kept, a)
		}
	}
	cfg.Secret.Allowlist = kept
	return wsconfig.Save(configPath, cfg)
}

// MuteAction mutes the workspace-relative directory rel in secret scans.
func MuteAction(configPath, workspacePath, rel string) plan.Action {
	abs := filepath.Join(workspacePath, filepath.FromSlash(rel))
	return plan.Action{
		ID:          "mute-" + abs,
		Description: fmt.Sprintf("Mute %s in secret scans", abs),
		Execute:     func() error { return AddSkipDir(configPath, rel) },
	}
}

// UnmuteAction scans the workspace-relative directory rel again.
func UnmuteAction(configPath, workspacePath, rel string) plan.Action {
	abs := filepath.Join(workspacePath, filepath.FromSlash(rel))
	return plan.Action{
		ID:          "unmute-" + abs,
		Description: fmt.Sprintf("Unmute %s in secret scans", abs),
		Execute:     func() error { return RemoveSkipDir(configPath, workspacePath, rel) },
	}
}

// RemoveSkipDir removes every secret.skip_dirs entry that names rel, however it
// is written there (relative, absolute or `~`).
func RemoveSkipDir(configPath, workspacePath, rel string) error {
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		return err
	}
	kept := make([]string, 0, len(cfg.Secret.SkipDirs))
	for _, d := range cfg.Secret.SkipDirs {
		if r, ok := workspaceRel(workspacePath, strings.TrimSpace(d)); ok && r == rel {
			continue
		}
		kept = append(kept, d)
	}
	cfg.Secret.SkipDirs = kept
	return wsconfig.Save(configPath, cfg)
}

// MutedBy reports which secret.skip_dirs entry covers the workspace-relative
// directory rel: the covering directory (workspace-relative, however the entry
// is written in the config), and whether that is rel itself (exact) rather
// than a parent. ok is false when rel is not muted.
func MutedBy(workspacePath string, skipDirs []string, rel string) (by string, exact, ok bool) {
	for _, d := range skipDirs {
		r, valid := workspaceRel(workspacePath, strings.TrimSpace(d))
		if !valid || r == "." {
			continue
		}
		if r == rel {
			return r, true, true
		}
		if strings.HasPrefix(rel, r+"/") && !ok {
			by, ok = r, true
		}
	}
	return by, false, ok
}

// StaleAnchors returns the allowlist anchors that no pattern matches any more:
// a scan without the allowlist finds no finding at that line. An anchor whose
// file the scan does not read (a muted directory, an excluded file) is not
// stale, only out of sight, so it is kept.
func StaleAnchors(opts ScanOptions, allowlist []string) ([]string, error) {
	if len(allowlist) == 0 {
		return nil, nil
	}
	opts.Allowlist = nil
	all, err := Scan(opts)
	if err != nil {
		return nil, err
	}
	_, pruned := PruneAllowlist(allowlist, all)
	var stale []string
	for _, a := range pruned {
		i := strings.LastIndex(a, ":")
		if i <= 0 {
			stale = append(stale, a) // malformed: it never matches anything
			continue
		}
		if hiddenFromScan(opts, a[:i]) {
			continue
		}
		stale = append(stale, a)
	}
	return stale, nil
}

// hiddenFromScan reports whether a scan skips the file rel, because it or a
// parent directory is muted, or the ignore engine leaves it out.
func hiddenFromScan(opts ScanOptions, rel string) bool {
	if matchesSkipDir(rel, opts.SkipDirs) {
		return true
	}
	parts := strings.Split(rel, "/")
	for i := 1; i < len(parts); i++ {
		if matchesSkipDir(strings.Join(parts[:i], "/"), opts.SkipDirs) {
			return true
		}
	}
	return opts.Engine != nil && !opts.Engine.Evaluate(rel, false).Included
}

// StoreAction copies a finding's value into pass as entry, then allows the
// finding. It is one action, because allow must not run when the insert fails.
// An existing entry is never overwritten.
func StoreAction(configPath, workspacePath string, v Violation, entry string) plan.Action {
	anchor, abs := Anchor(v), AbsAnchor(workspacePath, v)
	value := ExtractSecretValue(v.Snippet)
	return plan.Action{
		ID:          "store-" + abs,
		Description: fmt.Sprintf("Store the value of %s in pass as %s, then allow it", abs, entry),
		Execute: func() error {
			if value == "" {
				return fmt.Errorf("no value to read from %s; store it with `pass insert %s`", abs, entry)
			}
			if err := InsertEntry(entry, value); err != nil {
				return err
			}
			return wsconfig.AddToAllowlist(configPath, anchor)
		},
	}
}
