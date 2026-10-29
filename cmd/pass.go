package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/backup"
	"github.com/mugenkunou/ws-tool/internal/pass"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

const passUsage = "usage: ws pass <init|status|audit|backup> …"

// runPass dispatches `ws pass <verb>` (spec §5 pass). The store belongs to the
// machine, so no pass command needs an initialized workspace.
func runPass(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, passUsage)
		return 1
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "init":
		return passInit(rest, globals, stdin, stdout, stderr)
	case "status":
		return passStatus(rest, globals, stdout, stderr)
	case "audit":
		return passAudit(rest, globals, stdout, stderr)
	case "backup":
		return runBackup(passBackupSpec(globals), rest, globals, stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "unknown command: pass %s\n", verb)
	fmt.Fprintln(stderr, passUsage)
	return 1
}

// optionalWorkspace resolves the workspace without requiring it to be
// initialized. hasConfig is true when its ws.json exists.
func optionalWorkspace(globals globalFlags) (workspacePath, configPath string, hasConfig bool) {
	workspacePath, configPath, err := resolveWorkspacePaths(globals)
	if err != nil {
		return "", "", false
	}
	return workspacePath, configPath, workspace.ConfigExists(configPath)
}

// ── status ─────────────────────────────────────────────────────────

// passReport is everything `pass status` reports.
type passReport struct {
	Health     pass.PassHealth
	RemoteURL  string
	CredState  string // connected, stale or disconnected
	CredValue  string
	Workspace  string // "" when it cannot be resolved
	CopyTarget string // where the store is linked into the workspace, or ""
}

func gatherPassReport(globals globalFlags) passReport {
	r := passReport{Health: pass.CheckPass()}
	if r.Health.GitBacked && r.Health.GitRemote {
		r.RemoteURL = backup.RemoteURL(r.Health.StorePath)
	}
	r.CredValue = gitConfigGet("credential.helper")
	r.CredState, _ = helperCheck(r.CredValue)
	r.Workspace, _, _ = optionalWorkspace(globals)
	if r.Workspace != "" && r.Health.StorePath != "" {
		r.CopyTarget, _ = pass.WorkspaceCopy(r.Health.StorePath, r.Workspace)
	}
	return r
}

// data is the JSON of `pass status` (spec §5 pass status).
func (r passReport) data() map[string]any {
	return map[string]any{
		"gpg_available":     r.Health.GPGAvailable,
		"pass_installed":    r.Health.Installed,
		"initialized":       r.Health.Initialized,
		"store_path":        r.Health.StorePath,
		"entry_count":       r.Health.EntryCount,
		"git_backed":        r.Health.GitBacked,
		"git_remote":        r.Health.GitRemote,
		"remote_url":        r.RemoteURL,
		"cred_helper":       r.CredState == "connected",
		"cred_helper_value": r.CredValue,
	}
}

// copyTip is the one-line advice that ends `pass status`, `pass init` and
// `pass backup enable` (spec §5 pass, "A second copy in the workspace"). It is
// empty when the store is already linked into the workspace or no workspace
// can be resolved.
func (r passReport) copyTip() string {
	if r.CopyTarget != "" || r.Workspace == "" || r.Health.StorePath == "" {
		return ""
	}
	return "ws " + pass.CopyCommand(r.Health.StorePath)
}

// passTip is the tip line for the commands that end with one, "" when none.
func passTip(globals globalFlags) string {
	tip := gatherPassReport(globals).copyTip()
	if tip == "" {
		return ""
	}
	return "  Tip: " + tip
}

func passStatus(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pass-status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	r := gatherPassReport(globals)
	if globals.json {
		return writeJSON(stdout, stderr, "pass.status", r.data())
	}
	printPassReport(textOut(globals, stdout), r, globals.noColor)
	return 0
}

// printPassReport writes the `pass status` lines: a label column, then detail.
func printPassReport(out io.Writer, r passReport, nc bool) {
	h := r.Health
	line := func(icon, label, detail string) {
		if detail == "" {
			fmt.Fprintf(out, "%s %s\n", icon, label)
			return
		}
		fmt.Fprintf(out, "%s %-17s  %s\n", icon, label, detail)
	}
	check := func(ok bool) string {
		if ok {
			return style.IconCheck(nc)
		}
		return style.IconCross(nc)
	}
	line(check(h.GPGAvailable), "gpg available", "")
	line(check(h.Installed), "pass installed", "")
	if h.Initialized {
		line(check(true), "store initialized", fmt.Sprintf("%s, %s", h.StorePath, countEntries(h.EntryCount)))
	} else {
		line(check(false), "store initialized", "")
	}
	line(check(h.GitBacked), "git-backed", "")
	if h.GitBacked {
		if h.GitRemote {
			line(check(true), "remote", r.RemoteURL)
		} else {
			line(style.IconWarning(nc), "no remote", "secrets live only on this machine")
		}
	}
	switch r.CredState {
	case "connected":
		line(check(true), "credential helper", "connected")
	case "stale":
		line(style.IconWarning(nc), "credential helper", "stale")
	default:
		line(check(false), "credential helper", "not connected")
	}

	if next := r.next(); next != "" {
		fmt.Fprintf(out, "  Next: %s\n", next)
	}
	if r.CopyTarget != "" {
		line(style.IconCheck(nc), "workspace copy", r.CopyTarget)
	} else if tip := r.copyTip(); tip != "" {
		line(style.IconInfo(nc), "workspace copy", "none: the store syncs only through its git remote (a copy in the workspace is a second sync path)")
		fmt.Fprintf(out, "  Tip: %s\n", tip)
	}
}

// next is the first missing piece, as a command or instruction.
func (r passReport) next() string {
	h := r.Health
	switch {
	case !h.GPGAvailable:
		return "install the package gnupg"
	case !h.Installed:
		return "install the package pass"
	case !h.Initialized, !h.GitBacked:
		return "ws pass init"
	case !h.GitRemote:
		return "ws pass backup enable --remote <url>   (use a private repository)"
	case r.CredState != "connected":
		return "ws credential enable"
	}
	return ""
}

// ── audit ──────────────────────────────────────────────────────────

func passAudit(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pass-audit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	res := pass.AuditPassStore(pass.CheckPass())
	if globals.json {
		return writeJSON(stdout, stderr, "pass.audit", map[string]any{"findings": res.Findings})
	}
	out := textOut(globals, stdout)
	nc := globals.noColor
	if len(res.Findings) == 0 {
		fmt.Fprintln(out, style.ResultSuccess(nc, "No problems found in the pass store."))
		return 0
	}
	for _, f := range res.Findings {
		if f.Entry != "" {
			fmt.Fprintf(out, "%s %s  %s\n", style.IconWarning(nc), f.Entry, f.Message)
		} else {
			fmt.Fprintf(out, "%s %s\n", style.IconWarning(nc), f.Message)
		}
	}
	return 0
}

// ── init ───────────────────────────────────────────────────────────

func passInit(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pass-init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	// Hidden: `secret setup --git-remote <url>` still works and adds the
	// `pass backup enable` action.
	gitRemote := fs.String("git-remote", "", "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintln(stderr, "usage: ws pass init")
		return 1
	}
	out := quietOut(globals, stdout)
	nc := globals.noColor

	health := pass.CheckPass()
	if !health.GPGAvailable {
		fmt.Fprintln(stderr, style.ResultError(nc, "gpg not found. Install the package gnupg (sudo apt install gnupg)."))
		return 1
	}
	if !health.Installed {
		fmt.Fprintln(stderr, style.ResultError(nc, "pass not found. Install the package pass (sudo apt install pass)."))
		return 1
	}

	var gpgID string
	if !health.Initialized {
		var ok bool
		if gpgID, ok = chooseGPGKey(globals, stdin, stdout, stderr); !ok {
			return 1
		}
	}

	p := Plan{Command: "pass.init"}
	if !health.Initialized {
		id := gpgID
		p.Actions = append(p.Actions, Action{
			ID:          "pass-init",
			Description: fmt.Sprintf("Initialize the pass store %s with GPG key %s", health.StorePath, id),
			Execute:     func() error { return pass.InitStore(id) },
		})
	}
	if !health.GitBacked {
		p.Actions = append(p.Actions, Action{
			ID:          "pass-git-init",
			Description: "Turn on git versioning for the pass store",
			Execute:     pass.InitGit,
		})
	}
	if url := strings.TrimSpace(*gitRemote); url != "" && (!health.GitBacked || backup.RemoteURL(health.StorePath) != url) {
		spec := passBackupSpec(globals)
		p.Actions = append(p.Actions, Action{
			ID:          "set-remote",
			Description: fmt.Sprintf("Set the backup remote to %s", url),
			Execute: func() error {
				if err := backup.CheckRemote(url, "").Refusal(); err != nil {
					return err
				}
				return backup.SetRemote(spec.repoPath, url)
			},
		})
	}

	if len(p.Actions) == 0 {
		if globals.json {
			return writeJSON(stdout, stderr, "pass.init", map[string]any{
				"actions": []ActionStatus{}, "pass": gatherPassReport(globals).data(),
			})
		}
		fmt.Fprintln(out, style.ResultSuccess(nc, "The pass store is already set up."))
		return 0
	}

	planResult := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "pass.init", globals.dryRun, map[string]any{
			"actions": planResult.Actions, "pass": gatherPassReport(globals).data(),
		})
	}
	if planResult.ExecutedCount() > 0 && !planResult.HasFailures() {
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.Mutedf(nc, "  ws pass backup enable --remote <url>   backs the store up to a private remote"))
		if tip := passTip(globals); tip != "" {
			fmt.Fprintln(out, tip)
		}
	}
	return planResult.ExitCode()
}

// chooseGPGKey picks the GPG key for `pass init`: the only one, or the one the
// user numbers. --quiet and --json take the first and say which on stderr.
func chooseGPGKey(globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) (string, bool) {
	nc := globals.noColor
	keys, err := pass.ListGPGKeys()
	if err != nil || len(keys) == 0 {
		fmt.Fprintln(stderr, style.ResultError(nc, "No GPG secret key found."))
		fmt.Fprintln(stderr, style.Mutedf(nc, "Generate one with: gpg --full-generate-key"))
		fmt.Fprintln(stderr, style.Mutedf(nc, "Then run: ws pass init"))
		return "", false
	}
	if len(keys) == 1 {
		return keys[0].ID, true
	}
	if globals.json || globals.quiet {
		fmt.Fprintf(stderr, "Using GPG key %s (%s), the first of %d\n", keys[0].ID, keys[0].Name, len(keys))
		return keys[0].ID, true
	}
	fmt.Fprintln(stdout, "Available GPG keys:")
	for i, k := range keys {
		fmt.Fprintf(stdout, "  %d) %s  %s\n", i+1, k.ID, style.Mutedf(nc, "%s", k.Name))
	}
	answer := promptLine(stdin, stdout, globals, "Key number", "1")
	n := 0
	if _, err := fmt.Sscanf(answer, "%d", &n); err != nil || n < 1 || n > len(keys) {
		fmt.Fprintf(stderr, "%q is not a key number between 1 and %d\n", answer, len(keys))
		return "", false
	}
	return keys[n-1].ID, true
}

// ── backup ─────────────────────────────────────────────────────────

// countEntries is "1 entry" or "42 entries".
func countEntries(n int) string {
	if n == 1 {
		return "1 entry"
	}
	return fmt.Sprintf("%d entries", n)
}

// passBackupSpec is the pass side of the shared backup runner. The repository
// is the pass store itself (a git repository made by `pass git init`). pass
// commits every change on its own, so there is no auto-commit setting; the one
// ws.json setting is pass.pass_entry, kept only when a workspace exists.
func passBackupSpec(globals globalFlags) backupSpec {
	workspacePath, configPath, hasConfig := optionalWorkspace(globals)
	store := pass.CheckPass().StorePath
	return backupSpec{
		job:       "pass-backup",
		repoPath:  store,
		workspace: workspacePath,

		requires: func() error {
			h := pass.CheckPass()
			switch {
			case !h.Installed:
				return fmt.Errorf("pass not found. Install the package pass.")
			case !h.Initialized:
				return fmt.Errorf("the pass store is not initialized: run ws pass init")
			}
			return nil
		},
		initialized: func() bool { return backup.IsRepo(store) },
		initPlan:    "Turn on git versioning for the pass store",
		init:        pass.InitGit,

		load: func() (backupSettings, error) {
			var s backupSettings
			if hasConfig {
				cfg, err := wsconfig.Load(configPath)
				if err != nil {
					return s, err
				}
				s.PassEntry = cfg.Pass.PassEntry
			}
			if backup.IsRepo(store) {
				s.RemoteURL = backup.RemoteURL(store)
				s.Branch = backup.CurrentBranch(store)
			}
			return s, nil
		},
		save: func(s backupSettings) error {
			if !hasConfig {
				return nil // no workspace to keep the setting in
			}
			cfg, err := wsconfig.Load(configPath)
			if err != nil {
				return err
			}
			if cfg.Pass.PassEntry == s.PassEntry {
				return nil
			}
			cfg.Pass.PassEntry = s.PassEntry
			return wsconfig.Save(configPath, cfg)
		},
		commit: func(message string) (bool, error) {
			if _, err := os.Stat(store); err != nil {
				return false, err
			}
			return backup.CommitAll(store, message)
		},
		defaultMessage: "ws pass backup push",
		entryCount:     func() int { return pass.CheckPass().EntryCount },
		tip:            func() string { return passTip(globals) },
	}
}
