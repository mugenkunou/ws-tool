package cmd

import (
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/secret"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// ── ws git-credential-helper ──
//
// Two-tier command:
//   Git plumbing (called by git, not for direct use): get, store, erase
//   User commands: setup, status, disconnect

var credentialHelp = cmdHelp{
	Usage: "ws git-credential-helper <command>",
	Subcommands: []string{
		"",
		"  User commands:",
		"  setup        Connect credential helper and create missing pass entries",
		"  status       Show credential helper config, pass health, and remote coverage",
		"  disconnect   Remove ws credential helper from git config",
		"",
		"  Git plumbing (called by git — not for direct use):",
		"  get          Look up credentials from pass",
		"  store        No-op (pass is managed separately)",
		"  erase        No-op (pass is managed separately)",
	},
}

func runGitCredentialHelper(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, credentialHelp)
	}

	if len(args) == 0 {
		// git credential helper spec: no operation → silent exit.
		return 0
	}

	switch args[0] {
	// Git plumbing — called by git, not for direct use.
	case "get":
		return runCredentialGet(stdin, stdout)
	case "store", "erase":
		// Read and discard stdin to be a well-behaved helper.
		io.Copy(io.Discard, stdin)
		return 0

	// User commands.
	case "setup":
		return runCredentialSetup(args[1:], globals, stdin, stdout, stderr)
	case "status":
		return runCredentialStatus(args[1:], globals, stdout, stderr)
	case "disconnect":
		return runCredentialDisconnect(args[1:], globals, stdin, stdout, stderr)

	default:
		// Unknown operation — silently ignore per git credential helper spec.
		return 0
	}
}

func runCredentialGet(stdin io.Reader, stdout io.Writer) int {
	req := secret.ParseCredentialInput(stdin)
	resp := secret.LookupCredential(req)
	if resp.Password == "" {
		// No credentials found — exit silently so git tries the next helper.
		return 0
	}
	secret.FormatCredentialOutput(stdout, resp)
	return 0
}

// ── ws git-credential-helper setup (RW) ──

var credentialSetupHelp = cmdHelp{
	Usage:       "ws git-credential-helper setup",
	Description: "Connect credential helper to git and create missing pass entries for workspace remotes.",
}

func runCredentialSetup(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, credentialSetupHelp)
	}

	fs := flag.NewFlagSet("credential-setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dryRun := fs.Bool("dry-run", globals.dryRun, "")
	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if *dryRun {
		globals.dryRun = true
	}

	out := textOut(globals, stdout)
	nc := globals.noColor

	// Pre-check: pass must be initialized.
	health := secret.CheckPass()
	if !health.Initialized {
		fmt.Fprintln(stderr, style.ResultError(nc, "pass store is not initialized."))
		fmt.Fprintln(stderr, style.Mutedf(nc, "Run `ws secret setup` first to initialize pass."))
		return 1
	}

	plan := Plan{Command: "git-credential-helper.setup"}

	wsBin, binErr := os.Executable()
	if binErr != nil {
		wsBin = "ws"
	}
	helperValue := fmt.Sprintf("!%s git-credential-helper", wsBin)

	// Connect or reconcile global credential helper.
	currentHelper := gitConfigGet("credential.helper")
	if !strings.Contains(currentHelper, "ws git-credential-helper") {
		plan.Actions = append(plan.Actions, Action{
			ID:          "set-credential-helper",
			Description: fmt.Sprintf("Set global credential.helper to '%s'", helperValue),
			Execute: func() error {
				return gitConfigSetGlobal("credential.helper", helperValue)
			},
		})
	} else {
		// ws helper is configured — check for stale path.
		_, detail, _ := helperPathStatus(currentHelper, nc)
		if detail != "" {
			plan.Actions = append(plan.Actions, Action{
				ID:          "update-credential-helper",
				Description: fmt.Sprintf("Update global credential.helper (%s) to '%s'", detail, helperValue),
				Execute: func() error {
					return gitConfigSetGlobal("credential.helper", helperValue)
				},
			})
		}
	}

	// Enable useHttpPath so git sends the repo path to the helper,
	// allowing per-repo credential resolution on shared hosts.
	if gitConfigGet("credential.useHttpPath") != "true" {
		plan.Actions = append(plan.Actions, Action{
			ID:          "set-use-http-path",
			Description: "Set credential.useHttpPath = true",
			Execute: func() error {
				return gitConfigSetGlobal("credential.useHttpPath", "true")
			},
		})
	}

	// Discover workspace remotes and offer to create missing pass entries.
	workspacePath, _, _, wsErr := requireWorkspaceInitialized(globals)
	if wsErr == nil {
		// Reconcile stale local credential.helper overrides in repos.
		localOverrides := discoverLocalHelperOverrides(workspacePath)
		for _, lo := range localOverrides {
			if !lo.IsWs {
				continue // non-ws local overrides are user-managed, skip
			}
			_, loDetail, _ := helperPathStatus(lo.Helper, false)
			if loDetail != "" {
				capturedPath := filepath.Join(workspacePath, filepath.FromSlash(lo.RepoPath))
				capturedDisplay := lo.RepoPath
				plan.Actions = append(plan.Actions, Action{
					ID:          "update-local-helper-" + lo.RepoPath,
					Description: fmt.Sprintf("Update local credential.helper in %s (%s)", capturedDisplay, loDetail),
					Execute: func() error {
						return gitConfigSetLocal(capturedPath, "credential.helper", helperValue)
					},
				})
			}
		}

		entries := discoverRemoteEntries(workspacePath)
		for _, e := range entries {
			if !secret.PassEntryExists(e.PassEntry) {
				capturedEntry := e.PassEntry
				plan.Actions = append(plan.Actions, Action{
					ID:          "create-pass-entry-" + capturedEntry,
					Description: fmt.Sprintf("Create pass entry %s", capturedEntry),
					Execute: func() error {
						return runPassInsertInteractive(capturedEntry)
					},
				})
			}
		}
	}

	if len(plan.Actions) == 0 {
		fmt.Fprintln(out, style.ResultSuccess(nc, "git credential helper already configured."))
		if globals.json {
			return writeJSON(stdout, stderr, "git-credential-helper.setup", map[string]any{
				"message": "already configured",
			})
		}
		return 0
	}

	planOut := stdout
	if globals.json {
		planOut = io.Discard
	}
	planResult := RunPlan(plan, stdin, planOut, globals)

	if globals.json {
		return writeJSON(stdout, stderr, "git-credential-helper.setup", map[string]any{
			"actions": planResult.Actions,
		})
	}

	if !planResult.HasFailures() {
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.ResultSuccess(nc, "git credential helper configured."))
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.Mutedf(nc, "  Convention: git/<host> or git/<host>/<owner>/<repo> in pass."))
		fmt.Fprintln(out, style.Mutedf(nc, "  Entry format: password/token on line 1, username: <user> on line 2."))
	}

	return planResult.ExitCode()
}

// ── ws git-credential-helper status (RO) ──

func runCredentialStatus(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, cmdHelp{
			Usage:       "ws git-credential-helper status",
			Description: "Show credential helper config, pass health, and workspace remote coverage.",
		})
	}

	out := textOut(globals, stdout)
	nc := globals.noColor

	health := secret.CheckPass()
	currentHelper := gitConfigGet("credential.helper")
	globalStatus, globalDetail, connected := helperPathStatus(currentHelper, nc)

	var remotes []remoteEntry
	var localOverrides []localHelperOverride

	workspacePath, _, _, wsErr := requireWorkspaceInitialized(globals)
	if wsErr == nil {
		entries := discoverRemoteEntries(workspacePath)
		for _, e := range entries {
			e.Exists = secret.PassEntryExists(e.PassEntry)
			remotes = append(remotes, e)
		}
		localOverrides = discoverLocalHelperOverrides(workspacePath)
	}

	if globals.json {
		return writeJSON(stdout, stderr, "git-credential-helper.status", map[string]any{
			"connected":       connected,
			"helper":          currentHelper,
			"pass":            health,
			"remotes":         remotes,
			"local_overrides": localOverrides,
		})
	}

	style.Header(out, "Pass Store", nc)
	printCredentialPassHealth(out, health, nc)

	if !health.Initialized {
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.ResultWarning(nc, "pass is not initialized %s run `ws secret setup`", style.Mutedf(nc, "—")))
	}

	fmt.Fprintln(out)
	style.Header(out, "Git Credential Helper", nc)

	// Global config.
	fmt.Fprintf(out, "  Global config    %s", globalStatus)
	if globalDetail != "" {
		fmt.Fprintf(out, "  %s", style.Mutedf(nc, "%s", globalDetail))
	}
	fmt.Fprintln(out)
	if currentHelper != "" {
		fmt.Fprintf(out, "                   %s\n", style.Infof(nc, "%s", currentHelper))
	}

	// Local overrides.
	if len(localOverrides) > 0 {
		fmt.Fprintf(out, "  Local overrides  %s\n", style.Mutedf(nc, "%d repo(s) have local credential.helper set", len(localOverrides)))
		for _, lo := range localOverrides {
			loStatus, loDetail, _ := helperPathStatus(lo.Helper, nc)
			fmt.Fprintf(out, "                     %s  %s", style.Infof(nc, "%s", lo.RepoPath), loStatus)
			if loDetail != "" {
				fmt.Fprintf(out, "  %s", style.Mutedf(nc, "%s", loDetail))
			}
			fmt.Fprintln(out)
			fmt.Fprintf(out, "                     %s\n", style.Mutedf(nc, "%s", lo.Helper))
		}
	}

	if len(remotes) > 0 {
		fmt.Fprintln(out)
		style.Header(out, "Workspace Remotes", nc)
		for _, r := range remotes {
			icon := boolIcon(r.Exists, nc)
			status := "exists"
			if !r.Exists {
				status = "missing"
			}
			fmt.Fprintf(out, "  %s  %s → %s (%s)\n", icon, r.Host, style.Infof(nc, "%s", r.PassEntry), status)
		}
	}

	if !connected {
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.ResultWarning(nc, "credential helper not connected %s run `ws git-credential-helper setup`", style.Mutedf(nc, "—")))
	}

	missingCount := 0
	for _, r := range remotes {
		if !r.Exists {
			missingCount++
		}
	}
	if missingCount > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s %d remote(s) missing pass entries %s run `ws git-credential-helper setup`\n",
			style.IconWarning(nc), missingCount, style.Mutedf(nc, "—"))
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, style.Mutedf(nc, "  Convention: git/<host> or git/<host>/<owner>/<repo> in pass."))
	fmt.Fprintln(out, style.Mutedf(nc, "  Entry format: password/token on line 1, username: <user> on line 2."))

	return 0
}

// printCredentialPassHealth renders pass store health in credential status output.
func printCredentialPassHealth(w io.Writer, h secret.PassHealth, nc bool) {
	check := func(ok bool) string {
		if ok {
			return style.IconCheck(nc)
		}
		return style.IconCross(nc)
	}
	fmt.Fprintf(w, "  %s  gpg available\n", check(h.GPGAvailable))
	fmt.Fprintf(w, "  %s  pass installed\n", check(h.Installed))
	fmt.Fprintf(w, "  %s  store initialized", check(h.Initialized))
	if h.Initialized {
		fmt.Fprintf(w, "  %s", style.Mutedf(nc, "(%s, %d entries)", h.StorePath, h.EntryCount))
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s  git-backed\n", check(h.GitBacked))
	if h.GitBacked {
		if h.GitRemote {
			fmt.Fprintf(w, "  %s  git-remote\n", check(true))
		} else {
			fmt.Fprintf(w, "  %s  git-remote %s\n", style.IconWarning(nc), style.Mutedf(nc, "(local only, no remote)"))
		}
	}
}

// ── ws git-credential-helper disconnect (RW) ──

func runCredentialDisconnect(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, cmdHelp{
			Usage:       "ws git-credential-helper disconnect",
			Description: "Remove ws credential helper from global git config.",
		})
	}

	fs := flag.NewFlagSet("credential-disconnect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dryRun := fs.Bool("dry-run", globals.dryRun, "")
	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if *dryRun {
		globals.dryRun = true
	}

	out := textOut(globals, stdout)
	nc := globals.noColor

	currentHelper := gitConfigGet("credential.helper")
	globalConnected := strings.Contains(currentHelper, "ws git-credential-helper")

	// Also discover local ws overrides in workspace repos.
	var localOverrides []localHelperOverride
	workspacePath, _, _, wsErr := requireWorkspaceInitialized(globals)
	if wsErr == nil {
		localOverrides = discoverLocalHelperOverrides(workspacePath)
	}

	// Filter to only ws-managed local overrides.
	var wsLocalOverrides []localHelperOverride
	for _, lo := range localOverrides {
		if lo.IsWs {
			wsLocalOverrides = append(wsLocalOverrides, lo)
		}
	}

	if !globalConnected && len(wsLocalOverrides) == 0 {
		fmt.Fprintln(stderr, "credential helper is not connected")
		return 1
	}

	plan := Plan{Command: "git-credential-helper.disconnect"}

	if globalConnected {
		plan.Actions = append(plan.Actions, Action{
			ID:          "unset-credential-helper",
			Description: "Remove credential.helper from global git config",
			Execute: func() error {
				return gitConfigUnsetGlobal("credential.helper")
			},
		})
		if gitConfigGet("credential.useHttpPath") == "true" {
			plan.Actions = append(plan.Actions, Action{
				ID:          "unset-use-http-path",
				Description: "Remove credential.useHttpPath from global git config",
				Execute: func() error {
					return gitConfigUnsetGlobal("credential.useHttpPath")
				},
			})
		}
	}

	for _, lo := range wsLocalOverrides {
		capturedPath := filepath.Join(workspacePath, filepath.FromSlash(lo.RepoPath))
		capturedDisplay := lo.RepoPath
		plan.Actions = append(plan.Actions, Action{
			ID:          "unset-local-helper-" + lo.RepoPath,
			Description: fmt.Sprintf("Remove credential.helper from %s (local config)", capturedDisplay),
			Execute: func() error {
				return gitConfigUnsetLocal(capturedPath, "credential.helper")
			},
		})
	}

	planOut := stdout
	if globals.json {
		planOut = io.Discard
	}
	planResult := RunPlan(plan, stdin, planOut, globals)

	if globals.json {
		return writeJSON(stdout, stderr, "git-credential-helper.disconnect", map[string]any{
			"disconnected": planResult.ExecutedCount() > 0 && !planResult.HasFailures(),
			"actions":      planResult.Actions,
		})
	}

	if !planResult.HasFailures() && planResult.ExecutedCount() > 0 {
		fmt.Fprintln(out, style.ResultSuccess(nc, "Git credential helper disconnected."))
	}

	return planResult.ExitCode()
}

// ── Shared helpers ──

// remoteEntry represents a discovered git remote and its corresponding pass entry.
type remoteEntry struct {
	Host      string `json:"host"`
	Path      string `json:"path,omitempty"`
	PassEntry string `json:"pass_entry"`
	Exists    bool   `json:"exists"`
}

// discoverRemoteEntries scans workspace repos and returns pass entries for
// each unique remote. When multiple repos share the same host, per-repo
// entries (git/<host>/<owner>/<repo>) are used. When only one repo uses a
// host, a host-only entry (git/<host>) is used.
func discoverRemoteEntries(workspacePath string) []remoteEntry {
	repos, err := repo.Discover(workspacePath, nil, nil)
	if err != nil {
		return nil
	}

	type hostURL struct {
		host string
		path string // owner/repo, cleaned
	}

	var all []hostURL
	seen := make(map[string]bool) // dedup on host+path

	for _, r := range repos {
		absPath := filepath.Join(workspacePath, filepath.FromSlash(r.Path))
		remoteURLs := gitRemoteURLs(absPath)
		for _, u := range remoteURLs {
			host := extractHost(u)
			rpath := extractRepoPath(u)
			if host == "" {
				continue
			}
			key := host + "/" + rpath
			if seen[key] {
				continue
			}
			seen[key] = true
			all = append(all, hostURL{host: host, path: rpath})
		}
	}

	// Count repos per host to decide entry granularity.
	hostCount := make(map[string]int)
	for _, hu := range all {
		hostCount[hu.host]++
	}

	dedupEntry := make(map[string]bool)
	var entries []remoteEntry
	for _, hu := range all {
		var entry remoteEntry
		if hostCount[hu.host] > 1 && hu.path != "" {
			entry = remoteEntry{
				Host:      hu.host,
				Path:      hu.path,
				PassEntry: "git/" + hu.host + "/" + hu.path,
			}
		} else {
			entry = remoteEntry{
				Host:      hu.host,
				PassEntry: "git/" + hu.host,
			}
		}
		if dedupEntry[entry.PassEntry] {
			continue
		}
		dedupEntry[entry.PassEntry] = true
		entries = append(entries, entry)
	}

	return entries
}

// extractRepoPath extracts the owner/repo portion from a git remote URL.
// Returns empty string if the path cannot be determined.
func extractRepoPath(rawURL string) string {
	var path string

	// SSH shorthand: git@github.com:owner/repo.git
	if strings.Contains(rawURL, "@") && strings.Contains(rawURL, ":") && !strings.Contains(rawURL, "://") {
		at := strings.Index(rawURL, "@")
		colon := strings.Index(rawURL[at:], ":")
		if colon > 0 {
			path = rawURL[at+colon+1:]
		}
	} else {
		u, err := url.Parse(rawURL)
		if err != nil {
			return ""
		}
		path = strings.TrimPrefix(u.Path, "/")
	}

	path = strings.TrimSuffix(path, ".git")
	path = strings.TrimSuffix(path, "/")
	return path
}

// gitRemoteURLs returns all remote fetch URLs for a repo.
func gitRemoteURLs(repoPath string) []string {
	cmd := exec.Command("git", "-C", repoPath, "remote", "-v")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var urls []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			u := fields[1]
			if !seen[u] {
				seen[u] = true
				urls = append(urls, u)
			}
		}
	}
	return urls
}

// extractHost extracts the hostname from a git remote URL.
// Supports https://host/path, git@host:path, ssh://host/path.
func extractHost(rawURL string) string {
	// SSH shorthand: git@github.com:user/repo.git
	if strings.Contains(rawURL, "@") && strings.Contains(rawURL, ":") && !strings.Contains(rawURL, "://") {
		at := strings.Index(rawURL, "@")
		colon := strings.Index(rawURL[at:], ":")
		if colon > 0 {
			return rawURL[at+1 : at+colon]
		}
	}

	// Standard URL parsing.
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// gitConfigGet reads a git config value from the global scope.
func gitConfigGet(key string) string {
	cmd := exec.Command("git", "config", "--global", "--get", key)
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

// gitConfigGetLocal reads a git config value from a repo's local scope.
func gitConfigGetLocal(repoPath, key string) string {
	cmd := exec.Command("git", "-C", repoPath, "config", "--local", "--get", key)
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

// gitConfigSetGlobal sets a git config value globally.
func gitConfigSetGlobal(key, value string) error {
	cmd := exec.Command("git", "config", "--global", key, value)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git config --global %s: %s", key, strings.TrimSpace(string(out)))
	}
	return nil
}

// gitConfigUnsetGlobal removes a git config value globally.
func gitConfigUnsetGlobal(key string) error {
	cmd := exec.Command("git", "config", "--global", "--unset", key)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git config --global --unset %s: %s", key, strings.TrimSpace(string(out)))
	}
	return nil
}

// gitConfigUnsetLocal removes a git config value from a repo's local scope.
func gitConfigUnsetLocal(repoPath, key string) error {
	cmd := exec.Command("git", "-C", repoPath, "config", "--local", "--unset", key)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git -C %s config --local --unset %s: %s", repoPath, key, strings.TrimSpace(string(out)))
	}
	return nil
}

// gitConfigSetLocal sets a git config value in a repo's local scope.
func gitConfigSetLocal(repoPath, key, value string) error {
	cmd := exec.Command("git", "-C", repoPath, "config", "--local", key, value)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git -C %s config --local %s: %s", repoPath, key, strings.TrimSpace(string(out)))
	}
	return nil
}

// localHelperOverride represents a repo with a local credential.helper override.
type localHelperOverride struct {
	RepoPath string `json:"repo_path"`
	Helper   string `json:"helper"`
	IsWs     bool   `json:"is_ws"`
}

// discoverLocalHelperOverrides scans workspace repos for local credential.helper
// overrides that differ from the global value.
func discoverLocalHelperOverrides(workspacePath string) []localHelperOverride {
	repos, err := repo.Discover(workspacePath, nil, nil)
	if err != nil {
		return nil
	}

	var overrides []localHelperOverride
	for _, r := range repos {
		absPath := filepath.Join(workspacePath, filepath.FromSlash(r.Path))
		local := gitConfigGetLocal(absPath, "credential.helper")
		if local == "" {
			continue
		}
		overrides = append(overrides, localHelperOverride{
			RepoPath: r.Path,
			Helper:   local,
			IsWs:     strings.Contains(local, "ws git-credential-helper"),
		})
	}
	return overrides
}

// helperPathStatus checks the configured helper value and returns a status
// string and whether the helper is functional.
// Returns: (statusLabel, detail, connected)
func helperPathStatus(helperValue string, nc bool) (string, string, bool) {
	if helperValue == "" {
		return style.Badge("disconnected", nc), "", false
	}
	if !strings.Contains(helperValue, "ws git-credential-helper") {
		return style.Badge("disconnected", nc), style.Mutedf(nc, "(not a ws helper)"), false
	}

	// Extract binary path from "!<path> git-credential-helper"
	binPath := helperValue
	if strings.HasPrefix(binPath, "!") {
		binPath = binPath[1:]
	}
	if idx := strings.Index(binPath, " git-credential-helper"); idx >= 0 {
		binPath = binPath[:idx]
	}

	if _, err := os.Stat(binPath); err != nil {
		return style.Badge("stale", nc), fmt.Sprintf("binary not found: %s", binPath), false
	}

	// Check if the configured path matches the currently running binary.
	currentBin, err := os.Executable()
	if err == nil {
		// Resolve both to handle symlinks.
		resolvedConfigured, e1 := filepath.EvalSymlinks(binPath)
		resolvedCurrent, e2 := filepath.EvalSymlinks(currentBin)
		if e1 == nil && e2 == nil && resolvedConfigured != resolvedCurrent {
			return style.Badge("connected", nc),
				fmt.Sprintf("path mismatch (configured: %s, running: %s)", binPath, currentBin), true
		}
	}

	return style.Badge("connected", nc), "", true
}

// runPassInsertInteractive runs `pass insert <entry>` with the terminal
// attached so the user can type the password interactively.
func runPassInsertInteractive(entry string) error {
	cmd := exec.Command("pass", "insert", entry)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func boolIcon(ok bool, nc bool) string {
	if ok {
		return style.IconCheck(nc)
	}
	return style.IconCross(nc)
}
