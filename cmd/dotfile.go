package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/provision"
	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/secret"
	"github.com/mugenkunou/ws-tool/internal/style"
)

var dotfileHelp = cmdHelp{Usage: "ws dotfile <add|rm|ls|scan|fix|reset|git> [args]"}

func runDotfile(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, dotfileHelp)
	}

	workspacePath, configPath, manifestPath, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if len(args) == 0 {
		return printUsageError(stderr, dotfileHelp)
	}

	subcommand := args[0]
	subArgs := args[1:]

	switch subcommand {
	case "add":
		fs := flag.NewFlagSet("dotfile-add", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if len(fs.Args()) != 1 {
			fmt.Fprintln(stderr, "usage: ws dotfile add <path>")
			return 1
		}

		targetPath := fs.Args()[0]
		plan := Plan{Command: "dotfile.add"}
		plan.Actions = append(plan.Actions, Action{
			ID:          "dotfile-add",
			Description: fmt.Sprintf("Add dotfile %s", targetPath),
			Execute: func() error {
				result, err := dotfile.Add(dotfile.AddOptions{
					WorkspacePath: workspacePath,
					ManifestPath:  manifestPath,
					SystemPath:    targetPath,
					DryRun:        false,
				})
				if err != nil {
					return err
				}
				_ = provision.Record(provision.LedgerPath(workspacePath), provision.Entry{
					Type:    provision.TypeSymlink,
					Path:    result.Record.System,
					Target:  dotfile.DotfilePath(result.Record.Name),
					Command: "dotfile add",
				})
				return nil
			},
		})
		planResult := RunPlan(plan, stdin, stdout, globals)
		if planResult.ExecutedCount() > 0 && !planResult.HasFailures() {
			dotfileGitAutoSync(workspacePath, configPath, "dotfile add: "+targetPath, globals, stdout)
		}
		if globals.json {
			return writeJSONDryRun(stdout, stderr, "dotfile.add", globals.dryRun, map[string]any{
				"actions": planResult.Actions,
			})
		}
		return planResult.ExitCode()
	case "rm":
		fs := flag.NewFlagSet("dotfile-rm", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if len(fs.Args()) != 1 {
			fmt.Fprintln(stderr, "usage: ws dotfile rm <path>")
			return 1
		}

		targetPath := fs.Args()[0]
		plan := Plan{Command: "dotfile.rm"}
		plan.Actions = append(plan.Actions, Action{
			ID:          "dotfile-rm",
			Description: fmt.Sprintf("Remove dotfile %s", targetPath),
			Execute: func() error {
				result, err := dotfile.Remove(dotfile.RemoveOptions{
					WorkspacePath: workspacePath,
					ManifestPath:  manifestPath,
					SystemPath:    targetPath,
					DryRun:        false,
				})
				if err != nil {
					return err
				}
				_ = provision.Remove(provision.LedgerPath(workspacePath), provision.TypeSymlink, result.Record.System)
				return nil
			},
		})
		planResult := RunPlan(plan, stdin, stdout, globals)
		if planResult.ExecutedCount() > 0 && !planResult.HasFailures() {
			dotfileGitAutoSync(workspacePath, configPath, "dotfile rm: "+targetPath, globals, stdout)
		}
		if globals.json {
			return writeJSONDryRun(stdout, stderr, "dotfile.rm", globals.dryRun, map[string]any{
				"actions": planResult.Actions,
			})
		}
		return planResult.ExitCode()
	case "ls":
		fs := flag.NewFlagSet("dotfile-ls", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		records, err := dotfile.List(manifestPath)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if globals.json {
			return writeJSON(stdout, stderr, "dotfile.ls", records)
		}
		if len(records) == 0 {
			fmt.Fprintln(textOut(globals, stdout), "No managed dotfiles.")
			return 0
		}
		out := textOut(globals, stdout)
		for _, r := range records {
			fmt.Fprintf(out, "%s %s %s\n", style.Infof(globals.noColor, "%s", r.System), style.IconArrow(globals.noColor), style.Mutedf(globals.noColor, "%s", r.Name))
		}
		return 0
	case "scan":
		fs := flag.NewFlagSet("dotfile-scan", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		issues, err := dotfile.Scan(dotfile.ScanOptions{WorkspacePath: workspacePath, ManifestPath: manifestPath})
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if globals.json {
			return writeJSON(stdout, stderr, "dotfile.scan", issues)
		}
		out := textOut(globals, stdout)
		if len(issues) == 0 {
			fmt.Fprintln(out, style.ResultSuccess(globals.noColor, "Dotfile scan: %s", style.Badge("ok", globals.noColor)))
		} else {
			printDotfileIssues(out, issues, globals.noColor, false)
		}
		if len(issues) > 0 {
			return 2
		}
		return 0
	case "fix":
		fs := flag.NewFlagSet("dotfile-fix", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if *dryRun {
			globals.dryRun = true
		}

		fixResult, err := dotfile.Fix(dotfile.FixOptions{
			WorkspacePath: workspacePath,
			ManifestPath:  manifestPath,
			DryRun:        globals.dryRun,
		})
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		if globals.json {
			return writeJSON(stdout, stderr, "dotfile.fix", fixResult)
		}

		out := textOut(globals, stdout)
		nc := globals.noColor
		style.Header(out, style.IconLink(nc)+" Dotfile Fix", nc)

		for _, iss := range fixResult.Fixed {
			fmt.Fprintf(out, "  %s %s  %s  %s\n", style.IconCheck(nc), iss.SystemPath, style.IconArrow(nc), style.Mutedf(nc, "%s", iss.Message))
		}
		for _, iss := range fixResult.Unchanged {
			fmt.Fprintf(out, "  %s %s  %s\n", style.Mutedf(nc, "–"), iss.SystemPath, style.Mutedf(nc, "ok"))
		}
		for _, iss := range fixResult.Failed {
			fmt.Fprintf(out, "  %s %s  %s\n", style.IconCross(nc), iss.SystemPath, style.Errorf(nc, "%s", iss.Message))
		}

		fmt.Fprintln(out)
		fmt.Fprintf(out, "Created: %d   Skipped: %d   Failed: %d\n",
			len(fixResult.Fixed), len(fixResult.Unchanged), len(fixResult.Failed))

		if len(fixResult.Failed) > 0 {
			return 3
		}
		return 0
	case "reset":
		fs := flag.NewFlagSet("dotfile-reset", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		plan := Plan{Command: "dotfile.reset"}
		plan.Actions = append(plan.Actions, Action{
			ID:          "dotfile-reset-all",
			Description: "Reset all managed dotfiles",
			Execute: func() error {
				_, err := dotfile.Reset(dotfile.ResetOptions{
					WorkspacePath: workspacePath,
					ManifestPath:  manifestPath,
					DryRun:        false,
				})
				return err
			},
		})
		planResult := RunPlan(plan, stdin, stdout, globals)
		if globals.json {
			return writeJSONDryRun(stdout, stderr, "dotfile.reset", globals.dryRun, map[string]any{
				"actions": planResult.Actions,
			})
		}
		return planResult.ExitCode()
	case "git":
		if len(subArgs) == 0 {
			fmt.Fprintln(stderr, "usage: ws dotfile git <enable|disconnect|status>")
			return 1
		}
		cfg, err := config.Load(configPath)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		switch subArgs[0] {
		case "enable", "connect":
			fs := flag.NewFlagSet("dotfile-git-enable", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			remoteURL := fs.String("remote-url", "", "remote repository URL")
			username := fs.String("username", "", "auth username")
			passEntry := fs.String("pass-entry", "", "optional pass entry")
			branch := fs.String("branch", "main", "git branch")
			autoPush := fs.Bool("auto-push", false, "enable auto-push")
			dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
			registerGlobalFlags(fs, &globals)
			if err := fs.Parse(subArgs[1:]); err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}
			if strings.TrimSpace(*remoteURL) == "" || strings.TrimSpace(*username) == "" {
				fmt.Fprintln(stderr, "--remote-url and --username are required")
				return 1
			}
			if *dryRun {
				globals.dryRun = true
			}

			localRepoPath := filepath.Join(workspacePath, "ws", "dotfiles-git")

			// Enforce private remote — hard constraint, no override.
			out := textOut(globals, stdout)
			nc := globals.noColor
			fmt.Fprintln(out, style.Mutedf(nc, "Checking repository visibility…"))
			token := resolveGitToken(strings.TrimSpace(*remoteURL), strings.TrimSpace(*passEntry))
			vis := repo.CheckRepoVisibility(strings.TrimSpace(*remoteURL), token)
			if vis.Checked && !vis.Private {
				fmt.Fprintln(stderr, style.ResultError(nc, "%s", repo.ErrPublicRepository))
				fmt.Fprintln(stderr, style.Mutedf(nc, "Make the repository private, then retry."))
				return 1
			}
			if vis.Warning != "" {
				fmt.Fprintln(out, style.ResultWarning(nc, "%s", vis.Warning))
			}
			if vis.Error != "" && !vis.Checked {
				fmt.Fprintln(out, style.ResultWarning(nc, "visibility check failed: %s", vis.Error))
			}

			plan := Plan{Command: "dotfile.git.enable"}
			plan.Actions = append(plan.Actions, Action{
				ID:          "update-config",
				Description: fmt.Sprintf("Enable dotfile git to %s", strings.TrimSpace(*remoteURL)),
				Execute: func() error {
					cfg.Dotfile.Git.Enabled = true
					cfg.Dotfile.Git.RemoteURL = strings.TrimSpace(*remoteURL)
					cfg.Dotfile.Git.AuthUsername = strings.TrimSpace(*username)
					cfg.Dotfile.Git.PassEntry = strings.TrimSpace(*passEntry)
					cfg.Dotfile.Git.Branch = strings.TrimSpace(*branch)
					if cfg.Dotfile.Git.Branch == "" {
						cfg.Dotfile.Git.Branch = "main"
					}
					cfg.Dotfile.Git.AutoPush = *autoPush
					cfg.Dotfile.Git.AutoCommit = true
					return config.Save(configPath, cfg)
				},
			})
			plan.Actions = append(plan.Actions, Action{
				ID:          "create-git-dir",
				Description: fmt.Sprintf("Create %s", localRepoPath),
				Execute: func() error {
					if err := os.MkdirAll(localRepoPath, 0o755); err != nil {
						return err
					}
					_ = provision.Record(provision.LedgerPath(workspacePath), provision.Entry{
						Type:    provision.TypeDir,
						Path:    localRepoPath,
						Command: "dotfile git enable",
					})
					return nil
				},
			})

			planResult := RunPlan(plan, stdin, stdout, globals)

			if globals.json {
				return writeJSONDryRun(stdout, stderr, "dotfile.git.enable", globals.dryRun, map[string]any{
					"enabled":        true,
					"remote_url":     strings.TrimSpace(*remoteURL),
					"branch":         strings.TrimSpace(*branch),
					"auto_push":      *autoPush,
					"local_repo_dir": localRepoPath,
					"actions":        planResult.Actions,
				})
			}
			if globals.dryRun {
				fmt.Fprintln(out, style.ResultInfo(nc, "Would enable dotfile git remote."))
			} else if planResult.ExecutedCount() > 0 {
				fmt.Fprintln(out, style.ResultSuccess(nc, "Dotfile git remote enabled."))
			}
			style.KV(out, "Remote URL", style.Infof(nc, "%s", strings.TrimSpace(*remoteURL)), nc)
			style.KV(out, "Branch", strings.TrimSpace(*branch), nc)
			style.KV(out, "Auto-push", fmt.Sprintf("%t", *autoPush), nc)
			return planResult.ExitCode()
		case "disconnect":
			fs := flag.NewFlagSet("dotfile-git-disconnect", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
			registerGlobalFlags(fs, &globals)
			if err := fs.Parse(subArgs[1:]); err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}

			if !cfg.Dotfile.Git.Enabled {
				fmt.Fprintln(stderr, "dotfile git versioning is not enabled")
				return 1
			}
			if *dryRun {
				globals.dryRun = true
			}

			localRepoPath := filepath.Join(workspacePath, "ws", "dotfiles-git")

			plan := Plan{Command: "dotfile.git.disconnect"}
			plan.Actions = append(plan.Actions, Action{
				ID:          "disconnect-git",
				Description: "Disconnect dotfile git remote",
				Execute: func() error {
					cfg.Dotfile.Git.Enabled = false
					cfg.Dotfile.Git.AutoPush = false
					cfg.Dotfile.Git.AutoCommit = false
					if err := config.Save(configPath, cfg); err != nil {
						return err
					}
					_ = provision.Remove(provision.LedgerPath(workspacePath), provision.TypeDir, localRepoPath)
					return nil
				},
			})

			planResult := RunPlan(plan, stdin, stdout, globals)

			if globals.json {
				return writeJSONDryRun(stdout, stderr, "dotfile.git.disconnect", globals.dryRun, map[string]any{
					"disabled":       true,
					"remote_url":     cfg.Dotfile.Git.RemoteURL,
					"local_repo_dir": localRepoPath,
					"actions":        planResult.Actions,
				})
			}
			out := textOut(globals, stdout)
			nc := globals.noColor
			if globals.dryRun {
				fmt.Fprintln(out, style.ResultInfo(nc, "Would disconnect dotfile git remote."))
			} else if planResult.ExecutedCount() > 0 {
				fmt.Fprintln(out, style.ResultSuccess(nc, "Dotfile git remote disconnected."))
			}
			style.KV(out, "Remote URL", style.Infof(nc, "%s", cfg.Dotfile.Git.RemoteURL), nc)
			style.KV(out, "Local repo", localRepoPath, nc)
			return planResult.ExitCode()
		case "status":
			fs := flag.NewFlagSet("dotfile-git-status", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			registerGlobalFlags(fs, &globals)
			if err := fs.Parse(subArgs[1:]); err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}
			localRepoPath := filepath.Join(workspacePath, "ws", "dotfiles-git")
			_, statErr := os.Stat(localRepoPath)
			result := map[string]any{
				"enabled":           cfg.Dotfile.Git.Enabled,
				"remote_url":        cfg.Dotfile.Git.RemoteURL,
				"username":          cfg.Dotfile.Git.AuthUsername,
				"branch":            cfg.Dotfile.Git.Branch,
				"auto_commit":       cfg.Dotfile.Git.AutoCommit,
				"auto_push":         cfg.Dotfile.Git.AutoPush,
				"local_repo_dir":    localRepoPath,
				"local_repo_exists": statErr == nil,
			}
			if globals.json {
				return writeJSON(stdout, stderr, "dotfile.git.status", result)
			}
			out := textOut(globals, stdout)
			nc := globals.noColor
			stateStr := style.Badge("disabled", nc)
			if cfg.Dotfile.Git.Enabled {
				stateStr = style.Badge("enabled", nc)
			}
			style.KV(out, "Git versioning", stateStr, nc)
			style.KV(out, "Local repo", localRepoPath, nc)
			style.KV(out, "Remote URL", style.Infof(nc, "%s", cfg.Dotfile.Git.RemoteURL), nc)
			style.KV(out, "Branch", cfg.Dotfile.Git.Branch, nc)
			style.KV(out, "Auto-push", fmt.Sprintf("%t", cfg.Dotfile.Git.AutoPush), nc)
			return 0
		default:
			fmt.Fprintf(stderr, "unknown dotfile git subcommand: %s\n", subArgs[0])
			return 1
		}
	default:
		fmt.Fprintf(stderr, "unknown dotfile subcommand: %s\n", subcommand)
		fmt.Fprintln(stderr, "usage: ws dotfile <add|rm|ls|scan|fix|reset|git>")
		return 1
	}
}

func writeDotfileResult(globals globalFlags, command string, data any, stdout, stderr io.Writer) int {
	if globals.json {
		return writeJSONDryRun(stdout, stderr, command, globals.dryRun, data)
	}

	out := textOut(globals, stdout)
	switch v := data.(type) {
	case dotfile.AddResult:
		for _, line := range v.Messages {
			fmt.Fprintln(out, line)
		}
	case dotfile.RemoveResult:
		for _, line := range v.Messages {
			fmt.Fprintln(out, line)
		}
	case dotfile.FixResult:
		for _, line := range v.Messages {
			fmt.Fprintln(out, line)
		}
		if len(v.Failed) > 0 {
			for _, issue := range v.Failed {
				fmt.Fprintln(out, strings.TrimSpace(issue.Status+" "+issue.SystemPath+" "+issue.Message))
			}
		}
	default:
		fmt.Fprintln(stdout, "ok")
	}

	return 0
}

// dotfileGitAutoSync runs auto-commit/push if dotfile git is enabled.
// Errors are non-fatal — they are printed as warnings, never block the parent command.
func dotfileGitAutoSync(workspacePath, configPath, commitMessage string, globals globalFlags, stdout io.Writer) {
	cfg, err := config.Load(configPath)
	if err != nil || !cfg.Dotfile.Git.Enabled {
		return
	}
	repoPath := filepath.Join(workspacePath, "ws", "dotfiles-git")
	result := dotfile.GitSync(dotfile.GitSyncOptions{
		WorkspacePath: workspacePath,
		RepoPath:      repoPath,
		RemoteURL:     cfg.Dotfile.Git.RemoteURL,
		Branch:        cfg.Dotfile.Git.Branch,
		AutoCommit:    cfg.Dotfile.Git.AutoCommit,
		AutoPush:      cfg.Dotfile.Git.AutoPush,
		CommitMessage: commitMessage,
	})
	out := textOut(globals, stdout)
	nc := globals.noColor
	if result.Committed {
		fmt.Fprintln(out, style.Mutedf(nc, "dotfile git: committed"))
	}
	if result.Pushed {
		fmt.Fprintln(out, style.Mutedf(nc, "dotfile git: pushed"))
	}
	if result.Error != "" {
		fmt.Fprintln(out, style.ResultWarning(nc, "dotfile git: %s", result.Error))
	}
}

// resolveGitToken tries to find a token for the remote URL via pass.
// Returns empty string if no token is available (non-fatal).
func resolveGitToken(remoteURL, passEntry string) string {
	if passEntry != "" {
		resp, ok := tryPassForToken(passEntry)
		if ok {
			return resp
		}
	}
	// Try auto-derived entry from host.
	host := extractHostFromURL(remoteURL)
	if host != "" {
		resp, ok := tryPassForToken("git/" + host)
		if ok {
			return resp
		}
	}
	return ""
}

// tryPassForToken runs pass show and returns the password (token) if found.
func tryPassForToken(entry string) (string, bool) {
	resp := secret.LookupCredential(secret.CredentialRequest{
		Protocol: "https",
		Host:     entry,
	})
	if resp.Password != "" {
		return resp.Password, true
	}
	// Try direct pass show for explicit entry names.
	if secret.PassEntryExists(entry) {
		// Entry exists but LookupCredential couldn't find it via the host path.
		// This means the entry is at a custom path. We can't decrypt here without
		// the full pass lookup chain, so return empty.
		return "", false
	}
	return "", false
}

// extractHostFromURL extracts hostname from an HTTPS or SSH git URL.
func extractHostFromURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if strings.Contains(rawURL, "://") {
		if idx := strings.Index(rawURL, "://"); idx >= 0 {
			rest := rawURL[idx+3:]
			if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
				return rest[:slashIdx]
			}
			return rest
		}
	}
	if strings.HasPrefix(rawURL, "git@") {
		if colonIdx := strings.Index(rawURL, ":"); colonIdx >= 0 {
			return rawURL[4:colonIdx]
		}
	}
	return ""
}
