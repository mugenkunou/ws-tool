package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/trash"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

var trashHelp = cmdHelp{
	Usage: "ws trash <enable|setup|disable|ls|empty|purge|rm|status>",
	Subcommands: []string{
		"  enable   Configure soft-delete integrations",
		"  setup    Alias for enable",
		"  disable  Remove soft-delete integrations",
		"  ls       List trashed items, newest first",
		"  empty    Remove all entries from trash root",
		"  purge    Permanently delete trash items older than N days (--older-than)",
		"  rm       Permanently delete a single trash item by name",
		"  status   Show integration status and trash size",
	},
	SetupFlags: []string{
		"      --root-dir string      Trash root directory (default: config.trash.root_dir)",
		"      --no-shell-rm          Skip shell rm integration (default: false)",
		"      --no-vscode            Skip VS Code delete integration (default: false)",
		"      --no-file-explorer     Skip file explorer integration (default: false)",
		"      --dry-run              Preview only (default: false)",
	},
	Flags: listFlagHelp("ls", trash.Schema(resource.CLIView()).Keys()),
}

func runTrash(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, trashHelp)
	}
	if len(args) == 0 {
		return printCmdHelp(stdout, trashHelp)
	}

	workspacePath, configPath, _, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	// In the phase-1 layout, trash.root_dir is not in ws.json; use the XDG default.
	cfgTrashRoot, err := wsconfig.ExpandUserPath("~/.local/share/Trash")
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	// Keep a sentinel for the WarnSizeMB field (not in ws.json yet).
	const trashWarnSizeMB = 1024

	sub := args[0]
	subArgs := args[1:]
	switch sub {
	case "enable", "setup":
		fs := flag.NewFlagSet("trash-enable", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		rootDir := fs.String("root-dir", cfgTrashRoot, "trash root directory")
		noShell := fs.Bool("no-shell-rm", false, "skip shell rm configuration")
		noVSCode := fs.Bool("no-vscode", false, "skip vscode delete configuration")
		noExplorer := fs.Bool("no-file-explorer", false, "skip file explorer delete configuration")
		dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
		registerGlobalFlags(fs, &globals)
		if err := parseInterspersed(fs, subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if *rootDir != "" { // empty: trash package applies its default
			resolvedRoot, err := wsconfig.ExpandUserPath(*rootDir)
			if err != nil {
				fmt.Fprintf(stderr, "invalid --root-dir: %s\n", err.Error())
				return 1
			}
			*rootDir = resolvedRoot
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
					return trash.RecordShellProvisions(configPath)
				},
			})
		}

		if !*noExplorer {
			plan.Actions = append(plan.Actions, Action{
				ID:          "trash-record-explorer-provision",
				Description: "Record file-explorer trash symlink provision",
				Execute: func() error {
					return trash.RecordExplorerProvision(configPath, *rootDir)
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
		if err := parseInterspersed(fs, subArgs); err != nil {
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

	case "empty":
		fs := flag.NewFlagSet("trash-empty", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		rootDir := fs.String("root-dir", cfgTrashRoot, "trash root directory")
		dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
		registerGlobalFlags(fs, &globals)
		if err := parseInterspersed(fs, subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if *rootDir != "" { // empty: trash package applies its default
			resolvedRoot, err := wsconfig.ExpandUserPath(*rootDir)
			if err != nil {
				fmt.Fprintf(stderr, "invalid --root-dir: %s\n", err.Error())
				return 1
			}
			*rootDir = resolvedRoot
		}
		if *dryRun {
			globals.dryRun = true
		}

		preview, err := trash.Empty(trash.EmptyOptions{RootDir: *rootDir, DryRun: true})
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		result := preview
		plan := Plan{Command: "trash.empty"}
		plan.Actions = append(plan.Actions, Action{
			ID:          "trash-empty",
			Description: "Empty trash directory",
			Execute: func() error {
				execResult, err := trash.Empty(trash.EmptyOptions{RootDir: *rootDir, DryRun: false})
				if err != nil {
					return err
				}
				result = execResult
				return nil
			},
		})

		planResult := RunPlan(plan, stdin, stdout, globals)
		if globals.dryRun || !planResult.WasExecuted("trash-empty") {
			result = preview
		}

		if globals.json {
			return writeJSON(stdout, stderr, "trash.empty", map[string]any{
				"result":  result,
				"actions": planResult.Actions,
			})
		}

		out := textOut(globals, stdout)
		nc := globals.noColor
		if globals.dryRun {
			if !result.Exists {
				fmt.Fprintln(out, style.ResultInfo(nc, "Trash directory does not exist. Nothing to empty."))
			} else if result.EntryCount == 0 {
				fmt.Fprintln(out, style.ResultInfo(nc, "Trash is already empty."))
			} else {
				fmt.Fprintln(out, style.ResultInfo(nc, "Would empty trash."))
				style.KV(out, "Entries", fmt.Sprintf("%d", result.EntryCount), nc)
				style.KV(out, "Files", fmt.Sprintf("%d", result.FileCount), nc)
				style.KV(out, "Size", style.HumanBytes(result.SizeBytes), nc)
			}
		} else if planResult.WasExecuted("trash-empty") {
			if !result.Exists {
				fmt.Fprintln(out, style.ResultInfo(nc, "Trash directory does not exist. Nothing to empty."))
			} else if result.EntryCount == 0 {
				fmt.Fprintln(out, style.ResultInfo(nc, "Trash is already empty."))
			} else {
				fmt.Fprintln(out, style.ResultSuccess(nc, "Trash emptied."))
				style.KV(out, "Entries removed", fmt.Sprintf("%d", result.EntryCount), nc)
				style.KV(out, "Files removed", fmt.Sprintf("%d", result.FileCount), nc)
				style.KV(out, "Freed", style.HumanBytes(result.SizeBytes), nc)
			}
		}
		return planResult.ExitCode()

	case "purge":
		fs := flag.NewFlagSet("trash-purge", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		rootDir := fs.String("root-dir", cfgTrashRoot, "trash root directory")
		olderThan := fs.Int("older-than", 0, "delete items trashed more than this many days ago")
		dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
		registerGlobalFlags(fs, &globals)
		if err := parseInterspersed(fs, subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if *olderThan <= 0 {
			fmt.Fprintln(stderr, "usage: ws trash purge --older-than <days>  (days must be a positive integer; use `ws trash empty` to remove everything)")
			return 1
		}
		if *dryRun {
			globals.dryRun = true
		}
		age := time.Duration(*olderThan) * 24 * time.Hour
		preview, err := trash.Purge(trash.PurgeOptions{RootDir: *rootDir, OlderThan: age, DryRun: true})
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		result := preview
		plan := Plan{Command: "trash.purge"}
		if len(preview.Matched) > 0 {
			plan.Actions = append(plan.Actions, Action{
				ID: "trash-purge",
				Description: fmt.Sprintf("Permanently delete %s older than %d days (%s)",
					pluralize(len(preview.Matched), "trash item"), *olderThan, style.HumanBytes(preview.SizeBytes)),
				Execute: func() error {
					r, err := trash.Purge(trash.PurgeOptions{RootDir: *rootDir, OlderThan: age})
					if err != nil {
						return err
					}
					result = r
					return nil
				},
			})
		}
		planResult := RunPlan(plan, stdin, stdout, globals)
		if globals.dryRun || !planResult.WasExecuted("trash-purge") {
			result = preview
		}
		if globals.json {
			return writeJSONDryRun(stdout, stderr, "trash.purge", globals.dryRun, map[string]any{
				"result":  result,
				"actions": planResult.Actions,
			})
		}
		out := textOut(globals, stdout)
		nc := globals.noColor
		switch {
		case len(preview.Matched) == 0:
			fmt.Fprintln(out, style.ResultInfo(nc, "Nothing older than %d days.", *olderThan))
		case globals.dryRun:
			fmt.Fprintln(out, style.ResultInfo(nc, "Would purge %s older than %d days.", pluralize(len(result.Matched), "item"), *olderThan))
			style.KV(out, "Files", fmt.Sprintf("%d", result.FileCount), nc)
			style.KV(out, "Size", style.HumanBytes(result.SizeBytes), nc)
			for _, e := range result.Matched {
				fmt.Fprintf(out, "  %-40s %8s  %s ago\n", e.Name, style.HumanBytes(e.SizeBytes), formatAgeDays(e.DeletedAt))
			}
			style.KV(out, "Kept", fmt.Sprintf("%d (newer than %dd)", result.Kept, *olderThan), nc)
			if result.Skipped > 0 {
				style.KV(out, "Skipped", fmt.Sprintf("%d (no deletion date)", result.Skipped), nc)
			}
		case planResult.WasExecuted("trash-purge"):
			fmt.Fprintln(out, style.ResultSuccess(nc, "Purged %s, freed %s.", pluralize(len(result.Matched), "item"), style.HumanBytes(result.SizeBytes)))
		}
		return planResult.ExitCode()

	case "rm":
		fs := flag.NewFlagSet("trash-rm", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		rootDir := fs.String("root-dir", cfgTrashRoot, "trash root directory")
		dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
		registerGlobalFlags(fs, &globals)
		if err := parseInterspersed(fs, subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if *dryRun {
			globals.dryRun = true
		}
		if len(fs.Args()) != 1 {
			fmt.Fprintln(stderr, "usage: ws trash rm <item-name>")
			return 1
		}
		itemName := fs.Args()[0]
		// Reject path separators to prevent directory traversal.
		if strings.ContainsAny(itemName, "/\\") {
			fmt.Fprintln(stderr, "item name must not contain path separators; pass just the name shown by `ws trash ls`")
			return 1
		}
		resolvedRoot := *rootDir
		itemPath := resolvedRoot + "/files/" + itemName
		infoPath := resolvedRoot + "/info/" + itemName + ".trashinfo"

		plan := Plan{Command: "trash.rm"}
		plan.Actions = append(plan.Actions, Action{
			ID:          "trash-rm-item",
			Description: "Permanently delete " + itemName + " from trash",
			Execute: func() error {
				if err := os.RemoveAll(itemPath); err != nil {
					return err
				}
				_ = os.Remove(infoPath) // best-effort
				return nil
			},
		})
		planResult := RunPlan(plan, stdin, stdout, globals)
		if globals.json {
			return writeJSONDryRun(stdout, stderr, "trash.rm", globals.dryRun, map[string]any{
				"item":    itemName,
				"actions": planResult.Actions,
			})
		}
		out := textOut(globals, stdout)
		nc := globals.noColor
		if !globals.dryRun && planResult.WasExecuted("trash-rm-item") {
			fmt.Fprintln(out, style.ResultSuccess(nc, "Deleted %s from trash.", itemName))
		}
		return planResult.ExitCode()

	case "ls":
		fs := flag.NewFlagSet("trash-ls", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		rootDir := fs.String("root-dir", cfgTrashRoot, "trash root directory")
		lf := registerListFlags(fs, "")
		registerGlobalFlags(fs, &globals)
		if err := parseInterspersed(fs, subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		items, err := trash.Items(*rootDir)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		return renderList(globals, "trash.ls", trash.Schema(resource.CLIView()), items, lf, "Trash is empty.", stdout, stderr)

	case "status":
		fs := flag.NewFlagSet("trash-status", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		if err := parseInterspersed(fs, subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		status, err := trash.GetStatus(cfgTrashRoot)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		scanResult, err := trash.Scan(trash.ScanOptions{
			RootDir:    cfgTrashRoot,
			WarnSizeMB: trashWarnSizeMB,
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
		if !status.FileExplorerConfigured && status.FileExplorerUnconfiguredReason != "" {
			fmt.Fprintf(out, "     %s\n", style.Mutedf(nc, "↳ %s", status.FileExplorerUnconfiguredReason))
		}

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

func pluralize(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func formatAgeDays(t time.Time) string {
	return fmt.Sprintf("%dd", int(time.Since(t).Hours()/24))
}
