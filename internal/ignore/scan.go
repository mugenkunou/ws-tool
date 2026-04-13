package ignore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/style"
)

type Violation struct {
	Group        string `json:"group"`
	Type         string `json:"type"`
	Severity     string `json:"severity"`
	Path         string `json:"path"`
	Message      string `json:"message"`
	SizeBytes    int64  `json:"size_bytes,omitempty"`
	Depth        int    `json:"depth,omitempty"`
	InSafeHarbor bool   `json:"in_safe_harbor,omitempty"`
}

type ScanOptions struct {
	WorkspacePath string
	WarnSizeMB    int
	CritSizeMB    int
	MaxDepth      int
	Engine        *Engine
}

func Scan(opts ScanOptions) ([]Violation, error) {
	violations := make([]Violation, 0)

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

		// Evaluate inclusion and safe harbor status in a single pass.
		inSafeHarbor := false
		if opts.Engine != nil {
			eval := opts.Engine.Evaluate(rel, d.IsDir())
			if !eval.Included {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			inSafeHarbor = eval.SafeHarbor
		}

		depth := countDepth(rel)
		if depth > opts.MaxDepth {
			sev := "WARNING"
			if inSafeHarbor {
				sev = "INFO"
			}
			violations = append(violations, Violation{
				Group:        "Ignore",
				Type:         "depth",
				Severity:     sev,
				Path:         rel,
				Message:      fmt.Sprintf("depth %d exceeds max %d", depth, opts.MaxDepth),
				Depth:        depth,
				InSafeHarbor: inSafeHarbor,
			})
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
			if inSafeHarbor {
				sev = "INFO"
			}
			violations = append(violations, Violation{
				Group:        "Ignore",
				Type:         "bloat",
				Severity:     sev,
				Path:         rel,
				Message:      fmt.Sprintf("%s exceeds critical threshold %s", style.HumanBytes(info.Size()), style.HumanBytes(int64(opts.CritSizeMB)*1024*1024)),
				SizeBytes:    info.Size(),
				InSafeHarbor: inSafeHarbor,
			})
		} else if opts.WarnSizeMB > 0 && info.Size() > int64(opts.WarnSizeMB)*1024*1024 {
			sev := "WARNING"
			if inSafeHarbor {
				sev = "INFO"
			}
			violations = append(violations, Violation{
				Group:        "Ignore",
				Type:         "bloat",
				Severity:     sev,
				Path:         rel,
				Message:      fmt.Sprintf("%s exceeds warning threshold %s", style.HumanBytes(info.Size()), style.HumanBytes(int64(opts.WarnSizeMB)*1024*1024)),
				SizeBytes:    info.Size(),
				InSafeHarbor: inSafeHarbor,
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
	if strings.HasPrefix(rel, "../") {
		return EvalResult{}, rel, nil
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
			if includePath(opts, candidate) && exists(candidate) {
				rel, _ := filepath.Rel(opts.WorkspacePath, candidate)
				violations = append(violations, Violation{
					Group:    "Ignore",
					Type:     "project-meta",
					Severity: "WARNING",
					Path:     filepath.ToSlash(rel),
					Message:  "Go project build output directory should be excluded",
				})
			}
		}

		if name == "package.json" {
			for _, dir := range []string{"dist", "build", "node_modules"} {
				candidate := filepath.Join(root, dir)
				if includePath(opts, candidate) && exists(candidate) {
					rel, _ := filepath.Rel(opts.WorkspacePath, candidate)
					violations = append(violations, Violation{
						Group:    "Ignore",
						Type:     "project-meta",
						Severity: "WARNING",
						Path:     filepath.ToSlash(rel),
						Message:  "Node project build artifact directory should be excluded",
					})
				}
			}
		}

		return nil
	})

	return dedupe(violations)
}

func includePath(opts ScanOptions, absPath string) bool {
	rel, err := filepath.Rel(opts.WorkspacePath, absPath)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "../") {
		return false
	}
	if opts.Engine == nil {
		return true
	}
	st, err := os.Stat(absPath)
	isDir := err == nil && st.IsDir()
	return opts.Engine.Evaluate(rel, isDir).Included
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
