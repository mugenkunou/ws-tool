package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/editor"
	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// runDotfile dispatches `ws dotfile <verb>` (spec §5 dotfile).
func runDotfile(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	workspacePath, configPath, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ws dotfile <ls|add|link|unlink|edit|backup> …")
		return 1
	}

	verb, rest := args[0], args[1:]
	switch verb {
	case "ls":
		return runDotfileLs(rest, globals, stdout, stderr, workspacePath, configPath)
	case "add":
		return runDotfileAdd(rest, globals, stdin, stdout, stderr, workspacePath, configPath)
	case "link":
		return runDotfileLink(rest, globals, stdin, stdout, stderr, workspacePath, configPath)
	case "unlink":
		return runDotfileUnlink(rest, globals, stdin, stdout, stderr, workspacePath, configPath)
	case "edit":
		return runDotfileEdit(rest, globals, stdout, stderr, workspacePath, configPath)
	case "backup":
		return runBackup(dotfileBackupSpec(workspacePath, configPath), rest, globals, stdin, stdout, stderr)
	case "migrate": // hidden: a one-off legacy split, until it is deleted
		return runDotfileMigrate(rest, globals, stdin, stdout, stderr, workspacePath, configPath)
	}
	fmt.Fprintf(stderr, "unknown command: dotfile %s\n", verb)
	fmt.Fprintln(stderr, "usage: ws dotfile <ls|add|link|unlink|edit|backup> …")
	return 1
}

// classRemoved is the error for the removed --class flag (spec §3.5).
func classRemoved(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--class" || strings.HasPrefix(a, "--class=") {
			return true
		}
	}
	return false
}

const classRemovedMessage = "ws: --class was removed; where the copy lives says what it is. Use --to <ws-path> to choose where it goes"

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

func runDotfileLs(args []string, globals globalFlags, stdout, stderr io.Writer, workspacePath, configPath string) int {
	fs := flag.NewFlagSet("dotfile-ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	lf := registerListFlags(fs, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintln(stderr, "ws dotfile ls takes no targets; use --filter status!=ok to list only the problems")
		return 1
	}
	entries, err := dotfile.Entries(workspacePath, configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return renderList(globals, "dotfile.ls", dotfile.Schema(resource.CLIView()), entries, lf, "No managed dotfiles.", stdout, stderr)
}

// ── add ────────────────────────────────────────────────────────────

// addTarget is one path `dotfile add` was asked to take.
type addTarget struct {
	path string
	// kind: "new" (a file or whole directory), "again" (an overwritten entry).
	again bool
	whole bool // a directory taken whole (--dir)
}

func runDotfileAdd(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer, workspacePath, configPath string) int {
	if classRemoved(args) {
		fmt.Fprintln(stderr, classRemovedMessage)
		return 1
	}
	fs := flag.NewFlagSet("dotfile-add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	to := fs.String("to", "", "")
	wholeDir := fs.Bool("dir", false, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(stderr, "usage: ws dotfile add <path…> [--to <ws-path>] [--dir]")
		return 1
	}
	paths, err := resolveTargets(fs.Args())
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	toAbs := ""
	if *to != "" {
		if len(paths) != 1 {
			fmt.Fprintln(stderr, "--to takes exactly one path")
			return 1
		}
		if toAbs, err = wsconfig.ExpandUserPath(*to); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
	}

	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	issues, err := dotfile.Scan(dotfile.ScanOptions{WorkspacePath: workspacePath, ConfigPath: configPath})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	issueFor := map[string]dotfile.Issue{}
	for _, is := range issues {
		issueFor[is.SystemPath] = is
	}

	out := textOut(globals, stdout)
	if globals.json {
		out = io.Discard
	}
	nc := globals.noColor

	var targets []addTarget
	for _, abs := range paths {
		if link, managed := dotfile.FindLink(cfg, abs); managed {
			if *to != "" {
				fmt.Fprintf(stderr, "--to applies to a new path, and %s is already managed (ws dotfile link %s repairs it)\n", abs, abs)
				return 1
			}
			is, bad := issueFor[link.System]
			switch {
			case !bad:
				fmt.Fprintf(out, "  %s %s is already managed\n", style.Mutedf(nc, "–"), abs)
			case is.Status == dotfile.StatusOverwritten:
				targets = append(targets, addTarget{path: abs, again: true})
			default: // broken: nothing to take
				fmt.Fprintf(out, "  %s %s: nothing to take (%s); try: ws dotfile link %s\n", style.Mutedf(nc, "–"), abs, is.Message, abs)
			}
			continue
		}

		info, statErr := os.Lstat(abs)
		isDir := statErr == nil && info.IsDir()
		switch {
		case *wholeDir:
			if !isDir {
				fmt.Fprintf(stderr, "--dir needs a directory: %s\n", abs)
				return 1
			}
			targets = append(targets, addTarget{path: abs, whole: true})
		case isDir:
			files, code := selectDirectoryFiles(abs, globals, stdin, stdout, stderr)
			if code != 0 {
				return code
			}
			for _, f := range files {
				targets = append(targets, addTarget{path: f})
			}
		default:
			targets = append(targets, addTarget{path: abs})
		}
	}

	// Unless --to or --dir says where it goes, warn about files that look like
	// secrets, once, and let the user drop them.
	if *to == "" && !*wholeDir {
		var fresh []string
		for _, t := range targets {
			if !t.again {
				fresh = append(fresh, t.path)
			}
		}
		kept, _ := warnAndFilterSecrets(fresh, stdin, stdout, globals, configPath)
		targets = slices.DeleteFunc(targets, func(t addTarget) bool { return !t.again && !slices.Contains(kept, t.path) })
	}

	p := Plan{Command: "dotfile.add"}
	var labels []string
	for _, t := range targets {
		t := t
		labels = append(labels, t.path)
		switch {
		case t.again:
			link, _ := dotfile.FindLink(cfg, t.path)
			p.Actions = append(p.Actions, Action{
				ID: "dotfile-add-" + t.path,
				Description: fmt.Sprintf("Add %s again (the live file replaces %s; the old copy goes to the trash)",
					t.path, dotfile.WorkspaceCopy(workspacePath, link)),
				Execute: func() error {
					return dotfile.TakeLive(dotfile.TakeLiveOptions{WorkspacePath: workspacePath, ConfigPath: configPath, SystemPath: t.path})
				},
			})
		default:
			p.Actions = append(p.Actions, Action{
				ID:          "dotfile-add-" + t.path,
				Description: addDescription(workspacePath, t, toAbs),
				Execute: func() error {
					_, err := dotfile.Add(dotfile.AddOptions{
						WorkspacePath: workspacePath, ConfigPath: configPath, SystemPath: t.path, To: toAbs, Dir: t.whole,
					})
					return err
				},
			})
		}
	}

	planResult := RunPlan(p, stdin, stdout, globals)
	if planResult.ExecutedCount() > 0 {
		dotfileGitAutoSync(workspacePath, configPath, "dotfile add: "+strings.Join(labels, ", "), globals, stdout, stderr)
	}
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "dotfile.add", globals.dryRun, map[string]any{"actions": planResult.Actions})
	}
	return planResult.ExitCode()
}

// addDescription is the plan line of a new add: where the copy will go.
func addDescription(workspacePath string, t addTarget, toAbs string) string {
	dest := toAbs
	secret := false
	if dest == "" {
		isDir := t.whole
		secret = dotfile.LooksLikeSecret(t.path, isDir)
		rel, err := dotfile.StoragePath(t.path, secret)
		if err != nil {
			return "Add " + t.path
		}
		dest = filepath.Join(workspacePath, filepath.FromSlash(rel))
	}
	desc := fmt.Sprintf("Add %s → %s", t.path, dest)
	if t.whole {
		desc = fmt.Sprintf("Add directory %s → %s (whole)", t.path, dest)
	}
	if secret {
		desc += " (secret)"
	}
	return desc
}

// selectDirectoryFiles expands a directory into the files to track and asks
// which ones (spec §5 dotfile add). --quiet and --json take the defaults.
func selectDirectoryFiles(abs string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) ([]string, int) {
	out := textOut(globals, stdout)
	if globals.json {
		out = io.Discard
	}
	fmt.Fprintf(out, "Scanning %s ...\n", abs)
	fmt.Fprintln(out, style.Mutedf(globals.noColor, "  (pass --dir to link the whole directory as one entry instead)"))
	entries, expanded, err := dotfile.ExpandDir(abs)
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
		fs, err := dotfile.CollectFiles(e)
		if err != nil {
			fmt.Fprintf(stderr, "error reading %s: %v\n", e.AbsPath, err)
			return nil, 1
		}
		files = append(files, fs...)
	}
	return files, 0
}

// ── link ───────────────────────────────────────────────────────────

func runDotfileLink(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer, workspacePath, configPath string) int {
	if classRemoved(args) {
		fmt.Fprintln(stderr, classRemovedMessage)
		return 1
	}
	fs := flag.NewFlagSet("dotfile-link", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	to := fs.String("to", "", "")
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
	if *to != "" {
		return runDotfileLinkTo(paths, *to, globals, stdin, stdout, stderr, workspacePath, configPath)
	}
	return runDotfileRelink(paths, globals, stdin, stdout, stderr, workspacePath, configPath)
}

// runDotfileRelink repairs entries: the workspace copy wins.
func runDotfileRelink(paths []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer, workspacePath, configPath string) int {
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	issues, err := dotfile.Scan(dotfile.ScanOptions{WorkspacePath: workspacePath, ConfigPath: configPath})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	unhealthy := map[string]bool{}
	for _, is := range issues {
		unhealthy[is.SystemPath] = true
	}

	out := textOut(globals, stdout)
	if globals.json {
		out = io.Discard
	}
	nc := globals.noColor

	// Targets: every entry that is not ok, or the named ones. A named path that
	// is not an entry is an error before anything changes.
	var links []wsconfig.Link
	if len(paths) == 0 {
		for _, l := range cfg.Links {
			if unhealthy[l.System] {
				links = append(links, l)
			}
		}
	} else {
		for _, p := range paths {
			l, ok := dotfile.FindLink(cfg, p)
			if !ok {
				fmt.Fprintf(stderr, "not a managed dotfile: %s\n", p)
				return 1
			}
			links = append(links, l)
		}
	}

	p := Plan{Command: "dotfile.link"}
	var missing []ActionStatus
	for _, l := range links {
		l := l
		rp := dotfile.PlanRelink(workspacePath, l)
		switch {
		case rp.Missing:
			// Not offered as an action: nothing can be linked. Reported, and it
			// counts as failed in the exit code and the JSON.
			fmt.Fprintf(out, "%s %s: workspace copy %s is missing\n", style.IconCross(nc), l.System, rp.Workspace)
			missing = append(missing, ActionStatus{
				ID:     "dotfile-link-" + l.System,
				Status: "failed",
				Error:  fmt.Sprintf("workspace copy %s is missing", rp.Workspace),
			})
		case rp.OK:
			fmt.Fprintf(out, "  %s %s is already linked\n", style.Mutedf(nc, "–"), l.System)
		default:
			desc := fmt.Sprintf("Link %s → %s", l.System, rp.Workspace)
			if rp.Backup != "" {
				desc += fmt.Sprintf(" (the live file moves to %s)", rp.Backup)
			}
			p.Actions = append(p.Actions, Action{
				ID:          "dotfile-link-" + l.System,
				Description: desc,
				Execute: func() error {
					return dotfile.Relink(dotfile.RelinkOptions{WorkspacePath: workspacePath, ConfigPath: configPath, SystemPath: l.System})
				},
			})
		}
	}
	if len(p.Actions) == 0 && len(missing) == 0 && len(paths) == 0 {
		if globals.json {
			return writeJSON(stdout, stderr, "dotfile.link", map[string]any{"actions": []ActionStatus{}})
		}
		fmt.Fprintln(out, "All dotfile links are ok.")
		return 0
	}

	planResult := RunPlan(p, stdin, stdout, globals)
	planResult.Actions = append(planResult.Actions, missing...)
	// Repairing changes nothing in the backup repository, so it does not commit.
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "dotfile.link", globals.dryRun, map[string]any{"actions": planResult.Actions})
	}
	return planResult.ExitCode()
}

// runDotfileLinkTo manages a path as a link to a file already in the workspace.
func runDotfileLinkTo(paths []string, to string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer, workspacePath, configPath string) int {
	if len(paths) != 1 {
		fmt.Fprintln(stderr, "usage: ws dotfile link <path> --to <ws-path>")
		return 1
	}
	toAbs, err := wsconfig.ExpandUserPath(to)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	opts := dotfile.LinkToOptions{WorkspacePath: workspacePath, ConfigPath: configPath, SystemPath: paths[0], To: toAbs}
	lp, err := dotfile.PlanLinkTo(opts)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	desc := fmt.Sprintf("Link %s → %s (new entry)", lp.System, lp.Workspace)
	if lp.Backup != "" {
		desc = fmt.Sprintf("Link %s → %s (new entry; the live file moves to %s)", lp.System, lp.Workspace, lp.Backup)
	}
	p := Plan{Command: "dotfile.link", Actions: []Action{{
		ID:          "dotfile-link-" + lp.System,
		Description: desc,
		Execute: func() error {
			_, err := dotfile.LinkTo(opts)
			return err
		},
	}}}
	planResult := RunPlan(p, stdin, stdout, globals)
	if planResult.ExecutedCount() > 0 {
		dotfileGitAutoSync(workspacePath, configPath, "dotfile link: "+lp.System, globals, stdout, stderr)
	}
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "dotfile.link", globals.dryRun, map[string]any{
			"system":         lp.System,
			"workspace_path": lp.Workspace,
			"actions":        planResult.Actions,
		})
	}
	return planResult.ExitCode()
}

// ── unlink ─────────────────────────────────────────────────────────

func runDotfileUnlink(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer, workspacePath, configPath string) int {
	fs := flag.NewFlagSet("dotfile-unlink", flag.ContinueOnError)
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
		fmt.Fprintln(stderr, "usage: ws dotfile unlink <path…> | --all (paths or --all, not both)")
		return 1
	}
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	var links []wsconfig.Link
	if *all {
		links = cfg.Links
	} else {
		for _, p := range paths {
			l, ok := dotfile.FindLink(cfg, p)
			if !ok {
				fmt.Fprintf(stderr, "not a managed dotfile: %s\n", p)
				return 1
			}
			links = append(links, l)
		}
	}
	if len(links) == 0 {
		if globals.json {
			return writeJSON(stdout, stderr, "dotfile.unlink", map[string]any{"actions": []ActionStatus{}})
		}
		fmt.Fprintln(textOut(globals, stdout), "No managed dotfiles.")
		return 0
	}

	p := Plan{Command: "dotfile.unlink"}
	var labels []string
	for _, l := range links {
		l := l
		labels = append(labels, l.System)
		p.Actions = append(p.Actions, Action{
			ID:          "dotfile-unlink-" + l.System,
			Description: unlinkDescription(dotfile.PlanUnlink(workspacePath, cfg, l)),
			Execute: func() error {
				return dotfile.Unlink(dotfile.UnlinkOptions{WorkspacePath: workspacePath, ConfigPath: configPath, SystemPath: l.System})
			},
		})
	}
	planResult := RunPlan(p, stdin, stdout, globals)
	if planResult.ExecutedCount() > 0 {
		dotfileGitAutoSync(workspacePath, configPath, "dotfile unlink: "+strings.Join(labels, ", "), globals, stdout, stderr)
	}
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "dotfile.unlink", globals.dryRun, map[string]any{"actions": planResult.Actions})
	}
	return planResult.ExitCode()
}

// unlinkDescription is the plan line of one unlink: what happens to the copy,
// and what happens to a live file that replaced the link.
func unlinkDescription(p dotfile.UnlinkPlan) string {
	switch p.Mode {
	case dotfile.ModeMissing:
		return fmt.Sprintf("Unregister %s (workspace copy missing; nothing to restore)", p.Link.System)
	case dotfile.ModeCopy:
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

// ── edit ───────────────────────────────────────────────────────────

// runDotfileEdit opens an entry's workspace copy in the editor. It runs no
// plan: ws itself changes nothing, and the next auto-commit or backup push
// includes the edit.
func runDotfileEdit(args []string, globals globalFlags, stdout, stderr io.Writer, workspacePath, configPath string) int {
	fs := flag.NewFlagSet("dotfile-edit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) != 1 {
		fmt.Fprintln(stderr, "usage: ws dotfile edit <path>")
		return 1
	}
	abs, err := wsconfig.ExpandUserPath(fs.Args()[0])
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	link, ok := dotfile.FindLink(cfg, abs)
	if !ok {
		fmt.Fprintf(stderr, "not a managed dotfile: %s\n", abs)
		return 1
	}
	copyPath := dotfile.WorkspaceCopy(workspacePath, link)
	info, err := os.Stat(copyPath)
	if err != nil {
		fmt.Fprintf(stderr, "workspace copy %s is missing\n", copyPath)
		return 1
	}

	nc := globals.noColor
	if issues, _ := dotfile.Scan(dotfile.ScanOptions{WorkspacePath: workspacePath, ConfigPath: configPath}); slices.ContainsFunc(issues, func(i dotfile.Issue) bool {
		return i.SystemPath == abs && i.Status == dotfile.StatusOverwritten
	}) {
		fmt.Fprintln(stderr, style.ResultWarning(nc, "%s is a separate live file; edits to the workspace copy reach it only after ws dotfile link %s. To keep the live content, ws dotfile add it first.", abs, abs))
	}
	if !info.IsDir() {
		if f, err := os.OpenFile(copyPath, os.O_WRONLY, 0); err != nil {
			fmt.Fprintln(stderr, style.ResultWarning(nc, "you cannot write %s; ws does not run the editor with sudo", copyPath))
		} else {
			_ = f.Close()
		}
	}

	// With --json the envelope owns stdout, so the editor's screen goes to stderr.
	editorOut := stdout
	if globals.json {
		editorOut = stderr
	}
	if err := editor.Open(editorFor("", cfg), copyPath, editorOut); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if globals.json {
		return writeJSON(stdout, stderr, "dotfile.edit", map[string]any{"system": abs, "workspace_path": copyPath})
	}
	return 0
}

// ── backup ─────────────────────────────────────────────────────────

// dotfileBackupSpec is the dotfile side of the shared backup runner.
func dotfileBackupSpec(workspacePath, configPath string) backupSpec {
	repoPath := dotfile.RepoPath(workspacePath)
	load := func() (backupSettings, error) {
		cfg, err := wsconfig.Load(configPath)
		if err != nil {
			return backupSettings{}, err
		}
		g := cfg.Dotfile.Git
		return backupSettings{RemoteURL: g.RemoteURL, Branch: g.Branch, PassEntry: g.PassEntry, AutoCommit: g.AutoCommit, AutoPush: g.AutoPush}, nil
	}
	return backupSpec{
		noun:      "dotfile",
		job:       "dotfile-backup",
		oldJobs:   []string{"dotfile-sync"},
		repoPath:  repoPath,
		workspace: workspacePath,
		hasAutos:  true,

		initialized: func() bool { return dotfile.GitIsInitialized(repoPath) },
		initPlan:    fmt.Sprintf("Initialize git in %s (branch main)", repoPath),
		init:        func() error { return dotfile.EnsureGitRepo(repoPath, "", "main") },

		load: load,
		save: func(s backupSettings) error {
			cfg, err := wsconfig.Load(configPath)
			if err != nil {
				return err
			}
			cfg.Dotfile.Git = wsconfig.DotfileGit{
				RemoteURL: s.RemoteURL, Branch: s.Branch, PassEntry: s.PassEntry, AutoCommit: s.AutoCommit, AutoPush: s.AutoPush,
			}
			return wsconfig.Save(configPath, cfg)
		},
		commit: func(message string) (bool, error) {
			s, err := load()
			if err != nil {
				return false, err
			}
			return dotfile.CommitBackup(dotfile.BackupOptions{
				WorkspacePath: workspacePath,
				ConfigPath:    configPath,
				Git:           wsconfig.DotfileGit{RemoteURL: s.RemoteURL, Branch: s.Branch, PassEntry: s.PassEntry, AutoCommit: s.AutoCommit, AutoPush: s.AutoPush},
				Message:       message,
			})
		},
		defaultMessage: "ws dotfile backup push",
	}
}

// dotfileGitAutoSync runs the auto-commit (and auto-push) after add, link --to
// and unlink changed at least one entry. One commit covers the whole plan, even
// when some of its actions failed. A failed commit or push prints a warning and
// never changes the exit code. With --json the notes go to stderr, so stdout
// stays the envelope.
func dotfileGitAutoSync(workspacePath, configPath, commitMessage string, globals globalFlags, stdout, stderr io.Writer) {
	result, enabled := dotfile.AutoSync(workspacePath, configPath, commitMessage)
	if !enabled {
		return
	}
	var out io.Writer = textOut(globals, stdout)
	if globals.json {
		out = stderr
	}
	nc := globals.noColor
	if result.Committed {
		fmt.Fprintln(out, style.Mutedf(nc, "dotfile backup: committed"))
	}
	if result.Pushed {
		fmt.Fprintln(out, style.Mutedf(nc, "dotfile backup: pushed"))
	}
	if result.Error != "" {
		fmt.Fprintln(out, style.ResultWarning(nc, "dotfile backup: %s", result.Error))
	}
}

// ── hidden: migrate ────────────────────────────────────────────────

func runDotfileMigrate(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer, workspacePath, configPath string) int {
	fs := flag.NewFlagSet("dotfile-migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	out := textOut(globals, stdout)
	nc := globals.noColor

	dirLinks, err := dotfile.FindDirEntries(workspacePath, configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(dirLinks) == 0 {
		fmt.Fprintln(out, style.ResultSuccess(nc, "No directory entries found — all dotfiles are already file-level."))
		return 0
	}

	fmt.Fprintf(out, "Found %d directory entry(s) to migrate:\n\n", len(dirLinks))
	exitCode := 0

	for _, link := range dirLinks {
		wsDir := dotfile.WorkspaceCopy(workspacePath, link)
		fmt.Fprintf(out, "%s %s\n", style.Infof(nc, "%s", link.System), style.Mutedf(nc, "(→ %s)", wsDir))

		entries, expanded, expandErr := dotfile.ExpandDir(wsDir)
		if expandErr != nil {
			fmt.Fprintf(stderr, "  error scanning %s: %v\n", wsDir, expandErr)
			exitCode = 1
			continue
		}
		if len(entries) == 0 {
			fmt.Fprintln(out, style.Mutedf(nc, "  (empty — skipping)"))
			continue
		}

		selected := promptDirSelection(stdin, stdout, globals, entries, expanded, wsDir)
		if len(selected) == 0 {
			fmt.Fprintln(out, style.Mutedf(nc, "  Nothing selected — skipping."))
			continue
		}

		// The paths the files will have once Unlink puts the directory back.
		var sysPaths []string
		for _, e := range selected {
			files := []string{e.AbsPath}
			if e.IsDir {
				var collectErr error
				if files, collectErr = dotfile.CollectFiles(e); collectErr != nil {
					fmt.Fprintf(stderr, "  error reading %s: %v\n", e.AbsPath, collectErr)
					continue
				}
			}
			for _, f := range files {
				rel, _ := filepath.Rel(wsDir, f)
				sysPaths = append(sysPaths, filepath.Join(link.System, filepath.FromSlash(rel)))
			}
		}

		if globals.dryRun {
			fmt.Fprintln(out, style.Mutedf(nc, "  [dry-run] would migrate:"))
			for _, sp := range sysPaths {
				fmt.Fprintf(out, "    %s %s %s\n", style.Mutedf(nc, "[dry-run]"), sp, style.IconArrow(nc))
			}
			continue
		}

		if err := dotfile.Unlink(dotfile.UnlinkOptions{WorkspacePath: workspacePath, ConfigPath: configPath, SystemPath: link.System}); err != nil {
			fmt.Fprintf(stderr, "  failed to remove directory entry %s: %v\n", link.System, err)
			exitCode = 1
			continue
		}
		sysPaths, _ = warnAndFilterSecrets(sysPaths, stdin, stdout, globals, configPath)

		added := 0
		for _, sp := range sysPaths {
			if _, addErr := dotfile.Add(dotfile.AddOptions{WorkspacePath: workspacePath, ConfigPath: configPath, SystemPath: sp}); addErr != nil {
				fmt.Fprintf(stderr, "  failed to add %s: %v\n", sp, addErr)
				exitCode = 1
				continue
			}
			fmt.Fprintf(out, "  %s %s\n", style.IconCheck(nc), style.Mutedf(nc, "%s", sp))
			added++
		}
		fmt.Fprintf(out, "  Migrated %d file(s) from %s\n\n", added, link.System)
	}

	if exitCode == 0 {
		dotfileGitAutoSync(workspacePath, configPath, "dotfile migrate", globals, stdout, stderr)
	}
	return exitCode
}

// ── shared prompts ─────────────────────────────────────────────────

// promptDirSelection presents the expanded directory entries to the user and
// returns the subset they choose to track. Defaults to all FileClassConfig entries.
//
// In --json / --quiet mode the defaults are accepted automatically.
func promptDirSelection(stdin io.Reader, stdout io.Writer, globals globalFlags, entries []dotfile.DirEntry, expanded bool, rootPath string) []dotfile.DirEntry {
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
		switch e.Class {
		case dotfile.FileClassSecret:
			classStr = style.Warningf(nc, "⚠ secret")
		case dotfile.FileClassState:
			classStr = style.Mutedf(nc, "likely state")
		}
		var countStr string
		if e.IsDir {
			countStr = fmt.Sprintf("  (%d files)", e.FileCount)
		}
		fmt.Fprintf(out, "  %2d  %-40s %8s  %s%s\n", num, e.AbsPath, style.HumanBytes(e.Size), classStr, countStr)
		if e.Class == dotfile.FileClassConfig {
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

	defaults := func() []dotfile.DirEntry {
		var result []dotfile.DirEntry
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

	var result []dotfile.DirEntry
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

// warnAndFilterSecrets checks filesToAdd for likely secrets. If any are found
// it prints a warning and asks once whether to track them anyway. If the user
// declines, secrets are removed from the list and the rest are returned.
// The second return value is the set of secret paths that were filtered out.
func warnAndFilterSecrets(filesToAdd []string, stdin io.Reader, stdout io.Writer, globals globalFlags, _ string) ([]string, []string) {
	var secretFiles []string
	for _, f := range filesToAdd {
		if dotfile.ClassifyFile(f) == dotfile.FileClassSecret {
			secretFiles = append(secretFiles, f)
		}
	}
	if len(secretFiles) == 0 {
		return filesToAdd, nil
	}

	out := textOut(globals, stdout)
	if globals.json {
		out = io.Discard
	}
	nc := globals.noColor

	fmt.Fprintf(out, "%s %d file(s) look like secrets:\n", style.IconWarning(nc), len(secretFiles))
	for _, f := range secretFiles {
		fmt.Fprintf(out, "  %s\n", style.Mutedf(nc, "%s", f))
	}

	if confirm(stdin, io.Writer(stdout), globals, "Track anyway?") {
		return filesToAdd, nil
	}

	filtered := slices.DeleteFunc(slices.Clone(filesToAdd), func(f string) bool { return slices.Contains(secretFiles, f) })
	return filtered, secretFiles
}
