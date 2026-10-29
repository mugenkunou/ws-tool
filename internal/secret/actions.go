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

// SkipAction stops scanning the workspace-relative directory rel entirely
// (secret.skip_dirs): nothing under it is read, so findings there never
// appear, even at a quiet severity. This is for genuinely noisy non-code
// trees (vendor dumps, archives) — a directory-level opt-out, distinct from
// muting a finding, which keeps it in sight. Named Skip (not Mute) so the two
// never get confused: see MuteActions for the anchor-level, stays-visible kind.
func SkipAction(configPath, workspacePath, rel string) plan.Action {
	abs := filepath.Join(workspacePath, filepath.FromSlash(rel))
	return plan.Action{
		ID:          "skip-" + abs,
		Description: fmt.Sprintf("Skip %s in secret scans", abs),
		Execute:     func() error { return AddSkipDir(configPath, rel) },
	}
}

// UnskipAction scans the workspace-relative directory rel again.
func UnskipAction(configPath, workspacePath, rel string) plan.Action {
	abs := filepath.Join(workspacePath, filepath.FromSlash(rel))
	return plan.Action{
		ID:          "unskip-" + abs,
		Description: fmt.Sprintf("Scan %s again", abs),
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

// SkippedBy reports which secret.skip_dirs entry covers the workspace-relative
// directory rel: the covering directory (workspace-relative, however the entry
// is written in the config), and whether that is rel itself (exact) rather
// than a parent. ok is false when rel is not skipped.
func SkippedBy(workspacePath string, skipDirs []string, rel string) (by string, exact, ok bool) {
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

// ── mute (per-finding, stays visible) ─────────────────────────────

// MuteActions mutes findings by anchor (secret.muted): unlike allow or skip,
// a muted finding is never dropped from `secret ls` — it keeps appearing, at
// a quieter severity (INFO). This is the volume knob for "I looked at this,
// it's fine, but it's not worth hiding the way a false positive is."
func MuteActions(configPath, workspacePath string, findings []Violation) []plan.Action {
	var actions []plan.Action
	for _, v := range findings {
		anchor := Anchor(v)
		actions = append(actions, plan.Action{
			ID:          "mute-" + AbsAnchor(workspacePath, v),
			Description: fmt.Sprintf("Mute %s (it keeps appearing, quietly)", AbsAnchor(workspacePath, v)),
			Execute:     func() error { return wsconfig.AddToMuted(configPath, anchor) },
		})
	}
	return actions
}

// UnmuteActions unmutes findings by anchor, letting them appear at their
// matched severity again.
func UnmuteActions(configPath, workspacePath string, findings []Violation) []plan.Action {
	var actions []plan.Action
	for _, v := range findings {
		anchor := Anchor(v)
		actions = append(actions, plan.Action{
			ID:          "unmute-" + AbsAnchor(workspacePath, v),
			Description: "Unmute " + AbsAnchor(workspacePath, v),
			Execute:     func() error { _, err := wsconfig.RemoveFromMuted(configPath, anchor); return err },
		})
	}
	return actions
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
