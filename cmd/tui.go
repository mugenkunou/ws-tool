package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/tui"
)

type tuiSummary struct {
	Workspace  string `json:"workspace"`
	Violations int    `json:"violations"`
	Dotfiles   int    `json:"dotfiles"`
	Sessions   int    `json:"sessions"`
}

var tuiHelp = cmdHelp{
	Usage:       "ws tui",
	Description: "Interactive full-screen workspace health dashboard.",
}

func runTUI(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, tuiHelp)
	}

	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	workspacePath, configPath, manifestPath, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	// JSON mode: run scans and output structured data.
	if globals.json {
		data := tui.LoadDashboard(workspacePath, configPath, manifestPath, cfg)
		summary := tuiSummary{
			Workspace:  workspacePath,
			Violations: len(data.Violations),
			Dotfiles:   len(data.DotfileIssues),
			Sessions:   len(data.Sessions),
		}
		return writeJSON(stdout, stderr, "tui", summary)
	}

	// Interactive TUI: requires a real TTY.
	inFile := os.Stdin
	outFile, outOK := stdout.(*os.File)
	if outOK && tui.IsTTY(inFile.Fd()) && tui.IsTTY(outFile.Fd()) {
		app := &tui.App{
			WorkspacePath: workspacePath,
			ConfigPath:    configPath,
			ManifestPath:  manifestPath,
			Cfg:           cfg,
			NoColor:       globals.noColor,
			Stdin:         inFile,
			Stdout:        outFile,
		}
		code := app.Run()
		if code >= 0 {
			return code
		}
		// Fallthrough to summary if TUI cannot start.
	}

	// Non-TTY fallback: static summary.
	return runTUISummary(workspacePath, configPath, manifestPath, cfg, globals, stdout)
}

// runTUISummary renders the non-interactive summary (piped output or tiny terminal).
func runTUISummary(workspacePath, configPath, manifestPath string, cfg config.Config, globals globalFlags, stdout io.Writer) int {
	data := tui.LoadDashboard(workspacePath, configPath, manifestPath, cfg)

	out := textOut(globals, stdout)
	nc := globals.noColor
	style.Header(out, style.Boldf(nc, "ws")+" "+style.Mutedf(nc, "dashboard"), nc)
	style.KV(out, "Workspace", style.Infof(nc, "%s", data.Workspace), nc)

	dotLabel := style.Successf(nc, "0 issues")
	if len(data.DotfileIssues) > 0 {
		dotLabel = style.Warningf(nc, "%d issue(s)", len(data.DotfileIssues))
	}
	style.KV(out, "Dotfiles", dotLabel, nc)

	totalViolations := len(data.Violations)
	vLabel := style.Successf(nc, "0 violations")
	if totalViolations > 0 {
		vLabel = style.Warningf(nc, "%d violation(s)", totalViolations)
	}
	style.KV(out, "Violations", vLabel, nc)

	style.KV(out, "Sessions", fmt.Sprintf("%d", len(data.Sessions)), nc)

	fmt.Fprintln(out, style.Divider(nc))
	if totalViolations == 0 && len(data.DotfileIssues) == 0 {
		fmt.Fprintln(out, style.ResultSuccess(nc, "Status: %s", style.Badge("healthy", nc)))
	} else {
		fmt.Fprintln(out, style.ResultWarning(nc, "Status: %s  "+style.Mutedf(nc, "(run `ws scan` and `ws fix`)"), style.Badge("attention", nc)))
	}

	if totalViolations > 0 || len(data.DotfileIssues) > 0 {
		return 2
	}
	return 0
}
