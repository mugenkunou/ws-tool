package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

var ignoreHelp = cmdHelp{
	Usage: "ws ignore <check|ls|tree|scan|fix|edit>",
	Subcommands: []string{
		"  check <path>   Test whether a path would be synced or ignored",
		"  ls             List all excluded files (pipe-safe, greppable)",
		"  tree [dir] [-L level]  Browse workspace tree with sync/ignored status",
		"  scan           Find bloat, excessive depth, and build artifacts",
		"  fix            Interactively resolve violations",
		"  edit           Open ws/ignore.json in your editor",
	},
	Flags: listFlagHelp("scan", ignore.Schema(resource.CLIView(), "").Keys()),
}

func runIgnore(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, ignoreHelp)
	}
	if len(args) == 0 {
		return printCmdHelp(stdout, ignoreHelp)
	}

	workspacePath, configPath, _, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	wscfg, err := wsconfig.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	// Load layered ignore rules: built-in defaults + user overrides (from ws.json).
	userRules, err := wsconfig.LoadIgnoreRules(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	engine := ignore.BuildEngine(userRules)

	sub := args[0]
	subArgs := args[1:]
	megaignorePath := filepath.Join(workspacePath, ".megaignore")

	switch sub {
	case "check":
		fs := flag.NewFlagSet("ignore-check", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if len(fs.Args()) == 0 {
			fmt.Fprintln(stderr, "usage: ws ignore check <path>")
			return 1
		}
		return runIgnoreCheckPath(engine, workspacePath, fs.Args()[0], globals, stdout, stderr)
	case "tree":
		fs := flag.NewFlagSet("ignore-tree", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		depth := fs.Int("depth", 0, "max tree depth (0 = unlimited)")
		level := fs.Int("L", 0, "max tree depth (like tree -L)")
		pathFilter := fs.String("path", "", "start from a subpath (deprecated: use positional arg)")
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		// Positional args: [directory] [level]
		if len(fs.Args()) > 0 && strings.TrimSpace(*pathFilter) == "" {
			*pathFilter = fs.Args()[0]
		}
		if len(fs.Args()) > 1 && *depth == 0 && *level == 0 {
			if n, err := fmt.Sscanf(fs.Args()[1], "%d", depth); n != 1 || err != nil {
				fmt.Fprintln(stderr, "level must be an integer")
				return 1
			}
		}
		// -L takes precedence over --depth; both default to 0 (unlimited)
		if *level > 0 {
			*depth = *level
		}
		if *depth == 0 {
			*depth = -1 // signal: unlimited
		}
		return runIgnoreTreeView(engine, workspacePath, *pathFilter, *depth, globals, stdout, stderr)
	case "ls":
		return runIgnoreList(engine, workspacePath, subArgs, globals, stdout, stderr)
	case "edit":
		return runIgnoreEdit(configPath, megaignorePath, userRules, subArgs, globals, stdin, stdout, stderr)
	case "scan":
		fs := flag.NewFlagSet("ignore-scan", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		expandHarbors := fs.Bool("expand-suppressed", false, "show suppressed items individually")
		fs.Bool("expand-harbors", false, "alias for --expand-suppressed (deprecated)") // hidden compat alias
		lf := registerListFlags(fs, "")
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		violations, err := ignore.Scan(ignore.ScanOptions{
			WorkspacePath: workspacePath,
			WarnSizeMB:    wscfg.Ignore.WarnSizeMB,
			CritSizeMB:    wscfg.Ignore.CritSizeMB,
			MaxDepth:      wscfg.Ignore.MaxDepth,
			Engine:        engine,
		})
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		schema := ignore.Schema(resource.CLIView(), workspacePath)
		violations, ok := applyList(schema, violations, lf, stderr)
		if !ok {
			return 1
		}
		if globals.json {
			return writeJSON(stdout, stderr, "ignore.scan", schema.Records(violations))
		}
		out := textOut(globals, stdout)
		if len(violations) == 0 {
			fmt.Fprintln(out, style.ResultSuccess(globals.noColor, "Ignore scan: %s", style.Badge("ok", globals.noColor)))
		} else {
			// --expand-harbors is a deprecated alias for --expand-suppressed.
			expandSuppressed := *expandHarbors
			if f := fs.Lookup("expand-harbors"); f != nil && f.Value.String() == "true" {
				expandSuppressed = true
			}
			printIgnoreViolationsSplit(out, absIgnoreViolations(workspacePath, violations), globals.noColor, expandSuppressed)
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
			WarnSizeMB:    wscfg.Ignore.WarnSizeMB,
			CritSizeMB:    wscfg.Ignore.CritSizeMB,
			MaxDepth:      wscfg.Ignore.MaxDepth,
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
		return runIgnoreFixInteractive(actionable, configPath, megaignorePath, workspacePath, wscfg, globals, stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown ignore subcommand: %s\n", sub)
		return 1
	}
}

// runIgnoreFixInteractive walks through violations one by one, prompting
// the user for an action on each. Actions vary by violation type.
// Rules are added to ws.json and .megaignore is regenerated.
func runIgnoreFixInteractive(violations []ignore.Violation, configPath, megaignorePath, workspacePath string, cfg wsconfig.WsConfig, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	out := textOut(globals, stdout)
	nc := globals.noColor

	style.Header(out, style.IconWrench(nc)+" Ignore Fix "+fmt.Sprintf("(%d found)", len(violations)), nc)

	var added, deleted, moved, suppressed, skipped int
	needsRegen := false

	scratchRoot, _ := wsconfig.ResolvePath(workspacePath, cfg.Scratch.RootDir)

	for _, v := range violations {
		absPath := style.AbsPath(workspacePath, v.Path)
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
			style.Infof(nc, "%s", absPath))

		if globals.dryRun {
			fmt.Fprintf(out, "  %s would add to .megaignore: %s\n", style.Mutedf(nc, "[dry-run]"), absPath)
			added++
			continue
		}

		// Derive the parent directory for the suppress option. suppressDir is
		// the workspace-relative form used in the rule; suppressAbs is shown.
		suppressDir := filepath.ToSlash(filepath.Dir(v.Path))
		if suppressDir == "." {
			suppressDir = ""
		}
		suppressAbs := style.AbsPath(workspacePath, suppressDir)

		var choice string
		switch v.Type {
		case "bloat":
			if suppressDir != "" {
				choice = promptChoice(stdin, stdout, globals,
					"  Action?", "[m]egaignore  [s]uppress "+suppressAbs+"  [v]move to scratch  [d]elete  [k]skip  [q]uit", "msvdkq", "k")
			} else {
				choice = promptChoice(stdin, stdout, globals,
					"  Action?", "[m]egaignore  [v]move to scratch  [d]elete  [k]skip  [q]uit", "mvdkq", "k")
			}
		default:
			if suppressDir != "" {
				choice = promptChoice(stdin, stdout, globals,
					"  Action?", "[m]egaignore  [s]uppress "+suppressAbs+"  [d]elete  [k]skip  [q]uit", "msdkq", "k")
			} else {
				choice = promptChoice(stdin, stdout, globals,
					"  Action?", "[m]egaignore  [d]elete  [k]skip  [q]uit", "mdkq", "k")
			}
		}

		switch choice {
		case "m":
			ok, err := wsconfig.AddIgnoreExclude(configPath, v.Path, "")
			if err != nil {
				fmt.Fprintf(out, "  %s %s\n", style.IconCross(nc), err)
			} else if ok {
				fmt.Fprintf(out, "  %s Added to .megaignore: %s %s %s\n", style.IconCheck(nc), style.Infof(nc, "%s", absPath), style.IconArrow(nc), configPath)
				added++
				needsRegen = true
			} else {
				fmt.Fprintf(out, "  %s Rule already exists\n", style.Mutedf(nc, ""))
				skipped++
			}
		case "s":
			if suppressDir == "" {
				skipped++
				continue
			}
			suppressPattern := suppressDir + "/**"
			ok, err := wsconfig.AddIgnoreSafeHarbor(configPath, suppressPattern, "")
			if err != nil {
				fmt.Fprintf(out, "  %s %s\n", style.IconCross(nc), err)
			} else if ok {
				fmt.Fprintf(out, "  %s Suppressed warnings for %s %s %s\n", style.IconCheck(nc), style.Infof(nc, "%s", suppressAbs), style.IconArrow(nc), configPath)
				suppressed++
				needsRegen = true
			} else {
				fmt.Fprintf(out, "  %s Already suppressed: %s\n", style.Mutedf(nc, ""), style.Infof(nc, "%s", suppressAbs))
				skipped++
			}
		case "v":
			if scratchRoot == "" {
				fmt.Fprintf(out, "  %s scratch root not configured\n", style.IconWarning(nc))
				skipped++
				continue
			}
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
			if err := os.RemoveAll(absPath); err != nil {
				fmt.Fprintf(out, "  %s %s\n", style.IconCross(nc), err)
				skipped++
			} else {
				fmt.Fprintf(out, "  %s Deleted %s\n", style.IconCheck(nc), absPath)
				deleted++
			}
		case "q":
			skipped += len(violations) // approximate
			fmt.Fprintf(out, "\n  %s\n", style.Mutedf(nc, "Quit — remaining violations skipped."))
			goto done
		default: // "k" (skip) or anything else
			skipped++
		}
	}

done:
	// Regenerate .megaignore if any rules were added to ws.json.
	if needsRegen {
		userRules, err := wsconfig.LoadIgnoreRules(configPath)
		if err != nil {
			fmt.Fprintf(stderr, "warning: could not reload user rules: %s\n", err)
		} else {
			if err := ignore.WriteMegaignore(megaignorePath, userRules); err != nil {
				fmt.Fprintf(stderr, "warning: could not regenerate %s: %s\n", megaignorePath, err)
			} else {
				stats := ignore.GetRuleStats(userRules)
				fmt.Fprintf(out, "\n%s %s regenerated (%d rules: %d default + %d user)\n",
					style.IconCheck(nc), megaignorePath, stats.Total, stats.DefaultExclude+stats.DefaultHarbors, stats.UserExclude+stats.UserHarbors)
			}
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, style.Divider(nc))
	parts := []string{fmt.Sprintf("Added: %d", added)}
	if suppressed > 0 {
		parts = append(parts, fmt.Sprintf("Suppressed: %d", suppressed))
	}
	parts = append(parts, fmt.Sprintf("Moved: %d", moved), fmt.Sprintf("Deleted: %d", deleted), fmt.Sprintf("Skipped: %d", skipped))
	fmt.Fprintln(out, strings.Join(parts, "   "))

	if globals.json {
		return writeJSON(stdout, stderr, "ignore.fix", map[string]any{
			"added": added, "suppressed": suppressed, "moved": moved, "deleted": deleted, "skipped": skipped,
		})
	}
	return 0
}
