package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/provision"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/trash"
)

var trashHelp = cmdHelp{
	Usage: "ws trash <enable|disable|status>",
	Subcommands: []string{
		"  enable   Configure soft-delete integrations",
		"  disable  Remove soft-delete integrations",
		"  status   Show integration status and trash size",
	},
	SetupFlags: []string{
		"      --root-dir string      Trash root directory (default: config.trash.root_dir)",
		"      --no-shell-rm          Skip shell rm integration (default: false)",
		"      --no-vscode            Skip VS Code delete integration (default: false)",
		"      --no-file-explorer     Skip file explorer integration (default: false)",
		"      --dry-run              Preview only (default: false)",
	},
}

func runTrash(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, trashHelp)
	}

	workspacePath, configPath, _, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if len(args) == 0 {
		return printUsageError(stderr, trashHelp)
	}

	sub := args[0]
	subArgs := args[1:]
	switch sub {
	case "enable", "setup":
		fs := flag.NewFlagSet("trash-enable", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		rootDir := fs.String("root-dir", cfg.Trash.RootDir, "trash root directory")
		noShell := fs.Bool("no-shell-rm", false, "skip shell rm configuration")
		noVSCode := fs.Bool("no-vscode", false, "skip vscode delete configuration")
		noExplorer := fs.Bool("no-file-explorer", false, "skip file explorer delete configuration")
		dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if *dryRun {
			globals.dryRun = true
		}

		plan := Plan{Command: "trash.enable"}
		plan.Actions = append(plan.Actions, Action{
			ID:          "trash-setup",
			Description: "Enable soft-delete integrations",
			Execute: func() error {
				_, err := trash.Setup(trash.SetupOptions{
					RootDir:      *rootDir,
					ShellRM:      !*noShell,
					VSCodeDelete: !*noVSCode,
					FileExplorer: !*noExplorer,
					DryRun:       false,
				})
				return err
			},
		})

		if !*noShell {
			plan.Actions = append(plan.Actions, Action{
				ID:          "trash-record-provisions",
				Description: "Record trash provisions",
				Execute: func() error {
					home, _ := os.UserHomeDir()
					if home == "" {
						return nil
					}
					provPath := provision.LedgerPath(workspacePath)
					scriptPath := filepath.Join(home, ".local", "bin", "ws-trash-rm")
					_ = provision.Record(provPath, provision.Entry{
						Type:    provision.TypeFile,
						Path:    scriptPath,
						Command: "trash enable",
					})
					aliasLine := "alias rm='ws-trash-rm'"
					for _, rc := range []string{
						filepath.Join(home, ".bashrc"),
						filepath.Join(home, ".zshrc"),
					} {
						_ = provision.Record(provPath, provision.Entry{
							Type:    provision.TypeConfigLine,
							Path:    rc,
							Line:    aliasLine,
							Command: "trash enable",
						})
					}
					return nil
				},
			})
		}

		if !*noExplorer {
			plan.Actions = append(plan.Actions, Action{
				ID:          "trash-record-explorer-provision",
				Description: "Record file-explorer trash symlink provision",
				Execute: func() error {
					home, _ := os.UserHomeDir()
					if home == "" {
						return nil
					}
					symlinkPath := filepath.Join(home, ".local", "share", "Trash")
					provPath := provision.LedgerPath(workspacePath)
					_ = provision.Record(provPath, provision.Entry{
						Type:    provision.TypeSymlink,
						Path:    symlinkPath,
						Target:  *rootDir,
						Command: "trash enable",
					})
					return nil
				},
			})
		}

		planResult := RunPlan(plan, stdin, stdout, globals)

		if globals.json {
			result, _ := trash.Setup(trash.SetupOptions{
				RootDir:      *rootDir,
				ShellRM:      !*noShell,
				VSCodeDelete: !*noVSCode,
				FileExplorer: !*noExplorer,
				DryRun:       true,
			})
			return writeJSON(stdout, stderr, "trash.enable", map[string]any{
				"result":  result,
				"actions": planResult.Actions,
			})
		}

		out := textOut(globals, stdout)
		nc := globals.noColor
		if globals.dryRun {
			fmt.Fprintln(out, style.ResultInfo(nc, "Would configure soft-delete integrations."))
			style.KV(out, "Trash root", *rootDir, nc)
		} else if planResult.ExecutedCount() > 0 {
			fmt.Fprintln(out, style.ResultSuccess(nc, "Trash enabled."))
		}
		return planResult.ExitCode()

	case "disable":
		fs := flag.NewFlagSet("trash-reset", flag.ContinueOnError)
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

		plan := Plan{Command: "trash.reset"}
		plan.Actions = append(plan.Actions, Action{
			ID:          "trash-reset",
			Description: "Reset trash setup (remove script and aliases)",
			Execute: func() error {
				_, err := trash.Reset(trash.ResetOptions{
					WorkspacePath: workspacePath,
					DryRun:        false,
				})
				return err
			},
		})

		planResult := RunPlan(plan, stdin, stdout, globals)
		if globals.json {
			return writeJSONDryRun(stdout, stderr, "trash.reset", globals.dryRun, map[string]any{
				"actions": planResult.Actions,
			})
		}
		out := textOut(globals, stdout)
		nc := globals.noColor
		if globals.dryRun {
			fmt.Fprintln(out, style.ResultInfo(nc, "Dry run complete. No changes made."))
		} else if planResult.ExecutedCount() > 0 {
			fmt.Fprintln(out, style.ResultSuccess(nc, "Trash reset."))
		}
		return planResult.ExitCode()

	case "status":
		fs := flag.NewFlagSet("trash-status", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		status, err := trash.GetStatus(cfg.Trash.RootDir)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		scanResult, err := trash.Scan(trash.ScanOptions{
			RootDir:    cfg.Trash.RootDir,
			WarnSizeMB: cfg.Trash.WarnSizeMB,
		})
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		if globals.json {
			return writeJSON(stdout, stderr, "trash.status", map[string]any{
				"integrations": status,
				"scan":         scanResult,
			})
		}

		out := textOut(globals, stdout)
		nc := globals.noColor
		style.KV(out, "Trash root", style.Infof(nc, "%s", status.RootDir), nc)
		fmt.Fprintln(out)
		fmt.Fprintf(out, "  %s  shell-rm\n", statusBadge(status.ShellRMConfigured, nc))
		fmt.Fprintf(out, "  %s  vscode-delete\n", statusBadge(status.VSCodeConfigured, nc))
		fmt.Fprintf(out, "  %s  file-explorer\n", statusBadge(status.FileExplorerConfigured, nc))

		if scanResult.Exists {
			fmt.Fprintln(out)
			style.KV(out, "Files", fmt.Sprintf("%d", scanResult.FileCount), nc)
			style.KV(out, "Size", style.HumanBytes(scanResult.SizeBytes), nc)
			style.KV(out, "Threshold", style.HumanBytes(int64(scanResult.WarnSizeMB)*1024*1024), nc)
			if scanResult.OverLimit {
				fmt.Fprintln(out)
				fmt.Fprintln(out, style.Warningf(nc, "Trash size exceeds threshold (%s > %s)", style.HumanBytes(scanResult.SizeBytes), style.HumanBytes(int64(scanResult.WarnSizeMB)*1024*1024)))
			}
		}

		if status.WarningCount() > 0 || scanResult.OverLimit {
			return 2
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown trash subcommand: %s\n", sub)
		return 1
	}
}

func boolWord(v bool) string {
	if v {
		return "configured"
	}
	return "skipped"
}

func boolBadge(v bool, noColor bool) string {
	if v {
		return style.Badge("configured", noColor)
	}
	return style.Badge("skipped", noColor)
}

func statusWord(v bool) string {
	if v {
		return "OK"
	}
	return "WARNING"
}

func statusBadge(v bool, noColor bool) string {
	if v {
		return style.Badge("ok", noColor)
	}
	return style.Badge("warning", noColor)
}
