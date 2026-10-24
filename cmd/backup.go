package cmd

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/backup"
	"github.com/mugenkunou/ws-tool/internal/cron"
	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// ws pass backup (spec §5 pass backup): enable, disable, status and push. The
// runner is split from pass.go so the verbs read as one unit. A backupSpec says
// where the repository is and how the pass store's settings and git steps work.

// backupSettings are the ws.json values the pass backup keeps.
type backupSettings struct {
	RemoteURL string
	Branch    string
	PassEntry string
}

// backupSpec describes the pass backup: its repository, settings and git steps.
type backupSpec struct {
	job       string // the cron job that pushes it ("pass-backup")
	repoPath  string
	workspace string

	// requires, when set, is checked before `enable` does anything: it returns
	// the error that stops it (pass needs an initialized store).
	requires func() error

	// initialized reports whether the repository exists; initPlan names the
	// action that creates it and init runs it.
	initialized func() bool
	initPlan    string
	init        func() error

	load func() (backupSettings, error)
	save func(backupSettings) error
	// commit commits pending changes and reports whether a commit was made.
	commit func(message string) (bool, error)
	// defaultMessage is the commit message of `backup push`.
	defaultMessage string
	// entryCount, when set, adds the store's entry count to the repository line.
	entryCount func() int
	// tip, when set, is printed after enable and init (the pass store's
	// workspace-copy tip).
	tip func() string
}

// runBackup dispatches `ws pass backup <verb>`.
func runBackup(spec backupSpec, args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "unknown command: pass backup")
		fmt.Fprintln(stderr, "usage: ws pass backup <enable|disable|status|push>")
		return 1
	}
	switch args[0] {
	case "enable":
		return runBackupEnable(spec, args[1:], globals, stdin, stdout, stderr)
	case "disable":
		return runBackupDisable(spec, args[1:], globals, stdin, stdout, stderr)
	case "status":
		return runBackupStatus(spec, args[1:], globals, stdout, stderr)
	case "push":
		return runBackupPush(spec, args[1:], globals, stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "unknown command: pass backup %s\n", args[0])
	fmt.Fprintln(stderr, "usage: ws pass backup <enable|disable|status|push>")
	return 1
}

func (s backupSpec) command(verb string) string { return "pass.backup." + verb }

// cronRemoval is the plan that removes the backup's cron job from the live
// crontab and from configs/crontab, under every name it has had, so a later
// `cron apply` does not bring it back. An unreadable crontab, or a job that is
// not installed, gives no action.
func (s backupSpec) cronRemoval() []Action {
	opts := cron.PlanOptions{Bin: cron.WSBinary(), Workspace: s.workspace}
	if s.workspace != "" {
		opts.Declared = cron.DeclaredPath(s.workspace)
	}
	acts, _, err := cron.RemoveActions([]string{s.job}, opts)
	if err != nil {
		return nil
	}
	return acts
}

// visibilityJSON is the shape of data.visibility (spec §5 pass backup enable).
func visibilityJSON(v repo.VisibilityResult) map[string]any {
	return map[string]any{
		"provider": v.Provider,
		"private":  v.Private,
		"checked":  v.Checked,
		"warning":  v.Warning,
		"error":    v.Error,
	}
}

// checkRemote runs the private-remote check and prints what it found. It
// returns an error when the remote is refused (public, or not found).
func checkRemote(url, passEntry string, globals globalFlags, out, stderr io.Writer) (repo.VisibilityResult, error) {
	nc := globals.noColor
	vis := backup.CheckRemote(url, passEntry)
	if err := vis.Refusal(); err != nil {
		fmt.Fprintln(stderr, style.ResultError(nc, "%s", err))
		if err == repo.ErrPublicRepository {
			fmt.Fprintln(stderr, style.Mutedf(nc, "Make the repository private, then retry."))
		}
		return vis, err
	}
	if n := vis.Notice(); n != "" {
		fmt.Fprintln(out, style.ResultWarning(nc, "%s", n))
	}
	return vis, nil
}

// ── enable ─────────────────────────────────────────────────────────

func runBackupEnable(spec backupSpec, args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pass-backup-enable", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	remote := fs.String("remote", "", "")
	passEntry := fs.String("pass-entry", "", "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) > 0 {
		fmt.Fprint(stderr, "usage: ws pass backup enable [--remote <url>] [--pass-entry <entry>]\n")
		return 1
	}
	out := quietOut(globals, stdout)
	nc := globals.noColor

	if spec.requires != nil {
		if err := spec.requires(); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
	}
	cur, err := spec.load()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	// The URL: --remote, else the current setting, else the repo's own origin,
	// else ask.
	url := strings.TrimSpace(*remote)
	if url == "" {
		url = cur.RemoteURL
	}
	if url == "" && spec.initialized() {
		url = backup.RemoteURL(spec.repoPath)
	}
	if url == "" {
		if globals.json || globals.quiet {
			fmt.Fprintln(stderr, "--remote <url> is required")
			return 1
		}
		fmt.Fprintln(out, style.Mutedf(nc, "  A private git remote gives your pass store an off-machine backup. Create a PRIVATE repository first."))
		url = promptLine(stdin, stdout, globals, "Remote URL of a PRIVATE repository", "")
		if url == "" {
			fmt.Fprintln(stderr, "a remote is required: ws pass backup enable --remote <url>")
			return 1
		}
	}

	entry := cur.PassEntry
	if *passEntry != "" {
		entry = *passEntry
	}

	fmt.Fprintln(out, style.Mutedf(nc, "Checking repository visibility…"))
	vis, err := checkRemote(url, entry, globals, out, stderr)
	if err != nil {
		return 1
	}

	branch := cur.Branch
	if branch == "" {
		branch = backup.DefaultBranch
	}

	var plan Plan
	plan.Command = spec.command("enable")
	if !spec.initialized() {
		plan.Actions = append(plan.Actions, Action{
			ID:          "init",
			Description: spec.initPlan,
			Execute:     spec.init,
		})
	}
	if url != cur.RemoteURL || entry != cur.PassEntry || (spec.initialized() && backup.RemoteURL(spec.repoPath) != url) {
		plan.Actions = append(plan.Actions, Action{
			ID:          "set-remote",
			Description: fmt.Sprintf("Set the backup remote to %s", url),
			Execute: func() error {
				if err := backup.SetRemote(spec.repoPath, url); err != nil {
					return err
				}
				s, err := spec.load()
				if err != nil {
					return err
				}
				s.RemoteURL, s.Branch, s.PassEntry = url, branch, entry
				return spec.save(s)
			},
		})
	}

	if len(plan.Actions) == 0 {
		if globals.json {
			return writeJSON(stdout, stderr, spec.command("enable"), map[string]any{
				"actions": []ActionStatus{}, "remote_url": url, "visibility": visibilityJSON(vis),
			})
		}
		fmt.Fprintln(out, style.ResultSuccess(nc, "Backup is already enabled."))
		return 0
	}

	planResult := RunPlan(plan, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, spec.command("enable"), globals.dryRun, map[string]any{
			"actions": planResult.Actions, "remote_url": url, "visibility": visibilityJSON(vis),
		})
	}
	if planResult.ExecutedCount() > 0 && !planResult.HasFailures() {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  ws pass backup push           pushes now")
		fmt.Fprintf(out, "  ws cron add %-12s pushes every 30 minutes\n", spec.job)
		if spec.tip != nil {
			if t := spec.tip(); t != "" {
				fmt.Fprintln(out, t)
			}
		}
	}
	return planResult.ExitCode()
}

// ── disable ────────────────────────────────────────────────────────

func runBackupDisable(spec backupSpec, args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pass-backup-disable", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	out := quietOut(globals, stdout)
	nc := globals.noColor

	cur, err := spec.load()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	remote := cur.RemoteURL
	if remote == "" && spec.initialized() {
		remote = backup.RemoteURL(spec.repoPath)
	}

	var plan Plan
	plan.Command = spec.command("disable")
	if remote != "" {
		plan.Actions = append(plan.Actions, Action{
			ID:          "remove-remote",
			Description: fmt.Sprintf("Remove the backup remote %s", remote),
			Execute: func() error {
				if spec.initialized() {
					if err := backup.RemoveRemote(spec.repoPath); err != nil {
						return err
					}
				}
				s, err := spec.load()
				if err != nil {
					return err
				}
				s.RemoteURL = ""
				return spec.save(s)
			},
		})
	}
	plan.Actions = append(plan.Actions, spec.cronRemoval()...)

	if len(plan.Actions) == 0 {
		if globals.json {
			return writeJSON(stdout, stderr, spec.command("disable"), map[string]any{"actions": []ActionStatus{}})
		}
		fmt.Fprintln(out, "Backup is not enabled.")
		return 0
	}

	// Commits that never reached the remote stay local; say how many.
	unpushed := 0
	if remote != "" && spec.initialized() {
		unpushed, _ = backup.AheadBehind(spec.repoPath, cur.Branch)
	}

	planResult := RunPlan(plan, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, spec.command("disable"), globals.dryRun, map[string]any{"actions": planResult.Actions})
	}
	if planResult.WasExecuted("remove-remote") {
		fmt.Fprintln(out, style.Mutedf(nc, "  The repository and its history stay in %s.", spec.repoPath))
		if unpushed > 0 {
			fmt.Fprintln(out, style.ResultWarning(nc, "%d commit(s) were not pushed; they stay local.", unpushed))
		}
	}
	return planResult.ExitCode()
}

// ── status ─────────────────────────────────────────────────────────

// backupStatusData is the JSON of `pass backup status` (spec §5).
func backupStatusData(spec backupSpec, st backup.State, s backupSettings) map[string]any {
	data := map[string]any{
		"enabled":     s.RemoteURL != "",
		"initialized": st.Initialized,
		"repo_path":   spec.repoPath,
		"remote_url":  s.RemoteURL,
		"branch":      st.Branch,
		"ahead":       st.Ahead,
		"behind":      st.Behind,
		"pass_entry":  s.PassEntry,
		"uncommitted": st.Uncommitted,
		"last_commit": st.LastCommit,
		"commits":     st.Commits,
	}
	if spec.entryCount != nil {
		data["entry_count"] = spec.entryCount()
	}
	return data
}

func runBackupStatus(spec backupSpec, args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pass-backup-status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	count := fs.Int("n", 20, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	s, err := spec.load()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	branch := s.Branch
	if branch == "" {
		branch = backup.DefaultBranch
	}
	st := backup.Inspect(spec.repoPath, branch, *count)

	if globals.json {
		return writeJSON(stdout, stderr, spec.command("status"), backupStatusData(spec, st, s))
	}

	out := quietOut(globals, stdout)
	nc := globals.noColor
	if s.RemoteURL == "" {
		fmt.Fprintf(out, "%-13s %s\n", "Backup", "disabled")
		if !st.Initialized {
			fmt.Fprintf(out, "%-13s %s\n", "Repository", spec.repoPath+"  (not a git repository yet)")
		}
		fmt.Fprintln(out, style.Mutedf(nc, "  ws pass backup enable --remote <url>"))
		return 0
	}

	repoLine := spec.repoPath
	if spec.entryCount != nil {
		repoLine += fmt.Sprintf("  (%d entries)", spec.entryCount())
	}
	fmt.Fprintf(out, "%-13s %s\n", "Backup", "enabled")
	fmt.Fprintf(out, "%-13s %s\n", "Repository", repoLine)
	fmt.Fprintf(out, "%-13s %s\n", "Remote", s.RemoteURL)
	fmt.Fprintf(out, "%-13s %s\n", "Branch", st.Branch)
	fmt.Fprintf(out, "%-13s ↑%d ↓%d\n", "Ahead/behind", st.Ahead, st.Behind)
	if st.LastCommit != "" {
		fmt.Fprintf(out, "%-13s %s\n", "Last commit", st.LastCommit)
	}
	if len(st.Uncommitted) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Uncommitted")
		for _, l := range st.Uncommitted {
			fmt.Fprintf(out, "  %s\n", l)
		}
	}
	if len(st.Commits) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Recent commits")
		for _, c := range st.Commits {
			fmt.Fprintf(out, "  %s %s\n", c.Hash, c.Subject)
		}
	}
	return 0
}

// ── push ───────────────────────────────────────────────────────────

func runBackupPush(spec backupSpec, args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pass-backup-push", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	message := fs.String("m", "", "")
	// The old `git push --pass-entry` was parsed and ignored; it is still accepted.
	_ = fs.String("pass-entry", "", "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	out := quietOut(globals, stdout)
	nc := globals.noColor

	s, err := spec.load()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if s.RemoteURL == "" {
		fmt.Fprintf(stderr, "%s\n", style.ResultError(nc, "Backup is not enabled. Run: ws pass backup enable --remote <url>"))
		return 1
	}
	branch := s.Branch
	if branch == "" {
		branch = backup.DefaultBranch
	}
	msg := *message
	if msg == "" {
		msg = spec.defaultMessage
	}

	var committed, pushed bool
	var vis repo.VisibilityResult
	plan := Plan{Command: spec.command("push")}
	plan.Actions = append(plan.Actions, Action{
		ID:          "push",
		Description: fmt.Sprintf("Commit pending changes and push %s to %s", branch, s.RemoteURL),
		Execute: func() error {
			var err error
			if committed, err = spec.commit(msg); err != nil {
				return err
			}
			vis = backup.CheckRemote(s.RemoteURL, s.PassEntry)
			if err := vis.Refusal(); err != nil {
				return err
			}
			if err := backup.Push(spec.repoPath, branch); err != nil {
				return fmt.Errorf("push failed (commit kept locally): %w", err)
			}
			pushed = true
			return nil
		},
	})

	planResult := RunPlan(plan, stdin, stdout, globals)
	if n := vis.Notice(); n != "" && !globals.json {
		fmt.Fprintln(out, style.ResultWarning(nc, "%s", n))
	}
	if globals.json {
		return writeJSONDryRun(stdout, stderr, spec.command("push"), globals.dryRun, map[string]any{
			"actions":    planResult.Actions,
			"committed":  committed,
			"pushed":     pushed,
			"remote_url": s.RemoteURL,
			"branch":     branch,
		})
	}
	return planResult.ExitCode()
}
