package cmd

import (
	"flag"
	"fmt"
	"io"

	ctx "github.com/mugenkunou/ws-tool/internal/context"
	"github.com/mugenkunou/ws-tool/internal/style"
)

var contextHelp = cmdHelp{
	Usage: "ws context <create|list|rm> [args]",
	Flags: []string{
		"      --dry-run           Preview only (default: false)",
		"      --path <project>    Project path (for create/rm)",
		"      --all               Remove all contexts (for rm)",
		"      --update            Refresh list from filesystem scan",
		"      --find              Alias for --update",
	},
}

func runContext(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, contextHelp)
	}

	workspacePath, _, _, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if len(args) == 0 {
		return printUsageError(stderr, contextHelp)
	}

	sub := args[0]
	switch sub {
	case "create":
		// handled below
	case "list":
		fs := flag.NewFlagSet("context-list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		update := fs.Bool("update", false, "refresh from filesystem scan")
		find := fs.Bool("find", false, "alias for --update")
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(args[1:]); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		if *find {
			*update = true
		}

		res, err := ctx.List(ctx.ListOptions{WorkspacePath: workspacePath, Update: *update})
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		if globals.json {
			return writeJSON(stdout, stderr, "context.list", res)
		}

		out := textOut(globals, stdout)
		if len(res.Contexts) == 0 {
			fmt.Fprintln(out, "No contexts found.")
			return 0
		}
		for _, rec := range res.Contexts {
			fmt.Fprintf(out, "%s\t%s\n", rec.ProjectPath, rec.Task)
		}
		if res.Updated {
			fmt.Fprintf(out, "Updated context index: %s\n", res.IndexPath)
		}
		return 0
	case "rm":
		fs := flag.NewFlagSet("context-rm", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		path := fs.String("path", "", "project path")
		all := fs.Bool("all", false, "remove all contexts")
		dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
		registerGlobalFlags(fs, &globals)
		if err := parseInterspersed(fs, args[1:]); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if *dryRun {
			globals.dryRun = true
		}

		remaining := fs.Args()
		var task string
		if !*all {
			if len(remaining) == 0 {
				fmt.Fprintln(stderr, "task name required (or use --all)")
				return 1
			}
			task = remaining[0]
		}

		var res ctx.RemoveResult
		plan := Plan{Command: "context.rm"}
		desc := "Remove all contexts"
		if !*all {
			desc = fmt.Sprintf("Remove context %q", task)
		}
		plan.Actions = append(plan.Actions, Action{
			ID:          "context-rm",
			Description: desc,
			Execute: func() error {
				var err error
				res, err = ctx.Remove(ctx.RemoveOptions{
					WorkspacePath: workspacePath,
					ProjectPath:   *path,
					Task:          task,
					All:           *all,
					DryRun:        false,
				})
				return err
			},
		})

		planResult := RunPlan(plan, stdin, stdout, globals)

		if globals.json {
			return writeJSONDryRun(stdout, stderr, "context.rm", globals.dryRun, map[string]any{
				"result":  res,
				"actions": planResult.Actions,
			})
		}

		if planResult.WasExecuted("context-rm") {
			out := textOut(globals, stdout)
			nc := globals.noColor
			for _, e := range res.Entries {
				switch e.Action {
				case "removed":
					fmt.Fprintln(out, style.ResultSuccess(nc, "Removed %s (%s)", e.Task, e.Path))
				case "not-found":
					fmt.Fprintln(out, style.ResultInfo(nc, "Context %q not found in index", e.Task))
				}
			}
		}
		return planResult.ExitCode()
	default:
		fmt.Fprintf(stderr, "unknown context subcommand: %s\n", sub)
		return 1
	}

	// --- create subcommand ---

	fs := flag.NewFlagSet("context-create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("path", "", "project path")
	dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args[1:]); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		return printUsageError(stderr, contextHelp)
	}
	task := remaining[0]
	if *dryRun {
		globals.dryRun = true
	}

	var res ctx.CreateResult
	plan := Plan{Command: "context.create"}
	plan.Actions = append(plan.Actions, Action{
		ID:          "context-create",
		Description: fmt.Sprintf("Create context %q", task),
		Execute: func() error {
			var err error
			res, err = ctx.Create(ctx.CreateOptions{ProjectPath: *path, Task: task, DryRun: false})
			if err != nil {
				return err
			}
			return ctx.Track(workspacePath, res.ProjectPath, res.Task)
		},
	})

	planResult := RunPlan(plan, stdin, stdout, globals)

	if globals.json {
		return writeJSONDryRun(stdout, stderr, "context.create", globals.dryRun, map[string]any{
			"result":  res,
			"actions": planResult.Actions,
		})
	}

	if planResult.WasExecuted("context-create") {
		out := textOut(globals, stdout)
		nc := globals.noColor
		if res.GitRepo {
			if res.GitExcludeUpdated {
				fmt.Fprintln(out, style.ResultSuccess(nc, "Updated .git/info/exclude"))
			} else {
				fmt.Fprintln(out, style.ResultInfo(nc, ".git/info/exclude already has .ws-context/ %s skipped", style.IconArrow(nc)))
			}
		}
	}

	return planResult.ExitCode()
}
