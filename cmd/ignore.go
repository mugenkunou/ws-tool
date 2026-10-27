package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/editor"
	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

const ignoreUsage = "usage: ws ignore <ls|lint|add|rm|mute|unmute|check|tree|edit> …"

// ignoreEnv is what every ignore command needs.
type ignoreEnv struct {
	workspace  string
	configPath string
	megaignore string
	cfg        wsconfig.WsConfig
	rules      ignore.UserRules
	engine     *ignore.Engine
}

// runIgnore dispatches `ws ignore <verb>` (spec §5 ignore).
func runIgnore(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, ignoreUsage)
		return 1
	}
	workspacePath, configPath, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	e := ignoreEnv{
		workspace:  workspacePath,
		configPath: configPath,
		megaignore: filepath.Join(workspacePath, ".megaignore"),
		cfg:        cfg,
		rules:      cfg.IgnoreRules,
		engine:     ignore.BuildEngine(cfg.IgnoreRules),
	}

	verb, rest := args[0], args[1:]
	switch verb {
	case "ls":
		return e.ls(rest, globals, stdout, stderr)
	case "lint":
		return e.lint(rest, globals, stdout, stderr)
	case "add":
		return e.add(rest, globals, stdin, stdout, stderr)
	case "rm":
		return e.rm(rest, globals, stdin, stdout, stderr)
	case "mute":
		return e.mute(rest, globals, stdin, stdout, stderr)
	case "unmute":
		return e.unmute(rest, globals, stdin, stdout, stderr)
	case "check":
		return e.check(rest, globals, stdout, stderr)
	case "tree":
		return e.tree(rest, globals, stdout, stderr)
	case "edit":
		return e.edit(rest, globals, stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "unknown command: ignore %s\n", verb)
	fmt.Fprintln(stderr, ignoreUsage)
	return 1
}

// warnStale prints one warning on stderr when .megaignore differs from what
// ws.json generates. Read commands call it; the next RW command rewrites the file.
func (e ignoreEnv) warnStale(globals globalFlags, stderr io.Writer) {
	if globals.quiet {
		return
	}
	stale, err := ignore.Stale(e.megaignore, e.rules)
	if err != nil || !stale {
		return
	}
	fmt.Fprintf(stderr, "warning: %s differs from what ws.json generates; ws ignore add, rm or edit rewrites it\n", e.megaignore)
}

// hintStaleMuted prints one line on stderr when ignore.muted names paths that no
// longer exist. It only reports: `ws ignore unmute --stale` removes them.
func (e ignoreEnv) hintStaleMuted(globals globalFlags, stderr io.Writer) {
	if globals.quiet {
		return
	}
	if n := len(ignore.StaleMuted(e.workspace, e.cfg.Ignore.Muted)); n > 0 {
		fmt.Fprintf(stderr, "%s no longer exist: ws ignore unmute --stale\n", plural(n, "muted path"))
	}
}

// regenerate rewrites .megaignore from the rules now in ws.json.
func (e ignoreEnv) regenerate() error {
	rules, err := wsconfig.LoadIgnoreRules(e.configPath)
	if err != nil {
		return err
	}
	return ignore.WriteMegaignore(e.megaignore, rules)
}

// target resolves one path argument (spec §4.5). It must be inside the
// workspace and is never the workspace itself. When mustExist is false a
// missing path is accepted (isDir is then false).
func (e ignoreEnv) target(raw string, mustExist bool) (abs, rel string, isDir bool, err error) {
	abs, err = wsconfig.ExpandUserPath(raw)
	if err != nil {
		return "", "", false, fmt.Errorf("invalid path %q: %w", raw, err)
	}
	if !repo.IsWithin(abs, e.workspace) {
		return "", "", false, fmt.Errorf("path is outside the workspace %s: %s", e.workspace, abs)
	}
	r, err := filepath.Rel(e.workspace, abs)
	if err != nil {
		return "", "", false, err
	}
	rel = ignore.NormalizeRel(r)
	if rel == "" {
		return "", "", false, fmt.Errorf("%s is the workspace itself", abs)
	}
	st, statErr := os.Stat(abs)
	if statErr != nil {
		if mustExist {
			return "", "", false, fmt.Errorf("path does not exist: %s", abs)
		}
		return abs, rel, false, nil
	}
	return abs, rel, st.IsDir(), nil
}

// ruleLabel names a rule with where it comes from: "`-g:*.csv` (built-in)".
func ruleLabel(rule, source string) string {
	switch source {
	case ignore.SourceBuiltin:
		return fmt.Sprintf("`%s` (built-in)", rule)
	case ignore.SourceUser:
		return fmt.Sprintf("`%s` (ws.json)", rule)
	}
	return fmt.Sprintf("`%s`", rule)
}

// ── ls ─────────────────────────────────────────────────────────────

func (e ignoreEnv) ls(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ignore-ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	muted := fs.Bool("muted", false, "")
	lf := registerListFlags(fs, "")
	// Hidden: the flags of the old `ignore ls`.
	treeMode := fs.Bool("tree", false, "")
	depth := fs.Int("depth", 1, "")
	pathFilter := fs.String("path", "", "")
	ruleFilter := fs.String("rule", "", "")
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	// The old `ignore ls <path>` was `ignore check`.
	if len(fs.Args()) > 0 {
		return e.check(fs.Args()[:1], globals, stdout, stderr)
	}
	if *treeMode {
		start := ""
		if strings.TrimSpace(*pathFilter) != "" {
			start = *pathFilter
		}
		e.warnStale(globals, stderr)
		return runIgnoreTreeView(e.engine, e.workspace, start, *depth, globals, stdout, stderr)
	}
	e.warnStale(globals, stderr)

	if *muted {
		vs, code := e.scan(stderr)
		if code != 0 {
			return code
		}
		var rows []ignore.Violation
		for _, v := range vs {
			if v.Muted {
				rows = append(rows, v)
			}
		}
		rows = append(rows, ignore.StaleViolations(e.workspace, e.cfg.Ignore.Muted)...)
		return renderList(globals, "ignore.ls", ignore.Schema(resource.CLIView(), e.workspace), rows, lf, "No muted warnings.", stdout, stderr)
	}

	var under []string
	if strings.TrimSpace(*pathFilter) != "" {
		p, err := wsconfig.ExpandUserPath(*pathFilter)
		if err != nil {
			fmt.Fprintf(stderr, "invalid --path: %s\n", err.Error())
			return 1
		}
		under = []string{p}
	}
	rows := ignore.ListIgnored(e.engine, e.workspace, under)
	if r := strings.TrimSpace(*ruleFilter); r != "" {
		kept := rows[:0]
		for _, row := range rows {
			if row.Rule == r {
				kept = append(kept, row)
			}
		}
		rows = kept
	}
	code := renderList(globals, "ignore.ls", ignore.IgnoredSchema(resource.CLIView(), e.workspace), rows, lf, "Nothing is ignored.", stdout, stderr)
	if code == 0 {
		e.hintStaleMuted(globals, stderr)
	}
	return code
}

// scan runs the hygiene scan with the workspace's thresholds and muted paths.
func (e ignoreEnv) scan(stderr io.Writer) ([]ignore.Violation, int) {
	vs, err := ignore.Scan(ignore.ScanOptions{
		WorkspacePath: e.workspace,
		WarnSizeMB:    e.cfg.Ignore.WarnSizeMB,
		CritSizeMB:    e.cfg.Ignore.CritSizeMB,
		MaxDepth:      e.cfg.Ignore.MaxDepth,
		Engine:        e.engine,
		Muted:         e.cfg.Ignore.Muted,
	})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return nil, 1
	}
	return vs, 0
}

// ── lint ───────────────────────────────────────────────────────────

func (e ignoreEnv) lint(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ignore-lint", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	lf := registerListFlags(fs, "")
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	var under []string
	for _, raw := range fs.Args() {
		abs, err := wsconfig.ExpandUserPath(raw)
		if err != nil {
			fmt.Fprintf(stderr, "invalid path %q: %s\n", raw, err.Error())
			return 1
		}
		if !repo.IsWithin(abs, e.workspace) {
			fmt.Fprintf(stderr, "path is outside the workspace %s: %s\n", e.workspace, abs)
			return 1
		}
		if _, err := os.Stat(abs); err != nil {
			fmt.Fprintf(stderr, "path does not exist: %s\n", abs)
			return 1
		}
		under = append(under, abs)
	}
	e.warnStale(globals, stderr)

	vs, code := e.scan(stderr)
	if code != 0 {
		return code
	}
	var live []ignore.Violation
	mutedCount, mutedBytes := 0, int64(0)
	for _, v := range vs {
		if len(under) > 0 && !anyWithin(style.AbsPath(e.workspace, v.Path), under) {
			continue
		}
		if v.Muted {
			mutedCount++
			mutedBytes += v.SizeBytes
			continue
		}
		live = append(live, v)
	}
	code = renderList(globals, "ignore.lint", ignore.Schema(resource.CLIView(), e.workspace), live, lf, "No warnings.", stdout, stderr)
	if code == 0 && mutedCount > 0 && !globals.quiet {
		fmt.Fprintf(stderr, "%s (%s): ws ignore ls --muted\n", plural(mutedCount, "muted warning"), style.HumanBytes(mutedBytes))
	}
	if code == 0 {
		e.hintStaleMuted(globals, stderr)
	}
	return code
}

// anyWithin reports whether path is at or under one of dirs.
func anyWithin(path string, dirs []string) bool {
	for _, d := range dirs {
		if repo.IsWithin(path, d) {
			return true
		}
	}
	return false
}

// ── add ────────────────────────────────────────────────────────────

// symlinkOriginal names the link whose workspace copy a path is, contains or
// lies inside, or "" when there is none.
func (e ignoreEnv) symlinkOriginal(abs string) string {
	for _, l := range e.cfg.Symlinks {
		orig := filepath.Join(e.workspace, filepath.FromSlash(l.Original))
		if repo.IsWithin(abs, orig) || repo.IsWithin(orig, abs) {
			return l.System
		}
	}
	return ""
}

func (e ignoreEnv) add(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ignore-add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(stderr, "usage: ws ignore add <path…>")
		return 1
	}

	// Check every target first: one bad one changes nothing.
	type item struct{ abs, rel string }
	var items []item
	seen := map[string]bool{}
	bad := false
	out := quietOut(globals, stdout)
	for _, raw := range fs.Args() {
		abs, rel, isDir, err := e.target(raw, true)
		if err == nil && strings.Contains(rel, "*") {
			err = fmt.Errorf("%s: a name with * would be read as a pattern", abs)
		}
		if err == nil {
			if sys := e.symlinkOriginal(abs); sys != "" {
				err = fmt.Errorf("%s is the workspace copy of the symlink %s: it must sync for ws symlink link on your other machines", abs, sys)
			}
		}
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			bad = true
			continue
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		if res := e.engine.Evaluate(rel, isDir); !res.Included {
			fmt.Fprintf(out, "Already ignored: %s  %s\n", abs, style.Mutedf(globals.noColor, "rule %s", ruleLabel(res.Rule, res.RuleSource)))
			continue
		}
		items = append(items, item{abs, rel})
	}
	if bad {
		return 1
	}

	p := Plan{Command: "ignore.add"}
	for _, it := range items {
		it := it
		p.Actions = append(p.Actions, Action{
			ID:          "ignore-add-" + it.abs,
			Description: fmt.Sprintf("Ignore %s (ws.json ignore_rules.exclude)", it.abs),
			Execute: func() error {
				if _, err := wsconfig.AddIgnoreExclude(e.configPath, it.rel, ""); err != nil {
					return err
				}
				return e.regenerate()
			},
		})
	}
	return e.finish("ignore.add", p, globals, stdin, stdout, stderr)
}

// finish runs a plan and reports it: --json writes data.actions.
func (e ignoreEnv) finish(command string, p Plan, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	res := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, command, globals.dryRun, map[string]any{"actions": res.Actions})
	}
	return res.ExitCode()
}

// ── rm ─────────────────────────────────────────────────────────────

func (e ignoreEnv) rm(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ignore-rm", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(stderr, "usage: ws ignore rm <path…>")
		return 1
	}

	type item struct {
		abs   string
		entry string
		still string
	}
	var items []item
	seen := map[string]bool{}
	bad := false
	out := quietOut(globals, stdout)
	for _, raw := range fs.Args() {
		abs, rel, isDir, err := e.target(raw, false)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			bad = true
			continue
		}
		v := ignore.ClassifyRm(e.rules, rel, isDir)
		switch v.Kind {
		case ignore.RmNotIgnored:
			fmt.Fprintf(out, "Not ignored: %s: nothing to do\n", abs)
		case ignore.RmWider:
			fmt.Fprintf(stderr, "%s is ignored by your rule `%s`, not by an entry of its own: remove that rule, or change it with ws ignore edit\n", abs, v.Rule)
			bad = true
		case ignore.RmBuiltin:
			pattern := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(v.Rule, "-g:"), "-:"), "-s:")
			fmt.Fprintf(stderr, "%s is ignored by the built-in rule `%s`: ws does not edit built-in rules for one path.\n"+
				"  To sync it, turn the rule off everywhere: add %q to suppress_defaults with ws ignore edit\n", abs, v.Rule, pattern)
			bad = true
		case ignore.RmExact:
			if !seen[v.Entry] {
				seen[v.Entry] = true
				items = append(items, item{abs: abs, entry: v.Entry, still: v.Rule})
			}
		}
	}
	if bad {
		return 1
	}

	p := Plan{Command: "ignore.rm"}
	for _, it := range items {
		it := it
		desc := fmt.Sprintf("Stop ignoring %s (remove %q from ignore_rules.exclude)", it.abs, it.entry)
		if it.still != "" {
			desc += fmt.Sprintf("; still ignored by `%s`", it.still)
		}
		p.Actions = append(p.Actions, Action{
			ID:          "ignore-rm-" + it.abs,
			Description: desc,
			Execute: func() error {
				if _, err := wsconfig.RemoveIgnoreExclude(e.configPath, it.entry); err != nil {
					return err
				}
				return e.regenerate()
			},
		})
	}
	return e.finish("ignore.rm", p, globals, stdin, stdout, stderr)
}

// ── mute ───────────────────────────────────────────────────────────

func (e ignoreEnv) mute(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ignore-mute", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(stderr, "usage: ws ignore mute <path…>")
		return 1
	}

	muted := ignore.NormalizeMuted(e.workspace, e.cfg.Ignore.Muted)
	type item struct{ abs, rel string }
	var items []item
	seen := map[string]bool{}
	bad := false
	out := quietOut(globals, stdout)
	for _, raw := range fs.Args() {
		abs, rel, isDir, err := e.target(raw, true)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			bad = true
			continue
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		if st := ignore.MuteStateOf(e.engine, muted, rel, isDir); st.Muted {
			switch {
			case st.Entry == rel:
				fmt.Fprintf(out, "Already muted: %s\n", abs)
			case st.Entry != "":
				fmt.Fprintf(out, "Already muted: %s (its parent %s is muted)\n", abs, style.AbsPath(e.workspace, st.Entry))
			default:
				fmt.Fprintf(out, "Already muted: %s (included directory %s)\n", abs, ruleLabel(st.Rule, st.RuleSource))
			}
			continue
		}
		items = append(items, item{abs, rel})
	}
	if bad {
		return 1
	}

	p := Plan{Command: "ignore.mute"}
	for _, it := range items {
		it := it
		p.Actions = append(p.Actions, Action{
			ID:          "ignore-mute-" + it.abs,
			Description: fmt.Sprintf("Mute %s (it keeps syncing)", it.abs),
			Execute: func() error {
				_, err := wsconfig.AddIgnoreMuted(e.configPath, it.rel)
				return err
			},
		})
	}
	return e.finish("ignore.mute", p, globals, stdin, stdout, stderr)
}

// ── unmute ─────────────────────────────────────────────────────────

func (e ignoreEnv) unmute(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ignore-unmute", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	stale := fs.Bool("stale", false, "")
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if *stale {
		if len(fs.Args()) > 0 {
			fmt.Fprintln(stderr, "ws ignore unmute --stale takes no paths")
			return 1
		}
		return e.unmuteStale(globals, stdin, stdout, stderr)
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(stderr, "usage: ws ignore unmute <path…> | --stale")
		return 1
	}

	muted := ignore.NormalizeMuted(e.workspace, e.cfg.Ignore.Muted)
	type item struct{ abs, rel string }
	var items []item
	seen := map[string]bool{}
	bad := false
	out := quietOut(globals, stdout)
	for _, raw := range fs.Args() {
		abs, rel, isDir, err := e.target(raw, false)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			bad = true
			continue
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		st := ignore.MuteStateOf(e.engine, muted, rel, isDir)
		switch {
		case !st.Muted:
			fmt.Fprintf(out, "Not muted: %s: nothing to do\n", abs)
		case st.Entry == rel:
			items = append(items, item{abs, rel})
		case st.Entry != "":
			fmt.Fprintf(stderr, "%s is muted by its parent %s: unmute that instead\n", abs, style.AbsPath(e.workspace, st.Entry))
			bad = true
		default:
			fmt.Fprintf(stderr, "%s is under the included directory %s: its warnings stay muted, because that directory syncs everything by design.\n"+
				"  To change it, edit include with ws ignore edit\n", abs, ruleLabel(st.Rule, st.RuleSource))
			bad = true
		}
	}
	if bad {
		return 1
	}

	p := Plan{Command: "ignore.unmute"}
	for _, it := range items {
		it := it
		p.Actions = append(p.Actions, Action{
			ID:          "ignore-unmute-" + it.abs,
			Description: "Unmute " + it.abs,
			Execute: func() error {
				_, err := wsconfig.RemoveIgnoreMuted(e.configPath, e.workspace, it.rel)
				return err
			},
		})
	}
	return e.finish("ignore.unmute", p, globals, stdin, stdout, stderr)
}

// unmuteStale is `ignore unmute --stale`: it removes the ignore.muted entries
// whose path no longer exists, one checklist action each.
func (e ignoreEnv) unmuteStale(globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	entries := ignore.StaleMuted(e.workspace, e.cfg.Ignore.Muted)
	if len(entries) == 0 {
		fmt.Fprintln(quietOut(globals, stdout), "No stale muted paths: nothing to do")
	}
	p := Plan{Command: "ignore.unmute"}
	for _, rel := range entries {
		rel := rel
		abs := style.AbsPath(e.workspace, rel)
		p.Actions = append(p.Actions, Action{
			ID:          "ignore-unmute-" + abs,
			Description: "Unmute " + abs + " (no longer exists)",
			Execute: func() error {
				_, err := wsconfig.RemoveIgnoreMuted(e.configPath, e.workspace, rel)
				return err
			},
		})
	}
	return e.finish("ignore.unmute", p, globals, stdin, stdout, stderr)
}

// ── check ──────────────────────────────────────────────────────────

func (e ignoreEnv) check(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ignore-check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) != 1 {
		fmt.Fprintln(stderr, "usage: ws ignore check <path>")
		return 1
	}
	abs, err := wsconfig.ExpandUserPath(fs.Args()[0])
	if err != nil {
		fmt.Fprintf(stderr, "invalid path %q: %s\n", fs.Args()[0], err.Error())
		return 1
	}
	st, err := os.Stat(abs)
	if err != nil {
		fmt.Fprintf(stderr, "path does not exist: %s\n", abs)
		return 1
	}
	res, rel, err := ignore.Check(e.engine, e.workspace, abs, st.IsDir())
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	e.warnStale(globals, stderr)

	mute := ignore.MuteStateOf(e.engine, ignore.NormalizeMuted(e.workspace, e.cfg.Ignore.Muted), rel, st.IsDir())
	data := map[string]any{
		"path":        abs,
		"included":    res.Included,
		"rule":        res.Rule,
		"rule_source": res.RuleSource,
		"muted":       mute.Muted,
	}
	if !st.IsDir() {
		data["size_bytes"] = st.Size()
	}
	if globals.json {
		return writeJSON(stdout, stderr, "ignore.check", data)
	}

	out := textOut(globals, stdout)
	nc := globals.noColor
	var reason string
	switch {
	case !res.Included:
		reason = "rule " + ruleLabel(res.Rule, res.RuleSource)
	case res.ByInclude:
		reason = "included directory " + ruleLabel(res.Rule, res.RuleSource)
	default:
		reason = "no rule matches"
	}
	if res.Included {
		fmt.Fprintf(out, "%s %s  %s\n", style.IconCheck(nc), style.Badge("SYNCED", nc), style.Infof(nc, "%s", abs))
	} else {
		fmt.Fprintf(out, "%s %s  %s\n", style.IconCross(nc), style.Badge("IGNORED", nc), style.Infof(nc, "%s", abs))
	}
	fmt.Fprintf(out, "  Reason: %s\n", reason)
	if !st.IsDir() {
		fmt.Fprintf(out, "  Size: %s\n", style.Mutedf(nc, "%s", style.HumanBytes(st.Size())))
	}
	// Listing is not a verdict: an ignored path is a valid answer (exit 0).
	return 0
}

// ── tree ───────────────────────────────────────────────────────────

func (e ignoreEnv) tree(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ignore-tree", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	level := fs.Int("L", 0, "")
	// Hidden: the spellings of the old `ignore tree`.
	depth := fs.Int("depth", 0, "")
	pathFilter := fs.String("path", "", "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	// Positional arguments: [dir] and, hidden, [level].
	if len(fs.Args()) > 0 && strings.TrimSpace(*pathFilter) == "" {
		*pathFilter = fs.Args()[0]
	}
	if len(fs.Args()) > 1 && *depth == 0 && *level == 0 {
		if n, err := fmt.Sscanf(fs.Args()[1], "%d", depth); n != 1 || err != nil {
			fmt.Fprintln(stderr, "level must be an integer")
			return 1
		}
	}
	if *level > 0 {
		*depth = *level
	}
	if *depth <= 0 {
		*depth = -1 // unlimited
	}
	e.warnStale(globals, stderr)
	return runIgnoreTreeView(e.engine, e.workspace, *pathFilter, *depth, globals, stdout, stderr)
}

// ── edit ───────────────────────────────────────────────────────────

// normalizeRules makes nil lists empty and a missing schema 1, so rules compare
// by content.
func normalizeRules(r ignore.UserRules) ignore.UserRules {
	if r.Schema == 0 {
		r.Schema = ignore.UserRulesSchema
	}
	if r.Exclude == nil {
		r.Exclude = []ignore.RuleEntry{}
	}
	if r.Include == nil {
		r.Include = []ignore.RuleEntry{}
	}
	if r.SuppressDefaults == nil {
		r.SuppressDefaults = []string{}
	}
	return r
}

func (e ignoreEnv) edit(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ignore-edit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	editorFlag := fs.String("editor", "", "override editor command")
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	current := normalizeRules(e.rules)
	editPath := filepath.Join(e.workspace, ".ignore-edit.json")
	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if err := os.WriteFile(editPath, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	defer os.Remove(editPath)

	argv := editorFor(*editorFlag, e.cfg)
	if err := editor.Open(argv, editPath, stdout); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	edited, err := os.ReadFile(editPath)
	if err != nil {
		fmt.Fprintln(stderr, "error reading edited file: "+err.Error())
		return 1
	}
	var updated ignore.UserRules
	if err := json.Unmarshal(edited, &updated); err != nil {
		fmt.Fprintln(stderr, "validation error: "+err.Error())
		return 1
	}
	updated = normalizeRules(updated)

	changed := !reflect.DeepEqual(current, updated)
	stale, _ := ignore.Stale(e.megaignore, updated)
	p := Plan{Command: "ignore.edit"}
	if changed || stale {
		p.Actions = append(p.Actions, Action{
			ID:          "ignore-edit-save",
			Description: fmt.Sprintf("Save ignore_rules to %s and rewrite %s", e.configPath, e.megaignore),
			Execute: func() error {
				cfg, err := wsconfig.Load(e.configPath)
				if err != nil {
					return err
				}
				cfg.IgnoreRules = updated
				if err := wsconfig.Save(e.configPath, cfg); err != nil {
					return err
				}
				return ignore.WriteMegaignore(e.megaignore, updated)
			},
		})
	}
	res := RunPlan(p, stdin, stdout, globals)
	stats := ignore.GetRuleStats(current)
	if res.WasExecuted("ignore-edit-save") {
		stats = ignore.GetRuleStats(updated)
	}
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "ignore.edit", globals.dryRun, map[string]any{
			"actions": res.Actions,
			"stats":   stats,
		})
	}
	if len(p.Actions) == 0 {
		fmt.Fprintln(quietOut(globals, stdout), "Nothing to do: the rules are unchanged.")
	}
	return res.ExitCode()
}
