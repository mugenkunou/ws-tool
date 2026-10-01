package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/style"
)

var repoHelp = cmdHelp{
	Usage: "ws repo <ls|scan|fetch|pull|sync|run|add-root|ls-roots>",
	Flags: []string{
		"      --dry-run    Preview write operations (default: false)",
		"      --rebase     Use rebase for diverged repos in sync (default: merge)",
		"      --path       Restrict to repos under this workspace subpath",
		"      --dirty      Only repos with uncommitted changes",
		"      --ahead      Only repos ahead of upstream",
		"      --behind     Only repos behind upstream",
		"      --detached   Only repos in detached HEAD",
	},
}

// repoFilterFlags holds the common filter flags for repo subcommands.
type repoFilterFlags struct {
	path     string
	dirty    bool
	ahead    bool
	behind   bool
	detached bool
}

func registerRepoFilterFlags(fs *flag.FlagSet, f *repoFilterFlags) {
	fs.StringVar(&f.path, "path", "", "restrict to repos under this workspace subpath")
	fs.BoolVar(&f.dirty, "dirty", false, "only dirty repos")
	fs.BoolVar(&f.ahead, "ahead", false, "only repos ahead of upstream")
	fs.BoolVar(&f.behind, "behind", false, "only repos behind upstream")
	fs.BoolVar(&f.detached, "detached", false, "only detached HEAD repos")
}

// resolve turns --path into an absolute path (cwd-relative, ~ expanded) so it
// can be matched against absolute repo paths. See spec "Path Rules".
func (f *repoFilterFlags) resolve() error {
	if f.path == "" {
		return nil
	}
	abs, err := config.ExpandUserPath(f.path)
	if err != nil {
		return fmt.Errorf("invalid --path: %w", err)
	}
	f.path = abs
	return nil
}

func (f repoFilterFlags) toFilterOptions() repo.FilterOptions {
	return repo.FilterOptions{
		Path:     f.path,
		Dirty:    f.dirty,
		Ahead:    f.ahead,
		Behind:   f.behind,
		Detached: f.detached,
	}
}

func (f repoFilterFlags) hasFilter() bool {
	return f.path != "" || f.dirty || f.ahead || f.behind || f.detached
}

// filterRepos applies filter flags to a repo list via scan.
// Returns the filtered repos (or original repos if no filter is active).
func filterRepos(workspacePath string, repos []repo.Repository, f repoFilterFlags) []repo.Repository {
	if !f.hasFilter() {
		return repos
	}
	statuses := repo.Scan(workspacePath, repos)
	filtered := repo.Filter(statuses, f.toFilterOptions())
	result := make([]repo.Repository, 0, len(filtered))
	for _, s := range filtered {
		result = append(result, repo.Repository{Path: s.Path})
	}
	return result
}

// targetRepo filters the repo list to the single repo named by the optional
// positional argument. The argument is resolved like every path input
// (absolute, ~, or relative to cwd) and compared against absolute repo paths.
// Returns the original list if no argument is given, or an error message and
// nil slice if the argument is invalid or matches no repo.
func targetRepo(repos []repo.Repository, args []string) ([]repo.Repository, string) {
	if len(args) == 0 {
		return repos, ""
	}
	if len(args) > 1 {
		return nil, fmt.Sprintf("expected at most one repo path, got %d: %s", len(args), strings.Join(args, " "))
	}
	target, err := config.ExpandUserPath(args[0])
	if err != nil {
		return nil, fmt.Sprintf("invalid repo path %q: %s", args[0], err.Error())
	}
	for _, r := range repos {
		if filepath.Clean(r.Path) == target {
			return []repo.Repository{r}, ""
		}
	}
	return nil, fmt.Sprintf("no repo at %s — run `ws repo ls` to see available repos", target)
}

// isExternalRepo reports whether a repo lives outside the workspace (e.g. the
// pass store). External repos are not auto-fetched by scan/sync.
func isExternalRepo(workspacePath, repoPath string) bool {
	return !repo.IsWithin(repoPath, workspacePath)
}

func runRepo(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, repoHelp)
	}
	if len(args) == 0 {
		return printCmdHelp(stdout, repoHelp)
	}

	workspacePath, configPath, _, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	roots := make([]string, 0, len(cfg.Repo.Roots))
	for _, r := range cfg.Repo.Roots {
		resolved, err := config.ResolvePath(workspacePath, r)
		if err != nil {
			continue
		}
		roots = append(roots, resolved)
	}

	excludeDirs := cfg.Repo.ExcludeDirs

	sub := args[0]
	subArgs := args[1:]
	switch sub {
	case "ls":
		fs := flag.NewFlagSet("repo-ls", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		var filters repoFilterFlags
		registerRepoFilterFlags(fs, &filters)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if err := filters.resolve(); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos, err := repo.Discover(workspacePath, roots, excludeDirs)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos = repo.AppendMissing(repos, repo.SpecialRepos(workspacePath))
		repos = filterRepos(workspacePath, repos, filters)
		{
			var errMsg string
			repos, errMsg = targetRepo(repos, fs.Args())
			if errMsg != "" {
				fmt.Fprintln(stderr, errMsg)
				return 1
			}
		}
		return renderRepoList(globals, workspacePath, repos, stdout, stderr)
	case "scan":
		fs := flag.NewFlagSet("repo-scan", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		noFetch := fs.Bool("no-fetch", false, "skip fetch before scan")
		var checks stringSliceFlag
		fs.Var(&checks, "check", "run only this hygiene check (repeatable)")
		var filters repoFilterFlags
		registerRepoFilterFlags(fs, &filters)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if err := filters.resolve(); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if msg := validateHygieneChecks(checks); msg != "" {
			fmt.Fprintln(stderr, msg)
			return 1
		}
		repos, err := repo.Discover(workspacePath, roots, excludeDirs)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos = repo.AppendMissing(repos, repo.SpecialRepos(workspacePath))
		{
			var errMsg string
			repos, errMsg = targetRepo(repos, fs.Args())
			if errMsg != "" {
				fmt.Fprintln(stderr, errMsg)
				return 1
			}
		}

		// Fetch first (unless --no-fetch), then scan.
		var fetchWarnings []string
		if !*noFetch {
			for _, r := range repos {
				if isExternalRepo(workspacePath, r.Path) {
					continue // skip external repos (e.g. pass store)
				}
				result := repo.FetchOne(workspacePath, r)
				if !result.Success {
					fetchWarnings = append(fetchWarnings, fmt.Sprintf("%s: %s", r.Path, result.Error))
				}
			}
		}

		statuses := repo.Scan(workspacePath, repos)
		if filters.hasFilter() {
			statuses = repo.Filter(statuses, filters.toFilterOptions())
		}

		// Hygiene checks run after fetch so fetch-staleness reflects it,
		// and only over the repos that survived filtering.
		scanned := make([]repo.Repository, 0, len(statuses))
		for _, st := range statuses {
			scanned = append(scanned, repo.Repository{Path: st.Path})
		}
		findings := repo.Doctor(workspacePath, scanned, repo.DoctorOptions{Checks: checks})

		return renderRepoScan(globals, workspacePath, statuses, fetchWarnings, findings, stdout, stderr)
	case "doctor":
		fmt.Fprintln(stderr, "`ws repo doctor` has been merged into `ws repo scan` — hygiene findings now appear inline")
		return 1
	case "fetch":
		fs := flag.NewFlagSet("repo-fetch", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		var filters repoFilterFlags
		registerRepoFilterFlags(fs, &filters)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if err := filters.resolve(); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos, err := repo.Discover(workspacePath, roots, excludeDirs)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos = repo.AppendMissing(repos, repo.SpecialRepos(workspacePath))
		repos = filterRepos(workspacePath, repos, filters)
		{
			var errMsg string
			repos, errMsg = targetRepo(repos, fs.Args())
			if errMsg != "" {
				fmt.Fprintln(stderr, errMsg)
				return 1
			}
		}
		results := repo.FetchAll(workspacePath, repos)
		return renderRepoFetch(globals, workspacePath, results, stdout, stderr)
	case "pull":
		fs := flag.NewFlagSet("repo-pull", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		rebase := fs.Bool("rebase", false, "use git pull --rebase")
		var filters repoFilterFlags
		registerRepoFilterFlags(fs, &filters)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if err := filters.resolve(); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos, err := repo.Discover(workspacePath, roots, excludeDirs)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos = repo.AppendMissing(repos, repo.SpecialRepos(workspacePath))
		repos = filterRepos(workspacePath, repos, filters)
		{
			var errMsg string
			repos, errMsg = targetRepo(repos, fs.Args())
			if errMsg != "" {
				fmt.Fprintln(stderr, errMsg)
				return 1
			}
		}
		if globals.dryRun {
			if globals.json {
				return writeJSONDryRun(stdout, stderr, "repo.pull", true, map[string]any{"repos": repos})
			}
			fmt.Fprintf(textOut(globals, stdout), "Would pull %d repositories.\n", len(repos))
			return 0
		}

		plan := Plan{Command: "repo.pull"}
		for _, r := range repos {
			r := r // capture
			short := style.AbsPath(workspacePath, r.Path)
			plan.Actions = append(plan.Actions, Action{
				ID:          "pull-" + r.Path,
				Description: fmt.Sprintf("Pull %s", short),
				Execute: func() error {
					results := repo.PullAll(workspacePath, []repo.Repository{r}, *rebase)
					if len(results) > 0 && !results[0].Success {
						return fmt.Errorf("%s", results[0].Error)
					}
					return nil
				},
			})
		}
		planResult := RunPlan(plan, stdin, stdout, globals)
		if globals.json {
			return writeJSON(stdout, stderr, "repo.pull", map[string]any{"actions": planResult.Actions})
		}
		return planResult.ExitCode()
	case "sync":
		fs := flag.NewFlagSet("repo-sync", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		rebase := fs.Bool("rebase", false, "use rebase for diverged repos (default: merge)")
		var filters repoFilterFlags
		registerRepoFilterFlags(fs, &filters)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if err := filters.resolve(); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos, err := repo.Discover(workspacePath, roots, excludeDirs)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos = repo.AppendMissing(repos, repo.SpecialRepos(workspacePath))
		{
			var errMsg string
			repos, errMsg = targetRepo(repos, fs.Args())
			if errMsg != "" {
				fmt.Fprintln(stderr, errMsg)
				return 1
			}
		}

		// Fetch first to get accurate ahead/behind counts.
		nc := globals.noColor
		if !globals.json && !globals.quiet {
			out := textOut(globals, stdout)
			for i, r := range repos {
				if isExternalRepo(workspacePath, r.Path) {
					continue // skip external repos (e.g. pass store)
				}
				short := style.AbsPath(workspacePath, r.Path)
				fmt.Fprintf(out, "\r%s Fetching %s (%d/%d)…",
					style.IconGit(nc), style.Infof(nc, "%s", short), i+1, len(repos))
				repo.FetchOne(workspacePath, r)
			}
			fmt.Fprintln(out) // finish the progress line
		} else {
			for _, r := range repos {
				if isExternalRepo(workspacePath, r.Path) {
					continue // skip external repos (e.g. pass store)
				}
				repo.FetchOne(workspacePath, r)
			}
		}

		statuses := repo.Scan(workspacePath, repos)
		if filters.hasFilter() {
			statuses = repo.Filter(statuses, filters.toFilterOptions())
		}

		// Build sync plans.
		var syncPlans []repo.SyncPlan
		var warnings []string
		for _, s := range statuses {
			sp := repo.PlanSync(s)
			if sp.Strategy == repo.SyncSkip {
				if sp.Warning != "" {
					warnings = append(warnings, fmt.Sprintf("%s (%s)", sp.Path, sp.Warning))
				}
				continue
			}
			syncPlans = append(syncPlans, sp)
		}

		if len(syncPlans) == 0 {
			if globals.json {
				return writeJSON(stdout, stderr, "repo.sync", map[string]any{"actions": []any{}, "warnings": warnings})
			}
			out := textOut(globals, stdout)
			fmt.Fprintln(out, style.ResultSuccess(nc, "All repositories are up to date."))
			for _, w := range warnings {
				fmt.Fprintf(out, "%s %s\n", style.IconWarning(nc), style.Mutedf(nc, "Skipped: %s", w))
			}
			return 0
		}

		if globals.dryRun {
			if globals.json {
				return writeJSONDryRun(stdout, stderr, "repo.sync", true, map[string]any{"plans": syncPlans, "warnings": warnings})
			}
			out := textOut(globals, stdout)
			for _, sp := range syncPlans {
				short := style.AbsPath(workspacePath, sp.Path)
				strategy := string(sp.Strategy)
				switch sp.Strategy {
				case repo.SyncPullPush:
					if sp.Status.Dirty {
						strategy = "commit+pull(" + rebaseOrMerge(*rebase) + ")+push"
					} else {
						strategy = "pull(" + rebaseOrMerge(*rebase) + ")+push"
					}
				case repo.SyncPull:
					if sp.Status.Dirty {
						strategy = "commit+pull(" + rebaseOrMerge(*rebase) + ")+push"
					} else {
						strategy = "pull(ff)"
					}
				case repo.SyncCommitPush:
					strategy = "commit+push"
				}
				fmt.Fprintf(out, "[dry-run] %-12s %s  (%s)\n", strategy, style.Infof(nc, "%s", short), sp.Detail)
			}
			for _, w := range warnings {
				fmt.Fprintf(out, "%s %s\n", style.IconWarning(nc), style.Mutedf(nc, "Skipped: %s", w))
			}
			return 0
		}

		syncOpts := repo.SyncOptions{Rebase: *rebase}
		plan := Plan{Command: "repo.sync"}
		for _, sp := range syncPlans {
			sp := sp // capture
			desc := syncActionDescription(workspacePath, sp, *rebase)
			plan.Actions = append(plan.Actions, Action{
				ID:          "sync-" + sp.Path,
				Description: desc,
				Execute: func() error {
					result := repo.SyncOne(workspacePath, sp, syncOpts)
					if !result.Success {
						return fmt.Errorf("%s", result.Error)
					}
					return nil
				},
			})
		}
		planResult := RunPlan(plan, stdin, stdout, globals)

		if globals.json {
			return writeJSON(stdout, stderr, "repo.sync", map[string]any{"actions": planResult.Actions, "warnings": warnings})
		}

		out := textOut(globals, stdout)
		for _, w := range warnings {
			fmt.Fprintf(out, "%s %s\n", style.IconWarning(nc), style.Mutedf(nc, "Skipped: %s", w))
		}
		return planResult.ExitCode()
	case "run":
		fs := flag.NewFlagSet("repo-run", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		var filters repoFilterFlags
		registerRepoFilterFlags(fs, &filters)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if err := filters.resolve(); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		command := fs.Args()
		if len(command) == 0 {
			fmt.Fprintln(stderr, "usage: ws repo run -- <command...>")
			return 1
		}
		repos, err := repo.Discover(workspacePath, roots, excludeDirs)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos = repo.AppendMissing(repos, repo.SpecialRepos(workspacePath))
		repos = filterRepos(workspacePath, repos, filters)
		if globals.dryRun {
			if globals.json {
				return writeJSONDryRun(stdout, stderr, "repo.run", true, map[string]any{"command": command, "repos": repos})
			}
			fmt.Fprintf(textOut(globals, stdout), "Would run command in %d repositories.\n", len(repos))
			return 0
		}

		plan := Plan{Command: "repo.run"}
		for _, r := range repos {
			r := r
			short := style.AbsPath(workspacePath, r.Path)
			plan.Actions = append(plan.Actions, Action{
				ID:          "run-" + r.Path,
				Description: fmt.Sprintf("Run in %s", short),
				Execute: func() error {
					results := repo.RunAll(workspacePath, []repo.Repository{r}, command)
					if len(results) > 0 && !results[0].Success {
						return fmt.Errorf("%s", results[0].Error)
					}
					return nil
				},
			})
		}
		planResult := RunPlan(plan, stdin, stdout, globals)
		if globals.json {
			return writeJSON(stdout, stderr, "repo.run", map[string]any{"actions": planResult.Actions})
		}
		return planResult.ExitCode()
	case "add-root":
		fs := flag.NewFlagSet("repo-add-root", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		posArgs := fs.Args()
		if len(posArgs) == 0 {
			fmt.Fprintln(stderr, "usage: ws repo add-root <path>")
			return 1
		}

		if len(posArgs) > 1 {
			fmt.Fprintf(stderr, "expected one path, got %d: %s\n", len(posArgs), strings.Join(posArgs, " "))
			return 1
		}

		// Resolve like every path input: absolute, ~, or relative to cwd.
		resolvedPath, err := config.ExpandUserPath(posArgs[0])
		if err != nil {
			fmt.Fprintf(stderr, "invalid path: %s\n", err.Error())
			return 1
		}

		info, err := os.Stat(resolvedPath)
		if err != nil {
			fmt.Fprintf(stderr, "path does not exist or is not accessible: %s\n", resolvedPath)
			return 1
		}
		if !info.IsDir() {
			fmt.Fprintf(stderr, "path is not a directory: %s\n", resolvedPath)
			return 1
		}

		// Config values resolve against the workspace, so store the root
		// workspace-relative when it is inside the workspace, else absolute.
		storedRoot := resolvedPath
		if repo.IsWithin(resolvedPath, workspacePath) {
			if rel, err := filepath.Rel(workspacePath, resolvedPath); err == nil {
				storedRoot = filepath.ToSlash(rel)
			}
		}

		// Load current config
		cfg, err := config.Load(configPath)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		// Check if root already exists (config roots are workspace-relative).
		for _, r := range cfg.Repo.Roots {
			resolvedRoot, err := config.ResolvePath(workspacePath, r)
			if err != nil {
				continue
			}
			if resolvedRoot == resolvedPath {
				fmt.Fprintf(stderr, "path already configured as repo root: %s\n", resolvedPath)
				return 1
			}
		}

		if globals.dryRun {
			if globals.json {
				return writeJSONDryRun(stdout, stderr, "repo.add-root", true, map[string]any{"path": resolvedPath})
			}
			fmt.Fprintf(textOut(globals, stdout), "Would add repo root: %s\n", resolvedPath)
			return 0
		}

		plan := Plan{Command: "repo.add-root"}
		plan.Actions = append(plan.Actions, Action{
			ID:          "add-root-" + resolvedPath,
			Description: fmt.Sprintf("Add repo root: %s", resolvedPath),
			Execute: func() error {
				// Reload config to ensure we have the latest version
				cfg, err := config.Load(configPath)
				if err != nil {
					return err
				}
				cfg.Repo.Roots = append(cfg.Repo.Roots, storedRoot)
				return config.Save(configPath, cfg)
			},
		})

		planResult := RunPlan(plan, stdin, stdout, globals)
		if globals.json {
			return writeJSON(stdout, stderr, "repo.add-root", map[string]any{"actions": planResult.Actions})
		}
		return planResult.ExitCode()
	case "ls-roots":
		fs := flag.NewFlagSet("repo-ls-roots", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		if globals.json {
			absRoots := make([]string, 0, len(cfg.Repo.Roots))
			for _, r := range cfg.Repo.Roots {
				absRoots = append(absRoots, style.AbsPath(workspacePath, r))
			}
			return writeJSON(stdout, stderr, "repo.ls-roots", map[string]any{"roots": absRoots})
		}

		out := textOut(globals, stdout)
		if len(cfg.Repo.Roots) == 0 {
			fmt.Fprintln(out, "No repo roots configured.")
			return 0
		}

		fmt.Fprintln(out, "Configured repo roots:")
		for i, r := range cfg.Repo.Roots {
			short := style.AbsPath(workspacePath, r)
			fmt.Fprintf(out, "  %d. %s\n", i, short)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown repo subcommand: %s\n", sub)
		return 1
	}
}

func syncActionDescription(workspacePath string, sp repo.SyncPlan, rebase bool) string {
	return repo.DescribeSync(sp, style.AbsPath(workspacePath, sp.Path), rebase)
}

func rebaseOrMerge(rebase bool) string {
	if rebase {
		return "rebase"
	}
	return "merge"
}

func renderRepoList(globals globalFlags, workspacePath string, repos []repo.Repository, stdout, stderr io.Writer) int {
	if globals.json {
		return writeJSON(stdout, stderr, "repo.ls", repos)
	}

	out := textOut(globals, stdout)
	if len(repos) == 0 {
		fmt.Fprintln(out, "No repositories found.")
		return 0
	}
	for _, r := range repos {
		fmt.Fprintln(out, style.AbsPath(workspacePath, r.Path))
	}
	return 0
}

// validateHygieneChecks returns an error message if any requested check ID is unknown.
func validateHygieneChecks(checks []string) string {
	known := make(map[string]bool, len(repo.HygieneChecks))
	for _, c := range repo.HygieneChecks {
		known[c] = true
	}
	for _, c := range checks {
		if !known[c] {
			return fmt.Sprintf("unknown check %q — available: %s", c, strings.Join(repo.HygieneChecks, ", "))
		}
	}
	return ""
}

func renderRepoScan(globals globalFlags, workspacePath string, statuses []repo.RepoStatus, fetchWarnings []string, findings []repo.Finding, stdout, stderr io.Writer) int {
	warnCount := 0
	for _, f := range findings {
		if f.Severity >= repo.SeverityWarn {
			warnCount++
		}
	}
	needsAttention := warnCount > 0
	for _, s := range statuses {
		if s.Error != "" || s.Dirty || s.Detached || s.Ahead > 0 || s.Behind > 0 {
			needsAttention = true
			break
		}
	}
	exitCode := 0
	if needsAttention {
		exitCode = 2
	}

	if globals.json {
		data := map[string]any{"statuses": statuses, "findings": findings}
		if findings == nil {
			data["findings"] = []repo.Finding{}
		}
		if len(fetchWarnings) > 0 {
			data["fetch_warnings"] = fetchWarnings
		}
		return writeJSON(stdout, stderr, "repo.scan", data)
	}

	out := textOut(globals, stdout)
	nc := globals.noColor

	// Show fetch warnings first.
	for _, w := range fetchWarnings {
		fmt.Fprintf(out, "%s %s\n", style.Badge("fetch-failed", nc), style.Mutedf(nc, "%s", w))
	}
	if len(fetchWarnings) > 0 {
		fmt.Fprintln(out)
	}

	if len(statuses) == 0 {
		fmt.Fprintln(out, "No repositories found.")
		return 0
	}

	byRepo := map[string][]repo.Finding{}
	for _, f := range findings {
		byRepo[f.Repo] = append(byRepo[f.Repo], f)
	}
	hiddenInfo := 0

	for _, s := range statuses {
		short := style.AbsPath(workspacePath, s.Path)
		if s.Error != "" {
			fmt.Fprintf(out, "%s %s %s\n", style.IconGit(nc), style.Infof(nc, "%s", short), style.Badge("error", nc)+" "+style.Errorf(nc, "%s", s.Error))
		} else {
			dirtyBadge := style.Badge("clean", nc)
			if s.Dirty {
				dirtyBadge = style.Badge("dirty", nc)
			}
			detached := ""
			if s.Detached {
				detached = " " + style.Badge("detached", nc)
			}
			aheadBehind := ""
			if s.Ahead > 0 || s.Behind > 0 {
				aheadBehind = fmt.Sprintf(" %s %s",
					style.Successf(nc, "↑%d", s.Ahead),
					style.Warningf(nc, "↓%d", s.Behind))
			}
			fmt.Fprintf(out, "%s %s  %s %s%s%s\n",
				style.IconGit(nc),
				style.Infof(nc, "%s", short),
				style.Accentf(nc, "%s", s.Branch),
				dirtyBadge,
				detached,
				aheadBehind)
		}
		for _, f := range byRepo[s.Path] {
			if f.Severity < repo.SeverityWarn {
				if !globals.verbose {
					hiddenInfo++
					continue
				}
				fmt.Fprintf(out, "   %s %s\n", style.Mutedf(nc, "·"), style.Mutedf(nc, "[%s] %s", f.Check, f.Detail))
				continue
			}
			fmt.Fprintf(out, "   %s [%s] %s\n", style.IconWarning(nc), f.Check, f.Detail)
		}
	}

	if warnCount > 0 || hiddenInfo > 0 {
		fmt.Fprintln(out)
		summary := fmt.Sprintf("Hygiene: %d warning(s)", warnCount)
		if hiddenInfo > 0 {
			summary += style.Mutedf(nc, " · %d info hidden (--verbose to show)", hiddenInfo)
		}
		fmt.Fprintln(out, summary)
	}

	return exitCode
}

func renderRepoFetch(globals globalFlags, workspacePath string, results []repo.FetchResult, stdout, stderr io.Writer) int {
	if globals.json {
		return writeJSON(stdout, stderr, "repo.fetch", results)
	} else {
		out := textOut(globals, stdout)
		if len(results) == 0 {
			fmt.Fprintln(out, "No repositories found.")
			return 0
		}
		for _, r := range results {
			nc := globals.noColor
			short := style.AbsPath(workspacePath, r.Path)
			if r.Success {
				fmt.Fprintln(out, style.ResultSuccess(nc, "%s fetched", style.Infof(nc, "%s", short)))
			} else {
				fmt.Fprintln(out, style.ResultError(nc, "%s failed: %s", short, r.Error))
			}
		}
	}

	anyFailure := false
	for _, r := range results {
		if !r.Success {
			anyFailure = true
			break
		}
	}
	if anyFailure {
		return 3
	}
	return 0
}

func renderRepoOperation(globals globalFlags, verb string, results []repo.OperationResult, stdout, stderr io.Writer) int {
	if globals.json {
		return writeJSON(stdout, stderr, "repo."+verb, results)
	} else {
		out := textOut(globals, stdout)
		if len(results) == 0 {
			fmt.Fprintln(out, "No repositories found.")
			return 0
		}
		for _, r := range results {
			nc := globals.noColor
			if r.Success {
				fmt.Fprintln(out, style.ResultSuccess(nc, "%s %s", style.Infof(nc, "%s", r.Path), verb))
			} else {
				fmt.Fprintln(out, style.ResultError(nc, "%s failed: %s", r.Path, r.Error))
			}
		}
	}

	anyFailure := false
	for _, r := range results {
		if !r.Success {
			anyFailure = true
			break
		}
	}
	if anyFailure {
		return 3
	}
	return 0
}
