// Package system is the machine-integration screen: soft-delete (trash)
// status and actions, managed cron jobs, and workspace-level operations
// (view config, restore wizard, reset).
//
// Reset runs natively through the plan checklist and then asks the app to
// reload the environment (the workspace is no longer initialized). The
// restore wizard and config view are handed to the CLI.
//
// Navigation levels: list → job log. esc returns to the list; esc in the
// list asks the app to go up.
package system

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/cron"
	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/trash"
	"github.com/mugenkunou/ws-tool/internal/tui/confirm"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/handover"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/layout"
	"github.com/mugenkunou/ws-tool/internal/tui/listview"
	"github.com/mugenkunou/ws-tool/internal/tui/modal"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
	"github.com/mugenkunou/ws-tool/internal/workspace"
)

const cronLogLines = 200

// Job is one cron job or preset and its install state.
type Job struct {
	Name        string
	Description string
	Schedule    string // empty for presets
	Preset      bool
	Installed   bool
	LastRun     cron.RunRecord
}

// LoadedMsg carries trash and cron state. Other components (the dashboard)
// may observe it.
type LoadedMsg struct {
	Trash     trash.Status
	TrashScan trash.ScanResult
	Jobs      []Job
	CronErr   error // crontab not readable (preflight failure)
	Err       error
}

type logMsg struct {
	job  string
	text string
}

// cronReadyMsg reports a passed (or failed) preflight before a cron change.
type cronReadyMsg struct {
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

const (
	stepCron step = iota
	stepTrash
	stepReset
)

type handoverTag int

const handoverWizard handoverTag = iota

// Model is the system screen state.
type Model struct {
	env        env.Env
	keys       keys.SystemMap
	detailKeys keys.DetailMap
	list       listview.Model
	log        viewport.Model
	level      level
	modal      modal.Model

	loaded bool
	state  LoadedMsg
	logJob string
	width  int
	height int
}

const headerLines = 4 // trash (2) + blank + cron title

// New creates the screen.
func New(e env.Env) Model {
	dk := keys.Detail()
	vp := viewport.New()
	vp.KeyMap = dk.Viewport
	vp.SoftWrap = true
	return Model{
		env:        e,
		keys:       keys.System(),
		detailKeys: dk,
		log:        vp,
		list: listview.New([]layout.Col{
			{Title: "Cron job", Min: 14},
			{Title: "Schedule", Min: 12},
			{Title: "State", Min: 13},
			{Title: "Last run", Min: 16},
			{Title: "Description", Min: 12, Weight: 1},
		}, "No cron jobs available."),
	}
}

func trashRoot(e env.Env) string {
	root := config.WithAbsPaths(e.Config, e.Paths.Workspace).Trash.RootDir
	if root == "" {
		root, _ = config.ExpandUserPath("~/.Trash")
	}
	return root
}

// Load gathers trash status and cron job state.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		root := trashRoot(e)
		var msg LoadedMsg
		st, err := trash.GetStatus(root)
		if err != nil {
			return LoadedMsg{Err: err}
		}
		msg.Trash = st
		msg.TrashScan, _ = trash.Scan(trash.ScanOptions{RootDir: st.RootDir, WarnSizeMB: e.Config.Trash.WarnSizeMB})

		msg.CronErr = cron.Preflight()
		statePath, _ := cron.StateFilePath()
		for _, name := range cron.AllNames() {
			j := Job{Name: name}
			if b, ok := cron.Builtins[name]; ok {
				j.Description, j.Schedule = b.Description, b.Schedule
			} else if members, ok := cron.Presets[name]; ok {
				j.Preset = true
				j.Description = "preset: " + strings.Join(members, ", ")
			}
			if msg.CronErr == nil {
				if j.Preset {
					j.Installed = presetInstalled(name)
				} else {
					j.Installed, _ = cron.HasJob(name)
				}
			}
			if !j.Preset {
				j.LastRun, _ = cron.LastRun(statePath, name)
			}
			msg.Jobs = append(msg.Jobs, j)
		}
		return msg
	}
}

func presetInstalled(name string) bool {
	jobs, err := cron.Resolve(name)
	if err != nil || len(jobs) == 0 {
		return false
	}
	for _, j := range jobs {
		if ok, _ := cron.HasJob(j.Name); !ok {
			return false
		}
	}
	return true
}

func loadLog(job string) tea.Cmd {
	return func() tea.Msg {
		logPath, err := cron.LogFilePath()
		if err != nil {
			return logMsg{job: job, text: theme.Error.Render(err.Error())}
		}
		lines, err := cron.ReadLog(logPath, job, cronLogLines)
		if err != nil {
			return logMsg{job: job, text: theme.Error.Render(err.Error())}
		}
		if len(lines) == 0 {
			return logMsg{job: job, text: theme.Muted.Render("(no log entries)")}
		}
		return logMsg{job: job, text: strings.Join(lines, "\n")}
	}
}

// cronPreflight checks crontab access off the update loop before a change.
func cronPreflight(job string, remove bool) tea.Cmd {
	return func() tea.Msg { return cronReadyMsg{job: job, remove: remove, err: cron.Preflight()} }
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
	if st, ok := m.modal.Match(msg); ok {
		m.modal = m.modal.Close()
		return m.dialogResult(st.(step), msg.(confirm.DoneMsg))
	}

	switch msg := msg.(type) {
	case nav.ResizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list = m.list.SetSize(msg.Width, max(msg.Height-headerLines, 0))
		m.log.SetWidth(msg.Width)
		m.log.SetHeight(max(msg.Height-1, 0))
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.RefreshMsg:
		return m.reload()
	case LoadedMsg:
		m.loaded = true
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m.state = msg
		m.list = m.list.SetRows(m.rows())
		return m, nil
	case logMsg:
		if msg.job == m.logJob {
			m.log.SetContent(msg.text)
			m.log.GotoBottom()
		}
		return m, nil
	case cronReadyMsg:
		if msg.err != nil {
			text := msg.err.Error()
			var pe *cron.PreflightError
			if errors.As(msg.err, &pe) {
				text += " — " + pe.Remediation()
			}
			return m, nav.Notify(nav.LevelError, text)
		}
		return m.openCron(msg.job, msg.remove)
	case handover.DoneMsg:
		if _, ok := msg.Tag.(handoverTag); ok {
			return m.reload()
		}
		return m, nil
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
	return m.updateList(k)
}

func (m Model) selected() (Job, bool) {
	i := m.list.Cursor()
	if i < 0 || i >= len(m.state.Jobs) {
		return Job{}, false
	}
	return m.state.Jobs[i], true
}

func (m Model) updateList(k tea.KeyPressMsg) (nav.Component, tea.Cmd) {
	j, haveJob := m.selected()
	switch {
	case key.Matches(k, m.keys.Back):
		return m, nav.Up
	case key.Matches(k, m.keys.Refresh):
		return m.reload()
	case key.Matches(k, m.keys.CronLog) && haveJob:
		if j.Preset {
			return m, nav.Notify(nav.LevelInfo, "Presets have no log of their own; open a member job")
		}
		m.level, m.logJob = levelLog, j.Name
		m.log.SetContent(theme.Muted.Render("Loading…"))
		return m, loadLog(j.Name)
	case key.Matches(k, m.keys.CronAdd) && haveJob:
		return m, cronPreflight(j.Name, false)
	case key.Matches(k, m.keys.CronRemove) && haveJob:
		return m, cronPreflight(j.Name, true)
	case key.Matches(k, m.keys.TrashEnable):
		return m.openTrashEnable(), nil
	case key.Matches(k, m.keys.TrashDisable):
		return m.openTrashDisable(), nil
	case key.Matches(k, m.keys.TrashEmpty):
		return m.openTrashEmpty(), nil
	case key.Matches(k, m.keys.Config):
		return m, handover.Run(handover.Paged(handover.WS(m.env.Paths, "config", "view")), handoverWizard)
	case key.Matches(k, m.keys.Restore):
		return m, handover.Run(handover.Pause(handover.WS(m.env.Paths, "restore")), handoverWizard)
	case key.Matches(k, m.keys.Reset):
		return m.openReset(), nil
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
}

func (m Model) dialogResult(st step, done confirm.DoneMsg) (nav.Component, tea.Cmd) {
	if done.Result.Aborted {
		return m, nil
	}
	level := nav.LevelSuccess
	if done.Result.HasFailures() {
		level = nav.LevelWarn
	}
	notice := nav.Notify(level, confirm.Summary(done.Result))
	if st == stepReset && done.Result.WasExecuted("remove-ws-dir") {
		return m, tea.Batch(notice, nav.ReloadEnv)
	}
	m, cmd := m.reload()
	return m, tea.Batch(notice, cmd)
}

func (m Model) openCron(name string, remove bool) (Model, tea.Cmd) {
	jobs, err := cron.Resolve(name)
	if err != nil {
		return m, nav.Notify(nav.LevelError, err.Error())
	}
	p := m.env.Paths
	pl := plan.Plan{Command: "cron.add"}
	title := "Install cron job " + name
	if remove {
		pl = plan.Plan{Command: "cron.rm", Actions: cron.RemoveActions(jobs, p.Manifest)}
		title = "Remove cron job " + name
	} else {
		actions, err := cron.AddActions(jobs, cron.WSBinary(), cron.DefaultDisplay(), p.Workspace, p.Manifest)
		if err != nil {
			return m, nav.Notify(nav.LevelError, err.Error())
		}
		pl.Actions = actions
	}
	m.modal = m.modal.Confirm(stepCron, confirm.New(title, pl))
	return m, nil
}

// openTrashEnable mirrors `ws trash enable` with all integrations (use the
// CLI's --no-… flags for a subset).
func (m Model) openTrashEnable() Model {
	root, mp := trashRoot(m.env), m.env.Paths.Manifest
	pl := plan.Plan{Command: "trash.enable", Actions: []plan.Action{
		{
			ID:          "trash-setup",
			Description: "Enable soft-delete: shell rm, VS Code, file explorer → " + root,
			Execute: func() error {
				_, err := trash.Setup(trash.SetupOptions{RootDir: root, ShellRM: true, VSCodeDelete: true, FileExplorer: true})
				return err
			},
		},
		{
			ID:          "trash-record-provisions",
			Description: "Record trash provisions (so `ws reset` can undo them)",
			Execute: func() error {
				if err := trash.RecordShellProvisions(mp); err != nil {
					return err
				}
				return trash.RecordExplorerProvision(mp, root)
			},
		},
	}}
	c := confirm.New("Enable soft-delete", pl).WithNote("For a subset of integrations use `ws trash enable --no-shell-rm|--no-vscode|--no-file-explorer`.")
	m.modal = m.modal.Confirm(stepTrash, c)
	return m
}

func (m Model) openTrashDisable() Model {
	p := m.env.Paths
	pl := plan.Plan{Command: "trash.reset", Actions: []plan.Action{{
		ID:          "trash-reset",
		Description: "Remove soft-delete integrations (script, aliases, symlink)",
		Execute: func() error {
			_, err := trash.Reset(trash.ResetOptions{WorkspacePath: p.Workspace, ManifestPath: p.Manifest})
			return err
		},
	}}}
	m.modal = m.modal.Confirm(stepTrash, confirm.New("Disable soft-delete", pl))
	return m
}

func (m Model) openTrashEmpty() Model {
	root := m.state.Trash.RootDir
	if root == "" {
		root = trashRoot(m.env)
	}
	sc := m.state.TrashScan
	pl := plan.Plan{Command: "trash.empty", Actions: []plan.Action{{
		ID:          "trash-empty",
		Description: fmt.Sprintf("Permanently delete %s (%s) in %s", format.Plural(sc.FileCount, "file"), style.HumanBytes(sc.SizeBytes), root),
		Execute: func() error {
			_, err := trash.Empty(trash.EmptyOptions{RootDir: root})
			return err
		},
	}}}
	m.modal = m.modal.Confirm(stepTrash, confirm.New("Empty trash", pl))
	return m
}

// openReset mirrors `ws reset`: undo dotfiles and trash, remove ws/ and the
// config. Nothing is pre-checked — this is destructive.
func (m Model) openReset() Model {
	p := m.env.Paths
	wsDir := filepath.Join(p.Workspace, "ws")
	pl := plan.Plan{Command: "reset", Actions: []plan.Action{
		{
			ID:          "reset-dotfiles",
			Description: "Reset dotfiles (restore originals, remove symlinks)",
			Execute: func() error {
				_, err := dotfile.Reset(dotfile.ResetOptions{WorkspacePath: p.Workspace, ManifestPath: p.Manifest})
				return err
			},
		},
		{
			ID:          "reset-trash",
			Description: "Reset trash setup (remove script and aliases)",
			Execute: func() error {
				_, err := trash.Reset(trash.ResetOptions{WorkspacePath: p.Workspace, ManifestPath: p.Manifest})
				return err
			},
		},
		{
			ID:          "remove-ws-dir",
			Description: "Remove " + wsDir,
			Execute:     func() error { return os.RemoveAll(wsDir) },
		},
		{
			ID:          "remove-config",
			Description: "Remove " + p.Config,
			Execute: func() error {
				if err := os.Remove(p.Config); err != nil && !os.IsNotExist(err) {
					return err
				}
				return nil
			},
		},
	}}
	note := "Reverses `ws init`. Tick each step to undo (a selects all)."
	if entries, err := workspace.Provisions(p.Workspace); err == nil && len(entries) > 0 {
		note += fmt.Sprintf(" %s recorded.", format.Plural(len(entries), "provision"))
	}
	c := confirm.New("Reset workspace", pl).WithNote(note).WithChecked(func(int, plan.Action) bool { return false })
	m.modal = m.modal.Confirm(stepReset, c)
	return m
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

func (m Model) header() string {
	clip := lipgloss.NewStyle().MaxWidth(m.width)
	if !m.loaded {
		return clip.Render(theme.Muted.Render("Loading…")) + "\n\n\n"
	}
	t := m.state.Trash
	on := func(ok bool, label string) string {
		if ok {
			return theme.OK.Render("✔ " + label)
		}
		return theme.Muted.Render("✘ " + label)
	}
	sc := m.state.TrashScan
	size := style.HumanBytes(sc.SizeBytes) + ", " + format.Plural(sc.FileCount, "file")
	if sc.OverLimit {
		size = theme.Warn.Render(size + " — over " + fmt.Sprintf("%d MB", sc.WarnSizeMB))
	}
	line1 := theme.Bold.Render("Trash ") + m.env.Rel(t.RootDir) + theme.Muted.Render("  ") + size
	line2 := "      " + strings.Join([]string{on(t.ShellRMConfigured, "shell rm"), on(t.VSCodeConfigured, "VS Code"), on(t.FileExplorerConfigured, "file explorer")}, "  ")
	cronTitle := theme.Bold.Render("Cron")
	if m.state.CronErr != nil {
		cronTitle += "  " + theme.Warn.Render(m.state.CronErr.Error())
	}
	return strings.Join([]string{clip.Render(line1), clip.Render(line2), "", clip.Render(cronTitle)}, "\n")
}

// View implements nav.Component.
func (m Model) View() string {
	switch {
	case m.modal.Open():
		return m.modal.View()
	case m.level == levelLog:
		head := lipgloss.NewStyle().MaxWidth(m.width).Render(theme.Bold.Render("Cron log: " + m.logJob))
		return lipgloss.JoinVertical(lipgloss.Left, head, m.log.View())
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.header(), m.list.View())
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
