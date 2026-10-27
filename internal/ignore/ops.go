package ignore

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// NormalizeRel turns a workspace-relative path into the form rules and the
// muted list use: slash separators, no "./", no trailing slash. The workspace
// itself is "".
func NormalizeRel(rel string) string {
	rel = filepath.ToSlash(filepath.Clean(strings.TrimSpace(rel)))
	rel = strings.TrimPrefix(rel, "./")
	if rel == "." {
		return ""
	}
	return rel
}

// NormalizeMuted resolves ignore.muted entries to workspace-relative slash
// form. An entry may be hand-written absolute or with ~; one outside the
// workspace, or the workspace itself, is dropped.
func NormalizeMuted(workspacePath string, entries []string) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if strings.HasPrefix(e, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				e = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(e, "~"), "/"))
			}
		}
		if filepath.IsAbs(e) {
			rel, err := filepath.Rel(workspacePath, e)
			if err != nil {
				continue
			}
			e = rel
		}
		n := NormalizeRel(e)
		if n == "" || n == ".." || strings.HasPrefix(n, "../") {
			continue
		}
		out = append(out, n)
	}
	return out
}

// MutedBy reports the ignore.muted entry (normalized) that mutes rel: rel
// itself or a parent directory of it.
func MutedBy(muted []string, rel string) (string, bool) {
	rel = NormalizeRel(rel)
	if rel == "" {
		return "", false
	}
	best, found := "", false
	for _, m := range muted {
		if m == "" {
			continue
		}
		if rel == m || strings.HasPrefix(rel, m+"/") {
			// Prefer the widest entry: it is what the user must unmute.
			if !found || len(m) < len(best) {
				best, found = m, true
			}
		}
	}
	return best, found
}

// IgnoredPath is one file that does not sync, and the rule that keeps it out.
type IgnoredPath struct {
	Path       string `json:"path"` // workspace-relative
	SizeBytes  int64  `json:"size_bytes"`
	Rule       string `json:"rule"`
	RuleSource string `json:"rule_source"`
}

// ListIgnored walks the workspace and returns every file the engine excludes,
// sorted by path. Files inside an ignored directory are listed too. under
// restricts the walk to these absolute paths (empty: the whole workspace).
func ListIgnored(engine *Engine, workspacePath string, under []string) []IgnoredPath {
	rows := make([]IgnoredPath, 0)
	roots := under
	if len(roots) == 0 {
		roots = []string{workspacePath}
	}
	seen := map[string]bool{}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() || path == workspacePath {
				return nil
			}
			rel, err := filepath.Rel(workspacePath, path)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, "../") || seen[rel] {
				return nil
			}
			res := engine.Evaluate(rel, false)
			if res.Included {
				return nil
			}
			seen[rel] = true
			var size int64
			if info, err := d.Info(); err == nil {
				size = info.Size()
			}
			rows = append(rows, IgnoredPath{Path: rel, SizeBytes: size, Rule: res.Rule, RuleSource: res.RuleSource})
			return nil
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })
	return rows
}

// RmKind classifies a path for `ignore rm`.
type RmKind string

const (
	// RmExact: a user exclude entry equals the path; it can be removed.
	RmExact RmKind = "exact"
	// RmNotIgnored: nothing ignores the path.
	RmNotIgnored RmKind = "not-ignored"
	// RmWider: a user rule that is not the exact entry (a parent directory or
	// a pattern with *) ignores the path.
	RmWider RmKind = "wider"
	// RmBuiltin: a built-in rule ignores the path.
	RmBuiltin RmKind = "builtin"
)

// RmVerdict is the classification of one `ignore rm` target.
type RmVerdict struct {
	Kind RmKind
	// Rule is the rule that ignores the path: the one named for RmWider and
	// RmBuiltin; for RmExact the rule that still ignores it once the entry
	// is gone (empty when none does).
	Rule string
	// Entry is the ignore_rules.exclude pattern to remove (RmExact).
	Entry string
}

// ClassifyRm decides what `ignore rm` can do with rel (workspace-relative).
// isDir only matters for rules that end in a slash.
func ClassifyRm(rules UserRules, rel string, isDir bool) RmVerdict {
	rel = NormalizeRel(rel)
	for _, e := range rules.Exclude {
		p := strings.TrimSpace(e.Pattern)
		if p == "" || strings.Contains(p, "*") {
			continue
		}
		if NormalizeRel(p) == rel {
			rest := rules
			rest.Exclude = nil
			for _, o := range rules.Exclude {
				if o.Pattern != e.Pattern {
					rest.Exclude = append(rest.Exclude, o)
				}
			}
			res := BuildEngine(rest).Evaluate(rel, isDir)
			still := ""
			if !res.Included {
				still = res.Rule
			}
			return RmVerdict{Kind: RmExact, Entry: e.Pattern, Rule: still}
		}
	}
	res := BuildEngine(rules).Evaluate(rel, isDir)
	switch {
	case res.Included:
		return RmVerdict{Kind: RmNotIgnored}
	case res.RuleSource == SourceUser:
		return RmVerdict{Kind: RmWider, Rule: res.Rule}
	default:
		return RmVerdict{Kind: RmBuiltin, Rule: res.Rule}
	}
}

// MuteState says why a path's warnings are muted, if they are.
type MuteState struct {
	Muted bool
	// Entry is the ignore.muted entry (normalized) that mutes it, if one does.
	Entry string
	// Rule is the include rule that mutes it, when no entry does.
	Rule string
	// RuleSource is the source of Rule.
	RuleSource string
}

// MuteStateOf reports whether rel's warnings are muted: by an ignore.muted
// entry (itself or a parent) or by an included directory.
func MuteStateOf(engine *Engine, muted []string, rel string, isDir bool) MuteState {
	if e, ok := MutedBy(muted, rel); ok {
		return MuteState{Muted: true, Entry: e}
	}
	if engine != nil {
		if res := engine.Evaluate(rel, isDir); res.ByInclude {
			return MuteState{Muted: true, Rule: res.Rule, RuleSource: res.RuleSource}
		}
	}
	return MuteState{}
}

// StaleType is the Type of a Muted row that is not a warning: an ignore.muted
// entry whose path no longer exists.
const StaleType = "stale-mute"

// StaleMuted returns the ignore.muted entries (normalized, each once, in
// ws.json order) whose path no longer exists. Only "no such file" counts as
// missing: any other error (permissions, an I/O fault) leaves the entry alone,
// and so does a workspace that is itself unreadable, such as an unmounted drive.
func StaleMuted(workspacePath string, entries []string) []string {
	if _, err := os.Lstat(workspacePath); err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, rel := range NormalizeMuted(workspacePath, entries) {
		if seen[rel] {
			continue
		}
		seen[rel] = true
		_, err := os.Lstat(filepath.Join(workspacePath, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			out = append(out, rel)
		}
	}
	return out
}

// StaleViolations is StaleMuted as rows for the Muted view (`ignore ls --muted`).
// They are muted INFO rows of type StaleType; `ignore unmute` removes them.
func StaleViolations(workspacePath string, entries []string) []Violation {
	var out []Violation
	for _, rel := range StaleMuted(workspacePath, entries) {
		out = append(out, Violation{
			Group:    "Ignore",
			Type:     StaleType,
			Severity: "INFO",
			Path:     rel,
			Message:  "muted path no longer exists",
			Muted:    true,
		})
	}
	return out
}
