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

// runIgnoreList handles:
//   - ws ignore ls              → flat list of all excluded files
//   - ws ignore ls --tree       → hierarchical tree view with sync status
//   - ws ignore ls <path>       → single-path status check (replaces old "check")
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

// runIgnoreCheckPath checks a single path's sync status (replaces old "ws ignore check").
func runIgnoreCheckPath(engine *ignore.Engine, workspacePath, target string, globals globalFlags, stdout, stderr io.Writer) int {
	absPath := target
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(workspacePath, absPath)
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

	data := map[string]any{"path": rel, "included": res.Included, "rule": res.Rule, "safe_harbor": res.SafeHarbor}
	if globals.json {
		return writeJSON(stdout, stderr, "ignore.ls", data)
	}

	out := textOut(globals, stdout)
	nc := globals.noColor
	state := style.Badge("synced", nc)
	if !res.Included {
		state = style.Badge("ignored", nc)
	}
	harbor := ""
	if res.SafeHarbor {
		harbor = " " + style.Mutedf(nc, "[safe harbor]")
	}
	fmt.Fprintf(out, "%s  %s  %s %s%s\n",
		state,
		style.Infof(nc, "%s", rel),
		style.Mutedf(nc, "Rule:"),
		style.Mutedf(nc, "%s", res.Rule),
		harbor)

	if !res.Included {
		return 2
	}
	return 0
}

// runIgnoreTreeView shows the workspace tree annotated with sync status.
func runIgnoreTreeView(engine *ignore.Engine, workspacePath, pathFilter string, maxDepth int, globals globalFlags, stdout, stderr io.Writer) int {
	start := workspacePath
	if strings.TrimSpace(pathFilter) != "" {
		start = filepath.Join(workspacePath, strings.TrimSpace(pathFilter))
	}
	if _, err := os.Stat(start); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	type node struct {
		Path   string `json:"path"`
		Status string `json:"status"`
		Rule   string `json:"rule"`
		IsDir  bool   `json:"is_dir"`
	}
	rows := make([]node, 0)
	baseDepth := strings.Count(filepath.ToSlash(strings.TrimPrefix(start, workspacePath)), "/")
	_ = filepath.WalkDir(start, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(workspacePath, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			rel = ""
		}
		currentDepth := strings.Count(filepath.ToSlash(strings.TrimPrefix(path, start)), "/")
		if currentDepth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		_ = baseDepth
		res := engine.Evaluate(rel, d.IsDir())
		status := "SYNCED"
		if !res.Included {
			status = "IGNORED"
		}
		rows = append(rows, node{Path: rel, Status: status, Rule: res.Rule, IsDir: d.IsDir()})
		return nil
	})

	if globals.json {
		return writeJSON(stdout, stderr, "ignore.ls", rows)
	}
	out := textOut(globals, stdout)
	if len(rows) == 0 {
		fmt.Fprintln(out, "(empty)")
		return 0
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Path < rows[j].Path
	})
	for i, n := range rows {
		nc := globals.noColor
		displayPath := n.Path
		if displayPath == "" {
			displayPath = "."
		}
		d := 0
		if displayPath != "." {
			d = strings.Count(displayPath, "/") + 1
		}
		prefix := ""
		if i > 0 {
			branch := style.TreeBranch
			if i == len(rows)-1 {
				branch = style.TreeCorner
			}
			prefix = style.TreePrefix(strings.Repeat(style.TreePipe, maxInt(0, d-1))+branch, nc)
		}
		statusBadge := style.Badge(n.Status, nc)
		pathStr := style.Infof(nc, "%s", displayPath)
		if n.IsDir {
			pathStr = style.Boldf(nc, "%s", displayPath) + style.Mutedf(nc, "/")
		}
		fmt.Fprintf(out, "%s%s %s %s\n", prefix, statusBadge, pathStr, style.Mutedf(nc, "(%s)", n.Rule))
	}
	return 0
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
