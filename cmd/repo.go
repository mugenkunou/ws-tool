package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/style"
)

var repoHelp = cmdHelp{
	Usage: "ws repo <ls|scan|fetch|pull|sync|run>",
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

// wsSpecialRepos returns repos managed by ws itself (dotfiles and pass store)
// that should always be included in repo operations regardless of configured roots.
func wsSpecialRepos(workspacePath string) []repo.Repository {
	var special []repo.Repository

	// Dotfiles repo: <workspace>/ws/dotfiles/
	dotfilesPath := filepath.Join(workspacePath, "ws", "dotfiles")
	if isGitRepo(dotfilesPath) {
		special = append(special, repo.Repository{Path: "ws/dotfiles"})
	}

	// Pass store: $PASSWORD_STORE_DIR or ~/.password-store
	passStorePath := os.Getenv("PASSWORD_STORE_DIR")
	if passStorePath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			passStorePath = filepath.Join(home, ".password-store")
		}
	}
	if passStorePath != "" && isGitRepo(passStorePath) {
		special = append(special, repo.Repository{Path: passStorePath})
	}

	return special
}

func isGitRepo(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && info.IsDir()
}

// appendMissingRepos appends repos from extra that are not already in repos.
func appendMissingRepos(repos []repo.Repository, extra []repo.Repository) []repo.Repository {
	seen := make(map[string]struct{}, len(repos))
	for _, r := range repos {
		seen[r.Path] = struct{}{}
	}
	for _, r := range extra {
		if _, ok := seen[r.Path]; !ok {
			repos = append(repos, r)
		}
	}
	return repos
}

func runRepo(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, repoHelp)
	}

	workspacePath, configPath, _, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if len(args) == 0 {
		return printUsageError(stderr, repoHelp)
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
		if rel, err := filepath.Rel(workspacePath, resolved); err == nil {
			roots = append(roots, filepath.ToSlash(rel))
		} else {
			roots = append(roots, r)
		}
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
		repos, err := repo.Discover(workspacePath, roots, excludeDirs)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos = appendMissingRepos(repos, wsSpecialRepos(workspacePath))
		repos = filterRepos(workspacePath, repos, filters)
		return renderRepoList(globals, repos, stdout, stderr)
	case "scan":
		fs := flag.NewFlagSet("repo-scan", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		noFetch := fs.Bool("no-fetch", false, "skip fetch before scan")
		var filters repoFilterFlags
		registerRepoFilterFlags(fs, &filters)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos, err := repo.Discover(workspacePath, roots, excludeDirs)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos = appendMissingRepos(repos, wsSpecialRepos(workspacePath))

		// Fetch first (unless --no-fetch), then scan.
		var fetchWarnings []string
		if !*noFetch {
			for _, r := range repos {
				if filepath.IsAbs(r.Path) {
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
		return renderRepoScan(globals, statuses, fetchWarnings, stdout, stderr)
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
		repos, err := repo.Discover(workspacePath, roots, excludeDirs)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos = appendMissingRepos(repos, wsSpecialRepos(workspacePath))
		repos = filterRepos(workspacePath, repos, filters)
		results := repo.FetchAll(workspacePath, repos)
		return renderRepoFetch(globals, results, stdout, stderr)
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
		repos, err := repo.Discover(workspacePath, roots, excludeDirs)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos = appendMissingRepos(repos, wsSpecialRepos(workspacePath))
		repos = filterRepos(workspacePath, repos, filters)
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
			plan.Actions = append(plan.Actions, Action{
				ID:          "pull-" + r.Path,
				Description: fmt.Sprintf("Pull %s", r.Path),
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
		repos, err := repo.Discover(workspacePath, roots, excludeDirs)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos = appendMissingRepos(repos, wsSpecialRepos(workspacePath))

		// Fetch first to get accurate ahead/behind counts.
		nc := globals.noColor
		if !globals.json && !globals.quiet {
			out := textOut(globals, stdout)
			for i, r := range repos {
				if filepath.IsAbs(r.Path) {
					continue // skip external repos (e.g. pass store)
				}
				fmt.Fprintf(out, "\r%s Fetching %s (%d/%d)…",
					style.IconGit(nc), style.Infof(nc, "%s", r.Path), i+1, len(repos))
				repo.FetchOne(workspacePath, r)
			}
			fmt.Fprintln(out) // finish the progress line
		} else {
			for _, r := range repos {
				if filepath.IsAbs(r.Path) {
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
				fmt.Fprintf(out, "[dry-run] %-12s %s  (%s)\n", strategy, style.Infof(nc, "%s", sp.Path), sp.Detail)
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
			desc := syncActionDescription(sp, *rebase)
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
		repos = appendMissingRepos(repos, wsSpecialRepos(workspacePath))
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
			plan.Actions = append(plan.Actions, Action{
				ID:          "run-" + r.Path,
				Description: fmt.Sprintf("Run in %s", r.Path),
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
	default:
		fmt.Fprintf(stderr, "unknown repo subcommand: %s\n", sub)
		return 1
	}
}

func syncActionDescription(sp repo.SyncPlan, rebase bool) string {
	switch sp.Strategy {
	case repo.SyncPull:
		if sp.Status.Dirty {
			return fmt.Sprintf("Commit, pull (%s), push %s  (%s)", rebaseOrMerge(rebase), sp.Path, sp.Detail)
		}
		return fmt.Sprintf("Pull %s  (%s, ff)", sp.Path, sp.Detail)
	case repo.SyncPush:
		return fmt.Sprintf("Push %s  (%s)", sp.Path, sp.Detail)
	case repo.SyncCommitPush:
		return fmt.Sprintf("Commit and push %s  (%s)", sp.Path, sp.Detail)
	case repo.SyncPullPush:
		mode := rebaseOrMerge(rebase)
		if sp.Status.Dirty {
			return fmt.Sprintf("Commit, pull (%s), push %s  (%s)", mode, sp.Path, sp.Detail)
		}
		return fmt.Sprintf("Pull (%s) + push %s  (%s)", mode, sp.Path, sp.Detail)
	default:
		return fmt.Sprintf("Sync %s", sp.Path)
	}
}

func rebaseOrMerge(rebase bool) string {
	if rebase {
		return "rebase"
	}
	return "merge"
}

func renderRepoList(globals globalFlags, repos []repo.Repository, stdout, stderr io.Writer) int {
	if globals.json {
		return writeJSON(stdout, stderr, "repo.ls", repos)
	}

	out := textOut(globals, stdout)
	if len(repos) == 0 {
		fmt.Fprintln(out, "No repositories found.")
		return 0
	}
	for _, r := range repos {
		fmt.Fprintln(out, r.Path)
	}
	return 0
}

func renderRepoScan(globals globalFlags, statuses []repo.RepoStatus, fetchWarnings []string, stdout, stderr io.Writer) int {
	if globals.json {
		data := map[string]any{"statuses": statuses}
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
	for _, s := range statuses {
		if s.Error != "" {
			fmt.Fprintf(out, "%s %s %s\n", style.IconGit(nc), style.Infof(nc, "%s", s.Path), style.Badge("error", nc)+" "+style.Errorf(nc, "%s", s.Error))
			continue
		}
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
			style.Infof(nc, "%s", s.Path),
			style.Accentf(nc, "%s", s.Branch),
			dirtyBadge,
			detached,
			aheadBehind)
	}

	for _, s := range statuses {
		if s.Error != "" || s.Dirty || s.Detached || s.Ahead > 0 || s.Behind > 0 {
			return 2
		}
	}
	return 0
}

func renderRepoFetch(globals globalFlags, results []repo.FetchResult, stdout, stderr io.Writer) int {
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
			if r.Success {
				fmt.Fprintln(out, style.ResultSuccess(nc, "%s fetched", style.Infof(nc, "%s", r.Path)))
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
