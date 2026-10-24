package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/skill"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// runSkill dispatches `ws skill <verb>`: scan, install, uninstall.
func runSkill(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	workspacePath, _, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ws skill <scan|install|uninstall> …")
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "scan":
		return runSkillScan(rest, globals, stdout, stderr, home, workspacePath)
	case "install":
		return runSkillInstall(rest, globals, stdin, stdout, stderr, home, workspacePath)
	case "uninstall":
		return runSkillUninstall(rest, globals, stdin, stdout, stderr, home, workspacePath)
	}
	fmt.Fprintf(stderr, "unknown command: skill %s\n", verb)
	fmt.Fprintln(stderr, "usage: ws skill <scan|install|uninstall> …")
	return 1
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
	renderSkillGrid(textOut(globals, stdout), res, globals.noColor)
	return 0
}

// renderSkillGrid prints one row per skill and one column per tool. A cell is
// installed or not; a column whose tool is not detected has a dim header, and the
// footer says the same in words for a terminal without color.
func renderSkillGrid(out io.Writer, res skill.Result, nc bool) {
	if len(res.Rows) == 0 {
		fmt.Fprintln(out, "No skills. Put a folder with a SKILL.md in agent/skills/.")
		return
	}
	nameW := len("SKILL")
	for _, r := range res.Rows {
		if n := len(skillLabel(r)); n > nameW {
			nameW = n
		}
	}
	fmt.Fprintf(out, "%-*s", nameW, "SKILL")
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
	for _, r := range res.Rows {
		fmt.Fprintf(out, "%-*s", nameW, skillLabel(r))
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

// skillLabel is a skill's name, with a note when its link target is gone.
func skillLabel(r skill.Row) string {
	if r.Broken {
		return r.Skill + " (broken link)"
	}
	return r.Skill
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

func runSkillInstall(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer, home, workspacePath string) int {
	const usage = "ws skill install [<skill>…] [--tool <name>]"
	fs := flag.NewFlagSet("skill-install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	toolName := fs.String("tool", "", "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	skills, err := skill.List(workspacePath)
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
			s, found := findSkill(skills, n)
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
		var labels []string
		for _, s := range skills {
			if s.Broken || skill.Installed(home, tool, s.Name) {
				continue
			}
			cands = append(cands, s)
			labels = append(labels, s.Name)
		}
		if len(cands) == 0 {
			fmt.Fprintf(out, "Every skill is already in %s.\n", tool.Name)
			return 0
		}
		for _, i := range pickMany(stdin, out, "Install which skills into "+tool.Name+"?", labels) {
			chosen = append(chosen, cands[i])
		}
		if len(chosen) == 0 {
			return 1
		}
	}

	p := Plan{Command: "skill.install"}
	for _, s := range chosen {
		s := s
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
		for _, i := range pickMany(stdin, out, "Uninstall which skills from "+tool.Name+"?", linked) {
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
			Description: fmt.Sprintf("Unlink %s/%s (the skill in the workspace stays)", tool.Dir, n),
			Execute:     func() error { return skill.Uninstall(home, workspacePath, tool, n) },
		})
	}
	res := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "skill.uninstall", globals.dryRun, map[string]any{"actions": res.Actions})
	}
	return res.ExitCode()
}

func findSkill(skills []skill.Skill, name string) (skill.Skill, bool) {
	for _, s := range skills {
		if s.Name == name {
			return s, true
		}
	}
	return skill.Skill{}, false
}
