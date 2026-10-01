package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/app"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
	"github.com/mugenkunou/ws-tool/internal/workspace"
)

var tuiHelp = cmdHelp{
	Usage:       "ws tui",
	Description: "Open the interactive terminal UI: dashboard, repos, dotfiles, scratch,\nlogs, capture, ignore, secrets, cron, and trash.",
}

func runTUI(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
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
	if err := resolveGlobalPaths(&globals); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	inFile, inOK := stdin.(*os.File)
	outFile, outOK := stdout.(*os.File)
	if !inOK || !outOK || !isTerminal(inFile.Fd()) || !isTerminal(outFile.Fd()) {
		fmt.Fprintln(stderr, "ws tui requires an interactive terminal.")
		return 1
	}

	overrides := workspace.PathOverrides{
		Workspace: globals.workspace,
		Config:    globals.config,
		Manifest:  globals.manifest,
	}
	e, loadErr := env.Load(overrides)

	p := tea.NewProgram(app.New(overrides, e, loadErr), tea.WithInput(inFile), tea.WithOutput(outFile))
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	fmt.Fprintln(stdout, theme.Icon(theme.IconWave)+"See you next time.")
	return 0
}
