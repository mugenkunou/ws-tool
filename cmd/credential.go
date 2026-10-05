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
		return printCmdHelp(stdout, credentialHelp)
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
		if isTerminalWriter(stderr) {
			return printUsageError(stderr, credentialHelp)
		}
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
	_ = wsBin // the wrapper script resolves ws from PATH at runtime

	wrapperPath, wrapErr := credentialWrapperPath()
	if wrapErr != nil {
		fmt.Fprintln(stderr, style.ResultError(nc, "cannot resolve wrapper path: %v", wrapErr))
		return 1
	}
	// The wrapper already execs `ws git-credential-helper`; git appends the
	// get/store/erase verb, so the value must not repeat the subcommand.
	helperValue := "!" + wrapperPath

	// Install the wrapper script if missing or stale.
	if needsWrapperInstall(wrapperPath) {
		plan.Actions = append(plan.Actions, Action{
			ID:          "install-wrapper",
			Description: fmt.Sprintf("Install credential helper wrapper at %s", wrapperPath),
			Execute: func() error {
				return installCredentialWrapper(wrapperPath)
			},
		})
	}

	// Connect or reconcile global credential helper.
	currentHelper := gitConfigGet("credential.helper")
	if !isWsHelper(currentHelper) {
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
				capturedPath := lo.RepoPath
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

		// Setup turns useHttpPath on (above), so resolve as git will after it.
		// Remotes already covered by a host or repo token need nothing; for
		// the rest, offer one host-wide entry per host.
		var hosts []string
		uncovered := make(map[string]int)
		for _, e := range discoverRemoteEntries(workspacePath) {
			e = resolveRemoteEntry(e, true)
			if !e.HelperUsed || e.Exists {
				continue
			}
			if uncovered[e.Host] == 0 {
				hosts = append(hosts, e.Host)
			}
			uncovered[e.Host]++
		}
		for _, host := range hosts {
			capturedEntry := "git/" + host
			plan.Actions = append(plan.Actions, Action{
				ID:          "create-pass-entry-" + capturedEntry,
				Description: fmt.Sprintf("Create pass entry %s (host token for %d remote(s))", capturedEntry, uncovered[host]),
				Execute: func() error {
					return runPassInsertInteractive(capturedEntry)
				},
			})
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
		useHTTPPath := make(map[string]bool) // per host
		for _, e := range discoverRemoteEntries(workspacePath) {
			on, ok := useHTTPPath[e.Host]
			if !ok {
				on = useHTTPPathFor(e.Host)
				useHTTPPath[e.Host] = on
			}
			remotes = append(remotes, resolveRemoteEntry(e, on))
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
		printRemoteEntries(out, remotes, nc)
	}

	if !connected {
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.ResultWarning(nc, "credential helper not connected %s run `ws git-credential-helper setup`", style.Mutedf(nc, "—")))
	}

	missingCount, ignoredCount := 0, 0
	for _, r := range remotes {
		if r.HelperUsed && !r.Exists {
			missingCount++
		}
		if r.IgnoredEntry != "" {
			ignoredCount++
		}
	}
	if missingCount > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s %d remote(s) have no credential %s run `ws git-credential-helper setup`\n",
			style.IconWarning(nc), missingCount, style.Mutedf(nc, "—"))
	}
	if ignoredCount > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s %d repo token(s) ignored because credential.useHttpPath is off %s run `ws git-credential-helper setup`\n",
			style.IconWarning(nc), ignoredCount, style.Mutedf(nc, "—"))
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, style.Mutedf(nc, "  Convention: git/<host> or git/<host>/<owner>/<repo> in pass."))
	fmt.Fprintln(out, style.Mutedf(nc, "  Entry format: password/token on line 1, username: <user> on line 2."))

	return 0
}

// printRemoteEntries renders one line per remote: the pass entry the helper
// resolves it to and whether that is a host-wide or repo-scoped token.
// Only remotes with no credential at all are marked as failures.
func printRemoteEntries(w io.Writer, remotes []remoteEntry, nc bool) {
	width := 0
	for _, r := range remotes {
		if n := len(r.Label()); n > width {
			width = n
		}
	}
	for _, r := range remotes {
		label := fmt.Sprintf("%-*s", width, r.Label())
		switch {
		case !r.HelperUsed:
			fmt.Fprintf(w, "  %s  %s  %s\n", style.IconDot(nc), label,
				style.Mutedf(nc, "%s %s credential helper not used", r.Transport, "—"))
		case !r.Exists:
			fmt.Fprintf(w, "  %s  %s  %s\n", style.IconCross(nc), label,
				style.Errorf(nc, "no credential %s", style.Mutedf(nc, "(needs git/%s or git/%s)", r.Host, r.Label())))
		default:
			fmt.Fprintf(w, "  %s  %s  %s  %s\n", style.IconCheck(nc), label,
				style.Infof(nc, "%s", r.PassEntry), style.Mutedf(nc, "(%s token)", r.Scope))
		}
		if r.IgnoredEntry != "" {
			fmt.Fprintf(w, "     %*s  %s\n", width, "",
				style.Warningf(nc, "%s ignored %s credential.useHttpPath is off", r.IgnoredEntry, "—"))
		}
	}
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
	globalConnected := isWsHelper(currentHelper)

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
		capturedPath := lo.RepoPath
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

// remoteEntry is one unique workspace remote (host + owner/repo) and the pass
// entry the credential helper resolves it to.
type remoteEntry struct {
	Host      string `json:"host"`
	Path      string `json:"path,omitempty"`
	Transport string `json:"transport"` // "https", "http", "ssh", ...
	// HelperUsed is false for non-HTTP remotes: git never asks a credential
	// helper for SSH auth, so these are informational only.
	HelperUsed bool `json:"helper_used"`
	// PassEntry is the entry the helper actually uses ("" when none resolves).
	PassEntry string `json:"pass_entry,omitempty"`
	// Scope is "repo" (git/<host>/<owner>/<repo>), "host" (git/<host>), or ""
	// when nothing resolves.
	Scope  string `json:"scope,omitempty"`
	Exists bool   `json:"exists"`
	// IgnoredEntry is a repo-scoped entry that exists but that git never
	// asks for, because credential.useHttpPath is off for this host.
	IgnoredEntry string `json:"ignored_entry,omitempty"`
}

// Label is the remote as shown to the user: host/owner/repo.
func (e remoteEntry) Label() string {
	if e.Path == "" {
		return e.Host
	}
	return e.Host + "/" + e.Path
}

// discoverRemoteEntries scans workspace repos and returns one unresolved
// entry per unique host + owner/repo. Call resolveRemoteEntry to fill in the
// pass entry.
func discoverRemoteEntries(workspacePath string) []remoteEntry {
	repos, err := repo.Discover(workspacePath, nil, nil)
	if err != nil {
		return nil
	}

	seen := make(map[string]bool) // dedup on host+path
	var entries []remoteEntry
	for _, r := range repos {
		for _, u := range gitRemoteURLs(r.Path) { // repo.Discover returns absolute paths
			e, ok := newRemoteEntry(u)
			if !ok || seen[e.Label()] {
				continue
			}
			seen[e.Label()] = true
			entries = append(entries, e)
		}
	}
	return entries
}

// newRemoteEntry parses a remote URL. ok is false when no host can be found
// (e.g. a local-path remote).
func newRemoteEntry(rawURL string) (remoteEntry, bool) {
	host := extractHost(rawURL)
	if host == "" {
		return remoteEntry{}, false
	}
	transport := remoteTransport(rawURL)
	return remoteEntry{
		Host:       host,
		Path:       extractRepoPath(rawURL),
		Transport:  transport,
		HelperUsed: transport == "https" || transport == "http",
	}, true
}

// remoteTransport returns the URL scheme of a remote ("ssh" for scp-style
// git@host:owner/repo and for ssh variants like git+ssh).
func remoteTransport(rawURL string) string {
	scheme, _, ok := strings.Cut(rawURL, "://")
	if !ok {
		return "ssh"
	}
	scheme = strings.ToLower(scheme)
	if strings.Contains(scheme, "ssh") {
		return "ssh"
	}
	return scheme
}

// resolveRemoteEntry fills in the pass entry the helper would use for e,
// mirroring what git sends: the repo path is only part of the request when
// useHTTPPath is true. Non-HTTP remotes are returned unchanged.
func resolveRemoteEntry(e remoteEntry, useHTTPPath bool) remoteEntry {
	if !e.HelperUsed {
		return e
	}
	sentPath := ""
	if useHTTPPath {
		sentPath = e.Path
	}
	e.PassEntry = secret.ResolveCredentialEntry(e.Host, sentPath)
	e.Exists = e.PassEntry != ""
	switch {
	case !e.Exists:
		e.Scope = ""
	case e.PassEntry == "git/"+e.Host:
		e.Scope = "host"
	default:
		e.Scope = "repo"
	}
	if !useHTTPPath && e.Path != "" {
		repoEntry := "git/" + e.Host + "/" + e.Path
		if secret.PassEntryExists(repoEntry) {
			e.IgnoredEntry = repoEntry
		}
	}
	return e
}

// useHTTPPathFor reports whether git sends the repo path to credential
// helpers for host, honouring URL-scoped config (credential.<url>.useHttpPath).
func useHTTPPathFor(host string) bool {
	cmd := exec.Command("git", "config", "--global", "--type=bool", "--get-urlmatch",
		"credential.useHttpPath", "https://"+host+"/")
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out)) == "true"
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
		absPath := r.Path // repo.Discover returns absolute paths
		local := gitConfigGetLocal(absPath, "credential.helper")
		if local == "" {
			continue
		}
		overrides = append(overrides, localHelperOverride{
			RepoPath: r.Path,
			Helper:   local,
			IsWs:     isWsHelper(local),
		})
	}
	return overrides
}

// isWsHelper reports whether a credential.helper value points at ws, either
// directly ("ws git-credential-helper") or via the installed wrapper script.
func isWsHelper(v string) bool {
	return strings.Contains(v, "ws git-credential-helper") || strings.Contains(v, "ws-credential-helper")
}

// helperPathStatus checks the configured helper value and returns a status
// string and whether the helper is functional.
// Returns: (statusLabel, detail, connected)
func helperPathStatus(helperValue string, nc bool) (string, string, bool) {
	if helperValue == "" {
		return style.Badge("disconnected", nc), "", false
	}
	if !isWsHelper(helperValue) {
		return style.Badge("disconnected", nc), style.Mutedf(nc, "(not a ws helper)"), false
	}

	// Extract binary/script path from "!<path>" or "!<path> git-credential-helper"
	binPath := strings.TrimPrefix(helperValue, "!")
	if idx := strings.Index(binPath, " git-credential-helper"); idx >= 0 {
		binPath = binPath[:idx]
	}

	// Older setups wrote "!<wrapper> git-credential-helper"; the wrapper then
	// passed the subcommand twice and every lookup failed.
	if strings.HasSuffix(binPath, "ws-credential-helper") && binPath != helperValue[1:] {
		return style.Badge("stale", nc), "wrapper called with a duplicate subcommand", false
	}

	if _, err := os.Stat(binPath); err != nil {
		return style.Badge("stale", nc), fmt.Sprintf("binary not found: %s", binPath), false
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

// credentialWrapperPath returns the stable path for the ws credential helper
// wrapper script (~/.local/bin/ws-credential-helper). This path is written
// into gitconfig so that moving the ws binary only requires updating PATH,
// not reconfiguring git.
func credentialWrapperPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin", "ws-credential-helper"), nil
}

// needsWrapperInstall returns true when the wrapper script is absent or does
// not contain the expected shebang (i.e., it was never installed or is stale).
func needsWrapperInstall(wrapperPath string) bool {
	content, err := os.ReadFile(wrapperPath)
	if err != nil {
		return true
	}
	return !strings.Contains(string(content), "ws git-credential-helper")
}

// installCredentialWrapper writes the stable wrapper script to wrapperPath.
// The script execs `ws git-credential-helper "$@"`, resolving ws from PATH.
func installCredentialWrapper(wrapperPath string) error {
	if err := os.MkdirAll(filepath.Dir(wrapperPath), 0o755); err != nil {
		return err
	}
	script := "#!/bin/sh\nexec ws git-credential-helper \"$@\"\n"
	return os.WriteFile(wrapperPath, []byte(script), 0o755)
}
