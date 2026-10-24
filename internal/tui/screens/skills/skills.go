// Package skills is the agent-skills screen: every skill in agent/skills/ and
// the tools it is installed in, with install and uninstall on keys and as `:`
// commands (spec §6.5). It is the TUI side of `ws skill`.
package skills

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/skill"
	"github.com/mugenkunou/ws-tool/internal/tui/choose"
	"github.com/mugenkunou/ws-tool/internal/tui/confirm"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/modal"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Entry is one skill row.
type Entry = skill.Entry

// LoadedMsg carries the skills and the machine's home folder.
type LoadedMsg struct {
	Home    string
	Entries []Entry
	Err     error
}

type step int

const (
	stepInstallTool step = iota
	stepUninstallTool
	stepChange
)

const headerLines = 2

// Model is the skills screen state.
type Model struct {
	env     env.Env
	keys    keys.SkillsMap
	list    reslist.Model[Entry]
	modal   modal.Model
	home    string
	loaded  bool
	pending []Entry      // the rows the open chooser is for
	tools   []skill.Tool // the chooser's options, in order
	width   int
	height  int
}

// New creates the screen.
func New(e env.Env) Model {
	return Model{
		env:  e,
		keys: keys.Skills(),
		list: reslist.New(skill.Schema(e.View()), []reslist.Col[Entry]{
			{Key: "skill", Min: 16, Weight: 1},
			{Key: "installed_in", Min: 14, Weight: 2},
			{Key: "path", Min: 12, Weight: 1, Style: func(_ Entry, t string) string { return theme.Muted.Render(t) }},
		}, theme.Icon(theme.IconSparkles)+"No skills. Put a folder with a SKILL.md in agent/skills/.").Declare(reslist.Decls(actions)),
	}
}

// Load reads the skills and where each one is installed.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		home, err := os.UserHomeDir()
		if err != nil {
			return LoadedMsg{Err: err}
		}
		entries, err := skill.Entries(home, e.Paths.Workspace)
		return LoadedMsg{Home: home, Entries: entries, Err: err}
	}
}

// Init starts the first load.
func (m Model) Init() tea.Cmd { return Load(m.env) }

// Declared lists the screen's actions and their scopes.
func (m Model) Declared() []reslist.Decl { return m.list.Declared() }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() || m.list.Editing() }

func (m Model) reload() (Model, tea.Cmd) {
	m.list = m.list.SetLoading()
	return m, Load(m.env)
}

// Update implements nav.Component.
func (m Model) Update(msg tea.Msg) (nav.Component, tea.Cmd) {
	if st, ok := m.modal.Match(msg); ok {
		m.modal = m.modal.Close()
		switch st {
		case stepInstallTool, stepUninstallTool:
			r := msg.(choose.ResultMsg)
			if r.Cancelled || r.Index < 0 || r.Index >= len(m.tools) {
				return m, nil
			}
			if st == stepInstallTool {
				return m.confirmInstall(m.tools[r.Index], m.pending)
			}
			return m.confirmUninstall(m.tools[r.Index], m.pending)
		}
		done := msg.(confirm.DoneMsg)
		if done.Result.Aborted {
			return m, nil
		}
		m.list = m.list.ClearMarks()
		level := nav.LevelSuccess
		if done.Result.HasFailures() {
			level = nav.LevelWarn
		}
		m, cmd := m.reload()
		return m, tea.Batch(nav.Notify(level, confirm.Summary(done.Result)), cmd)
	}

	switch msg := msg.(type) {
	case nav.ResizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list = m.list.SetSize(msg.Width, max(msg.Height-headerLines-1, 0))
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.FilterMsg:
		m.list = m.list.SetFilter(msg.Filter)
		return m, nil
	case nav.RefreshMsg:
		return m.reload()
	case LoadedMsg:
		m.loaded = true
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m.home = msg.Home
		m.list = m.list.SetItems(msg.Entries)
		return m, nil
	}

	if c, ok := msg.(nav.CommandMsg); ok {
		if m.modal.Open() {
			return m, nil
		}
		next, cmd, _ := reslist.RunCommand(m, m.list, commands, c)
		return next, cmd
	}

	if m.modal.Open() {
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Update(msg)
		return m, cmd
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	if l, cmd, ok := m.list.HandleKey(k); ok {
		m.list = l
		return m, cmd
	}
	if next, cmd, ok := reslist.Run(m, m.list, actions, k); ok {
		return next, cmd
	}
	if key.Matches(k, m.keys.Back) {
		return m, nav.Up
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
}

// actions is every skills key and the rows it acts on (spec §6.5). A key and
// its `:` command share one function, so the key asks for the tool and the
// command takes it as an argument.
var actions = func() []reslist.Action[Model, Entry] {
	k := keys.Skills()
	return []reslist.Action[Model, Entry]{
		{Key: k.Install, Scope: reslist.Selected, Do: func(m Model, rows []Entry) (Model, tea.Cmd) { return m.install(rows, nil) }},
		{Key: k.Uninstall, Scope: reslist.Selected, Do: func(m Model, rows []Entry) (Model, tea.Cmd) { return m.uninstall(rows, nil) }},
	}
}()

// commands are the `:` commands: the CLI's words after `skill`. scan is the list
// itself, so it has no command.
var commands = []reslist.Command[Model, Entry]{
	{Name: "install", Args: "[<tool>]", Help: "link the selected skills into a tool", Scope: reslist.Selected, Do: Model.install},
	{Name: "uninstall", Args: "[<tool>]", Help: "remove the selected skills' links from a tool", Scope: reslist.Selected, Do: Model.uninstall},
}

// Commands implements nav.Commander.
func (m Model) Commands() []nav.CommandInfo { return reslist.CommandInfos(commands) }

// install is `skill install` for rows: into the tool args names, else into the
// one the chooser picks.
func (m Model) install(rows []Entry, args []string) (Model, tea.Cmd) {
	return m.withTool(rows, args, stepInstallTool, "Install into which tool?", m.toolOptions(nil))
}

// uninstall is `skill uninstall` for rows. The chooser lists the tools that hold
// one of ws's links to a selected skill.
func (m Model) uninstall(rows []Entry, args []string) (Model, tea.Cmd) {
	var holders []skill.Tool
	for _, t := range skill.Tools {
		for _, r := range rows {
			if skill.Ours(m.home, m.env.Paths.Workspace, t, r.Name) {
				holders = append(holders, t)
				break
			}
		}
	}
	if len(args) == 0 && len(holders) == 0 {
		return m, nav.Notify(nav.LevelInfo, "None of the selected skills is linked by ws.")
	}
	return m.withTool(rows, args, stepUninstallTool, "Uninstall from which tool?", m.toolOptions(holders))
}

// toolOptions orders the tools for the chooser: detected ones first. A nil pool
// means every known tool.
func (m Model) toolOptions(pool []skill.Tool) []skill.Tool {
	if pool == nil {
		pool = skill.Tools
	}
	var det, rest []skill.Tool
	for _, t := range pool {
		if t.Detected(m.home) {
			det = append(det, t)
		} else {
			rest = append(rest, t)
		}
	}
	return append(det, rest...)
}

// withTool resolves the tool from args, or opens the chooser for step.
func (m Model) withTool(rows []Entry, args []string, st step, title string, pool []skill.Tool) (Model, tea.Cmd) {
	if len(args) > 0 {
		t, ok := skill.FindTool(args[0])
		if !ok {
			return m, nav.Notify(nav.LevelWarn, fmt.Sprintf("Unknown tool %q (known: %s)", args[0], strings.Join(skill.ToolNames(), ", ")))
		}
		var next nav.Component
		var cmd tea.Cmd
		if st == stepInstallTool {
			next, cmd = m.confirmInstall(t, rows)
		} else {
			next, cmd = m.confirmUninstall(t, rows)
		}
		return next.(Model), cmd
	}
	opts := make([]choose.Option, len(pool))
	for i, t := range pool {
		detail := t.Dir
		if !t.Detected(m.home) {
			detail += " — not detected"
		}
		opts[i] = choose.Option{Label: t.Name, Detail: detail}
	}
	m.pending, m.tools = rows, pool
	m.modal = m.modal.Choose(st, choose.New(title, "One tool at a time; run it again for another.", opts))
	return m, nil
}

// confirmInstall is the CLI's plan: one link per skill that can be linked.
func (m Model) confirmInstall(t skill.Tool, rows []Entry) (nav.Component, tea.Cmd) {
	ws := m.env.Paths.Workspace
	pl := plan.Plan{Command: "skill.install"}
	var skipped []string
	for _, r := range rows {
		r := r
		switch {
		case r.Broken:
			skipped = append(skipped, r.Name+": its target is gone")
			continue
		case skill.Installed(m.home, t, r.Name):
			skipped = append(skipped, r.Name+": already there")
			continue
		}
		desc := fmt.Sprintf("Link %s → %s/%s", r.Name, t.Dir, r.Name)
		if !t.Detected(m.home) {
			desc += " (tool not detected; creates the folder)"
		}
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "skill-install-" + t.Name + "-" + r.Name,
			Description: desc,
			Execute:     func() error { return skill.Install(m.home, ws, t, skill.Skill{Name: r.Name, Path: r.Path}) },
		})
	}
	if len(pl.Actions) == 0 {
		return m, nav.Notify(nav.LevelInfo, "Nothing to install into "+t.Name+" — "+strings.Join(skipped, "; "))
	}
	c := confirm.New("Install "+format.Plural(len(pl.Actions), "skill")+" into "+t.Name, pl)
	if len(skipped) > 0 {
		c = c.WithNote("Skipped: " + strings.Join(skipped, "; "))
	}
	m.modal = m.modal.Confirm(stepChange, c)
	return m, nil
}

// confirmUninstall is the CLI's plan: one unlink per skill whose link is ws's.
func (m Model) confirmUninstall(t skill.Tool, rows []Entry) (nav.Component, tea.Cmd) {
	ws := m.env.Paths.Workspace
	pl := plan.Plan{Command: "skill.uninstall"}
	var skipped []string
	for _, r := range rows {
		r := r
		if !skill.Ours(m.home, ws, t, r.Name) {
			skipped = append(skipped, r.Name+": not a link into agent/skills/")
			continue
		}
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "skill-uninstall-" + t.Name + "-" + r.Name,
			Description: fmt.Sprintf("Unlink %s/%s (the skill in the workspace stays)", t.Dir, r.Name),
			Execute:     func() error { return skill.Uninstall(m.home, ws, t, r.Name) },
		})
	}
	if len(pl.Actions) == 0 {
		return m, nav.Notify(nav.LevelInfo, "Nothing to uninstall from "+t.Name+" — "+strings.Join(skipped, "; "))
	}
	c := confirm.New("Uninstall "+format.Plural(len(pl.Actions), "skill")+" from "+t.Name, pl)
	if len(skipped) > 0 {
		c = c.WithNote("Skipped: " + strings.Join(skipped, "; "))
	}
	m.modal = m.modal.Confirm(stepChange, c)
	return m, nil
}

// header is the screen's two lines: the folder and count, and the tools.
func (m Model) header() []string {
	if !m.loaded {
		return []string{theme.Muted.Render("Reading skills…"), ""}
	}
	var det []string
	for _, t := range skill.Tools {
		if t.Detected(m.home) {
			det = append(det, t.Name)
		}
	}
	slices.Sort(det)
	tools := theme.OK.Render("● ") + strings.Join(det, " ")
	if len(det) == 0 {
		tools = theme.Muted.Render("● no tool detected")
	}
	tools += "  " + theme.Muted.Render(fmt.Sprintf("○ %d not detected", len(skill.Tools)-len(det)))
	title := theme.Bold.Render(theme.Icon(nav.ScreenSkills.Icon())+m.env.Rel(skill.Root(m.env.Paths.Workspace))) +
		"  " + format.Plural(len(m.list.Items()), "skill")
	return []string{title, tools}
}

// View implements nav.Component.
func (m Model) View() string {
	if m.modal.Open() {
		return m.modal.View()
	}
	clip := lipgloss.NewStyle().MaxWidth(m.width)
	lines := m.header()
	for i := range lines {
		lines[i] = clip.Render(lines[i])
	}
	return lipgloss.JoinVertical(lipgloss.Left, strings.Join(lines, "\n"), "", m.list.View())
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	if m.modal.Open() {
		return m.modal.KeyMap()
	}
	return m.list.Help(m.keys)
}
