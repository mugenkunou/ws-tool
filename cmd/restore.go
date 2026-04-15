package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/provision"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/trash"
)

type restoreResult struct {
	WorkspacePath        string `json:"workspace_path"`
	TrashConfigured      bool   `json:"trash_configured"`
	DotfileFixed         int    `json:"dotfile_fixed"`
	DotfileFailed        int    `json:"dotfile_failed"`
	IgnoreGenerated      bool   `json:"ignore_generated"`
}

var restoreHelp = cmdHelp{
	Usage: "ws restore [--dry-run]",
	Flags: []string{"      --dry-run    Preview only (default: false)"},
}

func runRestore(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, restoreHelp)
	}

	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
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

	configPath := globals.config
	if configPath == "" {
		configPath = filepath.Join(resolvedWorkspace, "ws", "config.json")
	}
	manifestPath := globals.manifest
	if manifestPath == "" {
		manifestPath = filepath.Join(resolvedWorkspace, "ws", "manifest.json")
	}

	// Pre-check: workspace must already be initialized (config.json must exist).
	// restore is for synced workspaces on new machines, not for first-time setup.
	if _, err := os.Stat(configPath); err != nil {
		nc := globals.noColor
		if globals.json {
			return writeJSONDryRun(stdout, stderr, "restore", *dryRun, map[string]any{
				"error": "workspace not initialized",
				"hint":  "run 'ws init' first to initialize the workspace",
			})
		}
		fmt.Fprintln(stderr, style.ResultError(nc, "Workspace is not initialized: %s", configPath))
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, style.Mutedf(nc, "  ws restore only works on an already-initialized workspace."))
		fmt.Fprintln(stderr, style.Mutedf(nc, "  Run 'ws init' first to set up the workspace, then run 'ws restore'."))
		return 1
	}

	if !*dryRun && !confirm(stdin, stdout, globals, fmt.Sprintf("Restore workspace at %s?", resolvedWorkspace)) {
		fmt.Fprintln(stderr, "Aborted.")
		return 1
	}
	if *dryRun {
		globals.dryRun = true
	}

	out := textOut(globals, stdout)
	nc := globals.noColor
	result := restoreResult{WorkspacePath: resolvedWorkspace}

	if !globals.json {
		if globals.dryRun {
			style.Header(out, style.IconRestore(nc)+" WORKSPACE RESTORE "+style.Badge("dry-run", nc), nc)
		} else {
			style.Header(out, style.IconRestore(nc)+" WORKSPACE RESTORE", nc)
		}
		fmt.Fprintln(out, style.Mutedf(nc, "Steps: trash enable → dotfile fix → ignore generate"))
		fmt.Fprintln(out)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	ignorePath := filepath.Join(resolvedWorkspace, ".megaignore")

	// Build plan: one action per restore step.
	plan := Plan{Command: "restore"}

	plan.Actions = append(plan.Actions, Action{
		ID:          "trash-enable",
		Description: "[1/3] Trash enable",
		Execute: func() error {
			_, err := trash.Setup(trash.SetupOptions{RootDir: cfg.Trash.RootDir, ShellRM: true, VSCodeDelete: true, FileExplorer: true, DryRun: false})
			if err != nil {
				return err
			}
			result.TrashConfigured = true
			home, _ := os.UserHomeDir()
			if home != "" {
				provPath := provision.LedgerPath(resolvedWorkspace)
				scriptPath := filepath.Join(home, ".local", "bin", "ws-trash-rm")
				_ = provision.Record(provPath, provision.Entry{Type: provision.TypeFile, Path: scriptPath, Command: "trash enable"})
				aliasLine := "alias rm='ws-trash-rm'"
				for _, rc := range []string{filepath.Join(home, ".bashrc"), filepath.Join(home, ".zshrc")} {
					_ = provision.Record(provPath, provision.Entry{Type: provision.TypeConfigLine, Path: rc, Line: aliasLine, Command: "trash enable"})
				}
			}
			return nil
		},
	})

	plan.Actions = append(plan.Actions, Action{
		ID:          "dotfile-fix",
		Description: "[2/3] Dotfiles",
		Execute: func() error {
			fixResult, err := dotfile.Fix(dotfile.FixOptions{
				WorkspacePath: resolvedWorkspace,
				ManifestPath:  manifestPath,
				DryRun:        globals.dryRun,
			})
			if err != nil {
				return err
			}
			result.DotfileFixed = len(fixResult.Fixed)
			result.DotfileFailed = len(fixResult.Failed)
			if !globals.json {
				for _, msg := range fixResult.Messages {
					fmt.Fprintln(out, "  "+msg)
				}
			}
			return nil
		},
	})

	plan.Actions = append(plan.Actions, Action{
		ID:          "ignore-generate",
		Description: "[3/3] Ignore rules",
		Execute: func() error {
			if _, err := os.Stat(ignorePath); err != nil {
				userRulesPath := ignore.UserRulesPath(resolvedWorkspace)
				userRules, loadErr := ignore.LoadUserRules(userRulesPath)
				if loadErr != nil {
					userRules = ignore.DefaultUserRules()
				}
				if err := ignore.WriteMegaignore(ignorePath, userRules); err != nil {
					return err
				}
				_ = provision.Record(provision.LedgerPath(resolvedWorkspace), provision.Entry{
					Type:    provision.TypeFile,
					Path:    ignorePath,
					Command: "restore",
				})
			}
			result.IgnoreGenerated = true
			return nil
		},
	})



	planResult := RunPlan(plan, stdin, stdout, globals)

	// ── Footer ──
	if !globals.json {
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.Divider(nc))
		if planResult.HasFailures() {
			fmt.Fprintln(out, style.ResultWarning(nc, "Restore completed with issues."))
		} else {
			fmt.Fprintln(out, style.ResultSuccess(nc, "Restore complete"))
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.Mutedf(nc, "Your machine is configured. Suggested next steps:"))
		fmt.Fprintln(out, style.Mutedf(nc, "  ws completions bash    Generate shell completions"))
		fmt.Fprintln(out, style.Mutedf(nc, "  ws tui                 Open the workspace dashboard"))
	}

	if globals.json {
		return writeJSONDryRun(stdout, stderr, "restore", globals.dryRun, map[string]any{
			"workspace_path":   result.WorkspacePath,
			"trash_configured": result.TrashConfigured,
			"ignore_generated": result.IgnoreGenerated,
			"actions":          planResult.Actions,
		})
	}

	if result.DotfileFailed > 0 {
		return 3
	}
	return planResult.ExitCode()
}
