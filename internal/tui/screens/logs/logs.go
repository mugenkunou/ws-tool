// Package logs is the recorded-terminal-sessions screen: list, view, search,
// start/stop recording, delete, and prune.
//
// Starting a recording hands the terminal to `ws log start` (a script(1)
// PTY shell); the TUI resumes when that shell exits.
//
// Navigation levels: list → viewer | search results. esc returns to the
// list; esc in the list asks the app to go up.
package logs

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	wslog "github.com/mugenkunou/ws-tool/internal/log"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/search"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/tui/confirm"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/handover"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/layout"
	"github.com/mugenkunou/ws-tool/internal/tui/listview"
	"github.com/mugenkunou/ws-tool/internal/tui/modal"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/prompt"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// LoadedMsg carries the session list, newest first. Other components (the
// dashboard) may observe it.
type LoadedMsg struct {
	Sessions []wslog.Session
	Err      error
}

// Active reports whether a recording is in progress.
func (m LoadedMsg) Active() bool {
	for _, s := range m.Sessions {
		if s.Active {
			return true
		}
	}
	return false
}

type showMsg struct {
	tag, mode, text string
	err             error
}

type searchMsg struct {
	query   string
	matches []search.Match
	err     error
}

type level int

const (
	levelList level = iota
	levelViewer
	levelSearch
)

const (
	modeFull     = "full"
	modeCommands = "commands-only"
)

type step int

const (
	stepStartTag step = iota
	stepStop
	stepDelete
	stepPrune
	stepSearchQuery
)

type handoverTag int

const handoverRecording handoverTag = iota

// Model is the logs screen state.
type Model struct {
	env        env.Env
	keys       keys.LogsMap
	viewerKeys keys.ViewerMap
	list       listview.Model
	results    listview.Model
	viewer     viewport.Model
	level      level
	modal      modal.Model

	sessions []wslog.Session
	matches  []search.Match
	query    string
	viewTag  string
	viewMode string
	width    int
	height   int
}

// New creates the screen.
func New(e env.Env) Model {
	k := keys.Logs()
	dk := keys.Detail()
	vp := viewport.New()
	vp.KeyMap = dk.Viewport
	vp.SoftWrap = true
	return Model{
		env:        e,
		keys:       k,
		viewerKeys: keys.ViewerMap{Detail: dk, ToggleMode: k.ToggleMode},
		viewer:     vp,
		viewMode:   modeFull,
		list: listview.New([]layout.Col{
			{Title: "Tag", Min: 14, Weight: 3},
			{Title: "Started", Min: 16},
			{Title: "Duration", Min: 8},
			{Title: "Cmds", Min: 4},
			{Title: "Size", Min: 9},
			{Title: "State", Min: 11},
		}, "No recorded sessions. Press s to start one."),
		results: listview.New([]layout.Col{
			{Title: "Session", Min: 14, Weight: 1},
			{Title: "Line", Min: 5},
			{Title: "Match", Min: 20, Weight: 4},
		}, "No matches."),
	}
}

// Load lists recorded sessions.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		sessions, err := wslog.List(e.LogDir)
		return LoadedMsg{Sessions: sessions, Err: err}
	}
}

func show(e env.Env, tag, mode string) tea.Cmd {
	return func() tea.Msg {
		text, err := wslog.Show(e.LogDir, tag, mode)
		// Recordings are raw PTY streams: strip escapes and carriage returns.
		text = strings.ReplaceAll(ansi.Strip(text), "\r", "")
		return showMsg{tag: tag, mode: mode, text: text, err: err}
	}
}

func searchLogs(e env.Env, query string) tea.Cmd {
	return func() tea.Msg {
		matches, err := search.Run(search.Options{WorkspacePath: e.LogDir, Query: query})
		return searchMsg{query: query, matches: matches, err: err}
	}
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
		return m.dialogResult(st.(step), msg)
	}

	switch msg := msg.(type) {
	case nav.ResizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list = m.list.SetSize(msg.Width, msg.Height)
		m.results = m.results.SetSize(msg.Width, max(msg.Height-1, 0))
		m.viewer.SetWidth(msg.Width)
		m.viewer.SetHeight(max(msg.Height-1, 0))
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.RefreshMsg:
		return m.reload()
	case LoadedMsg:
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m.sessions = msg.Sessions
		m.list = m.list.SetRows(rows(msg.Sessions))
		return m, nil
	case showMsg:
		if msg.tag != m.viewTag || msg.mode != m.viewMode {
			return m, nil
		}
		if msg.err != nil {
			m.viewer.SetContent(theme.Error.Render("Error: " + msg.err.Error()))
		} else if strings.TrimSpace(msg.text) == "" {
			m.viewer.SetContent(theme.Muted.Render("(empty)"))
		} else {
			m.viewer.SetContent(msg.text)
		}
		return m, nil
	case searchMsg:
		if msg.err != nil {
			m.results = m.results.SetError(msg.err)
		} else {
			m.matches = msg.matches
			m.results = m.results.SetRows(m.matchRows())
		}
		return m, nil
	case handover.DoneMsg:
		if _, ok := msg.Tag.(handoverTag); !ok {
			return m, nil
		}
		m, cmd := m.reload()
		if msg.Err != nil {
			return m, tea.Batch(cmd, nav.Notify(nav.LevelWarn, "Recording ended: "+msg.Err.Error()))
		}
		return m, tea.Batch(cmd, nav.Notify(nav.LevelSuccess, "Recording saved"))
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
	switch m.level {
	case levelViewer:
		return m.updateViewer(k)
	case levelSearch:
		return m.updateSearch(k)
	}
	return m.updateList(k)
}

func (m Model) selected() (wslog.Session, bool) {
	i := m.list.Cursor()
	if i < 0 || i >= len(m.sessions) {
		return wslog.Session{}, false
	}
	return m.sessions[i], true
}

func (m Model) active() (wslog.Session, bool) {
	for _, s := range m.sessions {
		if s.Active {
			return s, true
		}
	}
	return wslog.Session{}, false
}

func (m Model) openViewer(tag string) (Model, tea.Cmd) {
	m.level = levelViewer
	m.viewTag = tag
	m.viewer.SetContent(theme.Muted.Render("Loading…"))
	m.viewer.GotoTop()
	return m, show(m.env, tag, m.viewMode)
}

func (m Model) updateList(k tea.KeyPressMsg) (nav.Component, tea.Cmd) {
	switch {
	case key.Matches(k, m.keys.Back):
		return m, nav.Up
	case key.Matches(k, m.keys.Refresh):
		return m.reload()
	case key.Matches(k, m.keys.View):
		if s, ok := m.selected(); ok {
			return m.openViewer(s.Tag)
		}
	case key.Matches(k, m.keys.Start):
		if s, ok := m.active(); ok {
			return m, nav.Notify(nav.LevelWarn, "Already recording: "+s.Tag)
		}
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepStartTag, prompt.New("Start a recorded shell session",
			prompt.WithHint("Optional tag (blank for a timestamp). Type `exit` or Ctrl-D to end and return here."),
			prompt.AllowEmpty()))
		return m, cmd
	case key.Matches(k, m.keys.Stop):
		s, ok := m.active()
		if !ok {
			return m, nav.Notify(nav.LevelInfo, "No recording in progress")
		}
		return m.openStop(s), nil
	case key.Matches(k, m.keys.Search):
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepSearchQuery, prompt.New("Search recorded sessions", prompt.WithValue(m.query)))
		return m, cmd
	case key.Matches(k, m.keys.Delete):
		if s, ok := m.selected(); ok {
			if s.Active {
				return m, nav.Notify(nav.LevelWarn, "Stop the recording before deleting it")
			}
			return m.openDelete(s), nil
		}
	case key.Matches(k, m.keys.Prune):
		return m.openPrune(), nil
	default:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(k)
		return m, cmd
	}
	return m, nil
}

func (m Model) updateViewer(k tea.KeyPressMsg) (nav.Component, tea.Cmd) {
	switch {
	case key.Matches(k, m.viewerKeys.Detail.Back):
		m.level = levelList
		return m, nil
	case key.Matches(k, m.viewerKeys.ToggleMode):
		if m.viewMode == modeFull {
			m.viewMode = modeCommands
		} else {
			m.viewMode = modeFull
		}
		return m.openViewer(m.viewTag)
	}
	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Update(k)
	return m, cmd
}

func (m Model) updateSearch(k tea.KeyPressMsg) (nav.Component, tea.Cmd) {
	switch {
	case key.Matches(k, m.keys.Back):
		m.level = levelList
		return m, nil
	case key.Matches(k, m.keys.View):
		if i := m.results.Cursor(); i >= 0 && i < len(m.matches) {
			return m.openViewer(sessionOf(m.matches[i].Path))
		}
		return m, nil
	case key.Matches(k, m.keys.Search):
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepSearchQuery, prompt.New("Search recorded sessions", prompt.WithValue(m.query)))
		return m, cmd
	}
	var cmd tea.Cmd
	m.results, cmd = m.results.Update(k)
	return m, cmd
}

func (m Model) dialogResult(st step, msg tea.Msg) (nav.Component, tea.Cmd) {
	switch st {
	case stepStartTag:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		args := []string{"log", "start"}
		if r.Value != "" {
			args = append(args, "--tag", r.Value)
		}
		// The CLI's own start plan has one action; --quiet-start accepts it
		// (the user already confirmed here) without the banner.
		return m, handover.Run(handover.WS(m.env.Paths, append(args, "--quiet-start")...), handoverRecording)
	case stepSearchQuery:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		m.query = r.Value
		m.level = levelSearch
		m.results = m.results.SetLoading()
		return m, searchLogs(m.env, r.Value)
	}

	done := msg.(confirm.DoneMsg)
	if done.Result.Aborted {
		return m, nil
	}
	level := nav.LevelSuccess
	if done.Result.HasFailures() {
		level = nav.LevelWarn
	}
	m, load := m.reload()
	return m, tea.Batch(nav.Notify(level, confirm.Summary(done.Result)), load)
}

// openStop mirrors `ws log stop`: signal the recorder, wait up to 3s, then
// finalize the session metadata.
func (m Model) openStop(s wslog.Session) Model {
	logDir := m.env.LogDir
	p := plan.Plan{Command: "log.stop", Actions: []plan.Action{{
		ID:          "log-stop",
		Description: "Stop recording session " + s.Tag,
		Execute: func() error {
			if pid := wslog.GetActivePID(logDir); pid > 0 {
				if proc, err := os.FindProcess(pid); err == nil {
					_ = proc.Signal(syscall.SIGTERM)
					for range 30 {
						if err := proc.Signal(syscall.Signal(0)); err != nil {
							break
						}
						time.Sleep(100 * time.Millisecond)
					}
				}
			}
			_, err := wslog.Stop(wslog.StopOptions{LogDir: logDir})
			return err
		},
	}}}
	m.modal = m.modal.Confirm(stepStop, confirm.New("Stop recording", p))
	return m
}

func (m Model) openDelete(s wslog.Session) Model {
	logDir, tag := m.env.LogDir, s.Tag
	p := plan.Plan{Command: "log.rm", Actions: []plan.Action{{
		ID:          "log-rm-" + tag,
		Description: fmt.Sprintf("Remove log session %s (%s)", filepath.Join(logDir, tag), style.HumanBytes(s.SizeBytes)),
		Execute: func() error {
			_, err := wslog.Remove(wslog.RemoveOptions{LogDir: logDir, Tag: tag})
			return err
		},
	}}}
	m.modal = m.modal.Confirm(stepDelete, confirm.New("Delete session", p))
	return m
}

// openPrune lists every finished session, none pre-checked: the CLI needs an
// explicit --older-than or --all, so the user chooses here (a toggles all).
func (m Model) openPrune() Model {
	logDir := m.env.LogDir
	now := time.Now()
	p := plan.Plan{Command: "log.prune"}
	for _, s := range m.sessions {
		if s.Active {
			continue
		}
		tag := s.Tag
		p.Actions = append(p.Actions, plan.Action{
			ID:          "prune-" + tag,
			Description: fmt.Sprintf("Remove %s  %s, %s old", tag, style.HumanBytes(s.SizeBytes), format.Age(now.Sub(s.StartedAt))),
			Execute: func() error {
				_, err := wslog.Remove(wslog.RemoveOptions{LogDir: logDir, Tag: tag})
				return err
			},
		})
	}
	c := confirm.New("Prune recorded sessions", p).
		WithNote("Tick the sessions to remove (a selects all).").
		WithChecked(func(int, plan.Action) bool { return false })
	m.modal = m.modal.Confirm(stepPrune, c)
	return m
}

// sessionOf maps a search match path (relative to the log dir, e.g.
// "<tag>/stdout.log") to its session tag.
func sessionOf(rel string) string {
	rel = filepath.ToSlash(rel)
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return rel
}

func (m Model) matchRows() []table.Row {
	out := make([]table.Row, 0, len(m.matches))
	for _, mt := range m.matches {
		rel := mt.Path
		if r, err := filepath.Rel(m.env.LogDir, mt.Path); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
		line := ""
		if mt.Line > 0 {
			line = strconv.Itoa(mt.Line)
		}
		out = append(out, table.Row{sessionOf(rel), line, strings.Join(strings.Fields(ansi.Strip(mt.Snippet)), " ")})
	}
	return out
}

func rows(sessions []wslog.Session) []table.Row {
	out := make([]table.Row, 0, len(sessions))
	for _, s := range sessions {
		state := theme.Muted.Render("done")
		duration := format.Duration(s.DurationSec)
		if s.Active {
			state = theme.Error.Render("● recording")
			duration = "—"
		}
		out = append(out, table.Row{
			s.Tag,
			s.StartedAt.Local().Format("2006-01-02 15:04"),
			duration,
			strconv.Itoa(s.Commands),
			style.HumanBytes(s.SizeBytes),
			state,
		})
	}
	return out
}

// View implements nav.Component.
func (m Model) View() string {
	clip := lipgloss.NewStyle().MaxWidth(m.width)
	switch {
	case m.modal.Open():
		return m.modal.View()
	case m.level == levelViewer:
		mode := "full output"
		if m.viewMode == modeCommands {
			mode = "commands only"
		}
		head := clip.Render(theme.Bold.Render(m.viewTag) + theme.Muted.Render("  ("+mode+")"))
		return lipgloss.JoinVertical(lipgloss.Left, head, m.viewer.View())
	case m.level == levelSearch:
		head := clip.Render(theme.Muted.Render("Results for ") + theme.Bold.Render(m.query) + theme.Muted.Render("  (enter views · / new search · esc back)"))
		return lipgloss.JoinVertical(lipgloss.Left, head, m.results.View())
	}
	return m.list.View()
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	switch {
	case m.modal.Open():
		return m.modal.KeyMap()
	case m.level == levelViewer:
		return m.viewerKeys
	}
	return m.keys
}
