package cmd

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/scratch"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/tui"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// scratchEnv is what every scratch command needs.
type scratchEnv struct {
	workspace  string
	configPath string
	cfg        wsconfig.WsConfig
	root       string // the scratch root, absolute
}

// runScratch dispatches `ws scratch <verb>` (spec §5 scratch).
func runScratch(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ws scratch <ls|new|add|open|tag|rm> …")
		return 1
	}
	workspacePath, configPath, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	root, err := wsconfig.ResolvePath(workspacePath, cfg.Scratch.RootDir)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	e := scratchEnv{workspace: workspacePath, configPath: configPath, cfg: cfg, root: root}

	verb, rest := args[0], args[1:]
	switch verb {
	case "ls":
		return e.ls(rest, globals, stdout, stderr)
	case "new":
		return e.new(rest, globals, stdin, stdout, stderr)
	case "add":
		return e.add(rest, globals, stdin, stdout, stderr)
	case "open":
		return e.open(rest, globals, stdin, stdout, stderr)
	case "tag":
		return e.tag(rest, globals, stdin, stdout, stderr)
	case "rm":
		return e.rm(rest, globals, stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "unknown command: scratch %s\n", verb)
	fmt.Fprintln(stderr, "usage: ws scratch <ls|new|add|open|tag|rm> …")
	return 1
}

// isInteractive reports whether stdin and stdout are both terminals, the
// condition for asking a missing name.
func isInteractive(stdin io.Reader, stdout io.Writer) bool {
	in, inOK := stdin.(*os.File)
	out, outOK := stdout.(*os.File)
	return inOK && outOK && tui.IsTTY(in.Fd()) && tui.IsTTY(out.Fd())
}

// names lists the existing directory names, for the name prompt.
func (e scratchEnv) names() []string {
	entries, _ := scratch.List(scratch.ListOptions{RootDir: e.root, SortBy: "age"})
	names := make([]string, len(entries))
	for i, en := range entries {
		names[i] = en.Name
	}
	return names
}

// askName prompts for a missing name on a terminal, listing the existing names
// as the user types. Without a terminal a missing name is a usage error. The
// second result is a non-zero exit code when the command must stop (130 when the
// prompt was cancelled).
func (e scratchEnv) askName(prompt string, tabComplete bool, usage string, globals globalFlags, stdin io.Reader, out, stderr io.Writer) (string, int) {
	if !isInteractive(stdin, out) {
		fmt.Fprintln(stderr, usage)
		return "", 1
	}
	gi := &tui.GhostInput{Prompt: prompt, Entries: e.names(), TabComplete: tabComplete, NoColor: globals.noColor}
	name, err := gi.Run(stdin, out)
	if errors.Is(err, tui.ErrCancelled) {
		return "", 130
	}
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return "", 1
	}
	if strings.TrimSpace(name) == "" {
		fmt.Fprintln(stderr, usage)
		return "", 1
	}
	return strings.TrimSpace(name), 0
}

// ── ls ─────────────────────────────────────────────────────────────

func (e scratchEnv) ls(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("scratch-ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	lf := registerListFlags(fs, "age")
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if strings.EqualFold(strings.TrimSpace(lf.sort), "size") { // the pre-schema spelling sorted biggest first
		lf.sort = "-size_bytes"
	}
	list, err := scratch.List(scratch.ListOptions{RootDir: e.root})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return renderList(globals, "scratch.ls", scratch.Schema(resource.CLIView()), list, lf, "No scratch directories.", stdout, stderr)
}

// ── new ────────────────────────────────────────────────────────────

func (e scratchEnv) new(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("scratch-new", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	noShell := fs.Bool("no-shell", false, "")
	noDate := fs.Bool("no-date", false, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) > 1 {
		fmt.Fprintln(stderr, "ws scratch new takes one name; quote a name that contains spaces")
		return 1
	}

	name := ""
	switch {
	case len(fs.Args()) == 1:
		name = fs.Args()[0]
	case isInteractive(stdin, stdout):
		// Ask, listing the existing names so a distinct one can be chosen. An
		// empty answer generates scratch-<n>.
		gi := &tui.GhostInput{Prompt: "Name", Entries: e.names(), TabComplete: false, NoColor: globals.noColor}
		input, err := gi.Run(stdin, stdout)
		if errors.Is(err, tui.ErrCancelled) {
			return 130
		}
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		name = input
	}

	// Work out the final path first, so the plan line names it.
	preview, err := scratch.New(scratch.NewOptions{RootDir: e.root, Name: name, NoDateSuffix: *noDate, SuffixMode: "auto", DryRun: true})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	var res scratch.NewResult
	p := Plan{Command: "scratch.new", Actions: []Action{{
		ID:          "scratch-new",
		Description: "Create " + preview.Path,
		Execute: func() error {
			var err error
			res, err = scratch.New(scratch.NewOptions{RootDir: e.root, Name: name, NoDateSuffix: *noDate, SuffixMode: "auto"})
			return err
		},
	}}}
	planResult := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		// --json does not start the editor.
		return writeJSONDryRun(stdout, stderr, "scratch.new", globals.dryRun, map[string]any{
			"result":  map[string]any{"path": preview.Path, "created": res.Created, "dry_run": globals.dryRun},
			"actions": planResult.Actions,
		})
	}
	if planResult.WasExecuted("scratch-new") {
		out := textOut(globals, stdout)
		fmt.Fprintf(out, "  %s\n", style.Mutedf(globals.noColor, "%s", res.Path))
		if !*noShell {
			enterScratchShell(res.Path, globals, stdin, stdout, stderr)
		}
	}
	return planResult.ExitCode()
}

// enterScratchShell starts the user's shell in path and returns when it exits,
// so `ws scratch new|open` leaves you inside the directory. It does nothing
// without a terminal (the path is already printed), and asks first when run from
// inside a scratch shell. The shell's own exit status is not ws's: Ctrl-D after
// a failed command is not a failure of ws. When the shell cannot start, ws warns
// and the command still succeeds.
func enterScratchShell(path string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) {
	in, inOK := stdin.(*os.File)
	out, outOK := stdout.(*os.File)
	if !inOK || !outOK || !tui.IsTTY(in.Fd()) || !tui.IsTTY(out.Fd()) {
		return
	}
	nc := globals.noColor
	if cur := scratch.InShell(); cur != "" {
		fmt.Fprintf(stderr, "%s\n", style.ResultWarning(nc, "You are already in a scratch shell (%s); a new one nests inside it.", cur))
		fmt.Fprint(stderr, "Start it anyway? [y/N] ")
		line, _ := bufio.NewReader(in).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			return
		}
	}
	fmt.Fprintf(out, "  %s\n", style.Mutedf(nc, "Entering %s. Type exit to come back.", path))
	c := scratch.ShellCommand(path)
	c.Stdin, c.Stdout, c.Stderr = in, out, os.Stderr
	var exitErr *exec.ExitError
	if err := c.Run(); err != nil && !errors.As(err, &exitErr) {
		fmt.Fprintln(out, style.ResultWarning(nc, "Shell launch skipped: %v", err))
	}
}

// ── add ────────────────────────────────────────────────────────────

func (e scratchEnv) add(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("scratch-add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(stderr, "usage: ws scratch add <path…>")
		return 1
	}
	paths, err := resolveTargets(fs.Args())
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	plans, err := scratch.PlanAdd(scratch.AddOptions{
		RootDir: e.root, WorkspacePath: e.workspace, ConfigPath: e.configPath, Links: e.cfg.Symlinks, Paths: paths,
	})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	moved := []map[string]string{}
	p := Plan{Command: "scratch.add"}
	for _, ap := range plans {
		ap := ap
		p.Actions = append(p.Actions, Action{
			ID:          "scratch-add-" + ap.Source,
			Description: fmt.Sprintf("Move %s → %s", ap.Source, ap.Dest),
			Execute: func() error {
				if err := scratch.Add(ap); err != nil {
					return err
				}
				moved = append(moved, map[string]string{"source": ap.Source, "path": ap.Dest})
				return nil
			},
		})
	}
	planResult := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "scratch.add", globals.dryRun, map[string]any{
			"moved": moved, "actions": planResult.Actions,
		})
	}
	return planResult.ExitCode()
}

// ── open ───────────────────────────────────────────────────────────

func (e scratchEnv) open(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("scratch-open", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	noShell := fs.Bool("no-shell", false, "")
	printPath := fs.Bool("print-path", false, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	// With --print-path only the resolved path reaches stdout, so `cd "$(ws
	// scratch open --print-path x)"` works. Status lines and the name prompt go
	// to stderr.
	status := stdout
	if *printPath {
		status = stderr
	}

	var name string
	switch len(fs.Args()) {
	case 0:
		var code int
		if name, code = e.askName("Open", true, "usage: ws scratch open <name>", globals, stdin, status, stderr); code != 0 {
			return code
		}
	case 1:
		name = fs.Args()[0]
	default:
		fmt.Fprintln(stderr, "usage: ws scratch open <name>")
		return 1
	}

	entry, err := scratch.Resolve(e.root, name)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if globals.json {
		return writeJSON(stdout, stderr, "scratch.open", map[string]any{"name": entry.Name, "path": entry.Path})
	}

	if *printPath {
		fmt.Fprintln(stdout, entry.Path)
		return 0
	}

	out := textOut(globals, stdout)
	fmt.Fprintf(out, "  %s\n", style.Mutedf(globals.noColor, "%s", entry.Path))
	if !*noShell {
		enterScratchShell(entry.Path, globals, stdin, stdout, stderr)
	}
	return 0
}

// ── tag ────────────────────────────────────────────────────────────

func (e scratchEnv) tag(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("scratch-tag", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var remove stringSliceFlag
	fs.Var(&remove, "rm", "")
	_ = fs.Bool("auto", false, "") // accepted and ignored: tags are a human label, never suggested
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	positional := fs.Args()
	var name string
	var add []string
	if len(positional) > 0 {
		name, add = positional[0], positional[1:]
	} else {
		var code int
		if name, code = e.askName("Scratch", true, "usage: ws scratch tag <name> [tag…] [--rm <tag>]", globals, stdin, stdout, stderr); code != 0 {
			return code
		}
	}
	entry, err := scratch.Resolve(e.root, name)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	// With no tags and no --rm, ask on a terminal; tags used before are offered
	// as completions and nothing is suggested from the directory's contents.
	if len(add) == 0 && len(remove) == 0 {
		if !isInteractive(stdin, stdout) {
			fmt.Fprintln(stderr, "usage: ws scratch tag <name> [tag…] [--rm <tag>]")
			return 1
		}
		tc, _ := scratch.LoadTags(e.configPath)
		gmi := &tui.GhostMultiInput{Prompt: "Tag", Entries: tc.Tags, NoColor: globals.noColor}
		typed, err := gmi.Run(stdin, stdout)
		if errors.Is(err, tui.ErrCancelled) {
			return 130
		}
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		add = typed
	}

	meta, err := scratch.LoadMeta(entry.Path)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	newTags, added, removed := scratch.ApplyTags(meta.Tags, add, remove)
	if len(added) == 0 && len(removed) == 0 {
		if globals.json {
			return writeJSON(stdout, stderr, "scratch.tag", map[string]any{
				"name": entry.Name, "path": entry.Path, "tags": meta.Tags,
				"added": []string{}, "removed": []string{}, "actions": []ActionStatus{},
			})
		}
		fmt.Fprintln(textOut(globals, stdout), "No change.")
		return 0
	}

	var change []string
	for _, t := range added {
		change = append(change, "+"+t)
	}
	for _, t := range removed {
		change = append(change, "−"+t)
	}
	p := Plan{Command: "scratch.tag", Actions: []Action{{
		ID:          "scratch-tag-" + entry.Name,
		Description: fmt.Sprintf("Tag %s: %s", entry.Name, strings.Join(change, " ")),
		Execute: func() error {
			meta.Tags = newTags
			if err := scratch.SaveMeta(entry.Path, meta); err != nil {
				return err
			}
			// Every tag ever added is kept in scratch.tags, the vocabulary
			// completion offers.
			tc, _ := scratch.LoadTags(e.configPath)
			if scratch.MergeTags(&tc, added) {
				return scratch.SaveTags(e.configPath, tc)
			}
			return nil
		},
	}}}
	planResult := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "scratch.tag", globals.dryRun, map[string]any{
			"name": entry.Name, "path": entry.Path, "tags": newTags,
			"added": nonNil(added), "removed": nonNil(removed), "actions": planResult.Actions,
		})
	}
	return planResult.ExitCode()
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ── rm ─────────────────────────────────────────────────────────────

func (e scratchEnv) rm(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("scratch-rm", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	names := fs.Args()
	if len(names) == 0 {
		name, code := e.askName("Delete", true, "usage: ws scratch rm <name…>", globals, stdin, stdout, stderr)
		if code != 0 {
			return code
		}
		names = []string{name}
	}

	// Every name must resolve to exactly one directory before anything is
	// deleted. A name that matches nothing, or several, stops the command.
	var chosen []scratch.Entry
	for _, n := range names {
		entry, err := scratch.Resolve(e.root, n)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if !slices.ContainsFunc(chosen, func(c scratch.Entry) bool { return c.Path == entry.Path }) {
			chosen = append(chosen, entry)
		}
	}

	p := Plan{Command: "scratch.rm"}
	for _, entry := range chosen {
		entry := entry
		p.Actions = append(p.Actions, Action{
			ID:          "scratch-rm-" + entry.Path,
			Description: fmt.Sprintf("Delete %s (%s)", entry.Path, style.HumanBytes(entry.SizeBytes)),
			Execute:     func() error { return scratch.Remove(entry) },
		})
	}
	planResult := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "scratch.rm", globals.dryRun, map[string]any{"actions": planResult.Actions})
	}
	return planResult.ExitCode()
}
