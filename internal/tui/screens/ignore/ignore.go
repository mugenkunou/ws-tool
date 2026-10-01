// Package ignore is the sync-hygiene screen: `ignore scan` violations with
// the `ignore fix` actions (exclude rule, safe-harbor the parent, move to
// scratch, delete) applied per violation through the plan checklist, plus
// `ignore check` for any path. The tree view, excluded-file listing, and
// rule editor are handed to the CLI.
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

	"github.com/mugenkunou/ws-tool/internal/config"
	wsignore "github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/plan"
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

type handoverTag int

const handoverRules handoverTag = iota

// Model is the ignore screen state.
type Model struct {
	env         env.Env
	keys        keys.IgnoreMap
	list        listview.Model
	modal       modal.Model
	all         []wsignore.Violation
	shown       []wsignore.Violation // all, or only actionable when harbors are hidden
	showHarbors bool
	scanning    bool
	width       int
	height      int
}

// New creates the screen.
func New(e env.Env) Model {
	return Model{
		env:      e,
		keys:     keys.Ignore(),
		scanning: true,
		list: listview.New([]layout.Col{
			{Title: "Severity", Min: 8},
			{Title: "Type", Min: 12},
			{Title: "Size", Min: 9},
			{Title: "Path", Min: 20, Weight: 1},
		}, theme.Icon(theme.IconBroom)+"Squeaky clean — no sync hygiene violations."),
	}
}

func engine(e env.Env) (*wsignore.Engine, error) {
	rules, err := manifest.LoadIgnoreRules(e.Paths.Manifest)
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
		abs, err := config.ExpandUserPath(input)
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
	m.list = m.list.SetLoading()
	return m, Load(m.env)
}

func (m Model) filter() Model {
	m.shown = m.shown[:0:0]
	for _, v := range m.all {
		if m.showHarbors || !v.InSafeHarbor {
			m.shown = append(m.shown, v)
		}
	}
	m.list = m.list.SetRows(m.rows())
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
		m.list = m.list.SetSize(msg.Width, max(msg.Height-1, 0))
		m.modal = m.modal.SetSize(msg.Width, msg.Height)
		return m, nil
	case nav.RefreshMsg:
		return m.rescan()
	case LoadedMsg:
		m.scanning = false
		if msg.Err != nil {
			m.list = m.list.SetError(msg.Err)
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
	v, haveV := m.selected()
	switch {
	case key.Matches(k, m.keys.Back):
		return m, nav.Up
	case key.Matches(k, m.keys.Refresh):
		return m.rescan()
	case key.Matches(k, m.keys.ToggleHarbor):
		m.showHarbors = !m.showHarbors
		return m.filter(), nil
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
	case key.Matches(k, m.keys.Exclude) && haveV:
		return m.openFix("Exclude", m.excludeAction(v)), nil
	case key.Matches(k, m.keys.Harbor) && haveV:
		a, ok := m.harborAction(v)
		if !ok {
			return m, nav.Notify(nav.LevelInfo, "Top-level path has no parent to harbor")
		}
		return m.openFix("Safe-harbor", a), nil
	case key.Matches(k, m.keys.Move) && haveV:
		if v.Type != "bloat" {
			return m, nav.Notify(nav.LevelInfo, "Only bloat can be moved to scratch")
		}
		return m.openFix("Move to scratch", m.moveAction(v)), nil
	case key.Matches(k, m.keys.Delete) && haveV:
		return m.openFix("Delete", m.deleteAction(v)), nil
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
}

func (m Model) selected() (wsignore.Violation, bool) {
	i := m.list.Cursor()
	if i < 0 || i >= len(m.shown) {
		return wsignore.Violation{}, false
	}
	return m.shown[i], true
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
	rules, err := manifest.LoadIgnoreRules(e.Paths.Manifest)
	if err != nil {
		return err
	}
	return wsignore.WriteMegaignore(filepath.Join(e.Paths.Workspace, ".megaignore"), rules)
}

func (m Model) excludeAction(v wsignore.Violation) plan.Action {
	e, rel := m.env, v.Path
	return plan.Action{
		ID:          "exclude-" + rel,
		Description: "Add exclude rule for " + m.abs(v),
		Execute: func() error {
			if _, err := manifest.AddIgnoreExclude(e.Paths.Manifest, rel, ""); err != nil {
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
		Description: "Mark " + style.AbsPath(e.Paths.Workspace, dir) + " as a safe harbor",
		Execute: func() error {
			if _, err := manifest.AddIgnoreSafeHarbor(e.Paths.Manifest, pattern, ""); err != nil {
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
	p := plan.Plan{Command: "ignore.fix", Actions: []plan.Action{a}}
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
	title := "Exclude " + format.Plural(len(p.Actions), "violation")
	m.modal = m.modal.Confirm(stepExcludeAll, confirm.New(title, p))
	return m
}

func (m Model) rows() []table.Row {
	out := make([]table.Row, 0, len(m.shown))
	for _, v := range m.shown {
		sev := v.Severity
		switch {
		case v.InSafeHarbor:
			sev = theme.Muted.Render("harbor")
		case sev == "CRITICAL":
			sev = theme.Error.Render(sev)
		case sev == "WARNING":
			sev = theme.Warn.Render(sev)
		}
		size := ""
		switch {
		case v.SizeBytes > 0:
			size = style.HumanBytes(v.SizeBytes)
		case v.Depth > 0:
			size = fmt.Sprintf("%d lvl", v.Depth)
		}
		out = append(out, table.Row{sev, v.Type, size, m.env.Rel(m.abs(v))})
	}
	return out
}

func (m Model) statusLine() string {
	if m.scanning {
		return theme.Muted.Render("Scanning workspace…")
	}
	actionable, harbored := 0, 0
	for _, v := range m.all {
		if v.InSafeHarbor {
			harbored++
		} else {
			actionable++
		}
	}
	parts := []string{format.Plural(actionable, "violation")}
	if harbored > 0 {
		label := fmt.Sprintf("%d in safe harbors", harbored)
		if !m.showHarbors {
			label += " (H to show)"
		}
		parts = append(parts, theme.Muted.Render(label))
	}
	return lipgloss.NewStyle().MaxWidth(m.width).Render(strings.Join(parts, theme.Muted.Render(" · ")))
}

// View implements nav.Component.
func (m Model) View() string {
	if m.modal.Open() {
		return m.modal.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.statusLine(), m.list.View())
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	if m.modal.Open() {
		return m.modal.KeyMap()
	}
	return m.keys
}
