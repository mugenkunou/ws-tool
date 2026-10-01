// Package app is the root of the ws TUI. It owns the screen components,
// routes messages to them, handles global keys, and lays out the frame:
//
//	header  (title + tabs)           1 line
//	rule                             1 line
//	body    (active component)       remaining height
//	notice  (last NoticeMsg)         0 or 1 line
//	footer  (help)                   1 line, or more with full help
//
// Routing: key presses go to the global key map first, then to the active
// component only. While the active component has a modal open
// (CapturesKeys), every key except ctrl+c goes straight to it. Every other
// message is broadcast to all components, so components can observe each
// other's outputs (the dashboard summarizes the other screens' LoadedMsg)
// without reaching into one another.
//
// When the workspace is not initialized the app shows only the setup
// screen; once setup reports success it reloads the environment and builds
// the real screens.
package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/clip"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/capture"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/cron"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/dashboard"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/dotfiles"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/ignore"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/logs"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/repos"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/scratch"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/secrets"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/setup"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/trash"
	"github.com/mugenkunou/ws-tool/internal/tui/selection"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
	"github.com/mugenkunou/ws-tool/internal/workspace"
)

const (
	headerHeight = 2 // tabs + rule

	// Below this size the frame can't be laid out; a notice is shown instead.
	minWidth  = 20
	minHeight = 6
)

// Model is the root TUI state.
type Model struct {
	overrides workspace.PathOverrides
	env       env.Env
	loadErr   error // set when the environment could not be loaded

	keys     keys.GlobalMap
	help     help.Model
	screens  []nav.Component // indexed by nav.Screen; nil until the workspace is usable
	setup    nav.Component   // shown instead of screens when not initialized
	active   nav.Screen
	fullHelp bool
	notice   *nav.NoticeMsg
	frame    int

	// Mouse selection: drag to select, release to copy (see selection).
	mouse     bool // capture the mouse (off with WS_NO_MOUSE=1)
	selecting bool
	sel       selection.Range
	toast     *toast // short-lived message drawn over the last footer line
	toastSeq  int

	width  int
	height int
}

// toastTTL is how long a toast stays on screen.
const toastTTL = 2500 * time.Millisecond

// toast is a short-lived footer message (e.g. "Copied").
type toast struct {
	id    int
	text  string
	level nav.Level
}

// toastExpireMsg removes toast id when it is still the one showing.
type toastExpireMsg struct{ id int }

// envLoadedMsg carries a (re)loaded environment after setup.
type envLoadedMsg struct {
	env env.Env
	err error
}

// New builds the app. e and loadErr come from env.Load(o); o is kept so the
// environment can be reloaded after `init` runs from the setup screen.
func New(o workspace.PathOverrides, e env.Env, loadErr error) Model {
	m := Model{
		overrides: o,
		keys:      keys.Global(len(nav.Screens())),
		help:      help.New(),
		mouse:     os.Getenv("WS_NO_MOUSE") == "",
	}
	return m.withEnv(e, loadErr)
}

func (m Model) withEnv(e env.Env, loadErr error) Model {
	m.env, m.loadErr = e, loadErr
	m.active = nav.ScreenDashboard
	m.screens, m.setup = nil, nil
	switch {
	case loadErr == env.ErrNotInitialized:
		m.setup = setup.New(e)
	case loadErr != nil:
	default:
		m.screens = make([]nav.Component, len(nav.Screens()))
		m.screens[nav.ScreenDashboard] = dashboard.New(e)
		m.screens[nav.ScreenRepos] = repos.New(e)
		m.screens[nav.ScreenDotfiles] = dotfiles.New(e)
		m.screens[nav.ScreenScratch] = scratch.New(e)
		m.screens[nav.ScreenLogs] = logs.New(e)
		m.screens[nav.ScreenCapture] = capture.New(e)
		m.screens[nav.ScreenIgnore] = ignore.New(e)
		m.screens[nav.ScreenSecrets] = secrets.New(e)
		m.screens[nav.ScreenCron] = cron.New(e)
		m.screens[nav.ScreenTrash] = trash.New(e)
	}
	return m
}

// components returns the live components: the setup screen, or all screens.
func (m Model) components() []nav.Component {
	if m.setup != nil {
		return []nav.Component{m.setup}
	}
	return m.screens
}

// current is the component receiving keys, or nil.
func (m Model) current() nav.Component {
	if m.setup != nil {
		return m.setup
	}
	if int(m.active) < len(m.screens) {
		return m.screens[m.active]
	}
	return nil
}

// frameInterval paces animations (spinners).
const frameInterval = 150 * time.Millisecond

// frameTickMsg drives the animation timer.
type frameTickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(frameInterval, func(time.Time) tea.Msg { return frameTickMsg{} })
}

// Init starts every screen's first load concurrently, and the animation timer.
func (m Model) Init() tea.Cmd { return tea.Batch(m.loadAll(), tick()) }

// loadAll starts every component's first load.
func (m Model) loadAll() tea.Cmd {
	var cmds []tea.Cmd
	for _, c := range m.components() {
		cmds = append(cmds, c.Init())
	}
	return tea.Batch(cmds...)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width)
		return m.resize()

	case tea.KeyPressMsg:
		m.selecting, m.sel = false, selection.Range{}
		return m.updateKey(msg)

	case tea.MouseClickMsg:
		if m.mouse && msg.Button == tea.MouseLeft {
			p := selection.Point{X: msg.X, Y: msg.Y}
			m.selecting, m.sel = true, selection.Range{Anchor: p, Head: p}
		}
		return m, nil

	case tea.MouseMotionMsg:
		if m.selecting {
			m.sel.Head = selection.Point{X: msg.X, Y: msg.Y}
		}
		return m, nil

	case tea.MouseReleaseMsg:
		if !m.selecting {
			return m, nil
		}
		m.selecting = false
		r := m.sel
		m.sel = selection.Range{}
		if text := selection.Extract(m.render(), r); strings.TrimSpace(text) != "" {
			return m, clip.Copy(text)
		}
		return m, nil

	case tea.MouseWheelMsg:
		// Terminals stop turning the wheel into arrow keys once the app
		// captures the mouse, so do it here.
		switch msg.Button {
		case tea.MouseWheelUp:
			return m.updateKey(tea.KeyPressMsg{Code: tea.KeyUp})
		case tea.MouseWheelDown:
			return m.updateKey(tea.KeyPressMsg{Code: tea.KeyDown})
		}
		return m, nil

	case clip.CopiedMsg:
		return m.showToast(copiedText(msg))

	case toastExpireMsg:
		if m.toast != nil && m.toast.id == msg.id {
			m.toast = nil
		}
		return m, nil

	case frameTickMsg:
		m.frame++
		m, cmd := m.broadcast(nav.FrameMsg{N: m.frame})
		return m, tea.Batch(cmd, tick())

	case nav.NoticeMsg:
		m.notice = &msg
		return m.resize() // notice line changes body height

	case nav.GotoMsg:
		if m.screens != nil {
			m.active = msg.Screen
		}
		return m, nil

	case nav.UpMsg:
		// Screens are one level below the dashboard; the dashboard is the top.
		m.active = nav.ScreenDashboard
		return m, nil

	case nav.ReloadEnvMsg:
		o := m.overrides
		return m, func() tea.Msg {
			e, err := env.Load(o)
			return envLoadedMsg{env: e, err: err}
		}

	case envLoadedMsg:
		m = m.withEnv(msg.env, msg.err)
		m, cmd := m.resize()
		return m, tea.Batch(cmd, m.loadAll())
	}

	return m.broadcast(msg)
}

func (m Model) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	hadNotice := m.notice != nil
	m.notice = nil
	cur := m.current()

	if key.Matches(msg, m.keys.ForceQuit) {
		return m, tea.Quit
	}
	if cur != nil && cur.CapturesKeys() {
		return m.updateCurrent(msg, hadNotice)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.fullHelp = !m.fullHelp
		return m.resize() // footer height changed
	}
	if m.screens != nil {
		switch {
		case key.Matches(msg, m.keys.NextTab):
			m.active = m.active.Next()
			return m.afterNotice(hadNotice)
		case key.Matches(msg, m.keys.PrevTab):
			m.active = m.active.Prev()
			return m.afterNotice(hadNotice)
		}
		for i, b := range m.keys.Screens {
			if key.Matches(msg, b) && i < len(m.screens) {
				m.active = nav.Screen(i)
				return m.afterNotice(hadNotice)
			}
		}
	}
	return m.updateCurrent(msg, hadNotice)
}

// showToast displays a short-lived footer message and schedules its expiry.
func (m Model) showToast(level nav.Level, text string) (Model, tea.Cmd) {
	m.toastSeq++
	id := m.toastSeq
	m.toast = &toast{id: id, text: text, level: level}
	return m, tea.Tick(toastTTL, func(time.Time) tea.Msg { return toastExpireMsg{id: id} })
}

// copiedText describes a finished copy for the toast.
func copiedText(msg clip.CopiedMsg) (nav.Level, string) {
	n := len([]rune(msg.Text))
	what := format.Plural(n, "character")
	if lines := strings.Count(msg.Text, "\n") + 1; lines > 1 {
		what = format.Plural(lines, "line") + ", " + what
	}
	switch {
	case msg.Err != nil:
		return nav.LevelWarn, fmt.Sprintf("Copied %s via the terminal (OSC 52); %s failed: %v", what, msg.Native, msg.Err)
	case msg.Native == "":
		return nav.LevelSuccess, fmt.Sprintf("Copied %s (via the terminal, OSC 52)", what)
	default:
		return nav.LevelSuccess, "Copied " + what + " to the clipboard"
	}
}

// afterNotice re-lays out the body when a dismissed notice freed a line.
func (m Model) afterNotice(hadNotice bool) (Model, tea.Cmd) {
	if hadNotice {
		return m.resize()
	}
	return m, nil
}

func (m Model) updateCurrent(msg tea.Msg, hadNotice bool) (Model, tea.Cmd) {
	m, resizeCmd := m.afterNotice(hadNotice)
	var cmd tea.Cmd
	if m.setup != nil {
		m.setup, cmd = m.setup.Update(msg)
		return m, tea.Batch(resizeCmd, cmd)
	}
	if int(m.active) >= len(m.screens) {
		return m, resizeCmd
	}
	screens := make([]nav.Component, len(m.screens))
	copy(screens, m.screens)
	screens[m.active], cmd = screens[m.active].Update(msg)
	m.screens = screens
	return m, tea.Batch(resizeCmd, cmd)
}

func (m Model) broadcast(msg tea.Msg) (Model, tea.Cmd) {
	if m.setup != nil {
		var cmd tea.Cmd
		m.setup, cmd = m.setup.Update(msg)
		return m, cmd
	}
	screens := make([]nav.Component, len(m.screens))
	cmds := make([]tea.Cmd, 0, len(m.screens))
	for i, s := range m.screens {
		var cmd tea.Cmd
		screens[i], cmd = s.Update(msg)
		cmds = append(cmds, cmd)
	}
	m.screens = screens
	return m, tea.Batch(cmds...)
}

// resize tells every component how much room the body has.
func (m Model) resize() (Model, tea.Cmd) {
	return m.broadcast(nav.ResizeMsg{Width: m.width, Height: m.bodyHeight()})
}

func (m Model) bodyHeight() int {
	h := m.height - headerHeight - lipgloss.Height(m.footer())
	if m.notice != nil {
		h--
	}
	return max(h, 0)
}

// View implements tea.Model. It renders state only.
func (m Model) View() tea.View {
	content := m.render()
	if m.selecting {
		content = selection.Highlight(content, m.sel, theme.Selection)
	}
	v := tea.NewView(content)
	v.AltScreen = true
	if m.mouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	v.WindowTitle = "ws"
	return v
}

func (m Model) render() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	frame := lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height)
	if m.width < minWidth || m.height < minHeight {
		return frame.Render(theme.Warn.Render("Terminal too small"))
	}
	bodyH := m.bodyHeight()
	var body string
	if cur := m.current(); cur != nil {
		body = cur.View()
	} else {
		body = lipgloss.NewStyle().Width(m.width).Render(
			theme.Error.Render("Error: "+m.loadErr.Error()) + "\n\n" +
				theme.Muted.Render("Workspace: "+m.env.Paths.Workspace))
	}
	body = lipgloss.NewStyle().Height(bodyH).MaxHeight(bodyH).MaxWidth(m.width).Render(body)

	parts := []string{m.tabs(), m.rule(), body}
	if m.notice != nil {
		parts = append(parts, m.noticeLine())
	}
	parts = append(parts, m.footer())
	// The footer can outgrow small terminals (full help), so clip the frame.
	return frame.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

// superscripts are the jump-key hints shown after each tab label.
var superscripts = map[string]string{
	"1": "¹", "2": "²", "3": "³", "4": "⁴", "5": "⁵", "6": "⁶", "7": "⁷", "8": "⁸", "9": "⁹", "0": "⁰",
}

// tabs renders the tab bar: "🏠 Dashboard¹  📦 Repos²  …", the jump key as a
// faint superscript. Detail is dropped until it fits the width:
// icon+title → title → icon → digit only.
func (m Model) tabs() string {
	brand := theme.Title.Render("ws")
	if m.screens == nil {
		if m.setup != nil {
			return lipgloss.NewStyle().MaxWidth(m.width).Render(brand + "  " + theme.TabActive.Render(theme.Icon(theme.IconSprout)+"Setup"))
		}
		return brand
	}
	type mode struct{ icon, title bool }
	var bar string
	for _, md := range []mode{{true, true}, {false, true}, {true, false}, {false, false}} {
		parts := []string{brand}
		for i, s := range nav.Screens() {
			digit := keys.ScreenDigit(i)
			var l string
			switch {
			case md.icon && md.title && theme.EmojiOn():
				l = theme.Icon(s.Icon()) + s.Title()
			case md.title:
				l = s.Title()
			case md.icon && theme.EmojiOn():
				l = strings.TrimSpace(theme.Icon(s.Icon()))
			default:
				l = digit
			}
			hint := ""
			if l != digit {
				hint = theme.TabHint.Render(superscripts[digit])
			}
			if s == m.active {
				parts = append(parts, theme.TabActive.Render(l)+hint)
			} else {
				parts = append(parts, theme.TabIdle.Render(l)+hint)
			}
		}
		bar = strings.Join(parts, "  ")
		if lipgloss.Width(bar) <= m.width {
			break
		}
	}
	return lipgloss.NewStyle().MaxWidth(m.width).Render(bar)
}

func (m Model) rule() string {
	return theme.Rule.Render(strings.Repeat("─", m.width))
}

func (m Model) noticeLine() string {
	st, icon := theme.Muted, theme.IconBulb
	switch m.notice.Level {
	case nav.LevelSuccess:
		st, icon = theme.OK, theme.IconCheck
	case nav.LevelWarn:
		st, icon = theme.Warn, theme.IconWork
	case nav.LevelError:
		st, icon = theme.Error, theme.IconFire
	}
	return lipgloss.NewStyle().MaxWidth(m.width).Render(theme.Icon(icon) + st.Render(firstLine(m.notice.Text)))
}

func (m Model) footer() string {
	clip := lipgloss.NewStyle().MaxWidth(m.width)
	cur := m.current()
	if cur == nil {
		return m.withToast(clip.Render(m.help.ShortHelpView([]key.Binding{m.keys.Quit})))
	}
	h := m.help
	h.ShowAll = m.fullHelp
	var km help.KeyMap = combinedKeys{cur.KeyMap(), m.keys}
	if m.setup != nil {
		km = combinedKeys{cur.KeyMap(), setupGlobals{m.keys}} // no screens to switch between
	}
	if cur.CapturesKeys() {
		km = cur.KeyMap() // global keys are inactive while a modal is open
	}
	return m.withToast(clip.Render(h.View(km)))
}

// withToast draws the active toast over the footer's last line, so showing
// and hiding it never changes the layout.
func (m Model) withToast(footer string) string {
	if m.toast == nil {
		return footer
	}
	st := theme.OK
	switch m.toast.level {
	case nav.LevelWarn:
		st = theme.Warn
	case nav.LevelError:
		st = theme.Error
	}
	lines := strings.Split(footer, "\n")
	lines[len(lines)-1] = lipgloss.NewStyle().MaxWidth(m.width).Render(theme.Icon(theme.IconClip) + st.Render(m.toast.text))
	return strings.Join(lines, "\n")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// combinedKeys shows the component's bindings followed by the global ones.
type combinedKeys struct {
	local, global help.KeyMap
}

func (c combinedKeys) ShortHelp() []key.Binding {
	return append(append([]key.Binding{}, c.local.ShortHelp()...), c.global.ShortHelp()...)
}

func (c combinedKeys) FullHelp() [][]key.Binding {
	return append(append([][]key.Binding{}, c.local.FullHelp()...), c.global.FullHelp()...)
}

// setupGlobals is the subset of global keys that apply on the setup screen.
type setupGlobals struct{ k keys.GlobalMap }

func (g setupGlobals) ShortHelp() []key.Binding { return []key.Binding{g.k.Help, g.k.Quit} }

func (g setupGlobals) FullHelp() [][]key.Binding { return [][]key.Binding{{g.k.Help, g.k.Quit}} }
