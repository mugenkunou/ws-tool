package cmd

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// runRepo dispatches `ws repo <verb>` (spec §5 repo).
func runRepo(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		renderHelpFor([]string{"repo"}, globals.noColor, stdout, stderr)
		return 0
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "ls":
		return repoLs(rest, globals, stdout, stderr)
	case "show":
		return repoShow(rest, globals, stdout, stderr)
	case "fetch":
		return repoFetch(rest, globals, stdout, stderr)
	case "pull":
		return repoPull(rest, globals, stdin, stdout, stderr)
	case "push":
		return repoPush(rest, globals, stdin, stdout, stderr)
	case "save":
		return repoSave(rest, globals, stdin, stdout, stderr)
	case "configure":
		return repoConfigure(rest, globals, stdin, stdout, stderr)
	case "run":
		return repoRun(rest, globals, stdin, stdout, stderr)
	case "root":
		return repoRoot(rest, globals, stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "unknown command: repo %s\n", verb)
	fmt.Fprintln(stderr, "usage: ws repo <ls|show|fetch|pull|push|save|configure|run|root> …")
	return 1
}

// repoFilterFlags holds the filter flags of every command that selects repos.
type repoFilterFlags struct {
	path     string
	dirty    bool
	ahead    bool
	behind   bool
	detached bool
}

func registerRepoFilterFlags(fs *flag.FlagSet, f *repoFilterFlags) {
	fs.StringVar(&f.path, "path", "", "only repos at or under this directory")
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
	abs, err := wsconfig.ExpandUserPath(f.path)
	if err != nil {
		return fmt.Errorf("invalid --path: %w", err)
	}
	f.path = abs
	return nil
}

// hasState reports whether a filter reads the repo's status.
func (f repoFilterFlags) hasState() bool { return f.dirty || f.ahead || f.behind || f.detached }

// stateOptions are the filters that read status; --path is applied earlier.
func (f repoFilterFlags) stateOptions() repo.FilterOptions {
	return repo.FilterOptions{Dirty: f.dirty, Ahead: f.ahead, Behind: f.behind, Detached: f.detached}
}

// repoEnv is the workspace a repo command runs in.
type repoEnv struct {
	globals    globalFlags
	workspace  string
	configPath string
	cfg        wsconfig.WsConfig
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
}

// loadRepoEnv requires an initialized workspace and reads ws.json.
func loadRepoEnv(globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) (*repoEnv, bool) {
	workspacePath, configPath, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return nil, false
	}
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return nil, false
	}
	return &repoEnv{globals: globals, workspace: workspacePath, configPath: configPath, cfg: cfg,
		stdin: stdin, stdout: stdout, stderr: stderr}, true
}

// roots are the effective discovery roots, absolute, in ws.json order. An empty
// list means the workspace itself.
func (e *repoEnv) roots() []string { return effectiveRoots(e.workspace, e.cfg.Repo.Roots) }

func effectiveRoots(workspacePath string, configured []string) []string {
	var roots []string
	for _, r := range configured {
		if resolved, err := wsconfig.ResolvePath(workspacePath, r); err == nil {
			roots = append(roots, resolved)
		}
	}
	if len(roots) == 0 {
		roots = []string{filepath.Clean(workspacePath)}
	}
	return roots
}

// targets discovers the repos, resolves the [repo…] arguments against them and
// applies --path. Every target is checked before any work starts: an argument
// that is not a discovered repo is an error (`ws: no repo at <abs path>`).
func (e *repoEnv) targets(args []string, f repoFilterFlags) ([]repo.Repository, bool) {
	repos, err := repo.Discover(e.workspace, e.roots(), e.cfg.Repo.ExcludeDirs)
	if err != nil {
		fmt.Fprintln(e.stderr, err.Error())
		return nil, false
	}
	repos, errMsg := selectTargets(repos, args)
	if errMsg != "" {
		fmt.Fprintln(e.stderr, errMsg)
		return nil, false
	}
	if f.path != "" {
		repos = slices.DeleteFunc(slices.Clone(repos), func(r repo.Repository) bool { return !repo.IsWithin(r.Path, f.path) })
	}
	return repos, true
}

// selectTargets keeps the discovered repos named by args, in path order. An
// argument is a path (absolute, ~, or relative to the current directory), cleaned
// and compared exactly: a path inside a repo, or a directory that holds repos,
// is not a target. Without arguments every repo stays.
func selectTargets(repos []repo.Repository, args []string) ([]repo.Repository, string) {
	if len(args) == 0 {
		return repos, ""
	}
	want := map[string]bool{}
	for _, a := range args {
		abs, err := wsconfig.ExpandUserPath(a)
		if err != nil {
			return nil, fmt.Sprintf("ws: invalid repo path %q: %s", a, err.Error())
		}
		if !slices.ContainsFunc(repos, func(r repo.Repository) bool { return filepath.Clean(r.Path) == abs }) {
			return nil, "ws: no repo at " + abs
		}
		want[abs] = true
	}
	var out []repo.Repository
	for _, r := range repos {
		if want[filepath.Clean(r.Path)] {
			out = append(out, r)
		}
	}
	return out, ""
}

// scan reads the status of repos and applies the status filters.
func (e *repoEnv) scan(repos []repo.Repository, f repoFilterFlags) []repo.RepoStatus {
	return repo.Filter(repo.Scan(e.workspace, repos), f.stateOptions())
}

// narrow applies the status filters to repos, reading status only when a
// filter needs it.
func (e *repoEnv) narrow(repos []repo.Repository, f repoFilterFlags) []repo.Repository {
	if !f.hasState() {
		return repos
	}
	return pathsOf(e.scan(repos, f))
}

func pathsOf(statuses []repo.RepoStatus) []repo.Repository {
	out := make([]repo.Repository, len(statuses))
	for i, s := range statuses {
		out[i] = repo.Repository{Path: s.Path}
	}
	return out
}

// short is a repo path as the CLI prints it: absolute.
func (e *repoEnv) short(path string) string { return style.AbsPath(e.workspace, path) }

// fetchAll fetches every repo, one at a time, and returns the results.
func (e *repoEnv) fetchAll(repos []repo.Repository) []repo.FetchResult {
	return repo.FetchAll(e.workspace, repos)
}

// oneLine flattens git's multi-line error text for a single-line message.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// validateHygieneChecks returns an error message if any requested check ID is unknown.
func validateHygieneChecks(checks []string) string {
	for _, c := range checks {
		if !slices.Contains(repo.HygieneChecks, c) {
			return fmt.Sprintf("unknown check %q — available: %s", c, strings.Join(repo.HygieneChecks, ", "))
		}
	}
	return ""
}

// ── ls ─────────────────────────────────────────────────────────────

func repoLs(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("repo-ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fetch := fs.Bool("fetch", false, "fetch each repo first")
	var checks stringSliceFlag
	fs.Var(&checks, "check", "run only this hygiene check (repeatable)")
	var filters repoFilterFlags
	registerRepoFilterFlags(fs, &filters)
	lf := registerListFlags(fs, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
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
	schema := repo.Schema(resource.CLIView())
	if err := schema.Validate(lf.query()); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	e, ok := loadRepoEnv(globals, nil, stdout, stderr)
	if !ok {
		return 1
	}
	repos, ok := e.targets(fs.Args(), filters)
	if !ok {
		return 1
	}

	// A fetch has no plan and no prompt: it changes only remote-tracking refs.
	// --dry-run skips it.
	var fetchWarnings []string
	if *fetch && !globals.dryRun {
		for _, r := range e.fetchAll(repos) {
			if !r.Success {
				fetchWarnings = append(fetchWarnings, r.Path+": "+oneLine(r.Error))
				fmt.Fprintf(stderr, "ws: fetch failed: %s: %s\n", r.Path, oneLine(r.Error))
			}
		}
	}

	statuses := e.scan(repos, filters)
	// Hygiene runs last, so fetch-staleness reflects the fetch.
	findings := repo.Doctor(e.workspace, pathsOf(statuses), repo.DoctorOptions{Checks: checks})
	entries, ok := applyList(schema, repo.Entries(statuses, findings), lf, stderr)
	if !ok {
		return 1
	}

	code := 0
	if len(fetchWarnings) > 0 {
		code = 3
	}
	if globals.json {
		var shown []repo.Finding
		for _, en := range entries {
			shown = append(shown, en.Findings...)
		}
		if shown == nil {
			shown = []repo.Finding{}
		}
		data := map[string]any{"statuses": schema.Records(entries), "findings": shown}
		if len(fetchWarnings) > 0 {
			data["fetch_warnings"] = fetchWarnings
		}
		if c := writeJSON(stdout, stderr, "repo.ls", data); c != 0 {
			return c
		}
		return code
	}
	out := textOut(globals, stdout)
	if len(entries) == 0 {
		fmt.Fprintln(out, "No repositories found.")
		return code
	}
	if err := schema.WriteTable(out, entries, lf.wide); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return code
}

// ── show ───────────────────────────────────────────────────────────

func repoShow(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("repo-show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) != 1 {
		fmt.Fprintln(stderr, "usage: ws repo show <repo>")
		return 1
	}
	e, ok := loadRepoEnv(globals, nil, stdout, stderr)
	if !ok {
		return 1
	}
	repos, ok := e.targets(fs.Args(), repoFilterFlags{})
	if !ok {
		return 1
	}
	// show never fetches: it reads the repo as it is.
	statuses := repo.Scan(e.workspace, repos)
	findings := repo.Doctor(e.workspace, repos, repo.DoctorOptions{})
	entry := repo.Entries(statuses, findings)[0]

	if globals.json {
		schema := repo.Schema(resource.CLIView())
		fnd := entry.Findings
		if fnd == nil {
			fnd = []repo.Finding{}
		}
		return writeJSON(stdout, stderr, "repo.show", map[string]any{
			"status":   schema.Records([]repo.Entry{entry})[0],
			"findings": fnd,
		})
	}
	fmt.Fprint(textOut(globals, stdout), repo.Detail(entry))
	return 0
}

// ── fetch ──────────────────────────────────────────────────────────

func repoFetch(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("repo-fetch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var filters repoFilterFlags
	registerRepoFilterFlags(fs, &filters)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if err := filters.resolve(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	e, ok := loadRepoEnv(globals, nil, stdout, stderr)
	if !ok {
		return 1
	}
	repos, ok := e.targets(fs.Args(), filters)
	if !ok {
		return 1
	}
	repos = e.narrow(repos, filters)

	out := textOut(globals, stdout)
	nc := globals.noColor
	if globals.dryRun {
		results := []repo.FetchResult{}
		for _, r := range repos {
			results = append(results, repo.FetchResult{Path: r.Path, Error: "not fetched (dry run)"})
			if !globals.json {
				fmt.Fprintf(out, "Would fetch %s\n", r.Path)
			}
		}
		if globals.json {
			return writeJSONDryRun(stdout, stderr, "repo.fetch", true, results)
		}
		return 0
	}

	results := e.fetchAll(repos)
	if results == nil {
		results = []repo.FetchResult{}
	}
	failed := 0
	for _, r := range results {
		if !r.Success {
			failed++
		}
	}
	// 0 when every fetch succeeds, 3 when some fail, 1 when all fail.
	code := 0
	switch {
	case failed == 0:
	case failed == len(results):
		code = 1
	default:
		code = 3
	}
	if globals.json {
		if c := writeJSON(stdout, stderr, "repo.fetch", results); c != 0 {
			return c
		}
		return code
	}
	if len(results) == 0 {
		fmt.Fprintln(out, "No repositories found.")
		return 0
	}
	for _, r := range results {
		if r.Success {
			fmt.Fprintln(out, style.ResultSuccess(nc, "%s fetched", r.Path))
		} else {
			fmt.Fprintln(out, style.ResultError(nc, "%s failed: %s", r.Path, oneLine(r.Error)))
		}
	}
	return code
}

// ── shared by pull, push, save ─────────────────────────────────────

// repoInteractive reports whether prompts can reach a person. Tests replace it.
var repoInteractive = isInteractive

// strategyFlagsOf registers --rebase and --merge.
func strategyFlagsOf(fs *flag.FlagSet) (rebase, merge *bool) {
	return fs.Bool("rebase", false, "rebase diverged repos"), fs.Bool("merge", false, "merge diverged repos")
}

// chooser answers "rebase, merge or skip" for a diverged repo: the flag when
// there is one, else the person at the terminal, else a skip. ws never
// chooses for you.
func (e *repoEnv) chooser(rebase, merge bool) repo.Chooser {
	return func(path string, local, remote int) (repo.Strategy, string) {
		switch {
		case rebase:
			return repo.StrategyRebase, ""
		case merge:
			return repo.StrategyMerge, ""
		}
		if e.globals.quiet || e.globals.json || !repoInteractive(e.stdin, e.stdout) {
			return repo.StrategySkip, repo.ReasonDiverged
		}
		commits := "commits"
		if remote == 1 {
			commits = "commit"
		}
		fmt.Fprintf(e.stdout, "%s has diverged (%d local, %d remote %s).\n", e.short(path), local, remote, commits)
		for {
			fmt.Fprint(e.stdout, "Pull with [r]ebase, [m]erge, or [s]kip this repo? ")
			line, err := readLine(e.stdin)
			if err != nil {
				return repo.StrategySkip, "diverged: skipped"
			}
			switch strings.ToLower(strings.TrimSpace(line)) {
			case "r", "rebase":
				return repo.StrategyRebase, ""
			case "m", "merge":
				return repo.StrategyMerge, ""
			case "", "s", "skip":
				return repo.StrategySkip, "diverged: skipped"
			}
		}
	}
}

// fetchTargets fetches repos for pull and save, and returns the failures by path.
// With --dry-run nothing is fetched.
func (e *repoEnv) fetchTargets(repos []repo.Repository) map[string]string {
	failed := map[string]string{}
	if e.globals.dryRun {
		return failed
	}
	for _, r := range e.fetchAll(repos) {
		if !r.Success {
			failed[r.Path] = oneLine(r.Error)
		}
	}
	return failed
}

// printSkipped lists the repos a command leaves out.
func (e *repoEnv) printSkipped(skipped []repo.Skip) {
	out := quietOut(e.globals, e.stdout)
	for _, s := range skipped {
		fmt.Fprintf(out, "Skipped: %s (%s)\n", e.short(s.Path), s.Reason)
	}
}

// printDryRunAge says how old the last fetch is, because a dry run plans from it.
func (e *repoEnv) printDryRunAge(statuses []repo.RepoStatus) {
	if !e.globals.dryRun || len(statuses) == 0 {
		return
	}
	var oldest time.Time
	never := 0
	for _, s := range statuses {
		t, ok := repo.LastFetch(s.Path)
		if !ok {
			never++
			continue
		}
		if oldest.IsZero() || t.Before(oldest) {
			oldest = t
		}
	}
	note := "Planned from the last fetch"
	switch {
	case never > 0 && oldest.IsZero():
		note += ", which never happened"
	case never > 0:
		note += fmt.Sprintf(": oldest %s ago, %d never fetched", humanAge(time.Since(oldest)), never)
	default:
		note += fmt.Sprintf(": oldest %s ago", humanAge(time.Since(oldest)))
	}
	fmt.Fprintln(quietOut(e.globals, e.stdout), note+".")
}

func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return pluralize(int(d.Minutes()), "minute")
	case d < 48*time.Hour:
		return pluralize(int(d.Hours()), "hour")
	}
	return pluralize(int(d.Hours()/24), "day")
}

// noneSkipped keeps the JSON list a list.
func noneSkipped(s []repo.Skip) []repo.Skip {
	if s == nil {
		return []repo.Skip{}
	}
	return s
}
