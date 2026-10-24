package cmd

import (
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/cron"
	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// runCron dispatches `ws cron <verb>` (spec §5 cron).
func runCron(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ws cron <ls|add|rm|run|log|apply> …")
		return 1
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "ls":
		return cronLs(rest, globals, stdout, stderr)
	case "add":
		return cronAdd(rest, globals, stdin, stdout, stderr)
	case "rm":
		return cronRm(rest, globals, stdin, stdout, stderr)
	case "run":
		return cronRun(rest, globals, stdout, stderr)
	case "log":
		return cronLog(rest, globals, stdout, stderr)
	case "apply":
		return cronApply(rest, globals, stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "unknown command: cron %s\n", verb)
	fmt.Fprintln(stderr, "usage: ws cron <ls|add|rm|run|log|apply> …")
	return 1
}

// cronPlanOptions says where `cron add`, `rm` and `apply` read and write: this
// binary, this workspace, and its configs/crontab.
func cronPlanOptions(globals globalFlags, display string) (cron.PlanOptions, error) {
	workspacePath, _, err := resolveWorkspacePaths(globals)
	if err != nil {
		return cron.PlanOptions{}, err
	}
	if display == "" {
		display = cron.DefaultDisplay()
	}
	return cron.PlanOptions{Bin: cron.WSBinary(), Workspace: workspacePath, Display: display, Declared: cron.DeclaredPath(workspacePath)}, nil
}

// aliasNote prints the old-name rename line for a job name, as a typed alias
// would (spec §3.5): `cron add`, `rm` and `log` do; `cron run` is plumbing and
// stays silent.
func aliasNote(globals globalFlags, stderr io.Writer, name string) {
	if canon, old := cron.Canonical(name); old && !cron.IsRetired(canon) && !globals.quiet && !globals.json {
		fmt.Fprintf(stderr, "ws: %q is now %q\n", name, canon)
	}
}

// preflight checks crontab access before any read or write of the live crontab.
func preflight(stderr io.Writer) bool {
	if err := cron.Preflight(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return false
	}
	return true
}

// ── ls ─────────────────────────────────────────────────────────────

func cronLs(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cron-ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	lf := registerListFlags(fs, "")
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	workspacePath, _, _ := resolveWorkspacePaths(globals)
	// An unreadable crontab is not an error: the rows show "crontab n/a" and
	// stderr gets one line with the reason.
	jobs, crontabErr := cron.Jobs(workspacePath)
	if crontabErr != nil {
		fmt.Fprintln(stderr, "crontab n/a: "+strings.SplitN(crontabErr.Error(), "\n", 2)[0])
	}
	return renderList(globals, "cron.ls", cron.Schema(resource.CLIView()), jobs, lf, "No cron jobs.", stdout, stderr)
}

// ── add ────────────────────────────────────────────────────────────

// resolveJobs expands job and preset names to built-in jobs, each once. An old
// job name prints the rename line. An unknown or retired name is an error.
func resolveJobs(names []string, globals globalFlags, stderr io.Writer) ([]*cron.BuiltinJob, error) {
	var jobs []*cron.BuiltinJob
	for _, name := range names {
		resolved, aliased, err := cron.Resolve(name)
		if err != nil {
			return nil, err
		}
		if aliased {
			aliasNote(globals, stderr, name)
		}
		for _, j := range resolved {
			if !slices.ContainsFunc(jobs, func(x *cron.BuiltinJob) bool { return x.Name == j.Name }) {
				jobs = append(jobs, j)
			}
		}
	}
	return jobs, nil
}

func cronAdd(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cron-add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	display := fs.String("display", "", "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(stderr, "usage: ws cron add <job|preset…> [--display <d>]")
		return 1
	}
	jobs, err := resolveJobs(fs.Args(), globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if !preflight(stderr) {
		return 1
	}
	opts, err := cronPlanOptions(globals, *display)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	actions, already, err := cron.AddActions(jobs, opts)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	out := quietOut(globals, stdout)
	for _, name := range already {
		fmt.Fprintf(out, "  %s %s is already installed\n", style.Mutedf(globals.noColor, "–"), name)
	}
	planResult := RunPlan(Plan{Command: "cron.add", Actions: actions}, stdin, stdout, globals)
	if globals.json {
		type jobInfo struct {
			Name     string `json:"name"`
			Schedule string `json:"schedule"`
		}
		infos := []jobInfo{}
		for _, j := range jobs {
			infos = append(infos, jobInfo{Name: j.Name, Schedule: j.Schedule})
		}
		return writeJSONDryRun(stdout, stderr, "cron.add", globals.dryRun, map[string]any{"jobs": infos, "actions": planResult.Actions})
	}
	if n := planResult.ExecutedCount(); n > 0 && !globals.dryRun {
		fmt.Fprintln(out, style.ResultSuccess(globals.noColor, "%s installed.", pluralize(n, "cron job")))
	}
	return planResult.ExitCode()
}

// ── rm ─────────────────────────────────────────────────────────────

func cronRm(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cron-rm", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(stderr, "usage: ws cron rm <job|preset…>")
		return 1
	}
	// rm takes built-in jobs, presets (their members), old job names and retired
	// jobs, whose blocks it still removes.
	var names []string
	for _, name := range fs.Args() {
		if members, ok := cron.Presets[name]; ok {
			names = append(names, members...)
			continue
		}
		if _, known := cron.Builtins[name]; !known && !cron.IsRetired(name) {
			if _, old := cron.Canonical(name); !old {
				fmt.Fprintf(stderr, "unknown cron job %q — run `ws cron ls` to see available jobs\n", name)
				return 1
			}
			aliasNote(globals, stderr, name)
		}
		names = append(names, name)
	}
	if !preflight(stderr) {
		return 1
	}
	opts, err := cronPlanOptions(globals, "")
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	actions, notInstalled, err := cron.RemoveActions(names, opts)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	out := quietOut(globals, stdout)
	for _, name := range notInstalled {
		fmt.Fprintf(out, "  %s %s is not installed\n", style.Mutedf(globals.noColor, "–"), name)
	}
	planResult := RunPlan(Plan{Command: "cron.rm", Actions: actions}, stdin, stdout, globals)
	if globals.json {
		removed := []string{}
		for _, a := range planResult.Actions {
			if a.Status == "executed" {
				removed = append(removed, strings.TrimPrefix(a.ID, "cron-rm-"))
			}
		}
		return writeJSONDryRun(stdout, stderr, "cron.rm", globals.dryRun, map[string]any{"removed": removed, "actions": planResult.Actions})
	}
	return planResult.ExitCode()
}

// ── run ────────────────────────────────────────────────────────────

// cronRun runs one job now. Crontab lines call it, so it never prompts and an old
// job name runs silently (it is plumbing, not a typed alias).
func cronRun(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cron-run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ifDue := fs.Bool("if-due", false, "")
	display := fs.String("display", "", "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) != 1 {
		fmt.Fprintln(stderr, "usage: ws cron run <job> [--if-due] [--display <d>]")
		return 1
	}
	workspacePath, _, err := resolveWorkspacePaths(globals)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	// The job's output also goes to ws's own stdout and stderr, unless --json.
	var jobOut, jobErr io.Writer = stdout, stderr
	if globals.json || globals.quiet {
		jobOut, jobErr = nil, nil
	}
	res, err := cron.RunJob(fs.Args()[0], cron.RunOptions{
		WorkspacePath: workspacePath, Display: *display, IfDue: *ifDue,
		Stdout: jobOut, Stderr: jobErr, DryRun: globals.dryRun,
	})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if globals.dryRun && !globals.json {
		fmt.Fprintf(stdout, "Would run %s:\n%s\n", res.Job, res.Command)
		return 0
	}
	if globals.json {
		// The job's own code is data.exit_code; the command's exit is 0 or 1.
		code := 0
		if res.ExitCode != 0 {
			code = 1
		}
		writeJSON(stdout, stderr, "cron.run", map[string]any{"job": res.Job, "exit_code": res.ExitCode, "skipped": res.Skipped})
		return code
	}
	if res.ExitCode != 0 {
		return 1
	}
	return 0
}

// ── log ────────────────────────────────────────────────────────────

func cronLog(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cron-log", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	lines := fs.Int("lines", 20, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	// A preset stands for its members; an old name for its job.
	var jobs []string
	for _, name := range fs.Args() {
		switch {
		case cron.Presets[name] != nil:
			jobs = append(jobs, cron.Presets[name]...)
		case cron.IsRetired(name):
			jobs = append(jobs, name)
		default:
			if _, _, err := cron.Resolve(name); err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}
			aliasNote(globals, stderr, name)
			jobs = append(jobs, name)
		}
	}
	logPath, err := cron.LogFilePath()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	entries, err := cron.ReadLog(logPath, jobs, *lines)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if globals.json {
		if jobs == nil {
			jobs = []string{}
		}
		if entries == nil {
			entries = []string{}
		}
		return writeJSON(stdout, stderr, "cron.log", map[string]any{"jobs": jobs, "lines": entries})
	}
	out := textOut(globals, stdout)
	if len(entries) == 0 {
		fmt.Fprintln(out, "No cron log entries.")
		return 0
	}
	for _, l := range entries {
		fmt.Fprintln(out, l)
	}
	return 0
}

// ── apply ──────────────────────────────────────────────────────────

func cronApply(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cron-apply", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if !preflight(stderr) {
		return 1
	}
	opts, err := cronPlanOptions(globals, "")
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	actions, res, err := cron.ApplyActions(opts)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	out := quietOut(globals, stdout)
	for _, name := range res.Skipped {
		fmt.Fprintf(out, "  %s skipped %s: not a ws job\n", style.Mutedf(globals.noColor, "–"), name)
	}
	if len(actions) == 0 && !globals.json {
		fmt.Fprintf(out, "The crontab already matches %s.\n", opts.Declared)
		return 0
	}
	planResult := RunPlan(Plan{Command: "cron.apply", Actions: actions}, stdin, stdout, globals)
	if globals.json {
		nonNilS := func(s []string) []string {
			if s == nil {
				return []string{}
			}
			return s
		}
		return writeJSONDryRun(stdout, stderr, "cron.apply", globals.dryRun, map[string]any{
			"installed": nonNilS(res.Installed), "removed": nonNilS(res.Removed), "unchanged": nonNilS(res.Unchanged),
			"skipped": nonNilS(res.Skipped), "lines_added": res.LinesAdded, "actions": planResult.Actions,
		})
	}
	return planResult.ExitCode()
}
