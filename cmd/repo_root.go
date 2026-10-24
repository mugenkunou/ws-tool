package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// repoRoot dispatches `ws repo root <add|ls|rm>`.
func repoRoot(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		renderHelpFor([]string{"repo", "root"}, globals.noColor, stdout, stderr)
		return 0
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "add":
		return repoRootAdd(rest, globals, stdin, stdout, stderr)
	case "ls":
		return repoRootLs(rest, globals, stdout, stderr)
	case "rm":
		return repoRootRm(rest, globals, stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "unknown command: repo root %s\n", verb)
	fmt.Fprintln(stderr, "usage: ws repo root <add|ls|rm> …")
	return 1
}

// rootArg parses the single <dir> argument of root add and root rm.
func rootArg(verb string, args []string, globals *globalFlags, stderr io.Writer) (string, bool) {
	fs := flag.NewFlagSet("repo-root-"+verb, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return "", false
	}
	if len(fs.Args()) != 1 {
		fmt.Fprintf(stderr, "usage: ws repo root %s <dir>\n", verb)
		return "", false
	}
	abs, err := wsconfig.ExpandUserPath(fs.Args()[0])
	if err != nil {
		fmt.Fprintf(stderr, "invalid path: %s\n", err.Error())
		return "", false
	}
	return abs, true
}

func repoRootAdd(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	abs, ok := rootArg("add", args, &globals, stderr)
	if !ok {
		return 1
	}
	e, ok := loadRepoEnv(globals, stdin, stdout, stderr)
	if !ok {
		return 1
	}
	info, err := os.Stat(abs)
	switch {
	case err != nil:
		fmt.Fprintf(stderr, "no such directory: %s\n", abs)
		return 1
	case !info.IsDir():
		fmt.Fprintf(stderr, "not a directory: %s\n", abs)
		return 1
	case !repo.IsWithin(abs, e.workspace):
		fmt.Fprintf(stderr, "outside the workspace: %s (discovery never lists repos outside it)\n", abs)
		return 1
	}
	if slices.Contains(e.roots(), abs) {
		fmt.Fprintf(stderr, "already a repo root: %s\n", abs)
		return 1
	}
	rel, err := filepath.Rel(filepath.Clean(e.workspace), abs)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	stored := filepath.ToSlash(rel)

	actions := []Action{{
		ID:          "root-add-" + abs,
		Description: "Add repo root " + abs,
		Execute: func() error {
			cfg, err := wsconfig.Load(e.configPath)
			if err != nil {
				return err
			}
			cfg.Repo.Roots = append(cfg.Repo.Roots, stored)
			return wsconfig.Save(e.configPath, cfg)
		},
	}}
	return e.finishRoot("repo.root.add", abs, actions)
}

func repoRootRm(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	abs, ok := rootArg("rm", args, &globals, stderr)
	if !ok {
		return 1
	}
	e, ok := loadRepoEnv(globals, stdin, stdout, stderr)
	if !ok {
		return 1
	}
	roots := e.roots()
	if !slices.Contains(roots, abs) {
		fmt.Fprintf(stderr, "not a repo root: %s\n", abs)
		return 1
	}
	remaining := 0
	for _, r := range roots {
		if r != abs {
			remaining++
		}
	}
	if remaining == 0 {
		// An empty list means the whole workspace, so removing the last root
		// would widen discovery instead of narrowing it.
		fmt.Fprintln(stderr, "cannot remove the last repo root; add another first")
		return 1
	}

	actions := []Action{{
		ID:          "root-rm-" + abs,
		Description: "Remove repo root " + abs,
		Execute: func() error {
			cfg, err := wsconfig.Load(e.configPath)
			if err != nil {
				return err
			}
			cfg.Repo.Roots = slices.DeleteFunc(slices.Clone(cfg.Repo.Roots), func(r string) bool {
				resolved, err := wsconfig.ResolvePath(e.workspace, r)
				return err == nil && resolved == abs
			})
			return wsconfig.Save(e.configPath, cfg)
		},
	}}
	return e.finishRoot("repo.root.rm", abs, actions)
}

// finishRoot runs the one-action plan of root add and root rm.
func (e *repoEnv) finishRoot(command, abs string, actions []Action) int {
	planResult := RunPlan(Plan{Command: command, Actions: actions}, e.stdin, e.stdout, e.globals)
	if e.globals.json {
		return writeJSONDryRun(e.stdout, e.stderr, command, e.globals.dryRun, map[string]any{
			"path": abs, "actions": planResult.Actions,
		})
	}
	return planResult.ExitCode()
}

func repoRootLs(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("repo-root-ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "usage: ws repo root ls")
		return 1
	}
	e, ok := loadRepoEnv(globals, nil, stdout, stderr)
	if !ok {
		return 1
	}
	roots := e.roots()
	if globals.json {
		return writeJSON(stdout, stderr, "repo.root.ls", map[string]any{"roots": roots})
	}
	out := textOut(globals, stdout)
	for _, r := range roots {
		fmt.Fprintln(out, r)
	}
	return 0
}
