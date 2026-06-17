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
	Usage: "ws repo <ls|scan|doctor|fetch|pull|sync|run|add-root|ls-roots>",
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

// targetRepo filters the repo list to a single repo matching the positional
// argument. Returns the original list if target is empty. Returns an error
// message and nil slice if the target does not match any repo.
func targetRepo(repos []repo.Repository, target string) ([]repo.Repository, string) {
	if target == "" {
		return repos, ""
	}
	normalized := filepath.ToSlash(filepath.Clean(target))
	for _, r := range repos {
		if filepath.ToSlash(r.Path) == normalized {
			return []repo.Repository{r}, ""
		}
	}
	return nil, fmt.Sprintf("no repo matching %q — run `ws repo ls` to see available repos", target)
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
		if target := strings.Join(fs.Args(), ""); target != "" {
			var errMsg string
			repos, errMsg = targetRepo(repos, target)
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
		if target := strings.Join(fs.Args(), ""); target != "" {
			var errMsg string
			repos, errMsg = targetRepo(repos, target)
			if errMsg != "" {
				fmt.Fprintln(stderr, errMsg)
				return 1
			}
		}

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

		// Cheap hygiene count footer for scan output.
		hygieneFindings := repo.Doctor(workspacePath, repos, repo.DoctorOptions{
			Checks: []string{"identity", "upstream", "dirty"},
		})
		hygieneWarnCount := 0
		for _, f := range hygieneFindings {
			if f.Severity >= repo.SeverityWarn {
				hygieneWarnCount++
			}
		}

		return renderRepoScan(globals, workspacePath, statuses, fetchWarnings, hygieneWarnCount, stdout, stderr)
	case "doctor":
		fs := flag.NewFlagSet("repo-doctor", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		var filters repoFilterFlags
		var checkFilter string
		registerRepoFilterFlags(fs, &filters)
		fs.StringVar(&checkFilter, "check", "", "run only this check")
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
		if target := strings.Join(fs.Args(), ""); target != "" {
			var errMsg string
			repos, errMsg = targetRepo(repos, target)
			if errMsg != "" {
				fmt.Fprintln(stderr, errMsg)
				return 1
			}
		}
		var checks []string
		if checkFilter != "" {
			checks = []string{checkFilter}
		}
		findings := repo.Doctor(workspacePath, repos, repo.DoctorOptions{Checks: checks})
		return renderRepoDoctor(globals, workspacePath, findings, stdout, stderr)
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
		if target := strings.Join(fs.Args(), ""); target != "" {
			var errMsg string
			repos, errMsg = targetRepo(repos, target)
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
		repos, err := repo.Discover(workspacePath, roots, excludeDirs)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos = appendMissingRepos(repos, wsSpecialRepos(workspacePath))
		repos = filterRepos(workspacePath, repos, filters)
		if target := strings.Join(fs.Args(), ""); target != "" {
			var errMsg string
			repos, errMsg = targetRepo(repos, target)
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
			short, _ := style.DisplayPath(workspacePath, r.Path)
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
		repos, err := repo.Discover(workspacePath, roots, excludeDirs)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repos = appendMissingRepos(repos, wsSpecialRepos(workspacePath))
		if target := strings.Join(fs.Args(), ""); target != "" {
			var errMsg string
			repos, errMsg = targetRepo(repos, target)
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
				if filepath.IsAbs(r.Path) {
					continue // skip external repos (e.g. pass store)
				}
				short, _ := style.DisplayPath(workspacePath, r.Path)
				fmt.Fprintf(out, "\r%s Fetching %s (%d/%d)…",
					style.IconGit(nc), style.Infof(nc, "%s", short), i+1, len(repos))
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
				short, _ := style.DisplayPath(workspacePath, sp.Path)
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
			short, _ := style.DisplayPath(workspacePath, r.Path)
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

		targetPath := posArgs[0]

		// Resolve and validate the path
		resolvedPath, err := config.ExpandUserPath(targetPath)
		if err != nil {
			fmt.Fprintf(stderr, "invalid path: %s\n", err.Error())
			return 1
		}

		// Check if path exists and is accessible
		if _, err := os.Stat(resolvedPath); err != nil {
			fmt.Fprintf(stderr, "path does not exist or is not accessible: %s\n", targetPath)
			return 1
		}

		// Load current config
		cfg, err := config.Load(configPath)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		// Check if root already exists
		for _, r := range cfg.Repo.Roots {
			resolvedRoot, err := config.ExpandUserPath(r)
			if err != nil {
				continue
			}
			if filepath.Clean(resolvedRoot) == filepath.Clean(resolvedPath) {
				fmt.Fprintf(stderr, "path already configured as repo root\n")
				return 1
			}
		}

		// Display the path for user confirmation
		short, full := style.DisplayPath(workspacePath, targetPath)
		displayStr := short
		if short != full {
			displayStr = fmt.Sprintf("%s (%s)", short, full)
		}

		if globals.dryRun {
			if globals.json {
				return writeJSONDryRun(stdout, stderr, "repo.add-root", true, map[string]any{"path": targetPath})
			}
			fmt.Fprintf(textOut(globals, stdout), "Would add repo root: %s\n", displayStr)
			return 0
		}

		plan := Plan{Command: "repo.add-root"}
		plan.Actions = append(plan.Actions, Action{
			ID:          "add-root-" + filepath.ToSlash(filepath.Clean(targetPath)),
			Description: fmt.Sprintf("Add repo root: %s", displayStr),
			Execute: func() error {
				// Reload config to ensure we have the latest version
				cfg, err := config.Load(configPath)
				if err != nil {
					return err
				}

				// Use the original path for storage (handle ~, relative, absolute)
				cfg.Repo.Roots = append(cfg.Repo.Roots, targetPath)

				// Save the updated config
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
			return writeJSON(stdout, stderr, "repo.ls-roots", map[string]any{"roots": cfg.Repo.Roots})
		}

		out := textOut(globals, stdout)
		if len(cfg.Repo.Roots) == 0 {
			fmt.Fprintln(out, "No repo roots configured.")
			return 0
		}

		fmt.Fprintln(out, "Configured repo roots:")
		for i, r := range cfg.Repo.Roots {
			short, _ := style.DisplayPath(workspacePath, r)
			fmt.Fprintf(out, "  %d. %s\n", i, short)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown repo subcommand: %s\n", sub)
		return 1
	}
}

func syncActionDescription(workspacePath string, sp repo.SyncPlan, rebase bool) string {
	short, _ := style.DisplayPath(workspacePath, sp.Path)
	switch sp.Strategy {
	case repo.SyncPull:
		if sp.Status.Dirty {
			return fmt.Sprintf("Commit, pull (%s), push %s  (%s)", rebaseOrMerge(rebase), short, sp.Detail)
		}
		return fmt.Sprintf("Pull %s  (%s, ff)", short, sp.Detail)
	case repo.SyncPush:
		return fmt.Sprintf("Push %s  (%s)", short, sp.Detail)
	case repo.SyncCommitPush:
		return fmt.Sprintf("Commit and push %s  (%s)", short, sp.Detail)
	case repo.SyncPullPush:
		mode := rebaseOrMerge(rebase)
		if sp.Status.Dirty {
			return fmt.Sprintf("Commit, pull (%s), push %s  (%s)", mode, short, sp.Detail)
		}
		return fmt.Sprintf("Pull (%s) + push %s  (%s)", mode, short, sp.Detail)
	default:
		return fmt.Sprintf("Sync %s", short)
	}
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
	nc := globals.noColor
	for _, r := range repos {
		short, full := style.DisplayPath(workspacePath, r.Path)
		fmt.Fprintln(out, short)
		if full != "" {
			fmt.Fprintf(out, "  %s\n", style.Mutedf(nc, "%s", full))
		}
	}
	return 0
}

func renderRepoScan(globals globalFlags, workspacePath string, statuses []repo.RepoStatus, fetchWarnings []string, hygieneWarnings int, stdout, stderr io.Writer) int {
	if globals.json {
		data := map[string]any{"statuses": statuses}
		if len(fetchWarnings) > 0 {
			data["fetch_warnings"] = fetchWarnings
		}
		if hygieneWarnings > 0 {
			data["hygiene_warnings"] = hygieneWarnings
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
		short, full := style.DisplayPath(workspacePath, s.Path)
		if s.Error != "" {
			fmt.Fprintf(out, "%s %s %s\n", style.IconGit(nc), style.Infof(nc, "%s", short), style.Badge("error", nc)+" "+style.Errorf(nc, "%s", s.Error))
			if full != "" {
				fmt.Fprintf(out, "   %s\n", style.Mutedf(nc, "%s", full))
			}
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
			style.Infof(nc, "%s", short),
			style.Accentf(nc, "%s", s.Branch),
			dirtyBadge,
			detached,
			aheadBehind)
		if full != "" {
			fmt.Fprintf(out, "   %s\n", style.Mutedf(nc, "%s", full))
		}
	}

	if hygieneWarnings > 0 {
		fmt.Fprintf(out, "\nHygiene: %d warning(s) — run `ws repo doctor`\n", hygieneWarnings)
	}

	for _, s := range statuses {
		if s.Error != "" || s.Dirty || s.Detached || s.Ahead > 0 || s.Behind > 0 {
			return 2
		}
	}
	return 0
}

func renderRepoDoctor(globals globalFlags, workspacePath string, findings []repo.Finding, stdout, stderr io.Writer) int {
	if globals.json {
		byRepo := make(map[string][]repo.Finding)
		for _, f := range findings {
			byRepo[f.Repo] = append(byRepo[f.Repo], f)
		}
		return writeJSON(stdout, stderr, "repo.doctor", map[string]any{
			"findings": findings,
			"by_repo":  byRepo,
		})
	}

	out := textOut(globals, stdout)
	nc := globals.noColor

	if len(findings) == 0 {
		fmt.Fprintln(out, style.ResultSuccess(nc, "All repositories passed hygiene checks."))
		return 0
	}

	// Group by repo, preserving first-seen order.
	var repoOrder []string
	byRepo := map[string][]repo.Finding{}
	for _, f := range findings {
		if _, seen := byRepo[f.Repo]; !seen {
			repoOrder = append(repoOrder, f.Repo)
		}
		byRepo[f.Repo] = append(byRepo[f.Repo], f)
	}

	for _, repoPath := range repoOrder {
		short, _ := style.DisplayPath(workspacePath, repoPath)
		fmt.Fprintf(out, "%s %s\n", style.IconGit(nc), style.Infof(nc, "%s", short))
		for _, f := range byRepo[repoPath] {
			icon := style.Mutedf(nc, "  ·")
			if f.Severity >= repo.SeverityWarn {
				icon = "  " + style.IconWarning(nc)
			}
			fmt.Fprintf(out, "%s [%s] %s\n", icon, f.Check, f.Detail)
		}
	}

	for _, f := range findings {
		if f.Severity >= repo.SeverityWarn {
			return 2
		}
	}
	return 0
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
			short, _ := style.DisplayPath(workspacePath, r.Path)
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
