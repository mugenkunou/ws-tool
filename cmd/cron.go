package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mugenkunou/ws-tool/internal/cron"
	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/provision"
	"github.com/mugenkunou/ws-tool/internal/style"
)

var cronHelp = cmdHelp{
	Usage: "ws cron <add|rm|remove|ls|list|status|log>",
	Subcommands: []string{
		"  add <job>      Install a managed cron job (or preset)",
		"  rm <job>       Remove a managed cron job (or preset)",
		"  remove <job>   Alias for rm",
		"  ls             List available jobs and their install status",
		"  list           Alias for ls",
		"  status [job]   Show last run, exit code, and next scheduled run",
		"  log [job]      Show recent log entries",
	},
	Flags: []string{
		"      --display string   X11 DISPLAY for mega-sync (default: $DISPLAY then :1)",
		"      --lines int        Lines of log to show for 'log' subcommand (default: 20)",
		"      --dry-run          Preview only (add/rm)",
	},
}

func runCron(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, cronHelp)
	}
	if len(args) == 0 {
		return printCmdHelp(stdout, cronHelp)
	}

	sub := args[0]
	subArgs := args[1:]
	switch sub {
	case "add":
		return runCronAdd(subArgs, globals, stdin, stdout, stderr)
	case "rm", "remove":
		return runCronRm(subArgs, globals, stdin, stdout, stderr)
	case "ls", "list":
		return runCronLs(subArgs, globals, stdout, stderr)
	case "status":
		return runCronStatus(subArgs, globals, stdout, stderr)
	case "log":
		return runCronLog(subArgs, globals, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown cron subcommand: %s\n", sub)
		return printUsageError(stderr, cronHelp)
	}
}

// ── add ──────────────────────────────────────────────────────────────────────

func runCronAdd(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cron-add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	display := fs.String("display", "", "X11 DISPLAY for mega-sync")
	dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if *dryRun {
		globals.dryRun = true
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		fmt.Fprintln(stderr, "usage: ws cron add <job>")
		fmt.Fprintln(stderr, "Run `ws cron ls` to see available jobs.")
		return 1
	}
	jobName := remaining[0]

	// Validate the job name before any system checks so the user gets a clear
	// "unknown job" error rather than a misleading "install cron" message.
	jobs, err := cron.Resolve(jobName)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if !globals.dryRun {
		if err := cron.Preflight(); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
	}

	workspacePath, _, manifestPath, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	// Detect X11 DISPLAY if not provided by flag.
	dispVal := *display
	if dispVal == "" {
		dispVal = os.Getenv("DISPLAY")
	}
	if dispVal == "" {
		dispVal = ":1"
	}

	// Resolve the ws binary path for embedding in wrapper scripts.
	wsBin, err := os.Executable()
	if err != nil {
		wsBin = "ws"
	}
	if resolved, resolveErr := filepath.EvalSymlinks(wsBin); resolveErr == nil {
		wsBin = resolved
	}

	statePath, err := cron.StateFilePath()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	logPath, err := cron.LogFilePath()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	nc := globals.noColor
	out := textOut(globals, stdout)

	plan := Plan{Command: "cron.add"}
	for _, job := range jobs {
		j := job // capture loop variable
		scriptPath, scriptErr := cron.ScriptPath(j.Name)
		if scriptErr != nil {
			fmt.Fprintln(stderr, scriptErr.Error())
			return 1
		}
		scriptContent := cron.GenerateScript(j, wsBin, dispVal, workspacePath, statePath, logPath)

		plan.Actions = append(plan.Actions, Action{
			ID:          "cron-write-script-" + j.Name,
			Description: fmt.Sprintf("Write wrapper script %s", scriptPath),
			Execute: func() error {
				if mkErr := os.MkdirAll(filepath.Dir(scriptPath), 0o755); mkErr != nil {
					return mkErr
				}
				return os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
			},
		})

		plan.Actions = append(plan.Actions, Action{
			ID:          "cron-install-" + j.Name,
			Description: fmt.Sprintf("Install crontab entries for %s (%s + @reboot)", j.Name, j.Schedule),
			Execute: func() error {
				if addErr := cron.AddJob(j, scriptPath); addErr != nil {
					return addErr
				}
				return manifest.RecordProvision(manifestPath, provision.Entry{
					Type:    provision.TypeCronJob,
					Path:    scriptPath,
					Line:    j.Name,
					Command: "cron add",
				})
			},
		})
	}

	planResult := RunPlan(plan, stdin, stdout, globals)

	if globals.dryRun {
		return 0
	}

	if globals.json {
		type jobResult struct {
			Name       string `json:"name"`
			Schedule   string `json:"schedule"`
			ScriptPath string `json:"script_path"`
		}
		var results []jobResult
		for _, j := range jobs {
			sp, _ := cron.ScriptPath(j.Name)
			results = append(results, jobResult{Name: j.Name, Schedule: j.Schedule, ScriptPath: sp})
		}
		return writeJSON(stdout, stderr, "cron.add", map[string]any{"jobs": results})
	}

	if !planResult.HasFailures() && planResult.ExecutedCount() > 0 {
		fmt.Fprintln(out, style.ResultSuccess(nc, "Cron job(s) installed. Run `ws cron status` to verify."))
	}
	return planResult.ExitCode()
}

// ── rm ───────────────────────────────────────────────────────────────────────

func runCronRm(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cron-rm", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if *dryRun {
		globals.dryRun = true
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		fmt.Fprintln(stderr, "usage: ws cron rm <job>")
		return 1
	}
	jobName := remaining[0]

	if !globals.dryRun {
		if err := cron.Preflight(); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
	}

	_, _, manifestPath, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	jobs, err := cron.Resolve(jobName)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	nc := globals.noColor
	out := textOut(globals, stdout)

	plan := Plan{Command: "cron.rm"}
	for _, job := range jobs {
		j := job // capture loop variable
		plan.Actions = append(plan.Actions, Action{
			ID:          "cron-rm-" + j.Name,
			Description: fmt.Sprintf("Remove crontab entries and wrapper script for %s", j.Name),
			Execute: func() error {
				if rmErr := cron.RemoveJob(j.Name); rmErr != nil {
					return rmErr
				}
				scriptPath, _ := cron.ScriptPath(j.Name)
				_ = os.Remove(scriptPath) // best-effort; may already be absent
				return manifest.RemoveCronJobProvision(manifestPath, j.Name)
			},
		})
	}

	planResult := RunPlan(plan, stdin, stdout, globals)

	if globals.json {
		var removed []string
		for _, j := range jobs {
			removed = append(removed, j.Name)
		}
		return writeJSON(stdout, stderr, "cron.rm", map[string]any{"removed": removed})
	}

	if !planResult.HasFailures() && planResult.ExecutedCount() > 0 {
		fmt.Fprintln(out, style.ResultSuccess(nc, "Removed."))
	}
	return planResult.ExitCode()
}

// ── ls ───────────────────────────────────────────────────────────────────────

func runCronLs(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cron-ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	nc := globals.noColor
	statePath, _ := cron.StateFilePath()

	// Collect last run records for installed-job status.
	records, _ := cron.ReadState(statePath)
	lastByJob := map[string]cron.RunRecord{}
	for _, r := range records {
		lastByJob[r.Job] = r
	}

	if globals.json {
		type jobInfo struct {
			Name         string `json:"name"`
			Schedule     string `json:"schedule,omitempty"`
			Description  string `json:"description"`
			Preset       bool   `json:"preset,omitempty"`
			Installed    bool   `json:"installed"`
			LastRunTime  string `json:"last_run,omitempty"`
			LastExitCode int    `json:"last_exit,omitempty"`
		}
		var infos []jobInfo
		for _, name := range cron.AllNames() {
			installed, _ := cron.HasJob(name)
			ji := jobInfo{Name: name, Installed: installed}
			if job, ok := cron.Builtins[name]; ok {
				ji.Schedule = job.Schedule
				ji.Description = job.Description
			} else {
				ji.Preset = true
				ji.Description = presetDescription(name)
			}
			if r, ok := lastByJob[name]; ok {
				ji.LastRunTime = r.Time.UTC().Format(time.RFC3339)
				ji.LastExitCode = r.ExitCode
			}
			infos = append(infos, ji)
		}
		return writeJSON(stdout, stderr, "cron.ls", map[string]any{"jobs": infos})
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, style.Boldf(nc, "Available cron jobs:"))
	fmt.Fprintln(tw, style.Mutedf(nc, "  NAME\tSCHEDULE\tDESCRIPTION\tSTATUS"))
	for _, name := range cron.AllNames() {
		installed, _ := cron.HasJob(name)
		status := style.Mutedf(nc, "not installed")
		if installed {
			status = style.Successf(nc, "installed")
		}
		if job, ok := cron.Builtins[name]; ok {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", name, job.Schedule, job.Description, status)
		} else {
			fmt.Fprintf(tw, "  %s\t(preset)\t%s\t%s\n", name, presetDescription(name), status)
		}
	}
	_ = tw.Flush()

	// Last-run summary for installed jobs.
	var installedNames []string
	for name := range cron.Builtins {
		if ok, _ := cron.HasJob(name); ok {
			installedNames = append(installedNames, name)
		}
	}
	if len(installedNames) > 0 {
		sort.Strings(installedNames)
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, style.Boldf(nc, "Last run:"))
		tw2 := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		for _, name := range installedNames {
			r, ok := lastByJob[name]
			if !ok {
				fmt.Fprintf(tw2, "  %s\tnever\n", name)
				continue
			}
			exitStr := style.Successf(nc, "exit 0")
			if r.ExitCode != 0 {
				exitStr = style.ResultError(nc, "exit %d", r.ExitCode)
			}
			fmt.Fprintf(tw2, "  %s\t%s\t%s\n",
				name,
				r.Time.Local().Format("2006-01-02 15:04"),
				exitStr,
			)
		}
		_ = tw2.Flush()
	}

	return 0
}

func presetDescription(name string) string {
	jobs, ok := cron.Presets[name]
	if !ok {
		return ""
	}
	return "installs: " + strings.Join(jobs, ", ")
}

// ── status ───────────────────────────────────────────────────────────────────

func runCronStatus(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cron-status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	jobFilter := ""
	if remaining := fs.Args(); len(remaining) > 0 {
		jobFilter = remaining[0]
	}

	nc := globals.noColor
	statePath, _ := cron.StateFilePath()
	logPath, _ := cron.LogFilePath()

	// Determine which jobs to show.
	var targetJobs []*cron.BuiltinJob
	if jobFilter == "" {
		// Show all installed jobs.
		for _, name := range cron.AllNames() {
			job, isJob := cron.Builtins[name]
			if !isJob {
				continue
			}
			if ok, _ := cron.HasJob(name); ok {
				targetJobs = append(targetJobs, job)
			}
		}
		if len(targetJobs) == 0 {
			fmt.Fprintln(stdout, "No ws-managed cron jobs installed. Run `ws cron add <job>`.")
			return 0
		}
	} else {
		jobs, err := cron.Resolve(jobFilter)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		targetJobs = jobs
	}

	if globals.json {
		type jobStatus struct {
			Name         string `json:"name"`
			Schedule     string `json:"schedule"`
			Installed    bool   `json:"installed"`
			LastRunTime  string `json:"last_run,omitempty"`
			LastExitCode int    `json:"last_exit"`
			NextRunTime  string `json:"next_run,omitempty"`
		}
		var statuses []jobStatus
		for _, job := range targetJobs {
			installed, _ := cron.HasJob(job.Name)
			r, _ := cron.LastRun(statePath, job.Name)
			js := jobStatus{
				Name:      job.Name,
				Schedule:  job.Schedule,
				Installed: installed,
			}
			if !r.Time.IsZero() {
				js.LastRunTime = r.Time.UTC().Format(time.RFC3339)
				js.LastExitCode = r.ExitCode
				next := r.Time.Add(time.Duration(job.IntervalSecs) * time.Second)
				js.NextRunTime = next.UTC().Format(time.RFC3339)
			}
			statuses = append(statuses, js)
		}
		return writeJSON(stdout, stderr, "cron.status", map[string]any{"jobs": statuses})
	}

	sep := strings.Repeat("─", 54)
	for i, job := range targetJobs {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		r, _ := cron.LastRun(statePath, job.Name)
		installed, _ := cron.HasJob(job.Name)

		fmt.Fprintln(stdout, style.Boldf(nc, job.Name))
		fmt.Fprintln(stdout, style.Mutedf(nc, sep))

		if installed {
			fmt.Fprintf(stdout, "  Status      %s\n",
				style.Successf(nc, "installed  (%s + @reboot)", job.Schedule))
		} else {
			fmt.Fprintf(stdout, "  Status      %s\n", style.Warningf(nc, "not installed"))
		}

		if r.Time.IsZero() {
			fmt.Fprintln(stdout, "  Last run    never")
		} else {
			ago := time.Since(r.Time).Truncate(time.Second)
			exitStr := style.Successf(nc, "exit 0")
			if r.ExitCode != 0 {
				exitStr = style.ResultError(nc, "exit %d", r.ExitCode)
			}
			fmt.Fprintf(stdout, "  Last run    %s  (%s ago)  %s\n",
				r.Time.Local().Format("2006-01-02 15:04:05"), ago, exitStr)

			next := r.Time.Add(time.Duration(job.IntervalSecs) * time.Second)
			if time.Now().After(next) {
				fmt.Fprintln(stdout, "  Next run    "+style.Warningf(nc, "overdue — check crontab"))
			} else {
				fmt.Fprintf(stdout, "  Next run    ~%s\n", next.Local().Format("2006-01-02 15:04"))
			}
		}

		// Show last 5 log lines for this job.
		logLines, _ := cron.ReadLog(logPath, job.Name, 5)
		if len(logLines) > 0 {
			fmt.Fprintln(stdout, "  Recent log:")
			for _, l := range logLines {
				fmt.Fprintf(stdout, "    %s\n", style.Mutedf(nc, "%s", l))
			}
		}
	}

	return 0
}

// ── log ──────────────────────────────────────────────────────────────────────

func runCronLog(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cron-log", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	lines := fs.Int("lines", 20, "number of log lines to show")
	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	jobName := ""
	if remaining := fs.Args(); len(remaining) > 0 {
		jobName = remaining[0]
	}

	logPath, err := cron.LogFilePath()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	logLines, err := cron.ReadLog(logPath, jobName, *lines)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if globals.json {
		return writeJSON(stdout, stderr, "cron.log", map[string]any{
			"job":   jobName,
			"lines": logLines,
		})
	}

	if len(logLines) == 0 {
		if jobName != "" {
			fmt.Fprintf(stdout, "No log entries for job %q.\n", jobName)
		} else {
			fmt.Fprintln(stdout, "No cron log entries found.")
		}
		return 0
	}

	for _, l := range logLines {
		fmt.Fprintln(stdout, l)
	}
	return 0
}
