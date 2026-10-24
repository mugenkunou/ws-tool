package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// expandUserPath resolves a ~ or absolute or relative path.
// Thin wrapper over wsconfig.ExpandUserPath for this package.
func expandUserPath(path string) (string, error) {
	return wsconfig.ExpandUserPath(path)
}

// treeEntry holds one entry in the ignore tree walk.
type treeEntry struct {
	rel              string // path relative to workspace
	name             string // last path component
	isDir            bool
	status           string // "synced", "ignored", "partial"
	sizeBytes        int64  // file size for regular files
	rule             string // matched rule (or "<default>")
	relDepth         int    // depth relative to start dir (0 = direct children)
	excludedChildren int    // dirs: number of excluded immediate sub-trees
}

// runIgnoreTreeView implements ws ignore tree.
//
// Two-pass algorithm:
//  1. Walk the full subtree, collecting every entry with its individual status.
//  2. Propagate "partial" status upward: any directory with at least one
//     excluded descendant (that isn't itself inside an excluded sub-tree) is
//     marked ◐ partial.
//
// The tree is rendered up to maxDepth, with proper ├──/└── connectors, ✔/✗/◐
// status icons, and a summary line.
func runIgnoreTreeView(engine *ignore.Engine, workspacePath, pathFilter string, maxDepth int, globals globalFlags, stdout, stderr io.Writer) int {
	start := filepath.Clean(workspacePath)
	if strings.TrimSpace(pathFilter) != "" {
		// Resolve like every path input: absolute, ~, or relative to cwd.
		p, err := expandUserPath(pathFilter)
		if err != nil {
			fmt.Fprintf(stderr, "invalid path %q: %s\n", pathFilter, err.Error())
			return 1
		}
		start = p
	}
	if _, err := os.Stat(start); err != nil {
		fmt.Fprintf(stderr, "path does not exist: %s\n", start)
		return 1
	}
	if !repo.IsWithin(start, workspacePath) {
		fmt.Fprintf(stderr, "path is outside the workspace %s: %s\n", workspacePath, start)
		return 1
	}

	startRel, _ := filepath.Rel(workspacePath, start)
	startRel = filepath.ToSlash(startRel)
	if startRel == "." {
		startRel = ""
	}

	// ── Pass 1: collect all entries (no depth limit, don't skip excluded dirs) ──
	entries := make([]treeEntry, 0, 256)
	entryIdx := map[string]int{} // relPath → index in entries

	_ = filepath.WalkDir(start, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == start {
			return nil // skip the root itself
		}
		rel, err := filepath.Rel(workspacePath, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		var status, rule string
		if engine != nil {
			eval := engine.Evaluate(rel, d.IsDir())
			if eval.Included {
				status = "synced"
			} else {
				status = "ignored"
			}
			rule = eval.Rule
		} else {
			status = "synced"
			rule = "<default>"
		}

		size := int64(0)
		if !d.IsDir() {
			if info, infoErr := d.Info(); infoErr == nil && info != nil {
				size = info.Size()
			}
		}

		// Depth relative to start.
		var relD int
		if startRel == "" {
			relD = strings.Count(rel, "/")
		} else {
			inner := strings.TrimPrefix(rel, startRel+"/")
			relD = strings.Count(inner, "/")
		}

		idx := len(entries)
		entries = append(entries, treeEntry{
			rel:       rel,
			name:      filepath.Base(path),
			isDir:     d.IsDir(),
			status:    status,
			sizeBytes: size,
			rule:      rule,
			relDepth:  relD,
		})
		entryIdx[rel] = idx
		return nil
	})

	// ── Pass 2: propagate partial status upward ──
	// Only propagate from entries that are excluded AND whose parent is NOT
	// also excluded (to avoid double-counting nested exclusions).
	for i := range entries {
		e := &entries[i]
		if e.status != "ignored" {
			continue
		}
		// Check if immediate parent is already excluded.
		parentRel := filepath.ToSlash(filepath.Dir(e.rel))
		if parentRel == "." {
			parentRel = ""
		}
		if parentRel != startRel {
			if pIdx, ok := entryIdx[parentRel]; ok && entries[pIdx].status == "ignored" {
				continue // parent already excluded; skip to avoid double-count
			}
		}
		// Walk up the ancestor chain, marking each as partial.
		cur := parentRel
		for {
			if cur == startRel || cur == "." || cur == "" {
				break
			}
			if aIdx, ok := entryIdx[cur]; ok {
				if entries[aIdx].status == "synced" {
					entries[aIdx].status = "partial"
				}
				entries[aIdx].excludedChildren++
			}
			next := filepath.ToSlash(filepath.Dir(cur))
			if next == "." {
				next = ""
			}
			if next == cur {
				break
			}
			cur = next
		}
	}

	// ── JSON output ──
	if globals.json {
		type jsonEntry struct {
			Path      string `json:"path"`
			Status    string `json:"status"`
			Rule      string `json:"rule"`
			IsDir     bool   `json:"is_dir"`
			SizeBytes int64  `json:"size_bytes,omitempty"`
			Depth     int    `json:"depth"`
		}
		out := make([]jsonEntry, 0, len(entries))
		for _, e := range entries {
			if maxDepth >= 0 && e.relDepth > maxDepth {
				continue
			}
			out = append(out, jsonEntry{
				Path:      style.AbsPath(workspacePath, e.rel),
				Status:    e.status,
				Rule:      e.rule,
				IsDir:     e.isDir,
				SizeBytes: e.sizeBytes,
				Depth:     e.relDepth,
			})
		}
		return writeJSON(stdout, stderr, "ignore.tree", out)
	}

	// ── Build parent→children map for display ──
	children := map[string][]int{} // parentRel → sorted child indices
	for i, e := range entries {
		parentRel := filepath.ToSlash(filepath.Dir(e.rel))
		if parentRel == "." {
			parentRel = ""
		}
		children[parentRel] = append(children[parentRel], i)
	}
	// Ensure consistent ordering within each parent group.
	for k := range children {
		sort.Slice(children[k], func(a, b int) bool {
			return entries[children[k][a]].name < entries[children[k][b]].name
		})
	}

	// ── Render ──
	w := textOut(globals, stdout)
	nc := globals.noColor

	// Header: the absolute start path. Rows below are tree nodes under it
	// (spec "Path Rules": tree views).
	fmt.Fprintf(w, "%s\n", style.Boldf(nc, "%s", start))

	var totalExcludedFiles int
	var totalExcludedSize int64

	var renderChildren func(parentRel, prefix string, depth int)
	renderChildren = func(parentRel, prefix string, depth int) {
		kids := children[parentRel]
		for i, idx := range kids {
			e := entries[idx]
			isLast := i == len(kids)-1

			connector := style.TreeBranch
			childPrefix := prefix + style.TreePipe
			if isLast {
				connector = style.TreeCorner
				childPrefix = prefix + style.TreeSpace
			}

			icon := treeStatusIcon(e.status, nc)
			name := e.name
			if e.isDir {
				name += "/"
			}

			extra := ""
			switch e.status {
			case "ignored":
				if !e.isDir {
					totalExcludedFiles++
					totalExcludedSize += e.sizeBytes
					if e.rule != "" && e.rule != "<default>" {
						extra = "  " + style.Mutedf(nc, "%s", e.rule)
					}
				} else {
					// Excluded dir: count it as one unit (don't descend).
					totalExcludedFiles++
					if e.rule != "" && e.rule != "<default>" {
						extra = "  " + style.Mutedf(nc, "%s", e.rule)
					}
				}
			case "partial":
				if e.excludedChildren > 0 {
					extra = "  " + style.Mutedf(nc, "(%d excluded)", e.excludedChildren)
				}
			}

			sizeStr := ""
			if e.sizeBytes > 0 {
				sizeStr = "  " + style.Mutedf(nc, "%s", style.HumanBytes(e.sizeBytes))
			}

			fmt.Fprintf(w, "%s%s %s%s%s\n",
				style.TreePrefix(prefix+connector, nc),
				icon,
				style.Infof(nc, "%s", name),
				sizeStr,
				extra,
			)

			// Recurse into dirs that are within the depth limit and not fully excluded.
			// depth is 0-based (0 = rendering direct children of start).
			// At depth D we are already showing level D+1 entries.
			// To honour -L N (show N levels), recurse only when D+1 < N.
			// maxDepth == -1 means unlimited.
			if e.isDir && (maxDepth < 0 || depth+1 < maxDepth) && e.status != "ignored" {
				renderChildren(e.rel, childPrefix, depth+1)
			}
		}
	}

	renderChildren(startRel, "", 0)

	// ── Summary ──
	if totalExcludedFiles > 0 {
		fmt.Fprintf(w, "\n%s excluded · %s not synced\n",
			style.Mutedf(nc, "%d files", totalExcludedFiles),
			style.Mutedf(nc, "%s", style.HumanBytes(totalExcludedSize)))
	} else {
		fmt.Fprintf(w, "\n%s\n", style.ResultSuccess(nc, "All files synced"))
	}

	return 0
}

// treeStatusIcon returns the ✔/✗/◐ icon for a tree entry status.
func treeStatusIcon(status string, nc bool) string {
	switch status {
	case "synced":
		return style.IconCheck(nc)
	case "ignored":
		return style.IconCross(nc)
	case "partial":
		if nc {
			return "~"
		}
		return "◐"
	default:
		return "?"
	}
}
