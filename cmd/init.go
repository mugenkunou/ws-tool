package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/skill"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/tui"
	"github.com/mugenkunou/ws-tool/internal/workspace"
)

// runInit makes a directory a ws workspace (spec "ws init"). It is idempotent:
// it creates only what is missing, fills ws.json with any keys an older ws did
// not write, and never replaces ws.json, .megaignore or references/RUNBOOKS.md.
func runInit(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "ws init takes no arguments (got %q); use --workspace\n", fs.Arg(0))
		return 1
	}

	workspacePath, _, err := resolveWorkspacePaths(globals)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	p, err := workspace.PlanInit(workspacePath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	pl := Plan{Command: "init"}
	if len(p.MissingDirs) > 0 {
		pl.Actions = append(pl.Actions, Action{
			ID:          "layout",
			Description: "Create " + describeDirs(workspacePath, p.MissingDirs),
			Execute:     p.CreateDirs,
		})
	}
	switch {
	case p.ConfigMissing:
		pl.Actions = append(pl.Actions, Action{
			ID:          "ws.json",
			Description: "Write " + p.Config,
			Execute:     p.WriteConfig,
		})
	case len(p.MissingKeys) > 0:
		pl.Actions = append(pl.Actions, Action{
			ID:          "ws.json",
			Description: fmt.Sprintf("Fill in %d missing %s in %s; values you set are kept", len(p.MissingKeys), pick(len(p.MissingKeys), "key", "keys"), p.Config),
			Execute:     p.FillConfig,
		})
	}
	if p.MegaignoreMissing {
		pl.Actions = append(pl.Actions, Action{
			ID:          "megaignore",
			Description: "Write " + p.Megaignore,
			Execute:     p.WriteMegaignore,
		})
	}
	if p.RunbooksMissing {
		pl.Actions = append(pl.Actions, Action{
			ID:          "runbooks",
			Description: "Write " + p.Runbooks,
			Execute:     p.WriteRunbooks,
		})
	}

	home, homeErr := os.UserHomeDir()
	if homeErr == nil && !globals.dryRun {
		refreshBuiltins(home, stderr)
	}

	if p.Empty() {
		if globals.json {
			return writeJSON(stdout, stderr, "init", initData(p, nil, PlanResult{}))
		}
		fmt.Fprintln(textOut(globals, stdout), "Workspace already initialized: "+workspacePath)
		if homeErr == nil {
			offerBuiltinSkills(globals, stdin, stdout, home, workspacePath)
		}
		return 0
	}

	res := RunPlan(pl, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "init", globals.dryRun, initData(p, &pl, res))
	}
	if res.WasExecuted("layout") || res.WasExecuted("ws.json") || res.WasExecuted("megaignore") || res.WasExecuted("runbooks") {
		fmt.Fprintln(textOut(globals, stdout), style.ResultSuccess(globals.noColor, "Workspace initialized: %s", workspacePath))
	}
	if homeErr == nil && res.ExitCode() == 0 {
		offerBuiltinSkills(globals, stdin, stdout, home, workspacePath)
	}
	return res.ExitCode()
}

// initStdinIsTerminal reports whether init's stdin is a terminal (not
// /dev/null, which is a character device too). Only then does init ask; a
// test's reader never is. It is a variable so tests can stand in.
var initStdinIsTerminal = func(stdin io.Reader) bool {
	f, ok := stdin.(*os.File)
	return ok && tui.IsTTY(f.Fd())
}

// offerBuiltinSkills ends `ws init`: in a terminal it offers to link the
// built-in skills into the detected tools (y/N, default no); otherwise it
// prints a one-line hint. It says nothing when every one is already there.
func offerBuiltinSkills(globals globalFlags, stdin io.Reader, stdout io.Writer, home, workspacePath string) {
	if globals.json || globals.quiet || globals.dryRun {
		return
	}
	var builtins []skill.Skill
	var names []string
	for _, s := range skill.Builtins(home) {
		if !s.Broken {
			builtins = append(builtins, s)
			names = append(names, s.Name)
		}
	}
	var tools []skill.Tool
	var toolNames []string
	missing := 0
	for _, t := range skill.Tools {
		if !t.Detected(home) {
			continue
		}
		tools = append(tools, t)
		toolNames = append(toolNames, t.Name)
		for _, s := range builtins {
			if !skill.Installed(home, t, s.Name) {
				missing++
			}
		}
	}
	if len(builtins) == 0 || (len(tools) > 0 && missing == 0) {
		return
	}
	nc := globals.noColor
	if len(tools) == 0 || !initStdinIsTerminal(stdin) {
		fmt.Fprintf(stdout, "Built-in skills (%s): `ws skill install` links them into a coding tool.\n", strings.Join(names, ", "))
		return
	}
	fmt.Fprintf(stdout, "Install the built-in skills (%s) into %s? %s ", strings.Join(names, ", "), strings.Join(toolNames, ", "), style.Mutedf(nc, "[y/N]"))
	line, err := readLine(stdin)
	if err != nil {
		fmt.Fprintln(stdout)
	}
	if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
		return
	}
	for _, t := range tools {
		for _, s := range builtins {
			if skill.Installed(home, t, s.Name) {
				continue
			}
			if err := skill.Install(home, workspacePath, t, s); err != nil {
				fmt.Fprintf(stdout, "  %s %s → %s/%s: %v\n", style.IconCross(nc), s.Name, t.Dir, s.Name, err)
				continue
			}
			fmt.Fprintf(stdout, "  %s Link %s → %s/%s\n", style.IconCheck(nc), s.Name, t.Dir, s.Name)
		}
	}
}

// initData is data for --json: absolute paths created by this run and those
// that were already there.
func initData(p workspace.InitPlan, pl *Plan, res PlanResult) map[string]any {
	created := []string{}
	existing := append([]string{}, p.Existing...)
	done := func(id string) bool { return res.WasExecuted(id) }
	if done("layout") {
		created = append(created, p.MissingDirs...)
	}
	if p.ConfigMissing && done("ws.json") {
		created = append(created, p.Config)
	}
	if p.MegaignoreMissing && done("megaignore") {
		created = append(created, p.Megaignore)
	}
	if p.RunbooksMissing && done("runbooks") {
		created = append(created, p.Runbooks)
	}
	actions := res.Actions
	if actions == nil {
		actions = []ActionStatus{}
	}
	return map[string]any{
		"workspace": p.Workspace,
		"config":    p.Config,
		"created":   created,
		"existing":  existing,
		"actions":   actions,
	}
}

// describeDirs names the directories init would create, relative to the
// workspace, e.g. `3 directories (notes, data, archive)`.
func describeDirs(workspacePath string, dirs []string) string {
	rel := make([]string, len(dirs))
	for i, d := range dirs {
		rel[i] = strings.TrimPrefix(strings.TrimPrefix(d, workspacePath), "/")
	}
	return fmt.Sprintf("%d %s in %s (%s)", len(dirs), pick(len(dirs), "directory", "directories"), workspacePath, strings.Join(rel, ", "))
}

func pick(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
