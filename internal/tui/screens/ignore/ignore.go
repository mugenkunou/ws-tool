// Package ignore is the sync-hygiene screen: `ignore scan` violations with
// the `ignore fix` actions (exclude rule, suppress warning, move to scratch,
// delete) applied per violation through the plan checklist, plus `ignore check`
// for any path. The screen has two levels: live violations and suppressed
// warnings. The tree view, excluded-file listing, and rule editor are handed
// to the CLI.
package ignore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	wsignore "github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/tui/complete"
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

// LoadedMsg carries scan violations. Other components (the dashboard) may
// observe it.
type LoadedMsg struct {
	Violations []wsignore.Violation
	Err        error
}

// Actionable counts violations outside safe harbors.
func (m LoadedMsg) Actionable() int {
	n := 0
	for _, v := range m.Violations {
		if !v.InSafeHarbor {
			n++
		}
	}
	return n
}

type checkMsg struct {
	text  string
	level nav.Level
}

type step int

const (
	stepFix step = iota
	stepExcludeAll
	stepCheckPath
)

type screenLevel int

const (
	levelLive screenLevel = iota // live violations
	levelSuppressed               // suppressed warnings
)

type handoverTag int

const handoverRules handoverTag = iota

// Model is the ignore screen state.
type Model struct {
	env      env.Env
	keys     keys.IgnoreMap
	liveList listview.Model
	suppList listview.Model
	modal    modal.Model
	all      []wsignore.Violation
	live     []wsignore.Violation // actionable (not in safe harbor)
	supp     []wsignore.Violation // suppressed (in safe harbor)
	marks    map[int]bool         // indices into the current level's slice marked for bulk action
	level    screenLevel
	scanning bool
	width    int
	height   int
}

// New creates the screen.
func New(e env.Env) Model {
	cols := []layout.Col{
		{Title: "Severity", Min: 8},
		{Title: "Type", Min: 12},
		{Title: "Size", Min: 9},
		{Title: "Path", Min: 20, Weight: 1},
	}
	return Model{
		env:      e,
		keys:     keys.Ignore(),
		scanning: true,
		liveList: listview.New(cols, theme.Icon(theme.IconBroom)+"Squeaky clean — no sync hygiene violations."),
		suppList: listview.New(cols, theme.Icon(theme.IconBroom)+"No suppressed warnings."),
	}
}

func engine(e env.Env) (*wsignore.Engine, error) {
	rules, err := wsconfig.LoadIgnoreRules(e.Paths.Config)
	if err != nil {
		return nil, err
	}
	return wsignore.BuildEngine(rules), nil
}

// Load runs `ignore scan`.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		eng, err := engine(e)
		if err != nil {
			return LoadedMsg{Err: err}
		}
		vs, err := wsignore.Scan(wsignore.ScanOptions{
			WorkspacePath: e.Paths.Workspace,
			WarnSizeMB:    e.Config.Ignore.WarnSizeMB,
			CritSizeMB:    e.Config.Ignore.CritSizeMB,
			MaxDepth:      e.Config.Ignore.MaxDepth,
			Engine:        eng,
		})
		return LoadedMsg{Violations: vs, Err: err}
	}
}

func check(e env.Env, input string) tea.Cmd {
	return func() tea.Msg {
		abs, err := wsconfig.ExpandUserPath(input)
		if err != nil {
			return checkMsg{level: nav.LevelError, text: "Invalid path: " + err.Error()}
		}
		st, err := os.Stat(abs)
		if err != nil {
			return checkMsg{level: nav.LevelError, text: "Path does not exist: " + abs}
		}
		eng, err := engine(e)
		if err != nil {
			return checkMsg{level: nav.LevelError, text: err.Error()}
		}
		res, _, err := wsignore.Check(eng, e.Paths.Workspace, abs, st.IsDir())
		if err != nil {
			return checkMsg{level: nav.LevelError, text: err.Error()}
		}
		switch {
		case !res.Included:
			return checkMsg{level: nav.LevelWarn, text: fmt.Sprintf("✘ IGNORED  %s — excluded by rule `%s`", abs, res.Rule)}
		case res.SafeHarbor:
			return checkMsg{level: nav.LevelSuccess, text: fmt.Sprintf("✔ SYNCED  %s — safe harbor `%s` overrides an exclude rule", abs, res.Rule)}
		case res.Rule == "<default>":
			return checkMsg{level: nav.LevelSuccess, text: fmt.Sprintf("✔ SYNCED  %s — no matching exclude rule", abs)}
		default:
			return checkMsg{level: nav.LevelSuccess, text: fmt.Sprintf("✔ SYNCED  %s — included by rule `%s`", abs, res.Rule)}
		}
	}
}

// Init starts the first scan.
func (m Model) Init() tea.Cmd { return Load(m.env) }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() }

func (m Model) rescan() (Model, tea.Cmd) {
	m.scanning = true
	m.marks = nil
	m.liveList = m.liveList.SetLoading()
	m.suppList = m.suppList.SetLoading()
	return m, Load(m.env)
}

// currentShown returns the violations shown in the active level.
func (m Model) currentShown() []wsignore.Violation {
	if m.level == levelSuppressed {
		return m.supp
	}
	return m.live
}

func (m Model) filter() Model {
	m.live = m.live[:0:0]
	m.supp = m.supp[:0:0]
	for _, v := range m.all {
		if v.InSafeHarbor {
			m.supp = append(m.supp, v)
		} else {
			m.live = append(m.live, v)
		}
	}
	m.liveList = m.liveList.SetRows(m.liveRows())
	m.suppList = m.suppList.SetRows(m.suppRows())
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
		listH := max(msg.Height-1, 0)
		m.liveList = m.liveList.SetSize(msg.Width, listH)
		m.suppList = m.suppList.SetSize(msg.Width, listH)
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.RefreshMsg:
		return m.rescan()
	case LoadedMsg:
		m.scanning = false
		if msg.Err != nil {
			m.liveList = m.liveList.SetError(msg.Err)
			return m, nil
		}
		m.all = msg.Violations
		return m.filter(), nil
	case checkMsg:
		return m, nav.Notify(msg.level, msg.text)
	case handover.DoneMsg:
		if _, ok := msg.Tag.(handoverTag); ok {
			return m.rescan()
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

	// Level switching: H opens suppressed screen; Back returns to live screen.
	if key.Matches(k, m.keys.ViewSuppressed) && m.level == levelLive {
		m.level = levelSuppressed
		m.marks = nil
		return m, nil
	}
	if key.Matches(k, m.keys.Back) {
		if m.level == levelSuppressed {
			m.level = levelLive
			m.marks = nil
			return m, nil
		}
		return m, nav.Up
	}

	// Route remaining keys to the active level.
	shown := m.currentShown()
	v, haveV := m.selectedFrom(shown)
	switch {
	case key.Matches(k, m.keys.Refresh):
		return m.rescan()
	case key.Matches(k, m.keys.Check):
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepCheckPath, prompt.New("Will this path sync?",
			prompt.WithValue(m.env.Paths.Workspace+"/"), prompt.WithCompleter(complete.Paths)))
		return m, cmd
	case key.Matches(k, m.keys.Tree):
		return m, handover.Run(handover.Paged(handover.WS(m.env.Paths, "ignore", "tree")), handoverRules)
	case key.Matches(k, m.keys.List):
		return m, handover.Run(handover.Paged(handover.WS(m.env.Paths, "ignore", "ls")), handoverRules)
	case key.Matches(k, m.keys.EditRules):
		return m, handover.Run(handover.WS(m.env.Paths, "ignore", "edit"), handoverRules)
	case key.Matches(k, m.keys.ExcludeAll):
		return m.openExcludeAll(), nil
	case key.Matches(k, m.keys.Select) && len(shown) > 0:
		idx := m.activeList().Cursor()
		if m.marks == nil {
			m.marks = make(map[int]bool)
		}
		if m.marks[idx] {
			delete(m.marks, idx)
		} else {
			m.marks[idx] = true
		}
		// Advance cursor one row after marking (ergonomic bulk-marking).
		m = m.advanceActiveCursor()
		return m.refreshRows(), nil
	case key.Matches(k, m.keys.SelectAll) && len(shown) > 0:
		if len(m.marks) == len(shown) {
			m.marks = nil
		} else {
			m.marks = make(map[int]bool, len(shown))
			for i := range shown {
				m.marks[i] = true
			}
		}
		return m.refreshRows(), nil

	// --- Live-level actions ---
	case m.level == levelLive && key.Matches(k, m.keys.Exclude) && haveV:
		targets := m.markedViolations(v, shown)
		var actions []plan.Action
		for _, t := range targets {
			actions = append(actions, m.excludeAction(t))
		}
		return m.openFixMulti("Add to .megaignore", actions), nil
	case m.level == levelLive && key.Matches(k, m.keys.Suppress) && haveV:
		targets := m.markedViolations(v, shown)
		var actions []plan.Action
		for _, t := range targets {
			a, ok := m.harborAction(t)
			if ok {
				actions = append(actions, a)
			}
		}
		if len(actions) == 0 {
			return m, nav.Notify(nav.LevelInfo, "Top-level path has no parent to suppress")
		}
		return m.openFixMulti("Suppress warning", actions), nil
	case m.level == levelLive && key.Matches(k, m.keys.Move) && haveV:
		if v.Type != "bloat" {
			return m, nav.Notify(nav.LevelInfo, "Only bloat can be moved to scratch")
		}
		targets := m.markedViolations(v, shown)
		var actions []plan.Action
		for _, t := range targets {
			if t.Type == "bloat" {
				actions = append(actions, m.moveAction(t))
			}
		}
		return m.openFixMulti("Move to scratch", actions), nil
	case m.level == levelLive && key.Matches(k, m.keys.Delete) && haveV:
		targets := m.markedViolations(v, shown)
		var actions []plan.Action
		for _, t := range targets {
			actions = append(actions, m.deleteAction(t))
		}
		return m.openFixMulti("Delete", actions), nil

	// --- Suppressed-level actions ---
	case m.level == levelSuppressed && key.Matches(k, m.keys.Unsuppress) && haveV:
		targets := m.markedViolations(v, shown)
		var actions []plan.Action
		for _, t := range targets {
			a, ok := m.unsuppressAction(t)
			if ok {
				actions = append(actions, a)
			}
		}
		if len(actions) == 0 {
			return m, nav.Notify(nav.LevelInfo, "Cannot determine suppress rule for selected items")
		}
		return m.openFixMulti("Remove suppress rules", actions), nil
	}

	// Delegate movement to the active list.
	var cmd tea.Cmd
	if m.level == levelSuppressed {
		m.suppList, cmd = m.suppList.Update(k)
	} else {
		m.liveList, cmd = m.liveList.Update(k)
	}
	return m, cmd
}

// activeList returns the list model for the current level.
func (m Model) activeList() listview.Model {
	if m.level == levelSuppressed {
		return m.suppList
	}
	return m.liveList
}

// advanceActiveCursor moves the cursor down one row in the active list.
func (m Model) advanceActiveCursor() Model {
	if m.level == levelSuppressed {
		m.suppList, _ = m.suppList.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	} else {
		m.liveList, _ = m.liveList.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	return m
}

func (m Model) selectedFrom(shown []wsignore.Violation) (wsignore.Violation, bool) {
	i := m.activeList().Cursor()
	if i < 0 || i >= len(shown) {
		return wsignore.Violation{}, false
	}
	return shown[i], true
}

// markedViolations returns the marked subset of shown, or [fallback] when no
// marks are set. This lets action keys work on a bulk selection or fall back
// to the cursor item seamlessly.
func (m Model) markedViolations(fallback wsignore.Violation, shown []wsignore.Violation) []wsignore.Violation {
	if len(m.marks) == 0 {
		return []wsignore.Violation{fallback}
	}
	out := make([]wsignore.Violation, 0, len(m.marks))
	for i, v := range shown {
		if m.marks[i] {
			out = append(out, v)
		}
	}
	return out
}

// refreshRows rebuilds both list rows to reflect current mark state.
func (m Model) refreshRows() Model {
	m.liveList = m.liveList.SetRows(m.liveRows())
	m.suppList = m.suppList.SetRows(m.suppRows())
	return m
}

func (m Model) dialogResult(st step, msg tea.Msg) (nav.Component, tea.Cmd) {
	if st == stepCheckPath {
		r := msg.(prompt.ResultMsg)
		if r.Cancelled {
			return m, nil
		}
		return m, check(m.env, r.Value)
	}
	done := msg.(confirm.DoneMsg)
	if done.Result.Aborted {
		return m, nil
	}
	level := nav.LevelSuccess
	if done.Result.HasFailures() {
		level = nav.LevelWarn
	}
	m, cmd := m.rescan()
	return m, tea.Batch(nav.Notify(level, confirm.Summary(done.Result)), cmd)
}

func (m Model) abs(v wsignore.Violation) string { return style.AbsPath(m.env.Paths.Workspace, v.Path) }

// regenerate rewrites .megaignore from the manifest's rules, as `ignore fix`
// does after adding rules.
func regenerate(e env.Env) error {
	rules, err := wsconfig.LoadIgnoreRules(e.Paths.Config)
	if err != nil {
		return err
	}
	return wsignore.WriteMegaignore(filepath.Join(e.Paths.Workspace, ".megaignore"), rules)
}

func (m Model) excludeAction(v wsignore.Violation) plan.Action {
	e, rel := m.env, v.Path
	return plan.Action{
		ID:          "exclude-" + rel,
		Description: "Add to .megaignore: " + m.abs(v),
		Execute: func() error {
			if _, err := wsconfig.AddIgnoreExclude(e.Paths.Config, rel, ""); err != nil {
				return err
			}
			return regenerate(e)
		},
	}
}

func (m Model) harborAction(v wsignore.Violation) (plan.Action, bool) {
	dir := filepath.ToSlash(filepath.Dir(v.Path))
	if dir == "." || dir == "" {
		return plan.Action{}, false
	}
	e, pattern := m.env, dir+"/**"
	return plan.Action{
		ID:          "harbor-" + dir,
		Description: "Suppress warnings for " + style.AbsPath(e.Paths.Workspace, dir) + "/**",
		Execute: func() error {
			if _, err := wsconfig.AddIgnoreSafeHarbor(e.Paths.Config, pattern, ""); err != nil {
				return err
			}
			return regenerate(e)
		},
	}, true
}

func (m Model) unsuppressAction(v wsignore.Violation) (plan.Action, bool) {
	dir := filepath.ToSlash(filepath.Dir(v.Path))
	if dir == "." || dir == "" {
		return plan.Action{}, false
	}
	e, pattern := m.env, dir+"/**"
	return plan.Action{
		ID:          "unsuppress-" + dir,
		Description: "Remove suppress rule for " + style.AbsPath(e.Paths.Workspace, dir) + "/**",
		Execute: func() error {
			if _, err := wsconfig.RemoveIgnoreSafeHarbor(e.Paths.Config, pattern); err != nil {
				return err
			}
			return regenerate(e)
		},
	}, true
}

func (m Model) moveAction(v wsignore.Violation) plan.Action {
	src := m.abs(v)
	dest := filepath.Join(m.env.ScratchDir, filepath.Base(v.Path))
	return plan.Action{
		ID:          "move-" + v.Path,
		Description: fmt.Sprintf("Move %s → %s", src, dest),
		Execute: func() error {
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			return os.Rename(src, dest)
		},
	}
}

func (m Model) deleteAction(v wsignore.Violation) plan.Action {
	p := m.abs(v)
	size := ""
	if v.SizeBytes > 0 {
		size = " (" + style.HumanBytes(v.SizeBytes) + ")"
	}
	return plan.Action{
		ID:          "delete-" + v.Path,
		Description: "Delete " + p + size,
		Execute:     func() error { return os.RemoveAll(p) },
	}
}

func (m Model) openFix(title string, a plan.Action) Model {
	return m.openFixMulti(title, []plan.Action{a})
}

func (m Model) openFixMulti(title string, actions []plan.Action) Model {
	p := plan.Plan{Command: "ignore.fix", Actions: actions}
	m.modal = m.modal.Confirm(stepFix, confirm.New(title, p))
	return m
}

func (m Model) openExcludeAll() Model {
	p := plan.Plan{Command: "ignore.fix"}
	for _, v := range m.all {
		if !v.InSafeHarbor {
			p.Actions = append(p.Actions, m.excludeAction(v))
		}
	}
	title := "Add " + format.Plural(len(p.Actions), "violation") + " to .megaignore"
	m.modal = m.modal.Confirm(stepExcludeAll, confirm.New(title, p))
	return m
}

func violationRow(v wsignore.Violation, marked bool, rel func(string) string, abs func(wsignore.Violation) string) table.Row {
	sev := v.Severity
	switch {
	case v.InSafeHarbor:
		sev = theme.Muted.Render("suppressed")
	case sev == "CRITICAL":
		sev = theme.Error.Render(sev)
	case sev == "WARNING":
		sev = theme.Warn.Render(sev)
	}
	if marked {
		sev = theme.Title.Render("✓")
	}
	size := ""
	switch {
	case v.SizeBytes > 0:
		size = style.HumanBytes(v.SizeBytes)
	case v.Depth > 0:
		size = fmt.Sprintf("%d lvl", v.Depth)
	}
	path := rel(abs(v))
	if v.InSafeHarbor {
		path = theme.Muted.Render(path)
	}
	return table.Row{sev, v.Type, size, path}
}

func (m Model) liveRows() []table.Row {
	out := make([]table.Row, 0, len(m.live))
	for i, v := range m.live {
		out = append(out, violationRow(v, m.level == levelLive && m.marks[i], m.env.Rel, m.abs))
	}
	return out
}

func (m Model) suppRows() []table.Row {
	out := make([]table.Row, 0, len(m.supp))
	for i, v := range m.supp {
		out = append(out, violationRow(v, m.level == levelSuppressed && m.marks[i], m.env.Rel, m.abs))
	}
	return out
}

func (m Model) statusLine() string {
	if m.scanning {
		return theme.Muted.Render("Scanning workspace…")
	}
	if m.level == levelSuppressed {
		parts := []string{
			theme.Title.Render("Suppressed warnings"),
			theme.Muted.Render(fmt.Sprintf("%d item(s)", len(m.supp))),
		}
		if len(m.marks) > 0 {
			parts = append([]string{theme.Title.Render(fmt.Sprintf("%d marked", len(m.marks)))}, parts[1:]...)
		}
		parts = append(parts, theme.Muted.Render("u unsuppress · esc back"))
		return lipgloss.NewStyle().MaxWidth(m.width).Render(strings.Join(parts, theme.Muted.Render(" · ")))
	}
	parts := []string{format.Plural(len(m.live), "violation")}
	if len(m.marks) > 0 {
		parts = []string{theme.Title.Render(fmt.Sprintf("%d marked", len(m.marks))), format.Plural(len(m.live), "violation")}
	}
	if len(m.supp) > 0 {
		parts = append(parts, theme.Muted.Render(fmt.Sprintf("%d suppressed (H to view)", len(m.supp))))
	}
	return lipgloss.NewStyle().MaxWidth(m.width).Render(strings.Join(parts, theme.Muted.Render(" · ")))
}

// View implements nav.Component.
func (m Model) View() string {
	if m.modal.Open() {
		return m.modal.View()
	}
	list := m.liveList
	if m.level == levelSuppressed {
		list = m.suppList
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.statusLine(), list.View())
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	if m.modal.Open() {
		return m.modal.KeyMap()
	}
	return m.keys
}
