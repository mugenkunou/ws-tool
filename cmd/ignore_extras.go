package cmd

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/megaignore"
	"github.com/mugenkunou/ws-tool/internal/provision"
	"github.com/mugenkunou/ws-tool/internal/style"
)

func runIgnoreList(engine *ignore.Engine, workspacePath string, args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ignore-ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	pathFilter := fs.String("path", "", "restrict to subpath")
	ruleFilter := fs.String("rule", "", "show only entries matching rule")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

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

func runIgnoreTree(engine *ignore.Engine, workspacePath string, args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ignore-tree", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	pathFilter := fs.String("path", "", "start from subpath")
	depth := fs.Int("depth", 1, "max depth")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	start := workspacePath
	if strings.TrimSpace(*pathFilter) != "" {
		start = filepath.Join(workspacePath, strings.TrimSpace(*pathFilter))
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
		if currentDepth > *depth {
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
		return writeJSON(stdout, stderr, "ignore.tree", rows)
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
		depth := 0
		if displayPath != "." {
			depth = strings.Count(displayPath, "/") + 1
		}
		prefix := ""
		if i > 0 {
			branch := style.TreeBranch
			if i == len(rows)-1 {
				branch = style.TreeCorner
			}
			prefix = style.TreePrefix(strings.Repeat(style.TreePipe, maxInt(0, depth-1))+branch, nc)
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

func runIgnoreEdit(megaignorePath string, args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ignore-edit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	editor := fs.String("editor", "", "override editor command")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
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
	cmd := exec.Command(cmdName, megaignorePath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if globals.json {
		return writeJSON(stdout, stderr, "ignore.edit", map[string]any{"path": megaignorePath, "editor": cmdName})
	}
	fmt.Fprintln(textOut(globals, stdout), style.ResultSuccess(globals.noColor, "Edited %s", style.Infof(globals.noColor, "%s", megaignorePath)))
	return 0
}

func runIgnoreGenerate(megaignorePath string, workspacePath string, args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ignore-generate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	merge := fs.Bool("merge", false, "merge with existing rules")
	scan := fs.Bool("scan", false, "append generated suggestions from workspace")
	dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	template := megaignore.EnsureFinalSentinel(megaignore.Template)
	rules := extractRules(template)
	added := []string{}
	action := "created"

	if *dryRun {
		globals.dryRun = true
	}

	plan := Plan{Command: "ignore.generate"}

	if *merge {
		plan.Actions = append(plan.Actions, Action{
			ID:          "ignore-merge",
			Description: fmt.Sprintf("Merge rules into %s", megaignorePath),
			Execute: func() error {
				a, err := ignore.AddRules(megaignorePath, rules)
				if err != nil {
					return err
				}
				added = a
				action = "merged"
				return nil
			},
		})
	} else {
		plan.Actions = append(plan.Actions, Action{
			ID:          "ignore-generate",
			Description: fmt.Sprintf("Generate %s", megaignorePath),
			Execute: func() error {
				if err := os.WriteFile(megaignorePath, []byte(template), 0o644); err != nil {
					return err
				}
				action = "generated"
				_ = provision.Record(provision.LedgerPath(workspacePath), provision.Entry{
					Type:    provision.TypeFile,
					Path:    megaignorePath,
					Command: "ignore generate",
				})
				return nil
			},
		})
	}

	if *scan {
		plan.Actions = append(plan.Actions, Action{
			ID:          "ignore-scan",
			Description: "Scan workspace for binary suggestions",
			Execute: func() error {
				suggestions := detectBinarySuggestions(workspacePath)
				if len(suggestions) > 0 {
					a, err := ignore.AddRules(megaignorePath, suggestions)
					if err != nil {
						return err
					}
					added = append(added, a...)
				}
				return nil
			},
		})
	}

	planResult := RunPlan(plan, stdin, stdout, globals)

	if globals.json {
		return writeJSONDryRun(stdout, stderr, "ignore.generate", globals.dryRun, map[string]any{
			"action":      action,
			"path":        megaignorePath,
			"added_rules": added,
			"actions":     planResult.Actions,
		})
	}
	out := textOut(globals, stdout)
	nc := globals.noColor
	if planResult.ExecutedCount() > 0 {
		fmt.Fprintf(out, "%s %s\n", style.Successf(nc, "%s", strings.Title(action)), style.Infof(nc, "%s", megaignorePath))
		if len(added) > 0 {
			fmt.Fprintf(out, "%s\n", style.Mutedf(nc, "Rules added: %d", len(added)))
		}
	}
	return planResult.ExitCode()
}

func extractRules(content string) []string {
	out := make([]string, 0)
	s := bufio.NewScanner(strings.NewReader(content))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func detectBinarySuggestions(workspacePath string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	_ = filepath.WalkDir(workspacePath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".pcap", ".bin":
			r := "-g:*" + ext
			if _, ok := seen[r]; !ok {
				seen[r] = struct{}{}
				out = append(out, r)
			}
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
