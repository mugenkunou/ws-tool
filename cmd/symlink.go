package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/symlink"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// runSymlink dispatches `ws symlink <verb>` (spec §5 symlink).
func runSymlink(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	workspacePath, configPath, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ws symlink <ls|add|link|unlink> …")
		return 1
	}

	verb, rest := args[0], args[1:]
	switch verb {
	case "ls":
		return runSymlinkLs(rest, globals, stdout, stderr, workspacePath, configPath)
	case "add":
		return runSymlinkAdd(rest, globals, stdin, stdout, stderr, workspacePath, configPath)
	case "link":
		return runSymlinkLink(rest, globals, stdin, stdout, stderr, workspacePath, configPath)
	case "unlink":
		return runSymlinkUnlink(rest, globals, stdin, stdout, stderr, workspacePath, configPath)
	}
	fmt.Fprintf(stderr, "unknown command: symlink %s\n", verb)
	if hint, gone := removedVerbHints[verb]; gone {
		fmt.Fprintln(stderr, hint)
	}
	fmt.Fprintln(stderr, "usage: ws symlink <ls|add|link|unlink> …")
	return 1
}

// removedVerbHints say why a verb that no longer exists is unknown (plan 0003).
var removedVerbHints = map[string]string{
	"edit":    "edit was removed: open the file under symlinks/ in your editor",
	"backup":  "backup was removed: symlinked files sync with the workspace; git init symlinks/ for history",
	"git":     "git was removed: symlinked files sync with the workspace; git init symlinks/ for history",
	"migrate": "migrate was removed",
}

// classRemoved is the error for the removed --class and --to flags.
func classRemoved(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--class" || strings.HasPrefix(a, "--class=") || a == "--to" || strings.HasPrefix(a, "--to=") {
			return true
		}
	}
	return false
}

const classRemovedMessage = "ws: --class/--to were removed: pass the workspace path as a second argument"

// resolveTargets resolves path arguments the way every command does (absolute,
// ~, or relative to the current directory) and drops repeats.
func resolveTargets(raw []string) ([]string, error) {
	var out []string
	for _, p := range raw {
		abs, err := wsconfig.ExpandUserPath(p)
		if err != nil {
			return nil, fmt.Errorf("invalid path %q: %w", p, err)
		}
		if !slices.Contains(out, abs) {
			out = append(out, abs)
		}
	}
	return out, nil
}

// ── ls ─────────────────────────────────────────────────────────────

func runSymlinkLs(args []string, globals globalFlags, stdout, stderr io.Writer, workspacePath, configPath string) int {
	fs := flag.NewFlagSet("symlink-ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	lf := registerListFlags(fs, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintln(stderr, "ws symlink ls takes no targets; use --filter to narrow the rows")
		return 1
	}
	// ls runs the $HOME walk for untracked links; the Dashboard does not.
	listing, err := symlink.Load(workspacePath, configPath, true)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if code := renderList(globals, "symlink.ls", symlink.Schema(resource.CLIView()), listing.All, lf, "No managed symlinks.", stdout, stderr); code != 0 {
		return code
	}
	if globals.json || globals.quiet {
		return 0
	}
	out := textOut(globals, stdout)
	for _, note := range listing.Notes {
		fmt.Fprintln(out, style.Mutedf(globals.noColor, "%s", note))
	}
	return 0
}

// ── add ────────────────────────────────────────────────────────────

// wsArg resolves the optional second path of `add <host> <ws-path>`: absolute, or
// relative to the workspace (not to the current directory).
func wsArg(workspacePath, raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if strings.HasPrefix(strings.TrimSpace(raw), "~") {
		return wsconfig.ExpandUserPath(raw)
	}
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw), nil
	}
	return filepath.Clean(filepath.Join(workspacePath, raw)), nil
}

func runSymlinkAdd(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer, workspacePath, configPath string) int {
	if classRemoved(args) {
		fmt.Fprintln(stderr, classRemovedMessage)
		return 1
	}
	fs := flag.NewFlagSet("symlink-add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	wholeDir := fs.Bool("dir", false, "")
	eachFile := fs.Bool("files", false, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	rest := fs.Args()
	if len(rest) == 0 || len(rest) > 2 {
		fmt.Fprintln(stderr, "usage: ws symlink add <host-path> [<ws-path>] [--dir | --files]")
		return 1
	}
	if *wholeDir && *eachFile {
		fmt.Fprintln(stderr, "--dir and --files cannot be used together")
		return 1
	}
	hosts, err := resolveTargets(rest[:1])
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	host := hosts[0]
	toAbs := ""
	if len(rest) == 2 {
		if toAbs, err = wsArg(workspacePath, rest[1]); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
	}

	out := textOut(globals, stdout)
	if globals.json {
		out = io.Discard
	}

	// One host path becomes one or more entries.
	var opts []symlink.AddOptions
	base := symlink.AddOptions{WorkspacePath: workspacePath, ConfigPath: configPath}
	li, lerr := os.Lstat(host)
	realDir := lerr == nil && li.Mode()&os.ModeSymlink == 0 && li.IsDir()
	if _, managed, _ := managedEntry(configPath, host); managed {
		realDir = false // an entry decides for itself (a drifted folder is refused by Add)
	}
	switch {
	case realDir && *eachFile:
		if toAbs != "" {
			fmt.Fprintln(stderr, "--files stores every file at its own mirror path; it takes no <ws-path>")
			return 1
		}
		files, ferr := symlink.CollectFiles(symlink.DirEntry{AbsPath: host, IsDir: true})
		if ferr != nil {
			fmt.Fprintln(stderr, ferr.Error())
			return 1
		}
		for _, f := range files {
			o := base
			o.Host = f
			opts = append(opts, o)
		}
	case realDir && (*wholeDir || symlink.IsCatalogFolder(host)):
		o := base
		o.Host, o.To, o.Dir = host, toAbs, true
		opts = append(opts, o)
	case realDir:
		if toAbs != "" {
			fmt.Fprintln(stderr, "a folder goes in whole with --dir to <ws-path>, or file by file with --files")
			return 1
		}
		files, code := selectDirectoryFiles(host, globals, stdin, stdout, stderr)
		if code != 0 {
			return code
		}
		for _, f := range files {
			o := base
			o.Host = f
			opts = append(opts, o)
		}
	default:
		if *wholeDir && lerr == nil && !li.IsDir() {
			fmt.Fprintf(stderr, "--dir needs a directory: %s\n", host)
			return 1
		}
		o := base
		o.Host, o.To, o.Dir = host, toAbs, *wholeDir
		opts = append(opts, o)
	}

	return runAddPlan(opts, globals, stdin, stdout, stderr, out, workspacePath)
}

// managedEntry finds the entry for a host path.
func managedEntry(configPath, host string) (wsconfig.Link, bool, error) {
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		return wsconfig.Link{}, false, err
	}
	l, ok := symlink.FindLink(cfg, host)
	return l, ok, nil
}

// runAddPlan plans every add (a refusal stops before anything changes), prints the
// plan lines naming both paths, and runs them.
func runAddPlan(opts []symlink.AddOptions, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer, out io.Writer, workspacePath string) int {
	nc := globals.noColor
	p := Plan{Command: "symlink.add"}
	// A path that cannot be added is reported and counts as failed, like a missing
	// copy in link; the others still go ahead, and the exit code follows the plan.
	var refused []ActionStatus
	for _, o := range opts {
		o := o
		ap, err := symlink.PlanAdd(o)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %s\n", o.Host, err)
			refused = append(refused, ActionStatus{ID: "symlink-add-" + o.Host, Status: "failed", Error: err.Error()})
			continue
		}
		var desc string
		switch ap.Case {
		case symlink.CaseManaged:
			fmt.Fprintf(out, "  %s %s is already managed\n", style.Mutedf(nc, "–"), ap.Host)
			continue
		case symlink.CaseNothing:
			fmt.Fprintf(out, "  %s %s: %s\n", style.Mutedf(nc, "–"), ap.Host, ap.Note)
			continue
		case symlink.CaseTake:
			desc = fmt.Sprintf("Add %s again (the live file replaces %s; the old copy goes to the trash)", ap.Host, ap.Workspace)
		case symlink.CaseRegister:
			desc = fmt.Sprintf("Register %s (already a link to %s; nothing moves)", ap.Host, ap.Workspace)
		case symlink.CaseLinkOut:
			desc = fmt.Sprintf("Link %s → %s (the workspace copy exists; nothing moves)", ap.Host, ap.Workspace)
		default:
			desc = fmt.Sprintf("Add %s → %s", ap.Host, ap.Workspace)
			if ap.Dir {
				desc = fmt.Sprintf("Add directory %s → %s (whole)", ap.Host, ap.Workspace)
			}
		}
		if ap.Sudo {
			desc += " (sudo)"
		}
		p.Actions = append(p.Actions, Action{
			ID:          "symlink-add-" + ap.Host,
			Description: desc,
			Execute: func() error {
				_, err := symlink.Add(o)
				return err
			},
		})
	}
	if len(p.Actions) == 0 && len(refused) == 0 {
		if globals.json {
			return writeJSON(stdout, stderr, "symlink.add", map[string]any{"actions": []ActionStatus{}})
		}
		return 0
	}
	planResult := RunPlan(p, stdin, stdout, globals)
	planResult.Actions = append(planResult.Actions, refused...)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "symlink.add", globals.dryRun, map[string]any{"actions": planResult.Actions})
	}
	return planResult.ExitCode()
}

// selectDirectoryFiles expands an unknown directory into the files to track and
// asks which ones (spec §5 symlink add). --quiet and --json take the defaults.
func selectDirectoryFiles(abs string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) ([]string, int) {
	out := textOut(globals, stdout)
	if globals.json {
		out = io.Discard
	}
	fmt.Fprintf(out, "Scanning %s ...\n", abs)
	fmt.Fprintln(out, style.Mutedf(globals.noColor, "  (pass --dir to link the whole directory as one entry instead)"))
	entries, expanded, err := symlink.ExpandDir(abs)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return nil, 1
	}
	if len(entries) == 0 {
		fmt.Fprintln(out, "Directory is empty — nothing to add.")
		return nil, 0
	}
	selected := promptDirSelection(stdin, stdout, globals, entries, expanded, abs)
	if len(selected) == 0 {
		fmt.Fprintln(out, "Nothing selected.")
		return nil, 0
	}
	var files []string
	for _, e := range selected {
		fs, err := symlink.CollectFiles(e)
		if err != nil {
			fmt.Fprintf(stderr, "error reading %s: %v\n", e.AbsPath, err)
			return nil, 1
		}
		files = append(files, fs...)
	}
	return files, 0
}

// ── link ───────────────────────────────────────────────────────────

func runSymlinkLink(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer, workspacePath, configPath string) int {
	if classRemoved(args) {
		fmt.Fprintln(stderr, classRemovedMessage)
		return 1
	}
	fs := flag.NewFlagSet("symlink-link", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	paths, err := resolveTargets(fs.Args())
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return runSymlinkRelink(paths, globals, stdin, stdout, stderr, workspacePath, configPath)
}

// runSymlinkRelink repairs entries: the workspace copy wins. A real file standing at
// the host path (drifted) is never replaced: `add` keeps the live file.
func runSymlinkRelink(paths []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer, workspacePath, configPath string) int {
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	out := textOut(globals, stdout)
	if globals.json {
		out = io.Discard
	}
	nc := globals.noColor

	// Targets: every entry, or the named paths (an entry, or an orphan whose mirror
	// path exists in the workspace). A named path that is neither is an error before
	// anything changes.
	var targets []string
	if len(paths) == 0 {
		for _, l := range cfg.Symlinks {
			targets = append(targets, l.System)
		}
	} else {
		targets = paths
	}

	p := Plan{Command: "symlink.link"}
	var missing []ActionStatus
	sudoOK := symlink.SudoAvailable()
	for _, host := range targets {
		host := host
		lp, err := symlink.PlanLink(workspacePath, configPath, host)
		if err != nil {
			fmt.Fprintf(stderr, "%s\n", err)
			return 1
		}
		switch {
		case lp.Missing:
			// Not offered as an action: nothing can be linked. Reported, and it
			// counts as failed in the exit code and the JSON.
			fmt.Fprintf(out, "%s %s: workspace copy %s is missing\n", style.IconCross(nc), host, lp.Workspace)
			missing = append(missing, ActionStatus{
				ID:     "symlink-link-" + host,
				Status: "failed",
				Error:  fmt.Sprintf("workspace copy %s is missing", lp.Workspace),
			})
		case lp.OK:
			if len(paths) > 0 {
				fmt.Fprintf(out, "  %s %s is already linked\n", style.Mutedf(nc, "–"), host)
			}
		case lp.Drifted:
			fmt.Fprintf(out, "  %s %s: drifted: ws symlink add %s keeps the live file\n", style.Mutedf(nc, "–"), host, host)
		case lp.Link.Sudo && !sudoOK:
			fmt.Fprintf(out, "  %s %s: needs sudo, which is not available; skipped\n", style.Mutedf(nc, "–"), host)
		default:
			desc := fmt.Sprintf("Link %s → %s", host, lp.Workspace)
			if lp.Register {
				desc = fmt.Sprintf("Register and link %s → %s", host, lp.Workspace)
			}
			if lp.Backup != "" {
				desc += fmt.Sprintf(" (what is there moves to %s)", lp.Backup)
			}
			p.Actions = append(p.Actions, Action{
				ID:          "symlink-link-" + host,
				Description: desc,
				Execute: func() error {
					return symlink.Link(symlink.LinkOptions{WorkspacePath: workspacePath, ConfigPath: configPath, SystemPath: host})
				},
			})
		}
	}
	if len(p.Actions) == 0 && len(missing) == 0 && len(paths) == 0 {
		if globals.json {
			return writeJSON(stdout, stderr, "symlink.link", map[string]any{"actions": []ActionStatus{}})
		}
		fmt.Fprintln(out, "Nothing to link.")
		return 0
	}

	planResult := RunPlan(p, stdin, stdout, globals)
	planResult.Actions = append(planResult.Actions, missing...)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "symlink.link", globals.dryRun, map[string]any{"actions": planResult.Actions})
	}
	return planResult.ExitCode()
}

// ── unlink ─────────────────────────────────────────────────────────

func runSymlinkUnlink(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer, workspacePath, configPath string) int {
	fs := flag.NewFlagSet("symlink-unlink", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	all := fs.Bool("all", false, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	paths, err := resolveTargets(fs.Args())
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if *all == (len(paths) > 0) {
		fmt.Fprintln(stderr, "usage: ws symlink unlink <path…> | --all (paths or --all, not both)")
		return 1
	}
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	var links []wsconfig.Link
	if *all {
		links = cfg.Symlinks
	} else {
		for _, p := range paths {
			l, ok := symlink.FindLink(cfg, p)
			if !ok {
				fmt.Fprintf(stderr, "not a managed symlink: %s\n", p)
				return 1
			}
			links = append(links, l)
		}
	}
	if len(links) == 0 {
		if globals.json {
			return writeJSON(stdout, stderr, "symlink.unlink", map[string]any{"actions": []ActionStatus{}})
		}
		fmt.Fprintln(textOut(globals, stdout), "No managed symlinks.")
		return 0
	}

	p := Plan{Command: "symlink.unlink"}
	for _, l := range links {
		l := l
		p.Actions = append(p.Actions, Action{
			ID:          "symlink-unlink-" + l.System,
			Description: unlinkDescription(symlink.PlanUnlink(workspacePath, cfg, l)),
			Execute: func() error {
				return symlink.Unlink(symlink.UnlinkOptions{WorkspacePath: workspacePath, ConfigPath: configPath, SystemPath: l.System})
			},
		})
	}
	planResult := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "symlink.unlink", globals.dryRun, map[string]any{"actions": planResult.Actions})
	}
	return planResult.ExitCode()
}

// unlinkDescription is the plan line of one unlink: what happens to the copy,
// and what happens to a live file that replaced the link.
func unlinkDescription(p symlink.UnlinkPlan) string {
	switch p.Mode {
	case symlink.ModeMissing:
		return fmt.Sprintf("Unregister %s (workspace copy missing; nothing to restore)", p.Link.System)
	case symlink.ModeCopy:
		d := fmt.Sprintf("Unlink %s (copy %s there; the original stays)", p.Link.System, p.Workspace)
		if p.Backup != "" {
			d = strings.TrimSuffix(d, ")") + fmt.Sprintf("; the live file moves to %s)", p.Backup)
		}
		return d
	}
	d := fmt.Sprintf("Unlink %s (move %s back)", p.Link.System, p.Workspace)
	if p.Backup != "" {
		d = strings.TrimSuffix(d, ")") + fmt.Sprintf("; the live file moves to %s)", p.Backup)
	}
	return d
}

// ── shared prompts ─────────────────────────────────────────────────

// promptDirSelection presents the expanded directory entries to the user and
// returns the subset they choose to track. Defaults to everything except catalog secrets.
//
// In --json / --quiet mode the defaults are accepted automatically.
func promptDirSelection(stdin io.Reader, stdout io.Writer, globals globalFlags, entries []symlink.DirEntry, expanded bool, rootPath string) []symlink.DirEntry {
	out := textOut(globals, stdout)
	if globals.json {
		out = io.Discard
	}
	nc := globals.noColor

	totalFiles := 0
	for _, e := range entries {
		totalFiles += e.FileCount
	}

	if expanded {
		fmt.Fprintf(out, "  %d file(s)\n\n", totalFiles)
	} else {
		fmt.Fprintf(out, "  %d file(s) total — showing top-level entries:\n\n", totalFiles)
	}

	var defaultIdxs []int
	for i, e := range entries {
		num := i + 1
		classStr := ""
		if e.Secret {
			classStr = style.Warningf(nc, "⚠ secret")
		}
		var countStr string
		if e.IsDir {
			countStr = fmt.Sprintf("  (%d files)", e.FileCount)
		}
		fmt.Fprintf(out, "  %2d  %-40s %8s  %s%s\n", num, e.AbsPath, style.HumanBytes(e.Size), classStr, countStr)
		if !e.Secret {
			defaultIdxs = append(defaultIdxs, num)
		}
	}

	var defaultParts []string
	for _, idx := range defaultIdxs {
		defaultParts = append(defaultParts, fmt.Sprintf("%d", idx))
	}
	defaultStr := strings.Join(defaultParts, ",")
	if defaultStr == "" {
		defaultStr = "none"
	}
	fmt.Fprintln(out)

	defaults := func() []symlink.DirEntry {
		var result []symlink.DirEntry
		for _, idx := range defaultIdxs {
			result = append(result, entries[idx-1])
		}
		return result
	}
	if globals.json || globals.quiet {
		return defaults()
	}

	fmt.Fprintf(out, "Select entries to track [%s] (or Enter for defaults, \"all\", \"none\"): ", style.Mutedf(nc, "%s", defaultStr))
	line, err := readLine(stdin)
	if err != nil || strings.TrimSpace(line) == "" {
		return defaults()
	}

	line = strings.TrimSpace(line)
	switch strings.ToLower(line) {
	case "all":
		return entries
	case "none":
		return nil
	}

	var result []symlink.DirEntry
	seen := make(map[int]bool)
	for _, part := range strings.Split(line, ",") {
		n := 0
		for _, c := range strings.TrimSpace(part) {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			}
		}
		if n >= 1 && n <= len(entries) && !seen[n] {
			seen[n] = true
			result = append(result, entries[n-1])
		}
	}
	return result
}
