package cmd

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/workspace"
)

// runInit makes a directory a ws workspace (spec "ws init"). It is idempotent:
// it creates only what is missing, fills ws.json with any keys an older ws did
// not write, and never replaces ws.json or .megaignore.
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

	if p.Empty() {
		if globals.json {
			return writeJSON(stdout, stderr, "init", initData(p, nil, PlanResult{}))
		}
		fmt.Fprintln(textOut(globals, stdout), "Workspace already initialized: "+workspacePath)
		return 0
	}

	res := RunPlan(pl, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "init", globals.dryRun, initData(p, &pl, res))
	}
	if res.WasExecuted("layout") || res.WasExecuted("ws.json") || res.WasExecuted("megaignore") {
		fmt.Fprintln(textOut(globals, stdout), style.ResultSuccess(globals.noColor, "Workspace initialized: %s", workspacePath))
	}
	return res.ExitCode()
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
