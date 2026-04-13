package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/config"
	ctx "github.com/mugenkunou/ws-tool/internal/context"
	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/trash"
	"github.com/mugenkunou/ws-tool/internal/workspace"
)

var resetHelp = cmdHelp{
	Usage: "ws reset [--dry-run]",
	Flags: []string{
		"      --dry-run    Preview what would be undone (default: false)",
	},
}

func runReset(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, resetHelp)
	}

	fs := flag.NewFlagSet("reset", flag.ContinueOnError)
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

	workspacePath := globals.workspace
	if workspacePath == "" {
		workspacePath = "~/Workspace"
	}
	resolvedWorkspace, err := config.ExpandUserPath(workspacePath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	wsDir := filepath.Join(resolvedWorkspace, "ws")
	nc := globals.noColor
	out := textOut(globals, stdout)

	// Pre-check: ws/ must exist.
	if _, err := os.Stat(wsDir); err != nil {
		if globals.json {
			return writeJSONDryRun(stdout, stderr, "reset", globals.dryRun, map[string]any{
				"error": "workspace not initialized",
				"hint":  "nothing to undo",
			})
		}
		fmt.Fprintln(stderr, style.ResultError(nc, "No ws/ directory found at %s", resolvedWorkspace))
		fmt.Fprintln(stderr, style.Mutedf(nc, "  Nothing to undo."))
		return 1
	}

	// Load provisions for the pre-reset summary.
	entries, err := workspace.Provisions(resolvedWorkspace)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	manifestPath := globals.manifest
	if manifestPath == "" {
		manifestPath = filepath.Join(wsDir, "manifest.json")
	}

	// Display summary before plan.
	if !globals.json {
		if globals.dryRun {
			style.Header(out, style.IconTrash(nc)+" WORKSPACE reset "+style.Badge("dry-run", nc), nc)
		} else {
			style.Header(out, style.IconTrash(nc)+" WORKSPACE reset", nc)
		}
		fmt.Fprintln(out)

		if len(entries) == 0 {
			fmt.Fprintln(out, style.Mutedf(nc, "No provisions recorded. Only ws/ will be removed."))
		} else {
			fmt.Fprintln(out, style.Boldf(nc, "Provisions to undo: %d", len(entries)))
			fmt.Fprintln(out)
			for _, e := range entries {
				label := workspace.UndoActionLabel(e)
				fmt.Fprintf(out, "  %s  %s  %s\n",
					style.Badge(string(e.Type), nc),
					style.Infof(nc, "%s", e.Path),
					style.Mutedf(nc, "(%s)", label),
				)
			}
		}
		fmt.Fprintln(out)
	}

	// Build plan: one action per subsystem reset + ws/ removal.
	plan := Plan{Command: "reset"}

	plan.Actions = append(plan.Actions, Action{
		ID:          "reset-dotfiles",
		Description: "Reset dotfiles (restore originals, remove symlinks)",
		Execute: func() error {
			_, err := dotfile.Reset(dotfile.ResetOptions{
				WorkspacePath: resolvedWorkspace,
				ManifestPath:  manifestPath,
				DryRun:        false,
			})
			return err
		},
	})

	plan.Actions = append(plan.Actions, Action{
		ID:          "reset-context",
		Description: "Remove all context directories",
		Execute: func() error {
			_, err := ctx.Remove(ctx.RemoveOptions{
				WorkspacePath: resolvedWorkspace,
				All:           true,
			})
			return err
		},
	})

	plan.Actions = append(plan.Actions, Action{
		ID:          "reset-trash",
		Description: "Reset trash setup (remove script and aliases)",
		Execute: func() error {
			_, err := trash.Reset(trash.ResetOptions{
				WorkspacePath: resolvedWorkspace,
				DryRun:        false,
			})
			return err
		},
	})

	plan.Actions = append(plan.Actions, Action{
		ID:          "remove-ws-dir",
		Description: fmt.Sprintf("Remove %s", wsDir),
		Execute: func() error {
			return os.RemoveAll(wsDir)
		},
	})

	planResult := RunPlan(plan, stdin, stdout, globals)

	// Footer.
	if !globals.json {
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.Divider(nc))
		if globals.dryRun {
			fmt.Fprintln(out, style.ResultInfo(nc, "Dry run complete. No changes made."))
		} else if planResult.HasFailures() {
			fmt.Fprintln(out, style.ResultWarning(nc, "reset completed with %d failure(s).", planResult.FailedCount()))
		} else {
			fmt.Fprintln(out, style.ResultSuccess(nc, "Workspace reset."))
		}
	}

	if globals.json {
		return writeJSONDryRun(stdout, stderr, "reset", globals.dryRun, map[string]any{
			"workspace_path": resolvedWorkspace,
			"ws_dir_removed": planResult.WasExecuted("remove-ws-dir"),
			"actions":        planResult.Actions,
		})
	}

	return planResult.ExitCode()
}
