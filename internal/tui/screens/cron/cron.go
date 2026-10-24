// Package cron is the managed-cron-jobs screen: every built-in job and
// preset with its install state and last run, plus add, remove, run now, the
// per-job output, and `:apply` (spec §6.5).
//
// Navigation levels: list → job log. esc returns to the list; esc in the
// list asks the app to go up.
package cron

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	wscron "github.com/mugenkunou/ws-tool/internal/cron"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/tui/confirm"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/modal"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

const logLines = 200

// Job is one cron job or preset and its install state.
type Job = wscron.Job

// LoadedMsg carries cron job state. Other components (the dashboard) may
// observe it.
type LoadedMsg struct {
	Jobs    []Job
	CronErr error // crontab not readable (preflight failure)
}

// Failing returns installed jobs whose last run exited non-zero.
func (m LoadedMsg) Failing() []Job {
	var out []Job
	for _, j := range m.Jobs {
		if j.Installed && !j.Preset && j.LastRun.ExitCode != 0 {
			out = append(out, j)
		}
	}
	return out
}

// Installed counts installed (non-preset) jobs.
func (m LoadedMsg) Installed() int {
	n := 0
	for _, j := range m.Jobs {
		if j.Installed && !j.Preset {
			n++
		}
	}
	return n
}

// SelectJobMsg asks the screen to select a job by name.
type SelectJobMsg struct{ Name string }

// RequestLogMsg asks the screen to select a job and show its log.
type RequestLogMsg struct{ Name string }

type logMsg struct {
	job  string
	text string
}

// readyMsg reports a passed (or failed) preflight before a crontab change.
type readyMsg struct {
	jobs   []string // job or preset names
	remove bool
	apply  bool // `cron apply`: no jobs named
	err    error
}

type level int

const (
	levelList level = iota
	levelLog
)

type step int

const stepChange step = iota

// Model is the cron screen state.
type Model struct {
	env        env.Env
	keys       keys.CronMap
	detailKeys keys.DetailMap
	list       reslist.Model[Job]
	log        viewport.Model
	level      level
	modal      modal.Model
	state      LoadedMsg
	loaded     bool
	logJob     string
	width      int
	height     int
}

// New creates the screen.
func New(e env.Env) Model {
	dk := keys.Detail()
	vp := viewport.New()
	vp.KeyMap = dk.Viewport
	vp.SoftWrap = true
	return Model{
		env:        e,
		keys:       keys.Cron(),
		detailKeys: dk,
		log:        vp,
		list: reslist.New(wscron.Schema(e.View()), []reslist.Col[Job]{
			{Key: "name", Min: 14},
			{Key: "schedule", Min: 12, Style: func(j Job, t string) string {
				if j.Preset {
					return theme.Muted.Render(t)
				}
				return t
			}},
			{Key: "state", Min: 14, Style: func(j Job, t string) string {
				switch {
				case !j.CrontabOK, j.State == wscron.StateOutdated, j.State == wscron.StateNotApplied, j.State == wscron.StateUndeclared:
					return theme.Warn.Render(t)
				case j.Installed:
					return theme.OK.Render("✔ " + t)
				}
				return theme.Muted.Render(t)
			}},
			{Key: "last_run", Min: 16, Style: func(j Job, t string) string {
				if j.LastRun.ExitCode != 0 {
					return theme.Error.Render(t)
				}
				return t
			}},
			{Key: "next_run", Min: 9},
			{Key: "description", Min: 12, Weight: 1},
		}, "No cron jobs available.").Declare(reslist.Decls(actions)),
	}
}

// Load gathers cron job state.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		jobs, err := wscron.Jobs(e.Paths.Workspace)
		return LoadedMsg{Jobs: jobs, CronErr: err}
	}
}

func loadLog(job string) tea.Cmd {
	return func() tea.Msg {
		logPath, err := wscron.LogFilePath()
		if err != nil {
			return logMsg{job: job, text: theme.Error.Render(err.Error())}
		}
		lines, err := wscron.ReadLog(logPath, []string{job}, logLines)
		if err != nil {
			return logMsg{job: job, text: theme.Error.Render(err.Error())}
		}
		if len(lines) == 0 {
			return logMsg{job: job, text: theme.Muted.Render("(no log entries yet)")}
		}
		return logMsg{job: job, text: strings.Join(lines, "\n")}
	}
}

// preflight checks crontab access off the update loop before a change.
func preflight(jobs []string, remove bool) tea.Cmd {
	return func() tea.Msg { return readyMsg{jobs: jobs, remove: remove, err: wscron.Preflight()} }
}

// preflightApply is preflight for `cron apply`.
func preflightApply() tea.Cmd {
	return func() tea.Msg { return readyMsg{apply: true, err: wscron.Preflight()} }
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
	if _, ok := m.modal.Match(msg); ok {
		m.modal = m.modal.Close()
		done := msg.(confirm.DoneMsg)
		if done.Result.Aborted {
			return m, nil
		}
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
		m.list = m.list.SetSize(msg.Width, max(msg.Height-1, 0))
		m.log.SetWidth(msg.Width)
		m.log.SetHeight(max(msg.Height-1, 0))
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.RefreshMsg:
		return m.reload()
	case LoadedMsg:
		m.loaded, m.state = true, msg
		m.list = m.list.SetItems(msg.Jobs)
		return m, nil
	case SelectJobMsg:
		m.level = levelList
		return m.selectJob(msg.Name), nil
	case RequestLogMsg:
		return m.selectJob(msg.Name).openLog(msg.Name)
	case logMsg:
		if msg.job == m.logJob {
			m.log.SetContent(msg.text)
			m.log.GotoBottom()
		}
		return m, nil
	case readyMsg:
		if msg.err != nil {
			text := msg.err.Error()
			var pe *wscron.PreflightError
			if errors.As(msg.err, &pe) {
				text += " — " + pe.Remediation()
			}
			return m, nav.Notify(nav.LevelError, text)
		}
		if msg.apply {
			return m.openApply()
		}
		return m.openChange(msg.jobs, msg.remove)
	case nav.CommandMsg:
		if m.modal.Open() || m.level != levelList {
			return m, nil
		}
		next, cmd, _ := reslist.RunCommand(m, m.list, commands, msg)
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
	if m.level == levelLog {
		if key.Matches(k, m.detailKeys.Back) {
			m.level = levelList
			return m, nil
		}
		var cmd tea.Cmd
		m.log, cmd = m.log.Update(k)
		return m, cmd
	}

	if l, cmd, ok := m.list.HandleKey(k); ok {
		m.list = l
		return m, cmd
	}
	if next, cmd, ok := reslist.Run(m, m.list, actions, k); ok {
		return next, cmd
	}
	switch {
	case key.Matches(k, m.keys.Back):
		return m, nav.Up
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
}

// names are the jobs' names.
func names(rows []Job) []string {
	out := make([]string, len(rows))
	for i, j := range rows {
		out[i] = j.Name
	}
	return out
}

// actions is every cron key and the rows it acts on (spec §6.5).
var actions = func() []reslist.Action[Model, Job] {
	k := keys.Cron()
	return []reslist.Action[Model, Job]{
		{Key: k.Output, Scope: reslist.Cursor, Do: func(m Model, rows []Job) (Model, tea.Cmd) {
			if rows[0].Preset {
				return m, nav.Notify(nav.LevelInfo, "Presets have no log of their own — open a member job")
			}
			return m.openLog(rows[0].Name)
		}},
		{Key: k.Add, Scope: reslist.Selected, Do: func(m Model, rows []Job) (Model, tea.Cmd) { return m, preflight(names(rows), false) }},
		{Key: k.Remove, Scope: reslist.Selected, Do: func(m Model, rows []Job) (Model, tea.Cmd) { return m, preflight(names(rows), true) }},
		{Key: k.Run, Scope: reslist.Selected, Do: Model.openRun},
	}
}()

// commands are the `:` commands: the CLI's words after `cron`.
var commands = []reslist.Command[Model, Job]{
	{Name: "apply", Help: "make the crontab match configs/crontab", Scope: reslist.Global,
		Do: func(m Model, _ []Job, _ []string) (Model, tea.Cmd) { return m, preflightApply() }},
}

// Commands implements nav.Commander.
func (m Model) Commands() []nav.CommandInfo { return reslist.CommandInfos(commands) }

// planOptions say where `cron add`, `rm` and `apply` read and write: this
// binary, this workspace, and its configs/crontab.
func (m Model) planOptions() wscron.PlanOptions {
	p := m.env.Paths
	return wscron.PlanOptions{Bin: wscron.WSBinary(), Workspace: p.Workspace, Display: wscron.DefaultDisplay(), Declared: wscron.DeclaredPath(p.Workspace)}
}

// openRun is `cron run <job>` (no --if-due) through a checklist with one
// action per job; a preset runs its members.
func (m Model) openRun(rows []Job) (Model, tea.Cmd) {
	var jobs []*wscron.BuiltinJob
	seen := map[string]bool{}
	for _, name := range names(rows) {
		resolved, _, err := wscron.Resolve(name)
		if err != nil {
			return m, nav.Notify(nav.LevelError, err.Error())
		}
		for _, j := range resolved {
			if !seen[j.Name] {
				seen[j.Name] = true
				jobs = append(jobs, j)
			}
		}
	}
	workspace := m.env.Paths.Workspace
	pl := plan.Plan{Command: "cron.run"}
	for _, j := range jobs {
		name := j.Name
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "cron-run-" + name,
			Description: "Run " + name + " now",
			Execute: func() error {
				res, err := wscron.RunJob(name, wscron.RunOptions{WorkspacePath: workspace, Display: wscron.DefaultDisplay()})
				if err != nil {
					return err
				}
				if res.ExitCode != 0 {
					return fmt.Errorf("%s exited with %d", name, res.ExitCode)
				}
				return nil
			},
		})
	}
	m.modal = m.modal.Confirm(stepChange, confirm.New("Run "+strings.Join(names(rows), ", "), pl))
	return m, nil
}

// openApply is `cron apply`: the CLI's plan.
func (m Model) openApply() (Model, tea.Cmd) {
	opts := m.planOptions()
	actions, _, err := wscron.ApplyActions(opts)
	if err != nil {
		return m, nav.Notify(nav.LevelError, err.Error())
	}
	if len(actions) == 0 {
		return m, nav.Notify(nav.LevelInfo, "The crontab already matches "+m.env.Rel(opts.Declared)+".")
	}
	m.modal = m.modal.Confirm(stepChange, confirm.New("Apply "+m.env.Rel(opts.Declared)+" to the crontab", plan.Plan{Command: "cron.apply", Actions: actions}))
	return m, nil
}

func (m Model) openLog(name string) (Model, tea.Cmd) {
	m.level, m.logJob = levelLog, name
	m.log.SetContent(theme.Muted.Render("Loading…"))
	return m, loadLog(name)
}

func (m Model) selectJob(name string) Model {
	m.list, _ = m.list.Select(func(j Job) bool { return j.Name == name })
	return m
}

// openChange installs or removes the named jobs; presets expand to their
// members, each job once.
func (m Model) openChange(names []string, remove bool) (Model, tea.Cmd) {
	var jobs []*wscron.BuiltinJob
	seen := map[string]bool{}
	for _, name := range names {
		resolved, _, err := wscron.Resolve(name)
		if err != nil {
			return m, nav.Notify(nav.LevelError, err.Error())
		}
		for _, j := range resolved {
			if !seen[j.Name] {
				seen[j.Name] = true
				jobs = append(jobs, j)
			}
		}
	}
	opts := m.planOptions()
	pl := plan.Plan{Command: "cron.add"}
	what := strings.Join(names, ", ")
	title := "Install cron job " + what
	if remove {
		var jobNames []string
		for _, j := range jobs {
			jobNames = append(jobNames, j.Name)
		}
		actions, _, err := wscron.RemoveActions(jobNames, opts)
		if err != nil {
			return m, nav.Notify(nav.LevelError, err.Error())
		}
		pl = plan.Plan{Command: "cron.rm", Actions: actions}
		title = "Remove cron job " + what
	} else {
		actions, _, err := wscron.AddActions(jobs, opts)
		if err != nil {
			return m, nav.Notify(nav.LevelError, err.Error())
		}
		pl.Actions = actions
	}
	m.modal = m.modal.Confirm(stepChange, confirm.New(title, pl))
	return m, nil
}

func (m Model) statusLine() string {
	if !m.loaded {
		return theme.Muted.Render("Reading crontab…")
	}
	if m.state.CronErr != nil {
		return theme.Warn.Render(m.state.CronErr.Error())
	}
	parts := []string{format.Plural(m.state.Installed(), "job") + " installed"}
	if f := m.state.Failing(); len(f) > 0 {
		parts = append(parts, theme.Error.Render(fmt.Sprintf("%d failing", len(f))))
	}
	return strings.Join(parts, theme.Muted.Render(" · "))
}

// View implements nav.Component.
func (m Model) View() string {
	clip := lipgloss.NewStyle().MaxWidth(m.width)
	switch {
	case m.modal.Open():
		return m.modal.View()
	case m.level == levelLog:
		return lipgloss.JoinVertical(lipgloss.Left, clip.Render(theme.Bold.Render("Cron log: "+m.logJob)), m.log.View())
	}
	return lipgloss.JoinVertical(lipgloss.Left, clip.Render(m.statusLine()), m.list.View())
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	switch {
	case m.modal.Open():
		return m.modal.KeyMap()
	case m.level == levelLog:
		return m.detailKeys
	}
	return m.list.Help(m.keys)
}
