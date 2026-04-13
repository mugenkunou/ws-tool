package cmd

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/secret"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// stringSliceFlag implements flag.Value for repeatable string flags.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

var secretHelp = cmdHelp{
	Usage: "ws secret <scan|fix|setup>",
	Subcommands: []string{
		"  scan     Scan for secrets + pass health status (--pass to audit pass store)",
		"  fix      Interactively resolve secret violations",
		"  setup    Set up Unix Password Store (pass)",
		"",
		"  For credential helper commands: ws git-credential-helper",
	},
}

// secretScanResult is the JSON envelope for ws secret scan.
type secretScanResult struct {
	Violations      []secret.Violation      `json:"violations"`
	SkippedDirs     []string                `json:"skipped_dirs,omitempty"`
	Pass            secret.PassHealth       `json:"pass"`
	PassAudit       *secret.PassAuditResult `json:"pass_audit,omitempty"`
	GitCredHelper   bool                    `json:"git_credential_helper"`
	CredentialHelper string                 `json:"credential_helper_value,omitempty"`
}

func runSecret(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, secretHelp)
	}

	workspacePath, configPath, manifestPath, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if len(args) == 0 {
		return printUsageError(stderr, secretHelp)
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "scan":
		return runSecretScan(subArgs, globals, workspacePath, configPath, manifestPath, stdout, stderr)
	case "fix":
		return runSecretFix(subArgs, globals, workspacePath, configPath, manifestPath, stdin, stdout, stderr)
	case "setup":
		return runSecretSetup(subArgs, globals, stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown secret subcommand: %s\n", sub)
		return 1
	}
}

// ── ws secret scan (RO) ──

func runSecretScan(args []string, globals globalFlags, workspacePath, configPath, manifestPath string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("secret-scan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	passAudit := fs.Bool("pass", false, "")
	var skipDirFlags stringSliceFlag
	fs.Var(&skipDirFlags, "skip-dir", "")
	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	skipDirs := mergeSkipDirs(cfg.Secret.SkipDirs, skipDirFlags)

	engine, err := ignore.LoadEngine(filepath.Join(workspacePath, ".megaignore"))
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	m, err := manifest.Load(manifestPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	allow := buildAllowlistMap(m)

	violations, err := secret.Scan(secret.ScanOptions{
		WorkspacePath: workspacePath,
		Engine:        engine,
		Allowlist:     allow,
		SkipDirs:      skipDirs,
	})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	// Prune stale allowlist entries: re-scan without allowlist to find
	// which anchors still match a pattern, then remove the rest.
	if len(m.Secret.Allowlist) > 0 {
		allViolations, scanErr := secret.Scan(secret.ScanOptions{
			WorkspacePath: workspacePath,
			Engine:        engine,
			Allowlist:     nil,
			SkipDirs:      skipDirs,
		})
		if scanErr == nil {
			kept, _ := secret.PruneAllowlist(m.Secret.Allowlist, allViolations)
			if len(kept) != len(m.Secret.Allowlist) {
				m.Secret.Allowlist = kept
				if kept == nil {
					m.Secret.Allowlist = []string{}
				}
				manifest.Save(manifestPath, m)
			}
		}
	}

	passHealth := secret.CheckPass()

	var auditResult *secret.PassAuditResult
	if *passAudit {
		r := secret.AuditPassStore(passHealth)
		auditResult = &r
	}

	credHelper := gitConfigGet("credential.helper")
	credConnected := strings.Contains(credHelper, "ws git-credential-helper")

	if globals.json {
		return writeJSON(stdout, stderr, "secret.scan", secretScanResult{
			Violations:       violations,
			SkippedDirs:      skipDirs,
			Pass:             passHealth,
			PassAudit:        auditResult,
			GitCredHelper:    credConnected,
			CredentialHelper: credHelper,
		})
	}

	out := textOut(globals, stdout)
	nc := globals.noColor

	if globals.verbose && len(skipDirs) > 0 {
		fmt.Fprintf(out, "%s secret: skipping directories: %s\n", style.Mutedf(nc, "[verbose]"), strings.Join(skipDirs, ", "))
	}

	if len(violations) == 0 {
		fmt.Fprintln(out, style.ResultSuccess(nc, "Secret scan: %s", style.Badge("ok", nc)))
	} else {
		printSecretViolations(out, violations, nc, false)
	}
	fmt.Fprintln(out)

	style.Header(out, "Pass Status", nc)
	printPassHealth(out, passHealth, nc)

	if !passHealth.Initialized {
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.ResultWarning(nc, "pass is not initialized %s run `ws secret setup` to secure your secrets", style.Mutedf(nc, "—")))
	}

	fmt.Fprintln(out)
	style.Header(out, "Git Credential Helper", nc)
	if credConnected {
		fmt.Fprintf(out, "  %s  connected %s\n", style.IconCheck(nc), style.Mutedf(nc, "(%s)", credHelper))
	} else {
		fmt.Fprintf(out, "  %s  not connected %s run `ws secret setup`\n", style.IconCross(nc), style.Mutedf(nc, "—"))
	}

	if auditResult != nil && len(auditResult.Findings) > 0 {
		fmt.Fprintln(out)
		style.Header(out, "Pass Audit", nc)
		for _, f := range auditResult.Findings {
			if f.Entry != "" {
				fmt.Fprintf(out, "  %s  %s  %s\n", style.IconInfo(nc), style.Infof(nc, "%s", f.Entry), f.Message)
			} else {
				fmt.Fprintf(out, "  %s  %s\n", style.IconInfo(nc), f.Message)
			}
		}
	}

	if len(violations) > 0 {
		return 2
	}
	return 0
}

func printPassHealth(w io.Writer, h secret.PassHealth, nc bool) {
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

// ── ws secret fix (RW) ──

var secretFixHelp = cmdHelp{
	Usage: "ws secret fix [--secret-mode allowlist|exclude|pass]",
	Flags: []string{
		"      --secret-mode string   Batch fix mode (interactive if omitted)",
	},
}

func runSecretFix(args []string, globals globalFlags, workspacePath, configPath, manifestPath string, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, secretFixHelp)
	}

	fs := flag.NewFlagSet("secret-fix", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	secretMode := fs.String("secret-mode", "", "")
	dryRun := fs.Bool("dry-run", false, "")
	var skipDirFlags stringSliceFlag
	fs.Var(&skipDirFlags, "skip-dir", "")
	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if *dryRun {
		globals.dryRun = true
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	skipDirs := mergeSkipDirs(cfg.Secret.SkipDirs, skipDirFlags)

	engine, err := ignore.LoadEngine(filepath.Join(workspacePath, ".megaignore"))
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	m, err := manifest.Load(manifestPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	allow := buildAllowlistMap(m)

	violations, err := secret.Scan(secret.ScanOptions{
		WorkspacePath: workspacePath,
		Engine:        engine,
		Allowlist:     allow,
		SkipDirs:      skipDirs,
	})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if len(violations) == 0 {
		if globals.json {
			return writeJSON(stdout, stderr, "secret.fix", secret.FixResult{
				Mode: "none", Messages: []string{"No violations found."},
			})
		}
		fmt.Fprintln(textOut(globals, stdout), style.ResultSuccess(globals.noColor, "No secret violations found."))
		return 0
	}

	megaignorePath := filepath.Join(workspacePath, ".megaignore")
	passHealth := secret.CheckPass()

	// Batch mode — use Plan pattern for per-action consent.
	if *secretMode != "" {
		return runSecretFixBatch(violations, *secretMode, workspacePath, manifestPath, megaignorePath, passHealth, globals, stdin, stdout, stderr)
	}

	// Interactive mode — per-violation menu.
	if globals.dryRun {
		out := textOut(globals, stdout)
		nc := globals.noColor
		fmt.Fprintln(out, style.Mutedf(nc, "[dry-run] %d secret violations found. Run without --dry-run to fix interactively.", len(violations)))
		for _, v := range violations {
			fmt.Fprintf(out, "  %s  %s:%d\n", style.Mutedf(nc, "[dry-run]"), v.Path, v.Line)
		}
		if globals.json {
			return writeJSON(stdout, stderr, "secret.fix", secret.FixResult{
				Mode: "interactive", Processed: len(violations), DryRun: true,
			})
		}
		return 0
	}

	return runSecretFixInteractive(violations, workspacePath, configPath, manifestPath, megaignorePath, passHealth, globals, stdin, stdout, stderr)
}

func runSecretFixBatch(violations []secret.Violation, mode string, workspacePath, manifestPath, megaignorePath string, passHealth secret.PassHealth, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if mode != "allowlist" && mode != "exclude" && mode != "pass" {
		fmt.Fprintf(stderr, "unknown secret-mode: %s (valid: allowlist, exclude, pass)\n", mode)
		return 1
	}

	if mode == "pass" && !passHealth.Initialized {
		fmt.Fprintln(stderr, "pass store is not initialized. Run `ws secret setup` first.")
		return 1
	}

	plan := Plan{Command: "secret.fix"}
	excludeSeen := make(map[string]bool) // dedup file-level excludes

	for _, v := range violations {
		v := v
		anchor := fmt.Sprintf("%s:%d", v.Path, v.Line)

		switch mode {
		case "allowlist":
			plan.Actions = append(plan.Actions, Action{
				ID:          "allowlist-" + anchor,
				Description: fmt.Sprintf("Allowlist %s", anchor),
				Execute: func() error {
					return addToAllowlist(manifestPath, anchor)
				},
			})
		case "exclude":
			if excludeSeen[v.Path] {
				continue
			}
			excludeSeen[v.Path] = true
			plan.Actions = append(plan.Actions, Action{
				ID:          "exclude-" + v.Path,
				Description: fmt.Sprintf("Add -:%s to .megaignore", v.Path),
				Execute: func() error {
					_, err := ignore.AddRules(megaignorePath, []string{"-:" + v.Path})
					return err
				},
			})
		case "pass":
			entryName := secret.SuggestPassEntry(v.Path, v.Snippet)
			value := secret.ExtractSecretValue(v.Snippet)
			plan.Actions = append(plan.Actions, Action{
				ID:          "pass-" + anchor,
				Description: fmt.Sprintf("Store in pass as %s", entryName),
				Execute: func() error {
					if value == "" {
						return fmt.Errorf("could not extract secret value from %s", anchor)
					}
					return secret.InsertEntry(entryName, value)
				},
			})
		}
	}

	planOut := stdout
	if globals.json {
		planOut = io.Discard
	}
	planResult := RunPlan(plan, stdin, planOut, globals)

	if globals.json {
		return writeJSON(stdout, stderr, "secret.fix", map[string]any{
			"mode":    mode,
			"actions": planResult.Actions,
		})
	}

	return planResult.ExitCode()
}

func runSecretFixInteractive(violations []secret.Violation, workspacePath, configPath, manifestPath, megaignorePath string, passHealth secret.PassHealth, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	out := textOut(globals, stdout)
	nc := globals.noColor

	var result secret.FixResult
	result.Mode = "interactive"
	result.Processed = len(violations)

	excludedFiles := make(map[string]bool)
	skippedDirs := make(map[string]bool)
	passAvailable := passHealth.Initialized

	for _, v := range violations {
		// Skip violations in directories already skipped.
		if isInSkippedDir(v.Path, skippedDirs) {
			result.DirSkipped++
			continue
		}

		// Skip violations in files already excluded.
		if excludedFiles[v.Path] {
			result.Excluded++
			result.Added = append(result.Added, "excluded:"+v.Path+" (file already excluded)")
			continue
		}

		anchor := fmt.Sprintf("%s:%d", v.Path, v.Line)
		absPath := filepath.Join(workspacePath, v.Path)

		// Show violation header.
		fmt.Fprintf(out, "\n%s  %s:%d: %s\n",
			severityLabel(v.Severity, nc),
			style.Infof(nc, "%s", v.Path), v.Line,
			style.Mutedf(nc, "%s", strings.TrimSpace(v.Snippet)))

		for {
			// Build prompt based on available options.
			var choiceLabels, validKeys string
			if passAvailable {
				choiceLabels = "[v]iew [p]ass [a]dd .megaignore [l]allowlist [d]ir-skip [s]kip [q]uit"
				validKeys = "vpaldsq"
			} else {
				choiceLabels = "[v]iew [a]dd .megaignore [l]allowlist [d]ir-skip [s]kip [q]uit"
				validKeys = "valdsq"
			}
			choice := promptChoice(stdin, stdout, globals, "  Action?", choiceLabels, validKeys, "s")

			switch choice {
			case "v":
				ctx, err := secret.GetFileContext(absPath, v.Line, 3)
				if err != nil {
					fmt.Fprintf(out, "  %s could not read context: %s\n", style.IconCross(nc), err)
				} else {
					fmt.Fprintln(out)
					for _, cl := range ctx {
						marker := "   "
						if cl.IsMatch {
							marker = " " + style.Warningf(nc, "%s", "→") + " "
						}
						fmt.Fprintf(out, "%s%4d  %s\n", marker, cl.Number, cl.Content)
					}
					fmt.Fprintln(out)
				}
				continue // re-prompt

			case "p":
				suggested := secret.SuggestPassEntry(v.Path, v.Snippet)
				entryName := promptLine(stdin, stdout, globals, "  Pass entry name", suggested)
				value := secret.ExtractSecretValue(v.Snippet)
				if value == "" {
					fmt.Fprintf(out, "  %s could not extract secret value — store manually with `pass insert %s`\n",
						style.IconWarning(nc), entryName)
					result.Skipped++
				} else {
					err := secret.InsertEntry(entryName, value)
					if err != nil {
						fmt.Fprintf(out, "  %s %s\n", style.IconCross(nc), err)
						result.Skipped++
					} else {
						fmt.Fprintf(out, "  %s Stored in pass as %s\n", style.IconCheck(nc), style.Infof(nc, "%s", entryName))
						fmt.Fprintf(out, "  %s Replace hardcoded value with `pass show %s` or env var reference\n",
							style.IconInfo(nc), entryName)
						result.PassStored++
						result.Added = append(result.Added, "pass:"+entryName)
						// Also add to manifest to track which violations were resolved via pass.
						trackPassEntry(manifestPath, anchor)
					}
				}

			case "a":
				_, err := ignore.AddRules(megaignorePath, []string{"-:" + v.Path})
				if err != nil {
					fmt.Fprintf(out, "  %s %s\n", style.IconCross(nc), err)
					result.Skipped++
				} else {
					excludedFiles[v.Path] = true
					fmt.Fprintf(out, "  %s Added -:%s to .megaignore\n", style.IconCheck(nc), v.Path)
					fmt.Fprintf(out, "  %s Rotate any real credentials in this file\n", style.IconWarning(nc))
					result.Excluded++
					result.Added = append(result.Added, "exclude:"+v.Path)
				}

			case "l":
				err := addToAllowlist(manifestPath, anchor)
				if err != nil {
					fmt.Fprintf(out, "  %s %s\n", style.IconCross(nc), err)
					result.Skipped++
				} else {
					fmt.Fprintf(out, "  %s Allowlisted %s\n", style.IconCheck(nc), anchor)
					result.Allowlisted++
					result.Added = append(result.Added, "allowlist:"+anchor)
				}

			case "d":
				dir := filepath.ToSlash(filepath.Dir(v.Path))
				suggested := dir
				dirName := promptLine(stdin, stdout, globals, "  Skip directory", suggested)
				dirName = filepath.ToSlash(strings.TrimRight(strings.TrimSpace(dirName), "/"))
				if dirName == "" {
					result.Skipped++
				} else {
					err := addSkipDirToConfig(configPath, dirName)
					if err != nil {
						fmt.Fprintf(out, "  %s %s\n", style.IconCross(nc), err)
						result.Skipped++
					} else {
						skippedDirs[dirName] = true
						fmt.Fprintf(out, "  %s Added \"%s\" to secret.skip_dirs in config\n", style.IconCheck(nc), dirName)
						result.DirSkipped++
						result.Added = append(result.Added, "skip-dir:"+dirName)
					}
				}

			case "s":
				result.Skipped++

			case "q":
				// Count remaining as skipped.
				result.Skipped += len(violations) - result.Allowlisted - result.Excluded - result.PassStored - result.Skipped
				goto done
			}
			break // exit inner re-prompt loop
		}
	}

done:
	// Print summary.
	fmt.Fprintln(out)
	parts := []string{}
	if result.Allowlisted > 0 {
		parts = append(parts, fmt.Sprintf("%d allowlisted", result.Allowlisted))
	}
	if result.Excluded > 0 {
		parts = append(parts, fmt.Sprintf("%d excluded", result.Excluded))
	}
	if result.PassStored > 0 {
		parts = append(parts, fmt.Sprintf("%d stored in pass", result.PassStored))
	}
	if result.DirSkipped > 0 {
		parts = append(parts, fmt.Sprintf("%d dir-skipped", result.DirSkipped))
	}
	if result.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", result.Skipped))
	}
	if len(parts) > 0 {
		fmt.Fprintf(out, "%s %d violations processed: %s\n",
			style.IconCheck(nc), result.Processed, strings.Join(parts, ", "))
	}

	if globals.json {
		return writeJSON(stdout, stderr, "secret.fix", result)
	}

	if result.Skipped > 0 {
		return 2
	}
	return 0
}

// ── ws secret setup (RW) ──

var secretSetupHelp = cmdHelp{
	Usage:       "ws secret setup [--git-remote <url>]",
	Description: "Set up Unix Password Store (pass). For credential helper: ws git-credential-helper setup",
	Flags: []string{
		"      --git-remote string   Git remote URL for pass store backup",
	},
}

func runSecretSetup(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, secretSetupHelp)
	}

	fs := flag.NewFlagSet("secret-setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	gitRemote := fs.String("git-remote", "", "")
	dryRun := fs.Bool("dry-run", false, "")
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

	health := secret.CheckPass()

	// Pre-checks: hard requirements.
	if !health.GPGAvailable {
		fmt.Fprintln(stderr, style.ResultError(nc, "GPG not found. Install with: sudo apt install gnupg"))
		return 1
	}
	if !health.Installed {
		fmt.Fprintln(stderr, style.ResultError(nc, "pass not found. Install with: sudo apt install pass"))
		return 1
	}

	// Determine GPG key for pass init.
	var gpgID string
	if !health.Initialized {
		keys, err := secret.ListGPGKeys()
		if err != nil || len(keys) == 0 {
			fmt.Fprintln(stderr, style.ResultError(nc, "No GPG secret keys found."))
			fmt.Fprintln(stderr, style.Mutedf(nc, "Generate one with: gpg --full-generate-key"))
			fmt.Fprintln(stderr, style.Mutedf(nc, "Then re-run: ws secret setup"))
			return 1
		}
		if len(keys) == 1 {
			gpgID = keys[0].ID
			fmt.Fprintf(out, "  %s Using GPG key: %s %s\n",
				style.IconInfo(nc), keys[0].ID, style.Mutedf(nc, "(%s)", keys[0].Name))
		} else {
			fmt.Fprintln(out, "  Available GPG keys:")
			for i, k := range keys {
				fmt.Fprintf(out, "    %d) %s  %s\n", i+1, k.ID, style.Mutedf(nc, "%s", k.Name))
			}
			choice := promptLine(stdin, stdout, globals, "  Select key number", "1")
			idx := 0
			for i := 0; i < len(choice); i++ {
				if choice[i] >= '0' && choice[i] <= '9' {
					idx = idx*10 + int(choice[i]-'0')
				}
			}
			idx-- // 1-indexed to 0-indexed
			if idx < 0 || idx >= len(keys) {
				idx = 0
			}
			gpgID = keys[idx].ID
		}
	}

	// Build plan.
	plan := Plan{Command: "secret.setup"}

	if !health.Initialized {
		capturedGPGID := gpgID
		plan.Actions = append(plan.Actions, Action{
			ID:          "pass-init",
			Description: fmt.Sprintf("Initialize pass store with GPG key %s", capturedGPGID),
			Execute: func() error {
				return secret.InitStore(capturedGPGID)
			},
		})
	}

	if !health.GitBacked {
		plan.Actions = append(plan.Actions, Action{
			ID:          "pass-git-init",
			Description: "Enable git versioning for pass store",
			Execute: func() error {
				return secret.InitGit()
			},
		})
	}

	if *gitRemote != "" {
		capturedRemote := *gitRemote
		plan.Actions = append(plan.Actions, Action{
			ID:          "pass-git-remote",
			Description: fmt.Sprintf("Add git remote: %s", capturedRemote),
			Execute: func() error {
				return secret.AddGitRemote(capturedRemote)
			},
		})
	}

	if len(plan.Actions) == 0 {
		fmt.Fprintln(out, style.ResultSuccess(nc, "pass store already set up."))
		printPassHealth(out, health, nc)
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.Mutedf(nc, "  To connect git credential helper: ws git-credential-helper setup"))
		if globals.json {
			return writeJSON(stdout, stderr, "secret.setup", map[string]any{
				"message": "already set up",
				"pass":    health,
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
		return writeJSON(stdout, stderr, "secret.setup", map[string]any{
			"actions": planResult.Actions,
			"pass":    secret.CheckPass(),
		})
	}

	// Show final status.
	if !planResult.HasFailures() {
		fmt.Fprintln(out)
		printPassHealth(out, secret.CheckPass(), nc)
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.Mutedf(nc, "  To connect git credential helper: ws git-credential-helper setup"))
	}

	return planResult.ExitCode()
}

// ── Helpers (secret) ──

func buildAllowlistMap(m manifest.Manifest) map[string]struct{} {
	allow := make(map[string]struct{}, len(m.Secret.Allowlist))
	for _, a := range m.Secret.Allowlist {
		allow[a] = struct{}{}
	}
	return allow
}

func addToAllowlist(manifestPath, anchor string) error {
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return err
	}
	for _, a := range m.Secret.Allowlist {
		if a == anchor {
			return nil
		}
	}
	m.Secret.Allowlist = append(m.Secret.Allowlist, anchor)
	return manifest.Save(manifestPath, m)
}

func trackPassEntry(manifestPath, anchor string) {
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return
	}
	for _, e := range m.Secret.PassEntries {
		if e == anchor {
			return
		}
	}
	m.Secret.PassEntries = append(m.Secret.PassEntries, anchor)
	manifest.Save(manifestPath, m)
}

// promptLine reads a full line of input with a default value.
// Returns defaultValue on EOF, --quiet, or --json.
func promptLine(stdin io.Reader, stdout io.Writer, globals globalFlags, prompt, defaultValue string) string {
	if globals.json || globals.quiet {
		return defaultValue
	}
	nc := globals.noColor
	if defaultValue != "" {
		fmt.Fprintf(stdout, "%s %s: ", prompt, style.Mutedf(nc, "[%s]", defaultValue))
	} else {
		fmt.Fprintf(stdout, "%s: ", prompt)
	}
	line, err := readLine(stdin)
	if err != nil || strings.TrimSpace(line) == "" {
		return defaultValue
	}
	return strings.TrimSpace(line)
}

// mergeSkipDirs combines config and flag skip dirs, deduplicating and
// normalizing to forward-slash relative paths.
func mergeSkipDirs(configDirs, flagDirs []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, d := range append(configDirs, flagDirs...) {
		d = filepath.ToSlash(strings.TrimRight(strings.TrimSpace(d), "/"))
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		result = append(result, d)
	}
	return result
}

// addSkipDirToConfig loads config, appends dir to secret.skip_dirs if not
// already present, and saves.
func addSkipDirToConfig(configPath, dir string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	for _, d := range cfg.Secret.SkipDirs {
		if filepath.ToSlash(d) == dir {
			return nil // already present
		}
	}
	cfg.Secret.SkipDirs = append(cfg.Secret.SkipDirs, dir)
	return config.Save(configPath, cfg)
}

// isInSkippedDir returns true if the file path is under any of the skipped directories.
func isInSkippedDir(filePath string, skippedDirs map[string]bool) bool {
	for d := range skippedDirs {
		if strings.HasPrefix(filePath, d+"/") || filePath == d {
			return true
		}
	}
	return false
}
