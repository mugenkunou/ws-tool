package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/skill"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// sourceDir is the checkout ws was built from, set at build time via ldflags.
// `ws skill install --dev` links the built-ins to <sourceDir>/skills/.
var sourceDir = ""

func init() { skill.Version = appVersion }

const skillUsage = "usage: ws skill <scan|show|install|uninstall|eject|check> …"

// runSkill dispatches `ws skill <verb>`. Every verb first refreshes the managed
// copy of the built-ins, so a new ws updates the tools linked to them.
func runSkill(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	workspacePath, configPath, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, skillUsage)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	refreshBuiltins(home, stderr)
	verb, rest := args[0], args[1:]
	switch verb {
	case "scan":
		return runSkillScan(rest, globals, stdout, stderr, home, workspacePath)
	case "show":
		return runSkillShow(rest, globals, stdout, stderr, home, workspacePath)
	case "install":
		return runSkillInstall(rest, globals, stdin, stdout, stderr, home, workspacePath)
	case "uninstall":
		return runSkillUninstall(rest, globals, stdin, stdout, stderr, home, workspacePath)
	case "eject":
		return runSkillEject(rest, globals, stdin, stdout, stderr, home, workspacePath)
	case "check":
		return runSkillCheck(rest, globals, stdout, stderr, home, workspacePath, configPath)
	}
	fmt.Fprintf(stderr, "unknown command: skill %s\n", verb)
	fmt.Fprintln(stderr, skillUsage)
	return 1
}

// refreshBuiltins brings the managed copy of the built-ins up to date. A
// failure is a warning: the skills of yours still work.
func refreshBuiltins(home string, stderr io.Writer) {
	if err := skill.Refresh(home); err != nil {
		fmt.Fprintf(stderr, "warning: %v\n", err)
	}
}

// toolFlag reads --tool and resolves it. An empty name is not an error.
func toolFlag(name string) (skill.Tool, bool, error) {
	if name == "" {
		return skill.Tool{}, false, nil
	}
	t, ok := skill.FindTool(name)
	if !ok {
		return skill.Tool{}, false, fmt.Errorf("unknown tool %q (known: %s)", name, strings.Join(skill.ToolNames(), ", "))
	}
	return t, true, nil
}

func runSkillScan(args []string, globals globalFlags, stdout, stderr io.Writer, home, workspacePath string) int {
	fs := flag.NewFlagSet("skill-scan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	toolName := fs.String("tool", "", "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintln(stderr, "usage: ws skill scan [--tool <name>]")
		return 1
	}
	tool, only, err := toolFlag(*toolName)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	var tools []skill.Tool
	if only {
		tools = []skill.Tool{tool}
	}
	res, err := skill.Scan(home, workspacePath, tools)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if globals.json {
		return writeJSON(stdout, stderr, "skill.scan", res)
	}
	renderSkillGrid(textOut(globals, stdout), res, globals.noColor, home)
	return 0
}

// renderSkillGrid prints one section per shelf, built-ins first, with one row
// per skill and one column per tool. A cell is installed or not; a column whose
// tool is not detected has a dim header, and the footer says the same in words
// for a terminal without color. A skill of yours that a built-in shadows gets a
// warning instead of cells.
func renderSkillGrid(out io.Writer, res skill.Result, nc bool, home string) {
	if len(res.Rows) == 0 {
		fmt.Fprintln(out, "No skills. Put a folder with a SKILL.md in agent/skills/.")
		return
	}
	var builtins, yours []skill.Row
	nameW := 0
	for _, r := range res.Rows {
		if r.Shelf == skill.Builtin {
			builtins = append(builtins, r)
		} else {
			yours = append(yours, r)
		}
		nameW = max(nameW, len(r.Skill))
	}
	type section struct {
		title string
		rows  []skill.Row
	}
	var sections []section
	if len(builtins) > 0 {
		sections = append(sections, section{"Built-in (ws " + res.Version + ")", builtins})
	}
	if len(yours) > 0 || len(builtins) == 0 {
		sections = append(sections, section{"Yours (" + tildePath(home, res.Root) + ")", yours})
	}
	labelW := len(sections[0].title)
	for _, r := range res.Rows {
		if !r.Shadowed {
			labelW = max(labelW, len(skillLabel(r, nameW)))
		}
	}
	for i, sec := range sections {
		if i > 0 {
			fmt.Fprintln(out, style.Boldf(nc, "%s", sec.title))
		} else {
			fmt.Fprint(out, style.Boldf(nc, "%s", sec.title)+strings.Repeat(" ", labelW-len(sec.title)))
			for _, t := range res.Tools {
				label := t.Name
				if t.Detected {
					label = style.Boldf(nc, "%s", t.Name)
				} else {
					label = style.Mutedf(nc, "%s", t.Name)
				}
				fmt.Fprintf(out, "  %s", label)
			}
			fmt.Fprintln(out)
		}
		for _, r := range sec.rows {
			if r.Shadowed {
				fmt.Fprintf(out, "  %s  %s\n", r.Skill, style.Warningf(nc, "%s same name as a built-in; the built-in wins", style.IconWarning(nc)))
				continue
			}
			fmt.Fprintf(out, "%-*s", labelW, skillLabel(r, nameW))
			var line strings.Builder
			for i, c := range r.Tools {
				mark := style.Mutedf(nc, "%s", notMark(nc))
				if c.Installed {
					mark = style.IconCheck(nc)
				}
				line.WriteString("  " + mark)
				if i < len(r.Tools)-1 {
					line.WriteString(strings.Repeat(" ", len(res.Tools[i].Name)-1))
				}
			}
			fmt.Fprintln(out, line.String())
		}
	}
	fmt.Fprintln(out)
	var det, undet []string
	for _, t := range res.Tools {
		label := t.Name
		if t.Unverified {
			label += "*"
		}
		if t.Detected {
			det = append(det, label)
		} else {
			undet = append(undet, label)
		}
	}
	fmt.Fprintf(out, "%s detected: %s\n", style.IconDot(nc), joinOrNone(det))
	fmt.Fprintf(out, "%s not detected: %s\n", style.Mutedf(nc, "%s", noToolMark(nc)), joinOrNone(undet))
	fmt.Fprintf(out, "%s installed   %s not installed (the tool or the skill)\n", style.IconCheck(nc), notMark(nc))
	for _, t := range res.Tools {
		if t.Unverified {
			fmt.Fprintln(out, "* path not confirmed by the tool's own docs")
			break
		}
	}
}

// skillLabel is a row's name column: the name, then a built-in's version or
// where an ejected copy came from, and a note when its link target is gone.
func skillLabel(r skill.Row, nameW int) string {
	extra := r.Version
	if r.Ejected != "" {
		extra = "ejected from " + r.Ejected
	}
	if r.Broken {
		extra = strings.TrimSpace(extra + " (broken link)")
	}
	if extra == "" {
		return "  " + r.Skill
	}
	return fmt.Sprintf("  %-*s  %s", nameW, r.Skill, extra)
}

// tildePath shortens a path under home to ~/….
func tildePath(home, p string) string {
	if rel, err := filepath.Rel(home, p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return "~/" + rel
	}
	return p
}

func noToolMark(nc bool) string {
	if nc {
		return "( )"
	}
	return "○"
}

func notMark(nc bool) string {
	if nc {
		return "-"
	}
	return "·"
}

func joinOrNone(s []string) string {
	if len(s) == 0 {
		return "none"
	}
	return strings.Join(s, " ")
}

// pickOne asks for one item of a numbered list. It returns -1 on cancel.
func pickOne(stdin io.Reader, out io.Writer, title string, items []string) int {
	fmt.Fprintln(out, title)
	for i, it := range items {
		fmt.Fprintf(out, "  %2d  %s\n", i+1, it)
	}
	fmt.Fprint(out, "Number (Enter to cancel): ")
	line, err := readLine(stdin)
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(items) {
		return -1
	}
	return n - 1
}

// pickMany asks for items of a numbered list: "1,3", a range, "all" or Enter to cancel.
func pickMany(stdin io.Reader, out io.Writer, title string, items []string) []int {
	fmt.Fprintln(out, title)
	for i, it := range items {
		fmt.Fprintf(out, "  %2d  %s\n", i+1, it)
	}
	fmt.Fprint(out, "Numbers like 1,3 or \"all\" (Enter to cancel): ")
	line, err := readLine(stdin)
	if err != nil {
		return nil
	}
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "all" {
		all := make([]int, len(items))
		for i := range all {
			all[i] = i
		}
		return all
	}
	var picked []int
	seen := map[int]bool{}
	for _, part := range strings.Split(line, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > len(items) || seen[n] {
			continue
		}
		seen[n] = true
		picked = append(picked, n-1)
	}
	return picked
}

// chooseTool resolves --tool, or asks. Detected tools come first in the list.
func chooseTool(name string, stdin io.Reader, out io.Writer, globals globalFlags, home string, pool []skill.Tool, usage string) (skill.Tool, bool, error) {
	t, ok, err := toolFlag(name)
	if err != nil {
		return skill.Tool{}, false, err
	}
	if ok {
		return t, true, nil
	}
	if globals.json || globals.quiet {
		return skill.Tool{}, false, fmt.Errorf("usage: %s", usage)
	}
	var detected, other []skill.Tool
	for _, p := range pool {
		if p.Detected(home) {
			detected = append(detected, p)
		} else {
			other = append(other, p)
		}
	}
	ordered := append(detected, other...)
	labels := make([]string, len(ordered))
	for i, p := range ordered {
		mark := style.IconDot(globals.noColor)
		if !p.Detected(home) {
			mark = style.Mutedf(globals.noColor, "%s", noToolMark(globals.noColor)) + " not detected"
		}
		labels[i] = fmt.Sprintf("%-10s %s  %s", p.Name, p.Dir, mark)
	}
	i := pickOne(stdin, out, "Which tool?", labels)
	if i < 0 {
		return skill.Tool{}, false, nil
	}
	return ordered[i], true, nil
}

// devFlag is --dev, a boolean that also takes a value: --dev uses the
// checkout ws was built from, --dev=<dir> another one.
type devFlag struct {
	set bool
	dir string
}

func (d *devFlag) String() string   { return d.dir }
func (d *devFlag) IsBoolFlag() bool { return true }
func (d *devFlag) Set(v string) error {
	d.set = true
	switch v {
	case "true":
		d.dir = ""
	case "false":
		d.set = false
	default:
		d.dir = v
	}
	return nil
}

// shelfLabels are the picklist lines: each name with its shelf.
func shelfLabels(skills []skill.Skill) []string {
	w := 0
	for _, s := range skills {
		w = max(w, len(s.Name))
	}
	out := make([]string, len(skills))
	for i, s := range skills {
		out[i] = fmt.Sprintf("%-*s  %s", w, s.Name, s.Shelf)
	}
	return out
}

func runSkillInstall(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer, home, workspacePath string) int {
	const usage = "ws skill install [<skill>…] [--tool <name>] [--dev[=<dir>]]"
	fs := flag.NewFlagSet("skill-install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	toolName := fs.String("tool", "", "")
	var dev devFlag
	fs.Var(&dev, "dev", "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if dev.set {
		return runSkillDev(fs.Args(), dev.dir, *toolName, globals, stdin, stdout, stderr, home, workspacePath)
	}
	skills, err := skill.All(home, workspacePath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	out := textOut(globals, stdout)
	if globals.json {
		out = io.Discard
	}
	if len(skills) == 0 {
		fmt.Fprintln(out, "No skills. Put a folder with a SKILL.md in agent/skills/.")
		return 0
	}
	tool, ok, err := chooseTool(*toolName, stdin, out, globals, home, skill.Tools, usage)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if !ok {
		return 1
	}

	var chosen []skill.Skill
	if names := fs.Args(); len(names) > 0 {
		for _, n := range names {
			s, found := skill.Find(skills, n)
			if !found {
				fmt.Fprintf(stderr, "no such skill: %s\n", n)
				return 1
			}
			chosen = append(chosen, s)
		}
	} else {
		if globals.json || globals.quiet {
			fmt.Fprintf(stderr, "usage: %s\n", usage)
			return 1
		}
		var cands []skill.Skill
		for _, s := range skills {
			if s.Broken || s.Shadowed || skill.Installed(home, tool, s.Name) {
				continue
			}
			cands = append(cands, s)
		}
		if len(cands) == 0 {
			fmt.Fprintf(out, "Every skill is already in %s.\n", tool.Name)
			return 0
		}
		for _, i := range pickMany(stdin, out, "Install which skills into "+tool.Name+"?", shelfLabels(cands)) {
			chosen = append(chosen, cands[i])
		}
		if len(chosen) == 0 {
			return 1
		}
	}

	p := Plan{Command: "skill.install"}
	var already []string
	for _, s := range chosen {
		s := s
		// A plain install of a built-in in dev mode switches it back to the
		// copy in the binary; the tool links stay as they are.
		if s.Dev != "" {
			p.Actions = append(p.Actions, Action{
				ID:          "skill-undev-" + s.Name,
				Description: fmt.Sprintf("Switch built-in %s back to the copy in ws %s (from the dev link to %s)", s.Name, skill.Version, tildePath(home, s.Dev)),
				Execute:     func() error { return skill.Undev(home, s.Name) },
			})
			s.Broken = false
		}
		if skill.Linked(home, tool, s) {
			already = append(already, s.Name)
			continue
		}
		desc := fmt.Sprintf("Link %s → %s/%s", s.Name, tool.Dir, s.Name)
		if !tool.Detected(home) {
			desc += " (tool not detected; creates the folder)"
		}
		p.Actions = append(p.Actions, Action{
			ID:          "skill-install-" + tool.Name + "-" + s.Name,
			Description: desc,
			Execute:     func() error { return skill.Install(home, workspacePath, tool, s) },
		})
	}
	if len(already) > 0 {
		fmt.Fprintf(out, "Already linked into %s: %s\n", tool.Name, strings.Join(already, ", "))
	}
	res := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "skill.install", globals.dryRun, map[string]any{"actions": res.Actions})
	}
	return res.ExitCode()
}

// runSkillDev is `install --dev`: the managed copy of each named built-in (all,
// without names) becomes a link to <source>/skills/<name>, so edits in the
// checkout are live. With --tool it also links them into that tool.
func runSkillDev(names []string, dir, toolName string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer, home, workspacePath string) int {
	if len(names) == 0 {
		names = skill.BuiltinNames()
	}
	if len(names) == 0 {
		fmt.Fprintln(textOut(globals, stdout), "This ws has no built-in skills.")
		return 0
	}
	for _, n := range names {
		if !skill.IsBuiltin(n) {
			fmt.Fprintf(stderr, "not a built-in skill: %s (--dev is for built-ins)\n", n)
			return 1
		}
	}
	source := dir
	if source == "" {
		source = sourceDir
	}
	if source == "" {
		fmt.Fprintln(stderr, "this ws was not built from a checkout; pass --dev=<dir>")
		return 1
	}
	if abs, err := wsconfig.ExpandUserPath(source); err == nil {
		source = abs
	}
	tool, linkToo, err := toolFlag(toolName)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	p := Plan{Command: "skill.install"}
	for _, n := range names {
		n := n
		target := filepath.Join(source, "skills", n)
		p.Actions = append(p.Actions, Action{
			ID:          "skill-dev-" + n,
			Description: fmt.Sprintf("Point built-in %s at %s (edits there are live)", n, tildePath(home, target)),
			Execute:     func() error { return skill.Dev(home, source, n) },
		})
		s := skill.Skill{Name: n, Path: filepath.Join(skill.ManagedRoot(home), n), Shelf: skill.Builtin}
		if linkToo && !skill.Linked(home, tool, s) {
			p.Actions = append(p.Actions, Action{
				ID:          "skill-install-" + tool.Name + "-" + n,
				Description: fmt.Sprintf("Link %s → %s/%s", n, tool.Dir, n),
				Execute:     func() error { return skill.Install(home, workspacePath, tool, s) },
			})
		}
	}
	res := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "skill.install", globals.dryRun, map[string]any{"actions": res.Actions})
	}
	return res.ExitCode()
}

func runSkillUninstall(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer, home, workspacePath string) int {
	const usage = "ws skill uninstall [<skill>…] [--tool <name>]"
	fs := flag.NewFlagSet("skill-uninstall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	toolName := fs.String("tool", "", "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	out := textOut(globals, stdout)
	if globals.json {
		out = io.Discard
	}
	// Only tools that hold one of our links are worth asking about.
	var pool []skill.Tool
	for _, t := range skill.Tools {
		if len(skill.InstalledOurs(home, workspacePath, t)) > 0 {
			pool = append(pool, t)
		}
	}
	if _, named, _ := toolFlag(*toolName); !named && len(pool) == 0 {
		fmt.Fprintln(out, "No skills are linked into any tool.")
		return 0
	}
	tool, ok, err := chooseTool(*toolName, stdin, out, globals, home, pool, usage)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if !ok {
		return 1
	}

	linked := skill.InstalledOurs(home, workspacePath, tool)
	var names []string
	if given := fs.Args(); len(given) > 0 {
		names = given
	} else {
		if globals.json || globals.quiet {
			fmt.Fprintf(stderr, "usage: %s\n", usage)
			return 1
		}
		if len(linked) == 0 {
			fmt.Fprintf(out, "No skills are linked into %s.\n", tool.Name)
			return 0
		}
		labels := linked
		if all, err := skill.All(home, workspacePath); err == nil {
			var shelved []skill.Skill
			for _, n := range linked {
				s, found := skill.Find(all, n)
				if !found {
					s = skill.Skill{Name: n, Shelf: "gone"}
				}
				shelved = append(shelved, s)
			}
			labels = shelfLabels(shelved)
		}
		for _, i := range pickMany(stdin, out, "Uninstall which skills from "+tool.Name+"?", labels) {
			names = append(names, linked[i])
		}
		if len(names) == 0 {
			return 1
		}
	}

	p := Plan{Command: "skill.uninstall"}
	for _, n := range names {
		n := n
		p.Actions = append(p.Actions, Action{
			ID:          "skill-uninstall-" + tool.Name + "-" + n,
			Description: fmt.Sprintf("Unlink %s/%s (the skill itself stays)", tool.Dir, n),
			Execute:     func() error { return skill.Uninstall(home, workspacePath, tool, n) },
		})
	}
	res := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "skill.uninstall", globals.dryRun, map[string]any{"actions": res.Actions})
	}
	return res.ExitCode()
}

// skillShowData is `skill show --json`.
type skillShowData struct {
	Name    string      `json:"name"`
	Shelf   skill.Shelf `json:"shelf"`
	Version string      `json:"version,omitempty"`
	Dev     string      `json:"dev,omitempty"`
	Ejected string      `json:"ejected_from,omitempty"`
	Path    string      `json:"path"`
	Tools   []string    `json:"tools"`
	// Shadowed is the path of a skill of yours that has this built-in's name.
	Shadowed string `json:"shadowed,omitempty"`
}

func runSkillShow(args []string, globals globalFlags, stdout, stderr io.Writer, home, workspacePath string) int {
	const usage = "usage: ws skill show <skill> [--path]"
	fs := flag.NewFlagSet("skill-show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	pathOnly := fs.Bool("path", false, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, usage)
		return 1
	}
	all, err := skill.All(home, workspacePath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	s, ok := skill.Find(all, fs.Arg(0))
	if !ok {
		fmt.Fprintf(stderr, "no such skill: %s\n", fs.Arg(0))
		return 1
	}
	if *pathOnly {
		fmt.Fprintln(stdout, s.Path)
		return 0
	}
	d := skillShowData{Name: s.Name, Shelf: s.Shelf, Version: s.VersionOf(), Dev: s.Dev, Ejected: s.Ejected, Path: s.Path, Tools: []string{}}
	for _, t := range skill.Tools {
		if skill.Installed(home, t, s.Name) {
			d.Tools = append(d.Tools, t.Name)
		}
	}
	for _, o := range all {
		if o.Name == s.Name && o.Shadowed {
			d.Shadowed = o.Path
		}
	}
	if globals.json {
		return writeJSON(stdout, stderr, "skill.show", d)
	}
	nc := globals.noColor
	out := textOut(globals, stdout)
	fmt.Fprintln(out, style.Boldf(nc, "%s", s.Name))
	style.KV(out, "shelf", string(s.Shelf), nc)
	switch {
	case s.Dev != "":
		style.KV(out, "version", "dev (linked to "+tildePath(home, s.Dev)+")", nc)
	case s.Ejected != "":
		style.KV(out, "version", "ejected from "+s.Ejected+"; ws no longer updates it", nc)
	case s.Shelf == skill.Builtin:
		style.KV(out, "version", d.Version, nc)
	}
	style.KV(out, "path", tildePath(home, s.Path), nc)
	style.KV(out, "tools", joinOrNone(d.Tools), nc)
	if d.Shadowed != "" {
		fmt.Fprintf(out, "%s %s has the same name; the built-in wins\n", style.IconWarning(nc), tildePath(home, d.Shadowed))
	}
	return 0
}

func runSkillEject(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer, home, workspacePath string) int {
	const usage = "usage: ws skill eject <skill>"
	fs := flag.NewFlagSet("skill-eject", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, usage)
		return 1
	}
	name := fs.Arg(0)
	all, err := skill.All(home, workspacePath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	s, ok := skill.Find(all, name)
	switch {
	case ok && s.Shelf == skill.Yours && s.Ejected != "":
		fmt.Fprintf(stderr, "%s is already yours (ejected from %s)\n", name, s.Ejected)
		return 1
	case !ok || s.Shelf != skill.Builtin:
		fmt.Fprintf(stderr, "not a built-in skill: %s\n", name)
		return 1
	}
	dest := filepath.Join(skill.Root(workspacePath), name)
	if _, err := os.Lstat(dest); err == nil {
		fmt.Fprintf(stderr, "%s already exists; move it away first\n", dest)
		return 1
	}
	desc := fmt.Sprintf("Copy built-in %s → %s (yours from now on; ws stops updating it)", name, tildePath(home, dest))
	var tools []string
	for _, t := range skill.BuiltinLinks(home, name) {
		tools = append(tools, t.Name)
	}
	if len(tools) > 0 {
		desc += "; repoint " + strings.Join(tools, ", ") + " at it"
	}
	p := Plan{Command: "skill.eject", Actions: []Action{{
		ID:          "skill-eject-" + name,
		Description: desc,
		Execute:     func() error { return skill.Eject(home, workspacePath, name) },
	}}}
	res := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "skill.eject", globals.dryRun, map[string]any{"actions": res.Actions})
	}
	return res.ExitCode()
}

func runSkillCheck(args []string, globals globalFlags, stdout, stderr io.Writer, home, workspacePath, configPath string) int {
	fs := flag.NewFlagSet("skill-check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "usage: ws skill check")
		return 1
	}
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	findings, err := skill.Check(skill.CheckInput{
		Home: home, Workspace: workspacePath,
		Command: wsCommandLine,
		Anchors: workspace.TopDirs(),
		Schema:  cfg.Schema,
		Env:     wsEnvValues(home, workspacePath, cfg),
	})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	errs := 0
	for _, f := range findings {
		if f.Level == skill.LevelError {
			errs++
		}
	}
	code := 0
	if errs > 0 {
		code = 1
	}
	if globals.json {
		writeJSON(stdout, stderr, "skill.check", map[string]any{
			"findings": findings, "errors": errs, "warnings": len(findings) - errs,
		})
		return code
	}
	nc := globals.noColor
	out := textOut(globals, stdout)
	if len(findings) == 0 {
		fmt.Fprintln(out, style.ResultSuccess(nc, "No drift: the skills, scripts and common.sh agree with this ws."))
		return 0
	}
	for _, f := range findings {
		mark := style.IconWarning(nc)
		if f.Level == skill.LevelError {
			mark = style.IconCross(nc)
		}
		where := checkPath(home, workspacePath, f.File)
		if f.Line > 0 {
			where += ":" + strconv.Itoa(f.Line)
		}
		fmt.Fprintf(out, "%s %s  %s\n", mark, where, f.Message)
	}
	fmt.Fprintf(out, "\n%d %s, %d %s\n", errs, pick(errs, "error", "errors"), len(findings)-errs, pick(len(findings)-errs, "warning", "warnings"))
	return code
}

// checkPath is a finding's file: workspace-relative inside the workspace,
// else under ~.
func checkPath(home, workspacePath, p string) string {
	if rel, err := filepath.Rel(workspacePath, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return tildePath(home, p)
}

// wsCommandLine tells `skill check` whether the words after "ws" in a skill
// name a real command. Words that do not start with a command or a noun are
// not a ws command line (known is false). A line that starts with a command is
// real, and so are a noun alone and a noun with its sub-noun (`ws repo root`).
func wsCommandLine(words []string) (real, known bool) {
	if len(words) == 0 {
		return false, false
	}
	if _, ok := findCommand(words[:1]); ok {
		return true, true // init, tui, version, help
	}
	if len(nextWords(words[:1])) == 0 {
		return false, false
	}
	if c, _, ok := matchCommand(words); ok && len(c.Path) > 1 {
		return true, true
	}
	return len(nextWords(words)) > 0, true
}
