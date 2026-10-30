// Package sessions is the Sessions screen (spec §6.5), a client of agent-tmux,
// which owns terminal sessions and their recordings. The list is `agent-tmux
// ls --json`: live sessions and ended recordings together. Every key runs one
// agent-tmux verb: open and create attach, show pages a recording, both with
// the TUI suspended; grep lists its matches here; kill and rm go through a
// checklist.
//
// Without agent-tmux on PATH the screen is greyed out and shows how to
// install it. When a newer agent-tmux release exists, the screen says so above
// the list (the version nudge, agenttmux.CheckNudge).
//
// Navigation levels: list → grep matches. esc in the matches returns to the
// list; esc in the list asks the app to go up.
package sessions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/agenttmux"
	"github.com/mugenkunou/ws-tool/internal/plan"
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
)

// LoadedMsg carries agent-tmux's sessions. Missing means agent-tmux is not
// installed. Other components (the dashboard) may observe it.
type LoadedMsg struct {
	Sessions []agenttmux.Session
	Missing  bool
	Err      error
}

// Live counts the live sessions.
func (m LoadedMsg) Live() int {
	n := 0
	for _, s := range m.Sessions {
		if s.Live() {
			n++
		}
	}
	return n
}

// NudgeMsg says a newer agent-tmux release is out. The dashboard observes it
// too.
type NudgeMsg struct{ Nudge agenttmux.Nudge }

// grepMsg carries what `agent-tmux grep --json` found.
type grepMsg struct {
	query string
	res   agenttmux.GrepResult
	err   error
}

type level int

const (
	levelList level = iota
	levelMatches
)

type step int

const (
	stepNew step = iota
	stepGrep
	stepKill
	stepDelete
)

// handoverTag marks the agent-tmux runs that had the terminal.
type handoverTag int

const (
	handoverOpen handoverTag = iota
	handoverShow
	handoverNew
)

// Model is the sessions screen state.
type Model struct {
	env       env.Env
	keys      keys.SessionsMap
	matchKeys keys.SessionMatchesMap
	list      reslist.Model[agenttmux.Session]
	matches   reslist.Model[agenttmux.Match]
	level     level
	modal     modal.Model

	missing   bool     // agent-tmux is not installed
	nudge     []string // the version nudge's lines, when there is a newer release
	query     string   // the grep the matches are for
	pending   string   // the grep being run
	truncated bool
	width     int
	height    int
}

// New creates the screen.
func New(e env.Env) Model {
	k := keys.Sessions()
	return Model{
		env:       e,
		keys:      k,
		matchKeys: keys.SessionMatches(),
		list: reslist.New(agenttmux.Schema(e.View()), []reslist.Col[agenttmux.Session]{
			{Key: "name", Min: 14, Weight: 3},
			{Key: "state", Min: 6, Style: func(s agenttmux.Session, t string) string {
				if s.Live() {
					return theme.OK.Render(t)
				}
				return theme.Muted.Render(t)
			}},
			{Key: "recorder", Min: 7, Style: func(s agenttmux.Session, t string) string {
				switch s.Recorder {
				case "running":
					return theme.Error.Render(t)
				case "stopped":
					return theme.Warn.Render(t)
				}
				return theme.Muted.Render(t)
			}},
			{Key: "owner", Min: 8},
			{Key: "clients", Min: 7},
			{Key: "started_at", Min: 16},
			{Key: "bytes", Min: 9},
		}, theme.Icon(theme.IconTape)+"No sessions yet. Press "+k.New.Help().Key+" to start one.").Declare(reslist.Decls(actions)),
		matches: reslist.New(agenttmux.MatchSchema(), []reslist.Col[agenttmux.Match]{
			{Key: "id", Min: 20, Weight: 1},
			{Key: "line", Min: 6},
			{Key: "text", Min: 20, Weight: 4},
		}, "No matches."),
	}
}

// Load lists the sessions with `agent-tmux ls --json`.
func Load() tea.Cmd {
	return func() tea.Msg {
		sessions, err := agenttmux.Ls(context.Background())
		if errors.Is(err, agenttmux.ErrMissing) {
			return LoadedMsg{Missing: true}
		}
		return LoadedMsg{Sessions: sessions, Err: err}
	}
}

// checkNudge asks for a newer agent-tmux release, in the background. It sends
// nothing when there is none or the check fails.
func checkNudge() tea.Cmd {
	return func() tea.Msg {
		if n, ok := agenttmux.CheckNudge(context.Background()); ok {
			return NudgeMsg{Nudge: n}
		}
		return nil
	}
}

func grep(query string) tea.Cmd {
	return func() tea.Msg {
		res, err := agenttmux.Grep(context.Background(), query, 0)
		return grepMsg{query: query, res: res, err: err}
	}
}

// Init starts the first load and the release check.
func (m Model) Init() tea.Cmd { return tea.Batch(Load(), checkNudge()) }

// Declared lists the screen's actions and their scopes.
func (m Model) Declared() []reslist.Decl { return m.list.Declared() }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool {
	return m.modal.Open() || m.list.Editing() || (m.level == levelMatches && m.matches.Editing())
}

func (m Model) reload() (Model, tea.Cmd) {
	m.list = m.list.SetLoading()
	return m, Load()
}

// headerLines is what stands above the list: the nudge, the grep's title.
func (m Model) headerLines() []string {
	var out []string
	for i, l := range m.nudge {
		if i == 0 {
			out = append(out, theme.Warn.Render(theme.Icon(theme.IconBulb)+l))
		} else {
			out = append(out, theme.Muted.Render("  "+l))
		}
	}
	if m.level == levelMatches {
		out = append(out, m.grepHeader())
	}
	return out
}

// layout sizes the lists under the header.
func (m Model) layout() Model {
	h := max(m.height-len(m.headerLines()), 0)
	m.list = m.list.SetSize(m.width, h)
	m.matches = m.matches.SetSize(m.width, h)
	return m
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
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m.layout(), nil
	case nav.FilterMsg:
		m.list = m.list.SetFilter(msg.Filter)
		return m, nil
	case nav.RefreshMsg:
		return m.reload()
	case LoadedMsg:
		m.missing = msg.Missing
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
			return m, nil
		}
		m.list = m.list.SetItems(msg.Sessions)
		return m, nil
	case NudgeMsg:
		m.nudge = msg.Nudge.Lines()
		return m.layout(), nil
	case grepMsg:
		if msg.query != m.pending {
			return m, nil // superseded by a newer grep
		}
		if msg.err != nil {
			return m, nav.Notify(nav.LevelWarn, msg.err.Error())
		}
		if len(msg.res.Matches) == 0 {
			return m, nav.Notify(nav.LevelInfo, "No matches for "+msg.query)
		}
		m.level, m.query, m.truncated = levelMatches, msg.query, msg.res.Truncated
		m.matches = m.matches.SetItems(msg.res.Matches)
		return m.layout(), nil
	case handover.DoneMsg:
		tag, ok := msg.Tag.(handoverTag)
		if !ok {
			return m, nil
		}
		m, cmd := m.reload()
		if msg.Err != nil {
			verb := map[handoverTag]string{handoverOpen: "open", handoverShow: "show", handoverNew: "create"}[tag]
			return m, tea.Batch(cmd, nav.Notify(nav.LevelWarn, "agent-tmux "+verb+": "+msg.Err.Error()))
		}
		return m, cmd
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
	if m.level == levelMatches {
		return m.updateMatches(k)
	}
	return m.updateList(k)
}

// actions is every sessions key and the rows it acts on (spec §6.5).
var actions = func() []reslist.Action[Model, agenttmux.Session] {
	k := keys.Sessions()
	return []reslist.Action[Model, agenttmux.Session]{
		{Key: k.Open, Scope: reslist.Cursor, Do: Model.open},
		{Key: k.Show, Scope: reslist.Cursor, Do: Model.show},
		{Key: k.New, Scope: reslist.Global, Do: Model.promptNew},
		{Key: k.Grep, Scope: reslist.Global, Do: Model.promptGrep},
		{Key: k.Kill, Scope: reslist.Selected, Do: Model.kill},
		{Key: k.Delete, Scope: reslist.Selected, Do: Model.delete},
	}
}()

func (m Model) updateList(k tea.KeyPressMsg) (nav.Component, tea.Cmd) {
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

func (m Model) updateMatches(k tea.KeyPressMsg) (nav.Component, tea.Cmd) {
	if l, cmd, ok := m.matches.HandleKey(k); ok {
		m.matches = l
		return m, cmd
	}
	switch {
	case key.Matches(k, m.matchKeys.Show):
		if mt, ok := m.matches.Current(); ok {
			return m, m.run(handoverShow, "show", mt.ID)
		}
		return m, nil
	case key.Matches(k, m.matchKeys.Back):
		m.level = levelList
		return m.layout(), nil
	}
	var cmd tea.Cmd
	m.matches, cmd = m.matches.Update(k)
	return m, cmd
}

// handOver gives the terminal to c. Tests swap it to see what would run.
var handOver = handover.Run

// run suspends the TUI and gives the terminal to `agent-tmux <args…>`.
func (m Model) run(tag handoverTag, args ...string) tea.Cmd {
	c, err := agenttmux.Command(args...)
	if err != nil {
		return nav.Notify(nav.LevelWarn, err.Error())
	}
	return handOver(c, tag)
}

// notInstalled is the notice of every key while agent-tmux is missing.
func notInstalled() tea.Cmd {
	return nav.Notify(nav.LevelWarn, agenttmux.ErrMissing.Error()+"; this screen shows how to install it")
}

// ── open, show, new ────────────────────────────────────────────────

// open is enter: attach to a live session; an ended one has only its
// recording, so it is shown.
func (m Model) open(rows []agenttmux.Session) (Model, tea.Cmd) {
	if m.missing {
		return m, notInstalled()
	}
	s := rows[0]
	if !s.Live() {
		return m.show(rows)
	}
	return m, m.run(handoverOpen, "open", s.Name)
}

// show is v: page the cursor row's recording (agent-tmux pages it itself).
func (m Model) show(rows []agenttmux.Session) (Model, tea.Cmd) {
	if m.missing {
		return m, notInstalled()
	}
	s := rows[0]
	if s.ID == "" {
		return m, nav.Notify(nav.LevelInfo, s.Name+" has no recording: an older agent-tmux started it")
	}
	return m, m.run(handoverShow, "show", s.Ref())
}

// promptNew is a: ask for a name, then `agent-tmux create <name>` attaches.
func (m Model) promptNew(_ []agenttmux.Session) (Model, tea.Cmd) {
	if m.missing {
		return m, notInstalled()
	}
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepNew, prompt.New("New session",
		prompt.WithHint("A name for it. agent-tmux creates it and attaches; detach to return here."),
		prompt.WithValidate(func(v string) error {
			if strings.TrimSpace(v) == "" || strings.ContainsAny(v, " \t") {
				return errors.New("a name, without spaces")
			}
			return nil
		})))
	return m, cmd
}

// ── grep ───────────────────────────────────────────────────────────

// promptGrep is s: ask for the text, then list the matching lines.
func (m Model) promptGrep(_ []agenttmux.Session) (Model, tea.Cmd) {
	if m.missing {
		return m, notInstalled()
	}
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepGrep, prompt.New("Grep every recording",
		prompt.WithHint("Text to find, in any case. The matching lines are listed; enter shows one.")))
	return m, cmd
}

func (m Model) startGrep(text string) (Model, tea.Cmd) {
	if m.missing {
		return m, notInstalled()
	}
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return m, nav.Notify(nav.LevelWarn, "usage: grep <text>")
	}
	m.pending = text // grepMsg checks it, so an older answer cannot replace this one
	return m, grep(text)
}

// ── kill, delete ───────────────────────────────────────────────────

// kill is x: end the live sessions among rows (`agent-tmux kill`). Their
// recordings stay.
func (m Model) kill(rows []agenttmux.Session) (Model, tea.Cmd) {
	if m.missing {
		return m, notInstalled()
	}
	p := plan.Plan{Command: "session.kill"}
	for _, s := range rows {
		if !s.Live() {
			continue
		}
		name := s.Name
		desc := "Kill session " + name
		if s.Clients > 0 {
			desc += " (" + format.Plural(s.Clients, "client") + " attached)"
		}
		p.Actions = append(p.Actions, plan.Action{
			ID: "session-kill-" + name, Description: desc,
			Execute: func() error { _, err := agenttmux.Output(context.Background(), "kill", name); return err },
		})
	}
	if len(p.Actions) == 0 {
		return m, nav.Notify(nav.LevelInfo, "Nothing to kill: the session has ended")
	}
	m.modal = m.modal.Confirm(stepKill, confirm.New("Kill "+format.Plural(len(p.Actions), "session"), p).
		WithNote("Their recordings stay."))
	return m, nil
}

// delete is ctrl+d: move the ended recordings among rows to the trash
// (`agent-tmux rm`). A live session is never offered: kill it first.
func (m Model) delete(rows []agenttmux.Session) (Model, tea.Cmd) {
	if m.missing {
		return m, notInstalled()
	}
	p := plan.Plan{Command: "session.rm"}
	for _, s := range rows {
		if s.Live() || s.ID == "" {
			continue
		}
		id := s.ID
		p.Actions = append(p.Actions, plan.Action{
			ID:          "session-rm-" + id,
			Description: fmt.Sprintf("Move recording %s (%s) to the trash", id, style.HumanBytes(s.Bytes)),
			Execute:     func() error { _, err := agenttmux.Output(context.Background(), "rm", id); return err },
		})
	}
	if len(p.Actions) == 0 {
		return m, nav.Notify(nav.LevelWarn, "Kill the session before deleting its recording")
	}
	m.modal = m.modal.Confirm(stepDelete, confirm.New("Delete "+format.Plural(len(p.Actions), "recording"), p))
	return m, nil
}

func (m Model) dialogResult(st step, msg tea.Msg) (nav.Component, tea.Cmd) {
	switch st {
	case stepNew:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		return m, m.run(handoverNew, "create", strings.TrimSpace(r.Value))
	case stepGrep:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		return m.startGrep(r.Value)
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

// grepHeader names the grep above its matches.
func (m Model) grepHeader() string {
	n := format.Plural(len(m.matches.Items()), "match")
	if m.truncated {
		n = "the first " + n
	}
	return theme.Muted.Render("grep ") + theme.Bold.Render(m.query) +
		theme.Muted.Render(fmt.Sprintf(" — %s · %s shows one · %s returns",
			n, m.matchKeys.Show.Help().Key, m.matchKeys.Back.Help().Key))
}

// missingView is the greyed-out screen without agent-tmux.
func (m Model) missingView() string {
	lines := []string{
		theme.Muted.Render(theme.Icon(theme.IconTape) + "Sessions are run by agent-tmux, which is not installed."),
		"",
	}
	for _, l := range agenttmux.InstallLines() {
		lines = append(lines, theme.Muted.Render("  "+l))
	}
	lines = append(lines, "", theme.Muted.Render("Then refresh this screen."))
	return lipgloss.NewStyle().MaxWidth(m.width).Render(strings.Join(lines, "\n"))
}

// View implements nav.Component.
func (m Model) View() string {
	switch {
	case m.modal.Open():
		return m.modal.View()
	case m.missing:
		return m.missingView()
	}
	body := m.list.View()
	if m.level == levelMatches {
		body = m.matches.View()
	}
	head := m.headerLines()
	if len(head) == 0 {
		return body
	}
	clip := lipgloss.NewStyle().MaxWidth(m.width)
	for i := range head {
		head[i] = clip.Render(head[i])
	}
	return lipgloss.JoinVertical(lipgloss.Left, append(head, body)...)
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	switch {
	case m.modal.Open():
		return m.modal.KeyMap()
	case m.level == levelMatches:
		return m.matches.Help(m.matchKeys)
	}
	return m.list.Help(m.keys)
}
