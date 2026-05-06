package cmd

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/scratch"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/tui"
)

var scratchHelp = cmdHelp{
	Usage: "ws scratch <new|open|ls|tag|search|prune|rm>",
	Subcommands: []string{
		"  new [name]      Create a scratch directory",
		"  open [name]     Open an existing scratch directory in editor",
		"  ls              List scratch directories",
		"  tag [name]      Add tags to a scratch directory",
		"  search [query]  Search scratch directories by tag/name/content",
		"  prune           Remove old/all scratch directories",
		"  rm <name>       Delete a scratch directory by name",
	},
}

func runScratch(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, scratchHelp)
	}

	workspacePath, configPath, _, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	wsDir := workspacePath + "/ws"
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	rootDir, err := config.ResolvePath("", cfg.Scratch.RootDir)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if len(args) == 0 {
		return printUsageError(stderr, scratchHelp)
	}

	sub := args[0]
	subArgs := args[1:]
	switch sub {
	case "new":
		fs := flag.NewFlagSet("scratch-new", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		noOpen := fs.Bool("no-open", false, "do not open editor")
		editor := fs.String("editor", cfg.Scratch.EditorCmd, "editor command")
		noDateSuffix := fs.Bool("no-date", false, "disable auto date suffix")
		dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		name := ""
		if len(fs.Args()) > 0 {
			name = strings.Join(fs.Args(), " ")
		} else {
			// Interactive: live ghost panel.
			entries, _ := scratch.List(scratch.ListOptions{RootDir: rootDir, SortBy: "age"})
			names := make([]string, len(entries))
			for i, e := range entries {
				names[i] = e.Name
			}
			gi := &tui.GhostInput{
				Prompt:      "Name",
				Entries:     names,
				TabComplete: false,
				NoColor:     globals.noColor,
			}
			inputName, err := gi.Run(stdin, stdout)
			if errors.Is(err, tui.ErrCancelled) {
				return 130
			}
			if err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}
			name = inputName
		}
		if *dryRun {
			globals.dryRun = true
		}

		var res scratch.NewResult
		plan := Plan{Command: "scratch.new"}
		plan.Actions = append(plan.Actions, Action{
			ID:          "scratch-new",
			Description: "Create scratch directory",
			Execute: func() error {
				var err error
				res, err = scratch.New(scratch.NewOptions{RootDir: rootDir, Name: name, NoDateSuffix: *noDateSuffix, SuffixMode: cfg.Scratch.NameSuffix, DryRun: false})
				return err
			},
		})
		planResult := RunPlan(plan, stdin, stdout, globals)
		if globals.json {
			return writeJSON(stdout, stderr, "scratch.new", map[string]any{"result": res, "actions": planResult.Actions})
		}
		if planResult.WasExecuted("scratch-new") {
			out := textOut(globals, stdout)
			nc := globals.noColor
			fmt.Fprintf(out, "  %s\n", style.Mutedf(nc, "%s", res.Path))
			if !*noOpen {
				cmd := exec.Command(*editor, res.Path)
				if err := cmd.Start(); err != nil {
					fmt.Fprintln(out, style.ResultWarning(nc, "Editor launch skipped: %v", err))
				}
			}
		}
		return planResult.ExitCode()
	case "ls":
		fs := flag.NewFlagSet("scratch-ls", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		sortBy := fs.String("sort", "age", "sort by age|size|name")
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		list, err := scratch.List(scratch.ListOptions{RootDir: rootDir, SortBy: *sortBy})
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if globals.json {
			return writeJSON(stdout, stderr, "scratch.ls", list)
		}
		out := textOut(globals, stdout)
		if len(list) == 0 {
			fmt.Fprintln(out, "No scratch directories.")
			return 0
		}
		for _, e := range list {
			nc := globals.noColor
			tagStr := ""
			if len(e.Tags) > 0 {
				tagStr = "  " + style.Mutedf(nc, "[%s]", strings.Join(e.Tags, ", "))
			}
			fmt.Fprintf(out, "%s  %s  %s  %s%s\n",
				style.Boldf(nc, "%-30s", e.Name),
				style.Mutedf(nc, "age=%s", formatScratchAge(e.Age)),
				style.Mutedf(nc, "size=%s", style.HumanBytes(e.SizeBytes)),
				style.Mutedf(nc, "items=%d", e.Items),
				tagStr)
			fmt.Fprintf(out, "  %s\n", style.Mutedf(nc, "%s", e.Path))
		}
		return 0
	case "prune":
		fs := flag.NewFlagSet("scratch-prune", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		olderThan := fs.String("older-than", "", "duration (e.g. 30d, 720h)")
		all := fs.Bool("all", false, "remove all scratch directories")
		name := fs.String("name", "", "name contains filter")
		dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		d := time.Duration(0)
		if strings.TrimSpace(*olderThan) != "" {
			dur, err := scratch.ParseOlderThan(*olderThan)
			if err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}
			d = dur
		} else if cfg.Scratch.PruneAfterDays > 0 && !*all {
			d = time.Duration(cfg.Scratch.PruneAfterDays) * 24 * time.Hour
		}

		if *dryRun {
			globals.dryRun = true
		}
		list, err := scratch.List(scratch.ListOptions{RootDir: rootDir, SortBy: "name"})
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		plan := Plan{Command: "scratch.prune"}
		for _, e := range list {
			remove := *all
			if !remove && d > 0 {
				remove = e.Age >= d
			}
			if remove && strings.TrimSpace(*name) != "" {
				remove = strings.Contains(strings.ToLower(e.Name), strings.ToLower(*name))
			}
			if !remove {
				continue
			}
			eName := e.Name
			ePath := e.Path
			plan.Actions = append(plan.Actions, Action{
				ID:          "prune-" + eName,
				Description: fmt.Sprintf("Remove %s (%s)", eName, style.HumanBytes(e.SizeBytes)),
				Execute: func() error {
					return os.RemoveAll(ePath)
				},
			})
		}

		planResult := RunPlan(plan, stdin, stdout, globals)
		if globals.json {
			return writeJSON(stdout, stderr, "scratch.prune", map[string]any{"actions": planResult.Actions})
		}
		out := textOut(globals, stdout)
		if globals.dryRun {
			fmt.Fprintf(out, "Would remove %d directory(s)\n", len(plan.Actions))
		} else {
			fmt.Fprintf(out, "Removed %d directory(s)\n", planResult.ExecutedCount())
		}
		return planResult.ExitCode()
	case "rm":
		fs := flag.NewFlagSet("scratch-rm", flag.ContinueOnError)
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

		var deleteName string
		if len(fs.Args()) == 1 {
			deleteName = fs.Args()[0]
		} else if len(fs.Args()) == 0 {
			// Interactive: live ghost panel with Tab completion.
			entries, _ := scratch.List(scratch.ListOptions{RootDir: rootDir, SortBy: "age"})
			names := make([]string, len(entries))
			for i, e := range entries {
				names[i] = e.Name
			}
			gi := &tui.GhostInput{
				Prompt:      "Delete",
				Entries:     names,
				TabComplete: true,
				NoColor:     globals.noColor,
			}
			inputName, err := gi.Run(stdin, stdout)
			if errors.Is(err, tui.ErrCancelled) {
				return 130
			}
			if err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}
			deleteName = strings.TrimSpace(inputName)
			if deleteName == "" {
				fmt.Fprintln(stderr, "no name given")
				return 1
			}
		} else {
			fmt.Fprintln(stderr, "usage: ws scratch rm [name]")
			return 1
		}
		plan := Plan{Command: "scratch.rm"}
		plan.Actions = append(plan.Actions, Action{
			ID:          "scratch-rm",
			Description: fmt.Sprintf("Delete scratch %q", deleteName),
			Execute: func() error {
				_, err := scratch.Delete(scratch.DeleteOptions{RootDir: rootDir, Name: deleteName, DryRun: false})
				return err
			},
		})
		planResult := RunPlan(plan, stdin, stdout, globals)
		if globals.json {
			return writeJSON(stdout, stderr, "scratch.rm", map[string]any{"actions": planResult.Actions})
		}
		return planResult.ExitCode()
	case "tag":
		fs := flag.NewFlagSet("scratch-tag", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		autoTag := fs.Bool("auto", false, "suggest tags from file/content heuristics")
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		// Resolve target scratch directory.
		var targetName, targetPath string
		if len(fs.Args()) > 0 {
			targetName = strings.Join(fs.Args(), " ")
		} else {
			entries, _ := scratch.List(scratch.ListOptions{RootDir: rootDir, SortBy: "age"})
			names := make([]string, len(entries))
			for i, e := range entries {
				names[i] = e.Name
			}
			gi := &tui.GhostInput{
				Prompt:      "Scratch",
				Entries:     names,
				TabComplete: true,
				NoColor:     globals.noColor,
			}
			inputName, err := gi.Run(stdin, stdout)
			if errors.Is(err, tui.ErrCancelled) {
				return 130
			}
			if err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}
			targetName = strings.TrimSpace(inputName)
			if targetName == "" {
				fmt.Fprintln(stderr, "no scratch directory given")
				return 1
			}
		}

		// Find matching entry.
		entries, err := scratch.List(scratch.ListOptions{RootDir: rootDir, SortBy: "name"})
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		for _, e := range entries {
			if e.Name == targetName || strings.Contains(strings.ToLower(e.Name), strings.ToLower(targetName)) {
				targetPath = e.Path
				targetName = e.Name
				break
			}
		}
		if targetPath == "" {
			fmt.Fprintf(stderr, "scratch entry not found: %s\n", targetName)
			return 1
		}

		meta, err := scratch.LoadMeta(targetPath)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		if *autoTag {
			suggested, err := scratch.AutoTag(targetPath)
			if err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}
			// Filter out tags already on this scratch.
			existing := make(map[string]struct{}, len(meta.Tags))
			for _, t := range meta.Tags {
				existing[t] = struct{}{}
			}
			var newSuggestions []string
			for _, s := range suggested {
				if _, ok := existing[s]; !ok {
					newSuggestions = append(newSuggestions, s)
				}
			}
			if len(newSuggestions) == 0 {
				out := textOut(globals, stdout)
				fmt.Fprintln(out, "No new tags suggested.")
				return 0
			}

			plan := Plan{Command: "scratch.tag.auto"}
			for _, tag := range newSuggestions {
				t := tag
				plan.Actions = append(plan.Actions, Action{
					ID:          "tag-" + t,
					Description: fmt.Sprintf("Add tag %q to %s", t, targetName),
					Execute: func() error {
						meta.Tags = append(meta.Tags, t)
						return nil
					},
				})
			}
			planResult := RunPlan(plan, stdin, stdout, globals)
			if planResult.ExecutedCount() > 0 {
				if err := scratch.SaveMeta(targetPath, meta); err != nil {
					fmt.Fprintln(stderr, err.Error())
					return 1
				}
				tc, _ := scratch.LoadTags(wsDir)
				if scratch.MergeTags(&tc, meta.Tags) {
					_ = scratch.SaveTags(wsDir, tc)
				}
			}
			if globals.json {
				return writeJSON(stdout, stderr, "scratch.tag", map[string]any{"name": targetName, "tags": meta.Tags, "actions": planResult.Actions})
			}
			return planResult.ExitCode()
		}

		// Interactive multi-tag input with ghost suggestions from tag collection.
		tc, _ := scratch.LoadTags(wsDir)
		gmi := &tui.GhostMultiInput{
			Prompt:  "Tag",
			Entries: tc.Tags,
			NoColor: globals.noColor,
		}
		newTags, err := gmi.Run(stdin, stdout)
		if errors.Is(err, tui.ErrCancelled) {
			return 130
		}
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if len(newTags) == 0 {
			return 0
		}

		// Normalize and deduplicate against existing.
		existing := make(map[string]struct{}, len(meta.Tags))
		for _, t := range meta.Tags {
			existing[t] = struct{}{}
		}
		var added []string
		for _, t := range newTags {
			t = scratch.NormalizeTag(t)
			if t == "" {
				continue
			}
			if _, ok := existing[t]; !ok {
				meta.Tags = append(meta.Tags, t)
				existing[t] = struct{}{}
				added = append(added, t)
			}
		}
		if len(added) == 0 {
			out := textOut(globals, stdout)
			fmt.Fprintln(out, "No new tags added.")
			return 0
		}

		if err := scratch.SaveMeta(targetPath, meta); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		// Merge into workspace tag collection.
		if scratch.MergeTags(&tc, added) {
			_ = scratch.SaveTags(wsDir, tc)
		}

		if globals.json {
			return writeJSON(stdout, stderr, "scratch.tag", map[string]any{"name": targetName, "tags": meta.Tags, "added": added})
		}
		out := textOut(globals, stdout)
		nc := globals.noColor
		fmt.Fprintf(out, "Tagged %s: %s\n",
			style.Boldf(nc, "%s", targetName),
			style.Mutedf(nc, "[%s]", strings.Join(meta.Tags, ", ")))
		fmt.Fprintf(out, "  %s\n", style.Mutedf(nc, "%s", targetPath))
		return 0

	case "search":
		fs := flag.NewFlagSet("scratch-search", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		maxResults := fs.Int("max", 0, "max results (0=unlimited)")
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		query := ""
		if len(fs.Args()) > 0 {
			query = strings.Join(fs.Args(), " ")
		} else {
			// Interactive ghost mode: filter scratch dirs by tag+name.
			entries, _ := scratch.List(scratch.ListOptions{RootDir: rootDir, SortBy: "age"})
			ghosts := make([]string, len(entries))
			for i, e := range entries {
				label := e.Name
				if len(e.Tags) > 0 {
					label += " [" + strings.Join(e.Tags, ", ") + "]"
				}
				ghosts[i] = label
			}
			gi := &tui.GhostInput{
				Prompt:      "Search",
				Entries:     ghosts,
				TabComplete: true,
				NoColor:     globals.noColor,
			}
			input, err := gi.Run(stdin, stdout)
			if errors.Is(err, tui.ErrCancelled) {
				return 130
			}
			if err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}
			query = strings.TrimSpace(input)
			if query == "" {
				return 0
			}
		}

		results, err := scratch.Search(scratch.SearchOptions{
			RootDir:    rootDir,
			Query:      query,
			MaxResults: *maxResults,
		})
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if globals.json {
			return writeJSON(stdout, stderr, "scratch.search", results)
		}
		out := textOut(globals, stdout)
		if len(results) == 0 {
			fmt.Fprintln(out, "No matches.")
			return 0
		}
		nc := globals.noColor
		for _, r := range results {
			tagStr := ""
			if len(r.Entry.Tags) > 0 {
				tagStr = "  " + style.Mutedf(nc, "[%s]", strings.Join(r.Entry.Tags, ", "))
			}
			fmt.Fprintf(out, "%s  %s%s\n",
				style.Boldf(nc, "%-30s", r.Entry.Name),
				style.Mutedf(nc, "match=%s", r.MatchOn),
				tagStr)
			fmt.Fprintf(out, "  %s\n", style.Mutedf(nc, "%s", r.Entry.Path))
			if r.Snippet != "" {
				fmt.Fprintf(out, "  %s\n", style.Mutedf(nc, "%s", r.Snippet))
			}
		}
		return 0

	case "open":
		fs := flag.NewFlagSet("scratch-open", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		editor := fs.String("editor", cfg.Scratch.EditorCmd, "editor command")
		printPath := fs.Bool("print-path", false, "print resolved path to stdout (TUI goes to stderr); useful for shell cd wrappers")
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		// When --print-path is set, direct TUI output to stderr so that
		// only the resolved path reaches stdout (allows `cd $(ws scratch open --print-path)`).
		tuiOut := stdout
		if *printPath {
			tuiOut = stderr
		}

		var openName string
		if len(fs.Args()) > 0 {
			openName = strings.Join(fs.Args(), " ")
		} else {
			// Interactive: live ghost panel with Tab completion.
			entries, _ := scratch.List(scratch.ListOptions{RootDir: rootDir, SortBy: "age"})
			names := make([]string, len(entries))
			for i, e := range entries {
				names[i] = e.Name
			}
			gi := &tui.GhostInput{
				Prompt:      "Open",
				Entries:     names,
				TabComplete: true,
				NoColor:     globals.noColor,
			}
			inputName, err := gi.Run(stdin, tuiOut)
			if errors.Is(err, tui.ErrCancelled) {
				return 130
			}
			if err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}
			openName = strings.TrimSpace(inputName)
			if openName == "" {
				fmt.Fprintln(stderr, "no name given")
				return 1
			}
		}

		// Resolve matching entry.
		entries, err := scratch.List(scratch.ListOptions{RootDir: rootDir, SortBy: "name"})
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		var matchPath, matchName string
		for _, e := range entries {
			if e.Name == openName || strings.Contains(strings.ToLower(e.Name), strings.ToLower(openName)) {
				matchPath = e.Path
				matchName = e.Name
				break
			}
		}
		if matchPath == "" {
			fmt.Fprintf(stderr, "scratch entry not found: %s\n", openName)
			return 1
		}

		if globals.json {
			return writeJSON(stdout, stderr, "scratch.open", map[string]any{"name": matchName, "path": matchPath})
		}

		if *printPath {
			// Launch editor in the background, then print the resolved path to
			// stdout so the calling shell function can cd to it.
			cmd := exec.Command(*editor, matchPath)
			if err := cmd.Start(); err != nil {
				nc := globals.noColor
				fmt.Fprintln(stderr, style.ResultWarning(nc, "Editor launch skipped: %v", err))
			} else {
				nc := globals.noColor
				fmt.Fprintf(stderr, "%s\n", style.ResultSuccess(nc, "Opening   %s → %s", *editor, matchPath))
			}
			fmt.Fprintln(stdout, matchPath)
			return 0
		}

		cmd := exec.Command(*editor, matchPath)
		if err := cmd.Start(); err != nil {
			out := textOut(globals, stdout)
			nc := globals.noColor
			fmt.Fprintln(out, style.ResultWarning(nc, "Editor launch skipped: %v", err))
			fmt.Fprintln(out, matchPath)
			return 0
		}
		out := textOut(globals, stdout)
		nc := globals.noColor
		fmt.Fprintf(out, "%s\n", style.ResultSuccess(nc, "Opening   %s → %s", *editor, matchPath))
		return 0

	default:
		fmt.Fprintf(stderr, "unknown scratch subcommand: %s\n", sub)
		return 1
	}
}

func formatScratchAge(d time.Duration) string {
	if d < 48*time.Hour {
		return d.Truncate(time.Second).String()
	}
	total := int64(d / time.Second)
	days := total / 86400
	total %= 86400
	hours := total / 3600
	total %= 3600
	mins := total / 60
	secs := total % 60
	return strconv.FormatInt(days, 10) + "d" + strconv.FormatInt(hours, 10) + "h" + strconv.FormatInt(mins, 10) + "m" + strconv.FormatInt(secs, 10) + "s"
}
