package ignore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/style"
)

type Violation struct {
	Group     string `json:"group"`
	Type      string `json:"type"`
	Severity  string `json:"severity"`
	Path      string `json:"path"`
	Message   string `json:"message"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	Depth     int    `json:"depth,omitempty"`
	Muted     bool   `json:"muted,omitempty"`
}

type ScanOptions struct {
	WorkspacePath string
	WarnSizeMB    int
	CritSizeMB    int
	MaxDepth      int
	Engine        *Engine
	// Muted is ws.json ignore.muted: paths whose warnings are hidden. Entries
	// are workspace-relative (see NormalizeMuted).
	Muted []string
}

func Scan(opts ScanOptions) ([]Violation, error) {
	violations := make([]Violation, 0)
	opts.Muted = NormalizeMuted(opts.WorkspacePath, opts.Muted)

	err := filepath.WalkDir(opts.WorkspacePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Skip entries that vanish, have broken symlinks, or are
			// unreadable. A scan must never abort on a single bad entry.
			return nil
		}

		if path == opts.WorkspacePath {
			return nil
		}

		rel, err := filepath.Rel(opts.WorkspacePath, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		// Evaluate inclusion and muted status in a single pass. A warning is
		// muted when the path lies under an included directory or under an
		// ignore.muted entry.
		muted := false
		if opts.Engine != nil {
			eval := opts.Engine.Evaluate(rel, d.IsDir())
			if !eval.Included {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			muted = eval.ByInclude
		}
		if !muted {
			_, muted = MutedBy(opts.Muted, rel)
		}

		depth := countDepth(rel)
		if depth > opts.MaxDepth {
			sev := "WARNING"
			if muted {
				sev = "INFO"
			}
			violations = append(violations, Violation{
				Group:    "Ignore",
				Type:     "depth",
				Severity: sev,
				Path:     rel,
				Message:  fmt.Sprintf("depth %d exceeds max %d", depth, opts.MaxDepth),
				Depth:    depth,
				Muted:    muted,
			})
		}

		// A synced symlink never arrives: .megaignore ends with -s:*, and
		// other clients (WorkDrive, rclone by default, Windows) drop links too.
		// WalkDir does not follow links, so this entry is the link itself.
		if d.Type()&os.ModeSymlink != 0 {
			sev := "WARNING"
			if muted {
				sev = "INFO"
			}
			target, _ := os.Readlink(path)
			violations = append(violations, Violation{
				Group:    "Ignore",
				Type:     "symlink",
				Severity: sev,
				Path:     rel,
				Message:  fmt.Sprintf("symlink to %s does not sync; replace it with a real file or folder, or ignore it", target),
				Muted:    muted,
			})
			return nil
		}

		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil // file may have vanished; skip
		}
		if opts.CritSizeMB > 0 && info.Size() > int64(opts.CritSizeMB)*1024*1024 {
			sev := "CRITICAL"
			if muted {
				sev = "INFO"
			}
			violations = append(violations, Violation{
				Group:     "Ignore",
				Type:      "bloat",
				Severity:  sev,
				Path:      rel,
				Message:   fmt.Sprintf("%s exceeds critical threshold %s", style.HumanBytes(info.Size()), style.HumanBytes(int64(opts.CritSizeMB)*1024*1024)),
				SizeBytes: info.Size(),
				Muted:     muted,
			})
		} else if opts.WarnSizeMB > 0 && info.Size() > int64(opts.WarnSizeMB)*1024*1024 {
			sev := "WARNING"
			if muted {
				sev = "INFO"
			}
			violations = append(violations, Violation{
				Group:     "Ignore",
				Type:      "bloat",
				Severity:  sev,
				Path:      rel,
				Message:   fmt.Sprintf("%s exceeds warning threshold %s", style.HumanBytes(info.Size()), style.HumanBytes(int64(opts.WarnSizeMB)*1024*1024)),
				SizeBytes: info.Size(),
				Muted:     muted,
			})
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	violations = append(violations, detectProjectMeta(opts)...)
	return violations, nil
}

func Check(engine *Engine, workspacePath, path string, isDir bool) (EvalResult, string, error) {
	rel, err := filepath.Rel(workspacePath, path)
	if err != nil {
		return EvalResult{}, "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return EvalResult{}, rel, fmt.Errorf("path is outside the workspace %s: %s", workspacePath, path)
	}
	if rel == "." {
		rel = ""
	}
	if engine == nil {
		return EvalResult{Included: true, Rule: "<default>"}, rel, nil
	}
	return engine.Evaluate(rel, isDir), rel, nil
}

func detectProjectMeta(opts ScanOptions) []Violation {
	violations := make([]Violation, 0)

	_ = filepath.WalkDir(opts.WorkspacePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}

		name := d.Name()
		if name != "go.mod" && name != "package.json" {
			return nil
		}

		root := filepath.Dir(path)
		if name == "go.mod" {
			candidate := filepath.Join(root, "bin")
			if ok, muted := includePath(opts, candidate); ok && exists(candidate) {
				rel, _ := filepath.Rel(opts.WorkspacePath, candidate)
				violations = append(violations, Violation{
					Group:    "Ignore",
					Type:     "project-meta",
					Severity: metaSeverity(muted),
					Path:     filepath.ToSlash(rel),
					Message:  "Go project build output directory should be excluded",
					Muted:    muted,
				})
			}
		}

		if name == "package.json" {
			for _, dir := range []string{"dist", "build", "node_modules"} {
				candidate := filepath.Join(root, dir)
				if ok, muted := includePath(opts, candidate); ok && exists(candidate) {
					rel, _ := filepath.Rel(opts.WorkspacePath, candidate)
					violations = append(violations, Violation{
						Group:    "Ignore",
						Type:     "project-meta",
						Severity: metaSeverity(muted),
						Path:     filepath.ToSlash(rel),
						Message:  "Node project build artifact directory should be excluded",
						Muted:    muted,
					})
				}
			}
		}

		return nil
	})

	return dedupe(violations)
}

// metaSeverity is a project-meta finding's severity: muted ones are INFO.
func metaSeverity(muted bool) string {
	if muted {
		return "INFO"
	}
	return "WARNING"
}

// includePath reports whether absPath syncs and whether its warnings are muted
// (under an included directory or an ignore.muted entry).
func includePath(opts ScanOptions, absPath string) (included, muted bool) {
	rel, err := filepath.Rel(opts.WorkspacePath, absPath)
	if err != nil {
		return false, false
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "../") {
		return false, false
	}
	if opts.Engine == nil {
		_, muted = MutedBy(opts.Muted, rel)
		return true, muted
	}
	st, err := os.Stat(absPath)
	isDir := err == nil && st.IsDir()
	eval := opts.Engine.Evaluate(rel, isDir)
	if !eval.Included {
		return false, false
	}
	muted = eval.ByInclude
	if !muted {
		_, muted = MutedBy(opts.Muted, rel)
	}
	return true, muted
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func countDepth(rel string) int {
	parts := strings.Split(strings.Trim(rel, "/"), "/")
	count := 0
	for _, p := range parts {
		if p != "" {
			count++
		}
	}
	return count
}

func dedupe(in []Violation) []Violation {
	seen := make(map[string]struct{}, len(in))
	out := make([]Violation, 0, len(in))
	for _, v := range in {
		k := v.Type + "|" + v.Path
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, v)
	}
	return out
}
