package cmd

import (
	"flag"
	"fmt"
	"io"
	"slices"

	"github.com/mugenkunou/ws-tool/internal/repo"
)

// ── git and run ────────────────────────────────────────────────────

func repoGit(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	return repoExec("git", []string{"git"}, args, globals, stdin, stdout, stderr)
}

func repoRun(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	return repoExec("run", nil, args, globals, stdin, stdout, stderr)
}

// repoExec is `ws repo git` and `ws repo run`: it runs prefix plus the
// command words in each selected repo. One repo on a terminal gets the
// terminal; otherwise the plan runs each repo with its output captured.
func repoExec(verb string, prefix, args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	usage := "usage: ws repo run [repo…] -- <cmd…>"
	if verb == "git" {
		usage = "usage: ws repo git [repo…] -- <git args…>"
	}
	var filters repoFilterFlags
	var yes bool
	fs := flag.NewFlagSet("repo-"+verb, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerRepoFilterFlags(fs, &filters)
	fs.BoolVar(&yes, "y", false, "run without asking")
	fs.BoolVar(&yes, "yes", false, "run without asking")
	registerGlobalFlags(fs, &globals)

	// With `--`, the words before it are targets and flags in any order, and
	// everything after it is the command. Without one, ws's flags come first
	// and the first other word starts the command, so the command keeps its
	// own flags (`ws repo git log --oneline`) and there are no targets.
	var targets, command []string
	if i := slices.Index(args, "--"); i >= 0 {
		if err := parseInterspersed(fs, args[:i]); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		targets, command = fs.Args(), args[i+1:]
	} else {
		if err := fs.Parse(args); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		command = fs.Args()
	}
	if len(command) == 0 {
		fmt.Fprintln(stderr, usage)
		return 1
	}
	command = append(slices.Clone(prefix), command...)
	if err := filters.resolve(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	globals.autoAccept = globals.autoAccept || yes
	e, ok := loadRepoEnv(globals, stdin, stdout, stderr)
	if !ok {
		return 1
	}
	repos, ok := e.targets(targets, filters)
	if !ok {
		return 1
	}
	repos = e.narrow(repos, filters)

	// One repo on a terminal: the command runs there as if typed in it, so a
	// pager, an editor, `rebase -i` and `add -p` work.
	if len(repos) == 1 && !globals.dryRun && !globals.json && !globals.quiet && repoInteractive(stdin, stdout) {
		res := repo.RunAttached(e.workspace, repos[0], command, stdin, stdout, stderr)
		if res.ExitCode == -1 {
			fmt.Fprintln(stderr, "ws: "+res.Error)
		}
		if !res.Success {
			return 1
		}
		return 0
	}

	var actions []Action
	byPath := map[string]*repo.OperationResult{}
	for _, r := range repos {
		var res repo.OperationResult
		actions = append(actions, Action{
			ID:          "run-" + r.Path,
			Description: "Run in " + e.short(r.Path),
			Execute: func() error {
				res = repo.RunOne(e.workspace, r, command)
				byPath[r.Path] = &res
				if !res.Success {
					return fmt.Errorf("%s", res.Error)
				}
				return nil
			},
			Output: func() string { return res.Output },
		})
	}
	if len(repos) == 0 {
		fmt.Fprintln(quietOut(globals, stdout), "No repositories found.")
	}

	planResult := RunPlan(Plan{Command: "repo." + verb, Actions: actions}, stdin, stdout, globals)
	if globals.json {
		type result struct {
			Path     string `json:"path"`
			ExitCode int    `json:"exit_code"`
			Output   string `json:"output"`
		}
		out := []result{}
		for _, r := range repos {
			if res, ran := byPath[r.Path]; ran {
				out = append(out, result{Path: res.Path, ExitCode: res.ExitCode, Output: res.Output})
			}
		}
		return writeJSONDryRun(stdout, stderr, "repo."+verb, globals.dryRun, map[string]any{
			"command": command, "actions": planResult.Actions, "results": out,
		})
	}
	return planResult.ExitCode()
}
