package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/style"
)

var ignoreHelp = cmdHelp{Usage: "ws ignore <scan|check|fix|ls|tree|edit|generate>"}

func runIgnore(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, ignoreHelp)
	}

	workspacePath, configPath, _, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	engine, err := ignore.LoadEngine(filepath.Join(workspacePath, ".megaignore"))
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if len(args) == 0 {
		return printUsageError(stderr, ignoreHelp)
	}

	sub := args[0]
	subArgs := args[1:]
	megaignorePath := filepath.Join(workspacePath, ".megaignore")

	switch sub {
	case "ls":
		return runIgnoreList(engine, workspacePath, subArgs, globals, stdout, stderr)
	case "tree":
		return runIgnoreTree(engine, workspacePath, subArgs, globals, stdout, stderr)
	case "edit":
		return runIgnoreEdit(megaignorePath, subArgs, globals, stdin, stdout, stderr)
	case "generate":
		return runIgnoreGenerate(megaignorePath, workspacePath, subArgs, globals, stdin, stdout, stderr)
	case "scan":
		fs := flag.NewFlagSet("ignore-scan", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		expandHarbors := fs.Bool("expand-harbors", false, "show safe harbor items individually")
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		violations, err := ignore.Scan(ignore.ScanOptions{
			WorkspacePath: workspacePath,
			WarnSizeMB:    cfg.Ignore.WarnSizeMB,
			CritSizeMB:    cfg.Ignore.CritSizeMB,
			MaxDepth:      cfg.Ignore.MaxDepth,
			Engine:        engine,
		})
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if globals.json {
			return writeJSON(stdout, stderr, "ignore.scan", violations)
		}
		out := textOut(globals, stdout)
		if len(violations) == 0 {
			fmt.Fprintln(out, style.ResultSuccess(globals.noColor, "Ignore scan: %s", style.Badge("ok", globals.noColor)))
		} else {
			printIgnoreViolationsSplit(out, violations, globals.noColor, *expandHarbors)
		}
		// Only actionable (non-safe-harbor) violations trigger exit 2.
		for _, v := range violations {
			if !v.InSafeHarbor {
				return 2
			}
		}
		return 0
	case "fix":
		fs := flag.NewFlagSet("ignore-fix", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if *dryRun {
			globals.dryRun = true
		}
		violations, err := ignore.Scan(ignore.ScanOptions{
			WorkspacePath: workspacePath,
			WarnSizeMB:    cfg.Ignore.WarnSizeMB,
			CritSizeMB:    cfg.Ignore.CritSizeMB,
			MaxDepth:      cfg.Ignore.MaxDepth,
			Engine:        engine,
		})
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		// Filter out safe harbor violations — they are acknowledged and
		// should not be offered for fixing.
		actionable := make([]ignore.Violation, 0, len(violations))
		for _, v := range violations {
			if !v.InSafeHarbor {
				actionable = append(actionable, v)
			}
		}
		if len(actionable) == 0 {
			if globals.json {
				return writeJSON(stdout, stderr, "ignore.fix", ignore.FixResult{AddedRules: []string{}, Messages: []string{"No violations."}})
			}
			fmt.Fprintln(textOut(globals, stdout), style.ResultSuccess(globals.noColor, "No ignore violations to fix."))
			return 0
		}
		return runIgnoreFixInteractive(actionable, megaignorePath, workspacePath, cfg, globals, stdin, stdout, stderr)
	case "check":
		fs := flag.NewFlagSet("ignore-check", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if len(fs.Args()) != 1 {
			fmt.Fprintln(stderr, "usage: ws ignore check <path>")
			return 1
		}
		absPath := fs.Args()[0]
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(workspacePath, absPath)
		}
		st, err := os.Stat(absPath)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		res, rel, err := ignore.Check(engine, workspacePath, absPath, st.IsDir())
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		data := map[string]any{"path": rel, "included": res.Included, "rule": res.Rule, "safe_harbor": res.SafeHarbor}
		if globals.json {
			return writeJSON(stdout, stderr, "ignore.check", data)
		} else {
			out := textOut(globals, stdout)
			nc := globals.noColor
			state := style.Badge("synced", nc)
			if !res.Included {
				state = style.Badge("ignored", nc)
			}
			harbor := ""
			if res.SafeHarbor {
				harbor = " " + style.Mutedf(nc, "[safe harbor]")
			}
			fmt.Fprintf(out, "%s (%s) %s %s%s\n", style.Infof(nc, "%s", rel), state, style.Mutedf(nc, "via"), style.Mutedf(nc, "%s", res.Rule), harbor)
		}
		if !res.Included {
			return 2
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown ignore subcommand: %s\n", sub)
		return 1
	}
}

// runIgnoreFixInteractive walks through violations one by one, prompting
// the user for an action on each. Actions vary by violation type.
func runIgnoreFixInteractive(violations []ignore.Violation, megaignorePath, workspacePath string, cfg config.Config, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	out := textOut(globals, stdout)
	nc := globals.noColor

	style.Header(out, style.IconWrench(nc)+" Ignore Fix "+fmt.Sprintf("(%d found)", len(violations)), nc)

	var added, deleted, moved, skipped int

	scratchRoot, _ := config.ResolvePath(workspacePath, cfg.Scratch.RootDir)

	for _, v := range violations {
		sizePart := ""
		if v.SizeBytes > 0 {
			sizePart = fmt.Sprintf("  %s", style.HumanBytes(v.SizeBytes))
		}
		depthPart := ""
		if v.Depth > 0 {
			depthPart = fmt.Sprintf("  %d lvl", v.Depth)
		}

		fmt.Fprintf(out, "\n%s  %s%s%s  %s\n",
			style.Badge(v.Severity, nc),
			style.Boldf(nc, "%s", v.Type),
			sizePart, depthPart,
			style.Infof(nc, "%s", v.Path))

		if globals.dryRun {
			fmt.Fprintf(out, "  %s would add rule: -p:%s\n", style.Mutedf(nc, "[dry-run]"), v.Path)
			added++
			continue
		}

		var choice string
		switch v.Type {
		case "bloat":
			choice = promptChoice(stdin, stdout, globals,
				"  Action?", "[a]dd to .megaignore  [m]ove to scratch  [d]elete  [s]kip  [q]uit", "amdsq", "s")
		default:
			choice = promptChoice(stdin, stdout, globals,
				"  Action?", "[a]dd to .megaignore  [d]elete  [s]kip  [q]uit", "adsq", "s")
		}

		switch choice {
		case "a":
			rule := "-p:" + v.Path
			_, err := ignore.AddRules(megaignorePath, []string{rule})
			if err != nil {
				fmt.Fprintf(out, "  %s %s\n", style.IconCross(nc), err)
			} else {
				fmt.Fprintf(out, "  %s Added rule: %s\n", style.IconCheck(nc), style.Mutedf(nc, "%s", rule))
				added++
			}
		case "m":
			if scratchRoot == "" {
				fmt.Fprintf(out, "  %s scratch root not configured\n", style.IconWarning(nc))
				skipped++
				continue
			}
			absPath := filepath.Join(workspacePath, v.Path)
			dest := filepath.Join(scratchRoot, filepath.Base(v.Path))
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				fmt.Fprintf(out, "  %s %s\n", style.IconCross(nc), err)
				skipped++
				continue
			}
			if err := os.Rename(absPath, dest); err != nil {
				fmt.Fprintf(out, "  %s %s\n", style.IconCross(nc), err)
				skipped++
				continue
			}
			fmt.Fprintf(out, "  %s Moved %s %s\n", style.IconCheck(nc), style.IconArrow(nc), style.Infof(nc, "%s", dest))
			moved++
		case "d":
			absPath := filepath.Join(workspacePath, v.Path)
			if err := os.RemoveAll(absPath); err != nil {
				fmt.Fprintf(out, "  %s %s\n", style.IconCross(nc), err)
				skipped++
			} else {
				fmt.Fprintf(out, "  %s Deleted %s\n", style.IconCheck(nc), v.Path)
				deleted++
			}
		case "q":
			skipped += len(violations) // approximate
			fmt.Fprintf(out, "\n  %s\n", style.Mutedf(nc, "Quit — remaining violations skipped."))
			goto done
		default:
			skipped++
		}
	}

done:
	fmt.Fprintln(out)
	fmt.Fprintln(out, style.Divider(nc))
	fmt.Fprintf(out, "Added: %d   Moved: %d   Deleted: %d   Skipped: %d\n", added, moved, deleted, skipped)

	if globals.json {
		return writeJSON(stdout, stderr, "ignore.fix", map[string]any{
			"added": added, "moved": moved, "deleted": deleted, "skipped": skipped,
		})
	}
	return 0
}
