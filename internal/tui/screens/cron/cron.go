// Package cron is the managed-cron-jobs screen: every built-in job and
// preset with its install state and last run, plus install, remove, and the
// per-job log.
//
// Navigation levels: list → job log. esc returns to the list; esc in the
// list asks the app to go up.
package cron

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	wscron "github.com/mugenkunou/ws-tool/internal/cron"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/tui/confirm"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/layout"
	"github.com/mugenkunou/ws-tool/internal/tui/listview"
	"github.com/mugenkunou/ws-tool/internal/tui/modal"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

const logLines = 200

// Job is one cron job or preset and its install state.
type Job struct {
	Name        string
	Description string
	Schedule    string // empty for presets
	Preset      bool
	Installed   bool
	LastRun     wscron.RunRecord
}

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
	job    string
	remove bool
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
	list       listview.Model
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
		list: listview.New([]layout.Col{
			{Title: "Job", Min: 14},
			{Title: "Schedule", Min: 12},
			{Title: "State", Min: 13},
			{Title: "Last run", Min: 16},
			{Title: "Description", Min: 12, Weight: 1},
		}, "No cron jobs available."),
	}
}

// Load gathers cron job state.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		msg := LoadedMsg{CronErr: wscron.Preflight()}
		statePath, _ := wscron.StateFilePath()
		for _, name := range wscron.AllNames() {
			j := Job{Name: name}
			if b, ok := wscron.Builtins[name]; ok {
				j.Description, j.Schedule = b.Description, b.Schedule
			} else if members, ok := wscron.Presets[name]; ok {
				j.Preset = true
				j.Description = "preset: " + strings.Join(members, ", ")
			}
			if msg.CronErr == nil {
				if j.Preset {
					j.Installed = presetInstalled(name)
				} else {
					j.Installed, _ = wscron.HasJob(name)
				}
			}
			if !j.Preset {
				j.LastRun, _ = wscron.LastRun(statePath, name)
			}
			msg.Jobs = append(msg.Jobs, j)
		}
		return msg
	}
}

func presetInstalled(name string) bool {
	jobs, err := wscron.Resolve(name)
	if err != nil || len(jobs) == 0 {
		return false
	}
	for _, j := range jobs {
		if ok, _ := wscron.HasJob(j.Name); !ok {
			return false
		}
	}
	return true
}

func loadLog(job string) tea.Cmd {
	return func() tea.Msg {
		logPath, err := wscron.LogFilePath()
		if err != nil {
			return logMsg{job: job, text: theme.Error.Render(err.Error())}
		}
		lines, err := wscron.ReadLog(logPath, job, logLines)
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
func preflight(job string, remove bool) tea.Cmd {
	return func() tea.Msg { return readyMsg{job: job, remove: remove, err: wscron.Preflight()} }
}

// Init starts the first load.
func (m Model) Init() tea.Cmd { return Load(m.env) }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() }

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
		m.list = m.list.SetRows(m.rows())
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
		return m.openChange(msg.job, msg.remove)
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

	j, haveJob := m.selected()
	switch {
	case key.Matches(k, m.keys.Back):
		return m, nav.Up
	case key.Matches(k, m.keys.Refresh):
		return m.reload()
	case key.Matches(k, m.keys.Log) && haveJob:
		if j.Preset {
			return m, nav.Notify(nav.LevelInfo, "Presets have no log of their own — open a member job")
		}
		return m.openLog(j.Name)
	case key.Matches(k, m.keys.Add) && haveJob:
		return m, preflight(j.Name, false)
	case key.Matches(k, m.keys.Remove) && haveJob:
		return m, preflight(j.Name, true)
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
}

func (m Model) openLog(name string) (Model, tea.Cmd) {
	m.level, m.logJob = levelLog, name
	m.log.SetContent(theme.Muted.Render("Loading…"))
	return m, loadLog(name)
}

func (m Model) selectJob(name string) Model {
	for i, j := range m.state.Jobs {
		if j.Name == name {
			m.list = m.list.SetCursor(i)
		}
	}
	return m
}

func (m Model) selected() (Job, bool) {
	i := m.list.Cursor()
	if i < 0 || i >= len(m.state.Jobs) {
		return Job{}, false
	}
	return m.state.Jobs[i], true
}

func (m Model) openChange(name string, remove bool) (Model, tea.Cmd) {
	jobs, err := wscron.Resolve(name)
	if err != nil {
		return m, nav.Notify(nav.LevelError, err.Error())
	}
	p := m.env.Paths
	pl := plan.Plan{Command: "cron.add"}
	title := "Install cron job " + name
	if remove {
		pl = plan.Plan{Command: "cron.rm", Actions: wscron.RemoveActions(jobs, p.Manifest)}
		title = "Remove cron job " + name
	} else {
		actions, err := wscron.AddActions(jobs, wscron.WSBinary(), wscron.DefaultDisplay(), p.Workspace, p.Manifest)
		if err != nil {
			return m, nav.Notify(nav.LevelError, err.Error())
		}
		pl.Actions = actions
	}
	m.modal = m.modal.Confirm(stepChange, confirm.New(title, pl))
	return m, nil
}

func (m Model) rows() []table.Row {
	out := make([]table.Row, 0, len(m.state.Jobs))
	for _, j := range m.state.Jobs {
		state := theme.Muted.Render("not installed")
		switch {
		case m.state.CronErr != nil:
			state = theme.Warn.Render("crontab n/a")
		case j.Installed:
			state = theme.OK.Render("✔ installed")
		}
		last := ""
		if !j.LastRun.Time.IsZero() {
			last = format.Age(time.Since(j.LastRun.Time)) + " ago"
			if j.LastRun.ExitCode != 0 {
				last = theme.Error.Render(fmt.Sprintf("%s (exit %d)", last, j.LastRun.ExitCode))
			}
		}
		sched := j.Schedule
		if j.Preset {
			sched = theme.Muted.Render("preset")
		}
		out = append(out, table.Row{j.Name, sched, state, last, j.Description})
	}
	return out
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
	return m.keys
}
