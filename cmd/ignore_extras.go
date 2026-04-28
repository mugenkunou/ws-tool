package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// runIgnoreList handles ws ignore ls (flat list of excluded files).
// Also supports positional path arg (legacy) and --tree flag for tree view.
func runIgnoreList(engine *ignore.Engine, workspacePath string, args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ignore-ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	treeMode := fs.Bool("tree", false, "show hierarchical tree view")
	depth := fs.Int("depth", 1, "max depth (with --tree)")
	pathFilter := fs.String("path", "", "restrict to subpath")
	ruleFilter := fs.String("rule", "", "show only entries matching rule")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	// Positional argument: single-path check mode.
	if len(fs.Args()) > 0 {
		return runIgnoreCheckPath(engine, workspacePath, fs.Args()[0], globals, stdout, stderr)
	}

	if *treeMode {
		return runIgnoreTreeView(engine, workspacePath, *pathFilter, *depth, globals, stdout, stderr)
	}

	// Default: flat list of excluded files.
	type row struct {
		Path string `json:"path"`
		Rule string `json:"rule"`
		Size int64  `json:"size_bytes"`
	}
	rows := make([]row, 0)

	_ = filepath.WalkDir(workspacePath, func(path string, d os.DirEntry, err error) error {
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
		if strings.HasPrefix(rel, "../") {
			return nil
		}
		if strings.TrimSpace(*pathFilter) != "" {
			p := filepath.ToSlash(strings.TrimSpace(*pathFilter))
			if rel != p && !strings.HasPrefix(rel, p+"/") {
				return nil
			}
		}
		res := engine.Evaluate(rel, false)
		if res.Included {
			return nil
		}
		if strings.TrimSpace(*ruleFilter) != "" && strings.TrimSpace(*ruleFilter) != res.Rule {
			return nil
		}
		st, _ := os.Stat(path)
		size := int64(0)
		if st != nil {
			size = st.Size()
		}
		rows = append(rows, row{Path: rel, Rule: res.Rule, Size: size})
		return nil
	})

	sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })
	if globals.json {
		return writeJSON(stdout, stderr, "ignore.ls", rows)
	}
	out := textOut(globals, stdout)
	if len(rows) == 0 {
		fmt.Fprintln(out, "No ignored files.")
		return 0
	}
	for _, r := range rows {
		nc := globals.noColor
		fmt.Fprintf(out, "%s %s %s %s\n",
			style.Badge("ignored", nc),
			style.Mutedf(nc, "%s", style.HumanBytes(r.Size)),
			style.Infof(nc, "%s", r.Path),
			style.Mutedf(nc, "%s", r.Rule))
	}
	return 0
}

// runIgnoreCheckPath implements ws ignore check <path>.
// It reports whether a path would be synced or ignored, with a reason line
// and (for files) a file-size line.
func runIgnoreCheckPath(engine *ignore.Engine, workspacePath, target string, globals globalFlags, stdout, stderr io.Writer) int {
	absPath := target
	if !filepath.IsAbs(absPath) {
		// Resolve relative to cwd first (mirrors how a user types a path at their
		// shell prompt), then fall back to workspace-relative for callers that
		// supply workspace-anchored paths directly (e.g. tests, scripting).
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		cwdAbs := filepath.Join(cwd, absPath)
		if _, statErr := os.Stat(cwdAbs); statErr == nil {
			absPath = cwdAbs
		} else {
			absPath = filepath.Join(workspacePath, absPath)
		}
	}
	st, err := os.Stat(absPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	res, rel, err := ignore.Check(engine, workspacePath, absPath, st.IsDir())
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	data := map[string]any{
		"path":        rel,
		"included":    res.Included,
		"rule":        res.Rule,
		"safe_harbor": res.SafeHarbor,
	}
	if !st.IsDir() {
		data["size_bytes"] = st.Size()
	}
	if globals.json {
		return writeJSON(stdout, stderr, "ignore.check", data)
	}

	out := textOut(globals, stdout)
	nc := globals.noColor

	statusBadge := style.Badge("SYNCED", nc)
	statusIcon := style.IconCheck(nc)
	if !res.Included {
		statusBadge = style.Badge("IGNORED", nc)
		statusIcon = style.IconCross(nc)
	}
	_ = statusBadge

	fmt.Fprintf(out, "%s %s  %s\n", statusIcon, style.Badge(map[bool]string{true: "SYNCED", false: "IGNORED"}[res.Included], nc), style.Infof(nc, "%s", rel))

	// Reason line
	if res.Included {
		if res.SafeHarbor {
			fmt.Fprintf(out, "  Reason: safe harbor — %s overrides an exclude rule\n", style.Mutedf(nc, "%s", res.Rule))
		} else if res.Rule == "<default>" {
			fmt.Fprintf(out, "  Reason: no matching exclude rule\n")
		} else {
			fmt.Fprintf(out, "  Reason: included by rule %s\n", style.Mutedf(nc, "`%s`", res.Rule))
		}
	} else {
		fmt.Fprintf(out, "  Reason: excluded by rule %s\n", style.Mutedf(nc, "`%s`", res.Rule))
	}

	// File size line (files only)
	if !st.IsDir() {
		fmt.Fprintf(out, "  File size: %s\n", style.Mutedf(nc, "%s", style.HumanBytes(st.Size())))
	}

	if !res.Included {
		return 2
	}
	return 0
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
	start := workspacePath
	if strings.TrimSpace(pathFilter) != "" {
		start = filepath.Join(workspacePath, strings.TrimSpace(pathFilter))
	}
	if _, err := os.Stat(start); err != nil {
		fmt.Fprintln(stderr, err.Error())
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
				Path:      e.rel,
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

	// Header: display the start path.
	displayRoot := start
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, start); err == nil && !strings.HasPrefix(rel, "..") {
			displayRoot = "~/" + filepath.ToSlash(rel)
		}
	}
	fmt.Fprintf(w, "%s\n", style.Boldf(nc, "%s/", displayRoot))

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

// runIgnoreEdit opens ws/ignore.json in $EDITOR, validates on save,
// and regenerates .megaignore.
func runIgnoreEdit(userRulesPath, megaignorePath string, currentRules ignore.UserRules, args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ignore-edit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	editor := fs.String("editor", "", "override editor command")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	// Ensure ws/ignore.json exists so the editor has something to open.
	if _, err := os.Stat(userRulesPath); os.IsNotExist(err) {
		if err := ignore.SaveUserRules(userRulesPath, currentRules); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
	}

	cmdName := strings.TrimSpace(*editor)
	if cmdName == "" {
		cmdName = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if cmdName == "" {
		cmdName = strings.TrimSpace(os.Getenv("VISUAL"))
	}
	if cmdName == "" {
		cmdName = "vi"
	}
	cmd := exec.Command(cmdName, userRulesPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	// Validate and regenerate .megaignore after edit.
	updatedRules, err := ignore.LoadUserRules(userRulesPath)
	if err != nil {
		fmt.Fprintln(stderr, "validation error: "+err.Error())
		return 1
	}

	if err := ignore.WriteMegaignore(megaignorePath, updatedRules); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	nc := globals.noColor
	stats := ignore.GetRuleStats(updatedRules)

	if globals.json {
		return writeJSON(stdout, stderr, "ignore.edit", map[string]any{
			"path":   userRulesPath,
			"editor": cmdName,
			"stats":  stats,
		})
	}
	out := textOut(globals, stdout)
	fmt.Fprintf(out, "%s .megaignore regenerated (%d rules: %d default + %d user)\n",
		style.IconCheck(nc), stats.Total, stats.DefaultExclude+stats.DefaultHarbors, stats.UserExclude+stats.UserHarbors)
	return 0
}
