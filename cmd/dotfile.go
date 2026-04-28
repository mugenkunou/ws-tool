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

var dotfileHelp = cmdHelp{Usage: "ws dotfile <add|rm|ls|scan|fix|reset|migrate|git> [args]"}

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

		// Resolve the path so we can stat it before passing to internal logic.
		absTarget, err := config.ExpandUserPath(targetPath)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		out := textOut(globals, stdout)

		info, statErr := os.Stat(absTarget)
		if statErr != nil {
			fmt.Fprintf(stderr, "path not found: %s\n", absTarget)
			return 1
		}

		var filesToAdd []string // absolute paths of files to track

		if info.IsDir() {
			fmt.Fprintf(out, "Scanning %s ...\n", absTarget)
			entries, expanded, expandErr := dotfile.ExpandDir(absTarget)
			if expandErr != nil {
				fmt.Fprintln(stderr, expandErr.Error())
				return 1
			}
			if len(entries) == 0 {
				fmt.Fprintln(out, "Directory is empty — nothing to add.")
				return 0
			}
			selected := promptDirSelection(stdin, stdout, globals, entries, expanded, absTarget)
			if len(selected) == 0 {
				fmt.Fprintln(out, "Nothing selected.")
				return 0
			}
			for _, e := range selected {
				files, collectErr := dotfile.CollectFiles(e)
				if collectErr != nil {
					fmt.Fprintf(stderr, "error reading %s: %v\n", e.AbsPath, collectErr)
					return 1
				}
				filesToAdd = append(filesToAdd, files...)
			}
		} else {
			filesToAdd = []string{absTarget}
		}

		// Warn about secret files and let user opt out.
		filesToAdd = warnAndFilterSecrets(filesToAdd, stdin, stdout, globals)
		if len(filesToAdd) == 0 {
			fmt.Fprintln(out, "Nothing to add.")
			return 0
		}

		// Build a plan: one action per file.
		plan := Plan{Command: "dotfile.add"}
		for _, f := range filesToAdd {
			f := f
			plan.Actions = append(plan.Actions, Action{
				ID:          "dotfile-add-" + f,
				Description: fmt.Sprintf("Add dotfile %s", f),
				Execute: func() error {
					result, addErr := dotfile.Add(dotfile.AddOptions{
						WorkspacePath: workspacePath,
						ManifestPath:  manifestPath,
						SystemPath:    f,
						DryRun:        false,
					})
					if addErr != nil {
						return addErr
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
		}
		planResult := RunPlan(plan, stdin, stdout, globals)
		if planResult.ExecutedCount() > 0 && !planResult.HasFailures() {
			label := targetPath
			dotfileGitAutoSync(workspacePath, configPath, "dotfile add: "+label, globals, stdout)
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
			sudoTag := ""
			if r.Sudo {
				sudoTag = "  " + style.Warningf(globals.noColor, "[sudo]")
			}
			fmt.Fprintf(out, "%s %s %s%s\n", style.Infof(globals.noColor, "%s", r.System), style.IconArrow(globals.noColor), style.Mutedf(globals.noColor, "%s", r.Name), sudoTag)
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
			fmt.Fprintln(stderr, "usage: ws dotfile git <remote|push|log|status|setup|disconnect>")
			return 1
		}
		cfg, err := config.Load(configPath)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repoPath := filepath.Join(workspacePath, "ws", "dotfiles")

		switch subArgs[0] {
		case "remote":
			fs := flag.NewFlagSet("dotfile-git-remote", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			passEntry := fs.String("pass-entry", "", "optional pass entry for auth")
			dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
			registerGlobalFlags(fs, &globals)
			if err := fs.Parse(subArgs[1:]); err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}
			if *dryRun {
				globals.dryRun = true
			}
			if len(fs.Args()) != 1 {
				fmt.Fprintln(stderr, "usage: ws dotfile git remote <url>")
				return 1
			}
			remoteURL := strings.TrimSpace(fs.Args()[0])

			if !dotfile.GitIsInitialized(repoPath) {
				fmt.Fprintln(stderr, style.ResultError(globals.noColor, "Git not initialized. Run: ws dotfile git setup"))
				return 1
			}

			// Enforce private remote — hard constraint.
			out := textOut(globals, stdout)
			nc := globals.noColor
			fmt.Fprintln(out, style.Mutedf(nc, "Checking repository visibility…"))
			token := resolveGitToken(remoteURL, *passEntry)
			vis := repo.CheckRepoVisibility(remoteURL, token)
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

			plan := Plan{Command: "dotfile.git.remote"}
			plan.Actions = append(plan.Actions, Action{
				ID:          "add-remote",
				Description: fmt.Sprintf("Set remote origin to %s", remoteURL),
				Execute: func() error {
					if err := dotfile.GitAddRemote(repoPath, remoteURL); err != nil {
						return err
					}
					cfg.Dotfile.Git.RemoteURL = remoteURL
					if *passEntry != "" {
						cfg.Dotfile.Git.PassEntry = *passEntry
					}
					return config.Save(configPath, cfg)
				},
			})
			planResult := RunPlan(plan, stdin, stdout, globals)
			if globals.json {
				return writeJSONDryRun(stdout, stderr, "dotfile.git.remote", globals.dryRun, map[string]any{
					"remote_url": remoteURL,
					"actions":    planResult.Actions,
				})
			}
			if planResult.WasExecuted("add-remote") {
				fmt.Fprintln(out, style.ResultSuccess(nc, "Remote set to %s", remoteURL))
			}
			return planResult.ExitCode()

		case "push":
			fs := flag.NewFlagSet("dotfile-git-push", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			message := fs.String("m", "", "commit message (default: auto)")
			passEntry := fs.String("pass-entry", "", "optional pass entry for auth")
			registerGlobalFlags(fs, &globals)
			if err := fs.Parse(subArgs[1:]); err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}
			_ = passEntry // reserved for future credential resolution before push

			if !dotfile.GitIsInitialized(repoPath) {
				fmt.Fprintln(stderr, style.ResultError(globals.noColor, "Git not initialized. Run: ws dotfile git setup"))
				return 1
			}
			if !dotfile.GitHasRemote(repoPath) {
				fmt.Fprintln(stderr, style.ResultError(globals.noColor, "No remote configured. Run: ws dotfile git remote <url>"))
				return 1
			}

			out := textOut(globals, stdout)
			nc := globals.noColor

			// Smart push: commit any pending changes first.
			commitMsg := *message
			if commitMsg == "" {
				commitMsg = "manual push"
			}
			result := dotfile.GitSync(dotfile.GitSyncOptions{
				WorkspacePath: workspacePath,
				RepoPath:      repoPath,
				RemoteURL:     cfg.Dotfile.Git.RemoteURL,
				Branch:        cfg.Dotfile.Git.Branch,
				AutoCommit:    true,
				AutoPush:      false, // we handle push with visibility check below
				CommitMessage: commitMsg,
			})
			if result.Error != "" {
				fmt.Fprintln(stderr, style.ResultError(nc, "commit: %s", result.Error))
				return 1
			}
			if result.Committed {
				fmt.Fprintln(out, style.ResultSuccess(nc, "Committed."))
			}

			// Pre-push private repo check.
			remoteURL := dotfile.GitRemoteURL(repoPath)
			token := resolveGitToken(remoteURL, cfg.Dotfile.Git.PassEntry)
			vis := repo.CheckRepoVisibility(remoteURL, token)
			if vis.Checked && !vis.Private {
				fmt.Fprintln(stderr, style.ResultError(nc, "%s", repo.ErrPublicRepository))
				return 1
			}

			branch := cfg.Dotfile.Git.Branch
			if err := dotfile.GitPush(repoPath, branch); err != nil {
				fmt.Fprintln(stderr, style.ResultError(nc, "push failed: %s", err))
				return 1
			}
			fmt.Fprintln(out, style.ResultSuccess(nc, "Pushed to %s", remoteURL))
			return 0

		case "log":
			fs := flag.NewFlagSet("dotfile-git-log", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			count := fs.Int("n", 20, "number of commits to show")
			registerGlobalFlags(fs, &globals)
			if err := fs.Parse(subArgs[1:]); err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}

			if !dotfile.GitIsInitialized(repoPath) {
				fmt.Fprintln(stderr, style.ResultError(globals.noColor, "Git not initialized. Run: ws dotfile git setup"))
				return 1
			}

			logOut, err := dotfile.GitLog(repoPath, *count)
			if err != nil {
				fmt.Fprintln(stderr, style.ResultError(globals.noColor, "git log: %s", err))
				return 1
			}
			if globals.json {
				return writeJSON(stdout, stderr, "dotfile.git.log", map[string]any{
					"log": strings.TrimSpace(logOut),
				})
			}
			out := textOut(globals, stdout)
			logStr := strings.TrimSpace(logOut)
			if logStr == "" {
				fmt.Fprintln(out, style.Mutedf(globals.noColor, "No commits yet."))
			} else {
				fmt.Fprintln(out, logStr)
			}
			return 0

		case "status":
			fs := flag.NewFlagSet("dotfile-git-status", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			registerGlobalFlags(fs, &globals)
			if err := fs.Parse(subArgs[1:]); err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}
			out := textOut(globals, stdout)
			nc := globals.noColor

			gitInited := dotfile.GitIsInitialized(repoPath)
			hasRemote := false
			remoteURL := ""
			branch := ""
			lastCommit := ""
			dirty := ""
			var ahead, behind int

			if gitInited {
				hasRemote = dotfile.GitHasRemote(repoPath)
				remoteURL = dotfile.GitRemoteURL(repoPath)
				branch = dotfile.GitBranch(repoPath)
				lastCommit, _ = dotfile.GitLastCommit(repoPath)
				lastCommit = strings.TrimSpace(lastCommit)
				porcelain, _ := dotfile.GitStatus(repoPath)
				if strings.TrimSpace(porcelain) != "" {
					dirty = "dirty"
				} else {
					dirty = "clean"
				}
				if hasRemote {
					ahead, behind = dotfile.GitAheadBehind(repoPath, branch)
				}
			}

			result := map[string]any{
				"git_initialized": gitInited,
				"git_remote":      hasRemote,
				"remote_url":      remoteURL,
				"branch":          branch,
				"auto_commit":     cfg.Dotfile.Git.AutoCommit,
				"auto_push":       cfg.Dotfile.Git.AutoPush,
				"working_tree":    dirty,
				"last_commit":     lastCommit,
				"ahead":           ahead,
				"behind":          behind,
				"repo_path":       repoPath,
			}
			if globals.json {
				return writeJSON(stdout, stderr, "dotfile.git.status", result)
			}

			if !gitInited {
				fmt.Fprintln(out, style.ResultInfo(nc, "Git not initialized."))
				fmt.Fprintln(out, style.Mutedf(nc, "  Run: ws dotfile git setup"))
				return 0
			}

			style.KV(out, "Git", style.Badge("initialized", nc), nc)
			style.KV(out, "Repo", repoPath, nc)
			style.KV(out, "Branch", branch, nc)
			if hasRemote {
				style.KV(out, "Remote", style.Infof(nc, "%s", remoteURL), nc)
				style.KV(out, "Ahead/behind", fmt.Sprintf("↑%d ↓%d", ahead, behind), nc)
			} else {
				style.KV(out, "Remote", style.ResultWarning(nc, "none"), nc)
			}
			style.KV(out, "Working tree", dirty, nc)
			style.KV(out, "Auto-commit", fmt.Sprintf("%t", cfg.Dotfile.Git.AutoCommit), nc)
			style.KV(out, "Auto-push", fmt.Sprintf("%t", cfg.Dotfile.Git.AutoPush), nc)
			if lastCommit != "" {
				style.KV(out, "Last commit", lastCommit, nc)
			}

			if !hasRemote {
				fmt.Fprintln(out)
				fmt.Fprintln(out, style.ResultWarning(nc, "No git remote. Dotfiles only exist on this machine."))
				fmt.Fprintln(out, style.Mutedf(nc, "  Add one with: ws dotfile git remote <url>"))
				fmt.Fprintln(out, style.Mutedf(nc, "  Use a PRIVATE repo — never push dotfiles to a public repo."))
			}
			return 0

		case "setup":
			fs := flag.NewFlagSet("dotfile-git-setup", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
			registerGlobalFlags(fs, &globals)
			if err := fs.Parse(subArgs[1:]); err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}
			if *dryRun {
				globals.dryRun = true
			}

			out := textOut(globals, stdout)
			nc := globals.noColor

			plan := Plan{Command: "dotfile.git.setup"}

			// Step 1: git init if needed.
			gitInited := dotfile.GitIsInitialized(repoPath)
			branch := cfg.Dotfile.Git.Branch
			if branch == "" {
				branch = "main"
			}

			if !gitInited {
				plan.Actions = append(plan.Actions, Action{
					ID:          "git-init",
					Description: fmt.Sprintf("Initialize git in %s (branch: %s)", repoPath, branch),
					Execute: func() error {
						if err := dotfile.EnsureGitRepo(repoPath, "", branch); err != nil {
							return err
						}
						cfg.Dotfile.Git.Enabled = true
						cfg.Dotfile.Git.AutoCommit = true
						if cfg.Dotfile.Git.Branch == "" {
							cfg.Dotfile.Git.Branch = branch
						}
						return config.Save(configPath, cfg)
					},
				})
			}

			// Step 2: remote if not configured.
			hasRemote := gitInited && dotfile.GitHasRemote(repoPath)
			var remoteURL string
			if !hasRemote {
				fmt.Fprintln(out)
				fmt.Fprintln(out, style.Mutedf(nc, "  A private git remote provides off-machine backup for your dotfiles."))
				fmt.Fprintln(out, style.Mutedf(nc, "  Create a PRIVATE repo first, then paste the URL below."))
				remoteURL = promptLine(stdin, stdout, globals, "  Remote URL (blank to skip)", "")

				if remoteURL != "" {
					// Enforce private remote.
					fmt.Fprintln(out, style.Mutedf(nc, "Checking repository visibility…"))
					token := resolveGitToken(remoteURL, cfg.Dotfile.Git.PassEntry)
					vis := repo.CheckRepoVisibility(remoteURL, token)
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

					capturedURL := remoteURL
					plan.Actions = append(plan.Actions, Action{
						ID:          "add-remote",
						Description: fmt.Sprintf("Set remote origin to %s", capturedURL),
						Execute: func() error {
							if err := dotfile.GitAddRemote(repoPath, capturedURL); err != nil {
								return err
							}
							cfg.Dotfile.Git.RemoteURL = capturedURL
							return config.Save(configPath, cfg)
						},
					})
				}
			}

			// Step 3: enable auto-push if remote is being set and not already enabled.
			if (hasRemote || remoteURL != "") && !cfg.Dotfile.Git.AutoPush {
				plan.Actions = append(plan.Actions, Action{
					ID:          "enable-auto-push",
					Description: "Enable auto-push on dotfile changes",
					Execute: func() error {
						cfg.Dotfile.Git.AutoPush = true
						return config.Save(configPath, cfg)
					},
				})
			}

			if len(plan.Actions) == 0 {
				fmt.Fprintln(out, style.ResultSuccess(nc, "Dotfile git already set up."))
				fmt.Fprintln(out)
				style.KV(out, "Repo", repoPath, nc)
				style.KV(out, "Remote", style.Infof(nc, "%s", dotfile.GitRemoteURL(repoPath)), nc)
				style.KV(out, "Branch", dotfile.GitBranch(repoPath), nc)
				return 0
			}

			planResult := RunPlan(plan, stdin, stdout, globals)

			if globals.json {
				return writeJSONDryRun(stdout, stderr, "dotfile.git.setup", globals.dryRun, map[string]any{
					"actions": planResult.Actions,
				})
			}

			if !planResult.HasFailures() {
				fmt.Fprintln(out)
				finalHasRemote := dotfile.GitHasRemote(repoPath)
				if !finalHasRemote {
					fmt.Fprintln(out, style.ResultWarning(nc, "No git remote. Dotfiles only exist on this machine."))
					fmt.Fprintln(out, style.Mutedf(nc, "  To add one later: ws dotfile git remote <url>"))
					fmt.Fprintln(out, style.Mutedf(nc, "  Use a PRIVATE repo — never push dotfiles to a public repo."))
				} else {
					fmt.Fprintln(out, style.ResultSuccess(nc, "Dotfile git setup complete."))
				}
			}
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
			if *dryRun {
				globals.dryRun = true
			}

			if !cfg.Dotfile.Git.Enabled {
				fmt.Fprintln(stderr, "dotfile git versioning is not enabled")
				return 1
			}

			plan := Plan{Command: "dotfile.git.disconnect"}
			plan.Actions = append(plan.Actions, Action{
				ID:          "disconnect-git",
				Description: "Disable dotfile git versioning",
				Execute: func() error {
					cfg.Dotfile.Git.Enabled = false
					cfg.Dotfile.Git.AutoPush = false
					cfg.Dotfile.Git.AutoCommit = false
					return config.Save(configPath, cfg)
				},
			})

			planResult := RunPlan(plan, stdin, stdout, globals)
			if globals.json {
				return writeJSONDryRun(stdout, stderr, "dotfile.git.disconnect", globals.dryRun, map[string]any{
					"actions": planResult.Actions,
				})
			}
			out := textOut(globals, stdout)
			nc := globals.noColor
			if planResult.WasExecuted("disconnect-git") {
				fmt.Fprintln(out, style.ResultSuccess(nc, "Dotfile git versioning disabled."))
				fmt.Fprintln(out, style.Mutedf(nc, "  .git directory preserved in %s", repoPath))
				fmt.Fprintln(out, style.Mutedf(nc, "  Re-enable with: ws dotfile git setup"))
			}
			return planResult.ExitCode()

		// Keep old names as aliases for backward compatibility.
		case "enable", "connect":
			fmt.Fprintln(stderr, style.ResultWarning(globals.noColor, "Use 'ws dotfile git setup' instead."))
			return 1

		default:
			fmt.Fprintf(stderr, "unknown dotfile git subcommand: %s\n", subArgs[0])
			fmt.Fprintln(stderr, "usage: ws dotfile git <remote|push|log|status|setup|disconnect>")
			return 1
		}
	case "migrate":
		fs := flag.NewFlagSet("dotfile-migrate", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		out := textOut(globals, stdout)
		nc := globals.noColor

		dirRecords, err := dotfile.FindDirEntries(workspacePath, manifestPath)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if len(dirRecords) == 0 {
			fmt.Fprintln(out, style.ResultSuccess(nc, "No directory entries found — all dotfiles are already file-level."))
			return 0
		}

		fmt.Fprintf(out, "Found %d directory entry(s) to migrate:\n\n", len(dirRecords))
		exitCode := 0

		for _, record := range dirRecords {
			wsDir := filepath.Join(workspacePath, filepath.FromSlash(dotfile.DotfilePath(record.Name)))
			fmt.Fprintf(out, "%s %s\n", style.Infof(nc, "%s", record.System), style.Mutedf(nc, "(→ %s)", record.Name))

			entries, expanded, expandErr := dotfile.ExpandDir(wsDir)
			if expandErr != nil {
				fmt.Fprintf(stderr, "  error scanning %s: %v\n", wsDir, expandErr)
				exitCode = 1
				continue
			}
			if len(entries) == 0 {
				fmt.Fprintln(out, style.Mutedf(nc, "  (empty — skipping)"))
				continue
			}

			selected := promptDirSelection(stdin, stdout, globals, entries, expanded, wsDir)
			if len(selected) == 0 {
				fmt.Fprintln(out, style.Mutedf(nc, "  Nothing selected — skipping."))
				continue
			}

			// Collect the paths the files will have AFTER Remove restores the directory.
			type fileTask struct {
				systemPath    string
				workspacePath string // for display
			}
			var tasks []fileTask
			for _, e := range selected {
				var relPaths []string
				if e.IsDir {
					// Walk the workspace subdirectory to gather all file paths.
					subFiles, collectErr := dotfile.CollectFiles(e)
					if collectErr != nil {
						fmt.Fprintf(stderr, "  error reading %s: %v\n", e.AbsPath, collectErr)
						continue
					}
					for _, sf := range subFiles {
						rel, _ := filepath.Rel(wsDir, sf)
						relPaths = append(relPaths, rel)
					}
				} else {
					rel, _ := filepath.Rel(wsDir, e.AbsPath)
					relPaths = append(relPaths, rel)
				}
				for _, rel := range relPaths {
					sysPath := filepath.Join(record.System, filepath.FromSlash(rel))
					tasks = append(tasks, fileTask{systemPath: sysPath, workspacePath: e.Name})
				}
			}

			if globals.dryRun {
				fmt.Fprintln(out, style.Mutedf(nc, "  [dry-run] would migrate:"))
				for _, task := range tasks {
					fmt.Fprintf(out, "    %s %s %s\n", style.Mutedf(nc, "[dry-run]"), task.systemPath, style.IconArrow(nc))
				}
				continue
			}

			// Step 1: Remove the directory entry, restoring the real directory.
			if _, removeErr := dotfile.Remove(dotfile.RemoveOptions{
				WorkspacePath: workspacePath,
				ManifestPath:  manifestPath,
				SystemPath:    record.System,
			}); removeErr != nil {
				fmt.Fprintf(stderr, "  failed to remove directory entry %s: %v\n", record.System, removeErr)
				exitCode = 1
				continue
			}

			// Step 2: Warn about secrets in the files about to be added.
			var sysPaths []string
			for _, t := range tasks {
				sysPaths = append(sysPaths, t.systemPath)
			}
			sysPaths = warnAndFilterSecrets(sysPaths, stdin, stdout, globals)

			// Step 3: Add each selected file individually.
			addedCount := 0
			for _, sysPath := range sysPaths {
				addResult, addErr := dotfile.Add(dotfile.AddOptions{
					WorkspacePath: workspacePath,
					ManifestPath:  manifestPath,
					SystemPath:    sysPath,
				})
				if addErr != nil {
					fmt.Fprintf(stderr, "  failed to add %s: %v\n", sysPath, addErr)
					exitCode = 1
					continue
				}
				_ = provision.Record(provision.LedgerPath(workspacePath), provision.Entry{
					Type:    provision.TypeSymlink,
					Path:    addResult.Record.System,
					Target:  dotfile.DotfilePath(addResult.Record.Name),
					Command: "dotfile migrate",
				})
				fmt.Fprintf(out, "  %s %s\n", style.IconCheck(nc), style.Mutedf(nc, "%s", sysPath))
				addedCount++
			}
			fmt.Fprintf(out, "  Migrated %d file(s) from %s\n\n", addedCount, record.System)
		}

		if exitCode == 0 {
			dotfileGitAutoSync(workspacePath, configPath, "dotfile migrate", globals, stdout)
		}
		return exitCode

	default:
		fmt.Fprintf(stderr, "unknown dotfile subcommand: %s\n", subcommand)
		fmt.Fprintln(stderr, "usage: ws dotfile <add|rm|ls|scan|fix|reset|migrate|git>")
		return 1
	}
}

// promptDirSelection presents the expanded directory entries to the user and
// returns the subset they choose to track. Defaults to all ClassConfig entries.
//
// In --json / --quiet mode the defaults are accepted automatically.
func promptDirSelection(stdin io.Reader, stdout io.Writer, globals globalFlags, entries []dotfile.DirEntry, expanded bool, rootPath string) []dotfile.DirEntry {
	out := textOut(globals, stdout)
	nc := globals.noColor

	totalFiles := 0
	for _, e := range entries {
		totalFiles += e.FileCount
	}

	if expanded {
		fmt.Fprintf(out, "  %d file(s)\n\n", totalFiles)
	} else {
		fmt.Fprintf(out, "  %d file(s) total — showing top-level entries:\n\n", totalFiles)
	}

	// Print numbered list.
	var defaultIdxs []int
	for i, e := range entries {
		num := i + 1
		sizeStr := style.HumanBytes(e.Size)
		classStr := ""
		switch e.Class {
		case dotfile.ClassSecret:
			classStr = style.Warningf(nc, "⚠ secret")
		case dotfile.ClassState:
			classStr = style.Mutedf(nc, "likely state")
		}

		var countStr string
		if e.IsDir {
			countStr = fmt.Sprintf("  (%d files)", e.FileCount)
		}

		fmt.Fprintf(out, "  %2d  %-40s %8s  %s%s\n",
			num,
			e.Name,
			sizeStr,
			classStr,
			countStr,
		)

		if e.Class == dotfile.ClassConfig {
			defaultIdxs = append(defaultIdxs, num)
		}
	}

	// Build default string.
	var defaultParts []string
	for _, idx := range defaultIdxs {
		defaultParts = append(defaultParts, fmt.Sprintf("%d", idx))
	}
	defaultStr := strings.Join(defaultParts, ",")
	if defaultStr == "" {
		defaultStr = "none"
	}

	fmt.Fprintln(out)

	// Auto-accept defaults in non-interactive modes.
	if globals.json || globals.quiet {
		var result []dotfile.DirEntry
		for _, idx := range defaultIdxs {
			result = append(result, entries[idx-1])
		}
		return result
	}

	fmt.Fprintf(out, "Select entries to track [%s] (or Enter for defaults, \"all\", \"none\"): ", style.Mutedf(nc, defaultStr))
	line, err := readLine(stdin)
	if err != nil || strings.TrimSpace(line) == "" {
		var result []dotfile.DirEntry
		for _, idx := range defaultIdxs {
			result = append(result, entries[idx-1])
		}
		return result
	}

	line = strings.TrimSpace(line)
	switch strings.ToLower(line) {
	case "all":
		return entries
	case "none":
		return nil
	}

	// Parse comma-separated numbers.
	var result []dotfile.DirEntry
	seen := make(map[int]bool)
	for _, part := range strings.Split(line, ",") {
		part = strings.TrimSpace(part)
		n := 0
		for _, c := range part {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			}
		}
		if n >= 1 && n <= len(entries) && !seen[n] {
			seen[n] = true
			result = append(result, entries[n-1])
		}
	}
	return result
}

// warnAndFilterSecrets checks filesToAdd for likely secrets. If any are found
// it prints a warning and asks once whether to track them anyway. If the user
// declines, secrets are removed from the list and the rest are returned.
func warnAndFilterSecrets(filesToAdd []string, stdin io.Reader, stdout io.Writer, globals globalFlags) []string {
	var secretFiles []string
	for _, f := range filesToAdd {
		if dotfile.ClassifyFile(f) == dotfile.ClassSecret {
			secretFiles = append(secretFiles, f)
		}
	}
	if len(secretFiles) == 0 {
		return filesToAdd
	}

	out := textOut(globals, stdout)
	nc := globals.noColor

	fmt.Fprintf(out, "\n%s %d file(s) look like secrets:\n", style.IconWarning(nc), len(secretFiles))
	for _, f := range secretFiles {
		fmt.Fprintf(out, "  %s\n", style.Mutedf(nc, "%s", f))
	}

	if confirm(stdin, stdout, globals, "Track anyway?") {
		return filesToAdd
	}

	// Remove secrets.
	secretSet := make(map[string]bool, len(secretFiles))
	for _, f := range secretFiles {
		secretSet[f] = true
	}
	var filtered []string
	for _, f := range filesToAdd {
		if !secretSet[f] {
			filtered = append(filtered, f)
		}
	}
	return filtered
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
	repoPath := filepath.Join(workspacePath, "ws", "dotfiles")
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
