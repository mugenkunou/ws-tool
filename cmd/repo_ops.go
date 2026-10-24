package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/mugenkunou/ws-tool/internal/repo"
)

// finish ends a plan-driven command: it runs the plan and writes the
// JSON or returns the plan's exit code.
func (e *repoEnv) finish(command string, actions []Action, skipped []repo.Skip, extra map[string]any) int {
	planResult := RunPlan(Plan{Command: command, Actions: actions}, e.stdin, e.stdout, e.globals)
	if e.globals.json {
		data := map[string]any{"actions": planResult.Actions, "skipped": noneSkipped(skipped)}
		for k, v := range extra {
			data[k] = v
		}
		return writeJSONDryRun(e.stdout, e.stderr, command, e.globals.dryRun, data)
	}
	return planResult.ExitCode()
}

// ── pull ───────────────────────────────────────────────────────────

func repoPull(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	var rebase, merge *bool
	var filters repoFilterFlags
	fs := flag.NewFlagSet("repo-pull", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	rebase, merge = strategyFlagsOf(fs)
	registerRepoFilterFlags(fs, &filters)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if *rebase && *merge {
		fmt.Fprintln(stderr, "ws: --rebase and --merge cannot be used together")
		return 1
	}
	if err := filters.resolve(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	e, ok := loadRepoEnv(globals, stdin, stdout, stderr)
	if !ok {
		return 1
	}
	repos, ok := e.targets(fs.Args(), filters)
	if !ok {
		return 1
	}

	// Fetch first, then filter: the filters read the fetched state.
	fetchFailed := e.fetchTargets(repos)
	statuses := e.scan(repos, filters)
	e.printDryRunAge(statuses)

	choose := e.chooser(*rebase, *merge)
	var actions []Action
	var skipped []repo.Skip
	for _, st := range statuses {
		if msg, bad := fetchFailed[st.Path]; bad {
			skipped = append(skipped, repo.Skip{Path: st.Path, Reason: "fetch failed: " + msg})
			continue
		}
		item, skip := repo.PlanPull(st, choose)
		if skip != nil {
			skipped = append(skipped, *skip)
			continue
		}
		if item == nil {
			continue
		}
		item2 := *item
		actions = append(actions, Action{
			ID:          "pull-" + item2.Path,
			Description: item2.Describe(e.short(item2.Path)),
			Execute:     func() error { return repo.Pull(item2.Path, item2.Strategy) },
		})
	}

	e.printSkipped(skipped)
	if len(statuses) == 0 {
		fmt.Fprintln(quietOut(globals, stdout), "No repositories found.")
	} else if len(actions) == 0 && len(skipped) == 0 {
		fmt.Fprintln(quietOut(globals, stdout), "All repositories are up to date.")
	}
	return e.finish("repo.pull", actions, skipped, nil)
}

// ── push ───────────────────────────────────────────────────────────

func repoPush(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	var filters repoFilterFlags
	fs := flag.NewFlagSet("repo-push", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
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
	e, ok := loadRepoEnv(globals, stdin, stdout, stderr)
	if !ok {
		return 1
	}
	repos, ok := e.targets(fs.Args(), filters)
	if !ok {
		return 1
	}

	// Push does nothing but push: no fetch, so the counts are the last fetch's.
	statuses := e.scan(repos, filters)
	var actions []Action
	var skipped []repo.Skip
	for _, st := range statuses {
		if reason := repo.PushSkip(st); reason != "" {
			skipped = append(skipped, repo.Skip{Path: st.Path, Reason: reason})
			continue
		}
		detail := fmt.Sprintf("%d ahead", st.Ahead)
		if st.Dirty {
			if n := repo.UncommittedCount(st.Path); n == 1 {
				detail += "; 1 uncommitted change stays local"
			} else if n > 1 {
				detail += fmt.Sprintf("; %d uncommitted changes stay local", n)
			}
		}
		path := st.Path
		actions = append(actions, Action{
			ID:          "push-" + path,
			Description: fmt.Sprintf("Push %s (%s)", e.short(path), detail),
			Execute:     func() error { return repo.Push(path) },
		})
	}

	e.printSkipped(skipped)
	if len(statuses) == 0 {
		fmt.Fprintln(quietOut(globals, stdout), "No repositories found.")
	}
	return e.finish("repo.push", actions, skipped, nil)
}

// ── save ───────────────────────────────────────────────────────────

func repoSave(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	var filters repoFilterFlags
	var message string
	fs := flag.NewFlagSet("repo-save", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&message, "m", "", "commit message")
	rebase, merge := strategyFlagsOf(fs)
	registerRepoFilterFlags(fs, &filters)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if *rebase && *merge {
		fmt.Fprintln(stderr, "ws: --rebase and --merge cannot be used together")
		return 1
	}
	if err := filters.resolve(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	e, ok := loadRepoEnv(globals, stdin, stdout, stderr)
	if !ok {
		return 1
	}
	repos, ok := e.targets(fs.Args(), filters)
	if !ok {
		return 1
	}
	if message == "" {
		host, _ := os.Hostname()
		message = repo.DefaultSaveMessage(time.Now(), host)
	}

	fetchFailed := e.fetchTargets(repos)
	statuses := e.scan(repos, filters)
	e.printDryRunAge(statuses)

	choose := e.chooser(*rebase, *merge)
	var actions []Action
	var skipped []repo.Skip
	for _, st := range statuses {
		if msg, bad := fetchFailed[st.Path]; bad {
			skipped = append(skipped, repo.Skip{Path: st.Path, Reason: "fetch failed: " + msg})
			continue
		}
		changes := 0
		if st.Dirty {
			changes = repo.UncommittedCount(st.Path)
		}
		item, skip := repo.PlanSave(st, changes, choose)
		if skip != nil {
			skipped = append(skipped, *skip)
			continue
		}
		if item == nil {
			continue
		}
		item2 := *item
		actions = append(actions, Action{
			ID:          "save-" + item2.Path,
			Description: item2.Describe(e.short(item2.Path)),
			Execute:     func() error { return repo.Save(item2, message) },
		})
	}

	e.printSkipped(skipped)
	if len(statuses) == 0 {
		fmt.Fprintln(quietOut(globals, stdout), "No repositories found.")
	} else if len(actions) == 0 && len(skipped) == 0 {
		fmt.Fprintln(quietOut(globals, stdout), "Nothing to save.")
	}
	return e.finish("repo.save", actions, skipped, nil)
}

// ── run ────────────────────────────────────────────────────────────

func repoRun(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	// Everything after the first `--` is the command. Without one, every
	// positional is the command and there are no targets.
	flagArgs, command, hasDash := args, []string(nil), false
	if i := slices.Index(args, "--"); i >= 0 {
		flagArgs, command, hasDash = args[:i], args[i+1:], true
	}
	var filters repoFilterFlags
	fs := flag.NewFlagSet("repo-run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerRepoFilterFlags(fs, &filters)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, flagArgs); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if err := filters.resolve(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	targets := fs.Args()
	if !hasDash {
		command, targets = fs.Args(), nil
	}
	if len(command) == 0 {
		fmt.Fprintln(stderr, "usage: ws repo run [repo…] -- <cmd…>")
		return 1
	}
	e, ok := loadRepoEnv(globals, stdin, stdout, stderr)
	if !ok {
		return 1
	}
	repos, ok := e.targets(targets, filters)
	if !ok {
		return 1
	}
	repos = e.narrow(repos, filters)

	results := []repo.OperationResult{}
	var actions []Action
	byPath := map[string]*repo.OperationResult{}
	for _, r := range repos {
		r := r
		var res repo.OperationResult
		actions = append(actions, Action{
			ID:          "run-" + r.Path,
			Description: "Run in " + e.short(r.Path),
			Execute: func() error {
				res = repo.RunOne(e.workspace, r, command)
				byPath[r.Path] = &res
				if !res.Success {
					return fmt.Errorf("%s", res.Error)
				}
				return nil
			},
			Output: func() string { return res.Output },
		})
	}
	if len(repos) == 0 {
		fmt.Fprintln(quietOut(globals, stdout), "No repositories found.")
	}

	planResult := RunPlan(Plan{Command: "repo.run", Actions: actions}, stdin, stdout, globals)
	if globals.json {
		for _, r := range repos {
			if res, ran := byPath[r.Path]; ran {
				results = append(results, *res)
			}
		}
		type result struct {
			Path     string `json:"path"`
			ExitCode int    `json:"exit_code"`
			Output   string `json:"output"`
		}
		out := []result{}
		for _, r := range results {
			out = append(out, result{Path: r.Path, ExitCode: r.ExitCode, Output: r.Output})
		}
		return writeJSONDryRun(stdout, stderr, "repo.run", globals.dryRun, map[string]any{
			"command": command, "actions": planResult.Actions, "results": out,
		})
	}
	return planResult.ExitCode()
}
