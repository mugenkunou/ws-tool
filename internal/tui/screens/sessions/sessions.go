// Package sessions is the recorded-terminal-sessions screen (spec §6.5): list,
// the transcript viewer, record, prune and delete, plus the `:stop` and
// `:grep` commands.
//
// Recording hands the terminal to `ws session record` (a script(1) PTY shell);
// the TUI resumes when that shell exits.
//
// Navigation levels: list → transcript. esc in the transcript returns to the
// list; esc in the list first clears a `:grep`, then asks the app to go up.
package sessions

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/age"
	"github.com/mugenkunou/ws-tool/internal/plan"
	wssession "github.com/mugenkunou/ws-tool/internal/session"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/tui/confirm"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/handover"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/modal"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/prompt"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// LoadedMsg carries the session list, newest first. Other components (the
// dashboard) may observe it.
type LoadedMsg struct {
	Sessions []wssession.Session
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

// grepMsg carries what `session grep` found.
type grepMsg struct {
	query   string
	matches []wssession.Match
	err     error
}

type level int

const (
	levelList level = iota
	levelViewer
)

const (
	modeFull     = "full"
	modeCommands = "commands-only"
)

type step int

const (
	stepRecordTag step = iota
	stepStop
	stepDelete
	stepPruneAge
	stepPrune
)

type handoverTag int

const handoverRecording handoverTag = iota

// grepHeaderLines is the line above the list while a grep narrows it.
const grepHeaderLines = 1

// Model is the sessions screen state.
type Model struct {
	env        env.Env
	keys       keys.SessionsMap
	viewerKeys keys.ViewerMap
	list       reslist.Model[wssession.Session]
	viewer     viewport.Model
	level      level
	modal      modal.Model

	sessions []wssession.Session // every session; the list shows those the grep leaves
	query    string              // the active `:grep` text; "" when none
	pending  string              // the `:grep` text being searched for
	hits     map[string]bool     // the tags with a match
	viewTag  string
	viewMode string
	width    int
	height   int
}

// New creates the screen.
func New(e env.Env) Model {
	k := keys.Sessions()
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
		list: reslist.New(wssession.Schema(e.View()), []reslist.Col[wssession.Session]{
			{Key: "tag", Min: 14, Weight: 3},
			{Key: "started_at", Min: 16},
			{Key: "duration_sec", Min: 8},
			{Key: "commands", Min: 4},
			{Key: "size_bytes", Min: 9},
			{Key: "active", Min: 11, Style: func(s wssession.Session, t string) string {
				if s.Active {
					return theme.Error.Render("● " + t)
				}
				return theme.Muted.Render(t)
			}},
		}, theme.Icon(theme.IconTape)+"No recorded sessions yet. Press "+k.Record.Help().Key+" to record one.").Declare(reslist.Decls(actions)),
	}
}

// Load lists recorded sessions.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		sessions, err := wssession.List(e.LogDir, stateDir())
		return LoadedMsg{Sessions: sessions, Err: err}
	}
}

// stateDir is the machine-local state directory, where the active recording's
// marker lives.
func stateDir() string {
	dir, _ := wsconfig.StateDir()
	return dir
}

func show(e env.Env, tag, mode string) tea.Cmd {
	return func() tea.Msg {
		// Recordings are raw PTY streams: Show strips escapes and carriage returns.
		res, err := wssession.Show(e.LogDir, tag, mode == modeCommands)
		return showMsg{tag: tag, mode: mode, text: res.Text, err: err}
	}
}

func grep(e env.Env, query string) tea.Cmd {
	return func() tea.Msg {
		found, err := wssession.Grep(e.LogDir, stateDir(), query, 0)
		return grepMsg{query: query, matches: found, err: err}
	}
}

// Init starts the first load.
func (m Model) Init() tea.Cmd { return Load(m.env) }

// Declared lists the screen's actions and their scopes.
func (m Model) Declared() []reslist.Decl { return m.list.Declared() }

// Commands implements nav.Commander: the `:` commands (spec §6.5).
func (m Model) Commands() []nav.CommandInfo { return reslist.CommandInfos(commands) }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() || m.list.Editing() }

func (m Model) reload() (Model, tea.Cmd) {
	m.list = m.list.SetLoading()
	return m, Load(m.env)
}

// layout sizes the list under the grep header, when there is one.
func (m Model) layout() Model {
	head := 0
	if m.query != "" {
		head = grepHeaderLines
	}
	m.list = m.list.SetSize(m.width, max(m.height-head, 0))
	return m
}

// showRows puts the sessions the grep leaves in the list.
func (m Model) showRows() Model {
	rows := m.sessions
	if m.query != "" {
		rows = nil
		for _, s := range m.sessions {
			if m.hits[s.Tag] {
				rows = append(rows, s)
			}
		}
	}
	m.list = m.list.SetItems(rows)
	return m.layout()
}

// clearGrep restores the full list.
func (m Model) clearGrep() Model {
	m.query, m.hits = "", nil
	return m.showRows()
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
		m = m.layout()
		m.viewer.SetWidth(msg.Width)
		m.viewer.SetHeight(max(msg.Height-1, 0))
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.FilterMsg:
		m.list = m.list.SetFilter(msg.Filter)
		return m, nil
	case nav.RefreshMsg:
		return m.reload()
	case nav.CommandMsg:
		if m.modal.Open() {
			return m, nil
		}
		m.level = levelList
		next, cmd, _ := reslist.RunCommand(m, m.list, commands, msg)
		return next, cmd
	case LoadedMsg:
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m.sessions = msg.Sessions
		return m.showRows(), nil
	case showMsg:
		if msg.tag != m.viewTag || msg.mode != m.viewMode {
			return m, nil
		}
		switch {
		case msg.err != nil:
			m.viewer.SetContent(theme.Error.Render("Error: " + msg.err.Error()))
		case strings.TrimSpace(msg.text) == "":
			m.viewer.SetContent(theme.Muted.Render("(empty)"))
		default:
			m.viewer.SetContent(msg.text)
			m.viewer.SetYOffset(firstMatch(msg.text, m.query))
		}
		return m, nil
	case grepMsg:
		if msg.query != m.pending {
			return m, nil // superseded by a newer grep
		}
		if msg.err != nil {
			return m.clearGrep(), nav.Notify(nav.LevelWarn, msg.err.Error())
		}
		m.hits = map[string]bool{}
		for _, mt := range msg.matches {
			m.hits[mt.Tag] = true
		}
		if len(m.hits) == 0 {
			return m.clearGrep(), nav.Notify(nav.LevelInfo, "No matches for "+msg.query)
		}
		m.query = msg.query
		return m.showRows(), nil
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
	if m.level == levelViewer {
		return m.updateViewer(k)
	}
	return m.updateList(k)
}

// firstMatch is the line of text that has query in it (case-insensitive), or 0.
func firstMatch(text, query string) int {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return 0
	}
	for i, line := range strings.Split(text, "\n") {
		if strings.Contains(strings.ToLower(line), needle) {
			return i
		}
	}
	return 0
}

func (m Model) active() (wssession.Session, bool) {
	for _, s := range m.sessions {
		if s.Active {
			return s, true
		}
	}
	return wssession.Session{}, false
}

func (m Model) openViewer(tag string) (Model, tea.Cmd) {
	m.level = levelViewer
	m.viewTag = tag
	m.viewer.SetContent(theme.Muted.Render("Loading…"))
	m.viewer.GotoTop()
	return m, show(m.env, tag, m.viewMode)
}

// actions is every sessions key and the rows it acts on (spec §6.5).
var actions = func() []reslist.Action[Model, wssession.Session] {
	k := keys.Sessions()
	return []reslist.Action[Model, wssession.Session]{
		{Key: k.View, Scope: reslist.Cursor, Do: Model.show},
		{Key: k.Record, Scope: reslist.Global, Do: Model.promptRecord},
		// Prune takes all the sessions, like `ws session prune`: not the marks,
		// not the filter.
		{Key: k.Prune, Scope: reslist.Global, Do: Model.promptPrune},
		{Key: k.Delete, Scope: reslist.Selected, Do: Model.delete},
	}
}()

// commands is every `:` command: the CLI's words after `session`.
var commands = []reslist.Command[Model, wssession.Session]{
	{Name: "stop", Scope: reslist.Global,
		Help: "stop the recording in progress", Do: Model.stop},
	{Name: "grep", Args: "<text>", Scope: reslist.Global,
		Help: "narrow the list to sessions with a match; enter opens the first one, esc clears", Do: Model.grepCommand},
}

func (m Model) updateList(k tea.KeyPressMsg) (nav.Component, tea.Cmd) {
	if l, cmd, ok := m.list.HandleKey(k); ok {
		m.list = l
		return m, cmd
	}
	if next, cmd, ok := reslist.Run(m, m.list, actions, k); ok {
		return next, cmd
	}
	switch {
	case key.Matches(k, m.keys.Back):
		if m.query != "" {
			return m.clearGrep(), nil
		}
		return m, nav.Up
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
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

// ── show, record ───────────────────────────────────────────────────

// show is enter: the transcript of the cursor row. After a `:grep` it opens at
// the first match.
func (m Model) show(rows []wssession.Session) (Model, tea.Cmd) { return m.openViewer(rows[0].Tag) }

// promptRecord is r: ask for a tag, then hand the terminal to the recorded
// shell.
func (m Model) promptRecord(_ []wssession.Session) (Model, tea.Cmd) {
	if s, ok := m.active(); ok {
		return m, nav.Notify(nav.LevelWarn, "Already recording: "+s.Tag)
	}
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepRecordTag, prompt.New("Record a shell session",
		prompt.WithHint("Optional tag (blank for a timestamp). Type `exit` or Ctrl-D to end and return here."),
		prompt.AllowEmpty()))
	return m, cmd
}

// ── stop ───────────────────────────────────────────────────────────

// stop is `:stop`, mirroring `ws session stop`: signal the recorder, wait up to
// 3s, then finalize the session metadata.
func (m Model) stop(_ []wssession.Session, _ []string) (Model, tea.Cmd) {
	s, ok := m.active()
	if !ok {
		return m, nav.Notify(nav.LevelInfo, "No recording in progress")
	}
	logDir := m.env.LogDir
	p := plan.Plan{Command: "session.stop", Actions: []plan.Action{{
		ID:          "session-stop",
		Description: "Stop recording session " + s.Tag,
		Execute: func() error {
			_, err := wssession.StopActive(wssession.StopOptions{LogDir: logDir, StateDir: stateDir()})
			return err
		},
	}}}
	m.modal = m.modal.Confirm(stepStop, confirm.New("Stop recording", p))
	return m, nil
}

// ── grep ───────────────────────────────────────────────────────────

// grepCommand is `:grep <text>`: search every session's commands and transcript
// and narrow the list to the sessions with a match.
func (m Model) grepCommand(_ []wssession.Session, args []string) (Model, tea.Cmd) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		return m, nav.Notify(nav.LevelWarn, "usage: grep <text>")
	}
	m.pending = text // grepMsg checks it, so an older answer cannot replace this one
	return m, grep(m.env, text)
}

// ── delete, prune ──────────────────────────────────────────────────

// delete is ctrl+d: remove finished sessions (the recording one is never
// offered), as `ws session rm`.
func (m Model) delete(rows []wssession.Session) (Model, tea.Cmd) {
	done := finished(rows)
	if len(done) == 0 {
		return m, nav.Notify(nav.LevelWarn, "Stop the recording before deleting it")
	}
	logDir := m.env.LogDir
	p := plan.Plan{Command: "session.rm"}
	for _, s := range done {
		tag := s.Tag
		p.Actions = append(p.Actions, plan.Action{
			ID:          "session-rm-" + tag,
			Description: fmt.Sprintf("Delete %s (%s)", filepath.Join(logDir, tag), style.HumanBytes(s.SizeBytes)),
			Execute: func() error {
				return wssession.Remove(wssession.RemoveOptions{LogDir: logDir, StateDir: stateDir(), Tag: tag})
			},
		})
	}
	m.modal = m.modal.Confirm(stepDelete, confirm.New("Delete "+format.Plural(len(done), "session"), p))
	return m, nil
}

// finished drops the session being recorded.
func finished(rows []wssession.Session) []wssession.Session {
	var out []wssession.Session
	for _, s := range rows {
		if !s.Active {
			out = append(out, s)
		}
	}
	return out
}

// promptPrune is p: an age, or blank to enforce the size cap, then a checklist
// of what goes (`ws session prune`). It covers all the sessions.
func (m Model) promptPrune(_ []wssession.Session) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepPruneAge, prompt.New("Prune sessions older than (age)",
		prompt.WithHint("An age such as 30d, 12h or 2w (a bare number is days). Blank enforces the size cap (session.cap_mb)."),
		prompt.WithValidate(func(v string) error {
			if strings.TrimSpace(v) == "" {
				return nil
			}
			_, err := age.Parse(v)
			return err
		}),
		prompt.AllowEmpty()))
	return m, cmd
}

// openPrune turns the typed age into the checklist: one action per session the
// CLI would remove, all pre-checked.
func (m Model) openPrune(raw string) (Model, tea.Cmd) {
	raw = strings.TrimSpace(raw)
	now := m.env.Now()
	sel := wssession.PruneSelection{CapMB: m.env.Config.Session.CapMB, Now: now}
	if raw != "" {
		d, err := age.Parse(raw)
		if err != nil {
			return m, nav.Notify(nav.LevelWarn, err.Error())
		}
		sel.OlderThan = d
	}
	chosen := wssession.SelectPrune(m.sessions, sel)
	if len(chosen) == 0 {
		switch {
		case raw != "":
			return m, nav.Notify(nav.LevelInfo, "Nothing to prune: no session older than "+raw+".")
		case sel.CapMB <= 0:
			return m, nav.Notify(nav.LevelInfo, "Nothing to prune: no size cap is set (session.cap_mb).")
		}
		return m, nav.Notify(nav.LevelInfo, fmt.Sprintf("Nothing to prune: %s of the %d MB cap.", style.HumanBytes(wssession.TotalBytes(m.sessions)), sel.CapMB))
	}
	logDir := m.env.LogDir
	p := plan.Plan{Command: "session.prune"}
	for _, s := range chosen {
		tag := s.Tag
		days := int(now.Sub(s.StartedAt).Hours() / 24)
		p.Actions = append(p.Actions, plan.Action{
			ID:          "session-rm-" + tag,
			Description: fmt.Sprintf("Delete %s (%s, %dd old)", filepath.Join(logDir, tag), style.HumanBytes(s.SizeBytes), days),
			Execute: func() error {
				return wssession.Remove(wssession.RemoveOptions{LogDir: logDir, StateDir: stateDir(), Tag: tag})
			},
		})
	}
	title := "Prune sessions older than " + raw
	if raw == "" {
		title = "Prune sessions over the size cap"
	}
	m.modal = m.modal.Confirm(stepPrune, confirm.New(title, p))
	return m, nil
}

func (m Model) dialogResult(st step, msg tea.Msg) (nav.Component, tea.Cmd) {
	switch st {
	case stepRecordTag:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		args := []string{"session", "record"}
		if r.Value != "" {
			args = append(args, r.Value)
		}
		// --quiet: no banner, the user just asked for the recording here.
		return m, handover.Run(handover.WS(m.env.Paths, append(args, "--quiet")...), handoverRecording)
	case stepPruneAge:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		return m.openPrune(r.Value)
	}

	done := msg.(confirm.DoneMsg)
	if done.Result.Aborted {
		return m, nil
	}
	m.list = m.list.ClearMarks() // the marks were for this action
	level := nav.LevelSuccess
	if done.Result.HasFailures() {
		level = nav.LevelWarn
	}
	m, load := m.reload()
	return m, tea.Batch(nav.Notify(level, confirm.Summary(done.Result)), load)
}

// grepHeader names the active grep above the list.
func (m Model) grepHeader() string {
	n := len(m.list.Items())
	return lipgloss.NewStyle().MaxWidth(m.width).Render(
		theme.Muted.Render("grep ") + theme.Bold.Render(m.query) +
			theme.Muted.Render(fmt.Sprintf(" — %s with a match · %s clears · %s opens at the first match",
				format.Plural(n, "session"), m.keys.Back.Help().Key, m.keys.View.Help().Key)))
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
	case m.query != "":
		return lipgloss.JoinVertical(lipgloss.Left, m.grepHeader(), m.list.View())
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
	return m.list.Help(m.keys)
}

var _ nav.Commander = Model{}
