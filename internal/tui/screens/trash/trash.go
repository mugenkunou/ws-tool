// Package trash is the soft-delete screen: which integrations route deletes
// to the trash, how full it is, the most recently deleted items, and
// enable / disable / empty.
package trash

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/age"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/style"
	wstrash "github.com/mugenkunou/ws-tool/internal/trash"
	"github.com/mugenkunou/ws-tool/internal/tui/confirm"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/modal"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/prompt"
	"github.com/mugenkunou/ws-tool/internal/tui/reslist"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Item is one top-level entry in the trash root.
type Item = wstrash.Item

// LoadedMsg carries trash state. Other components (the dashboard) may
// observe it.
type LoadedMsg struct {
	Status wstrash.Status // integrations, location and totals
	Items  []Item         // newest first
	Err    error
}

// RequestEnableMsg asks the screen to open the enable-soft-delete checklist.
type RequestEnableMsg struct{}

// RequestEmptyMsg asks the screen to open the empty-trash checklist.
type RequestEmptyMsg struct{}

type step int

const (
	stepChange step = iota
	stepDeleteItem
	stepPurgeDays
	stepPurge
)

// Model is the trash screen state.
type Model struct {
	env    env.Env
	keys   keys.TrashMap
	list   reslist.Model[Item]
	modal  modal.Model
	state  LoadedMsg
	loaded bool
	purge  map[string]bool // item names the purge prompt is for; nil = all
	width  int
	height int
}

const headerLines = 5 // root+size, blank, 3 integrations… see header()

// New creates the screen.
func New(e env.Env) Model {
	return Model{
		env:  e,
		keys: keys.Trash(),
		list: reslist.New(wstrash.Schema(e.View()), []reslist.Col[Item]{
			{Key: "name", Min: 20, Weight: 2},
			{Key: "original_path", Min: 12, Weight: 1, Style: func(_ Item, t string) string { return theme.Muted.Render(t) }},
			{Key: "size_bytes", Min: 9},
			{Key: "deleted_at", Min: 9},
		}, theme.Icon(theme.IconSparkles)+"Trash is empty.").Declare(reslist.Decls(actions)),
	}
}

// root is the trash directory (always the XDG trash).
func root(env.Env) string {
	r, _ := wstrash.Root()
	return r
}

// Load reads trash status, size, and the trashed items (under the trash
// root's files/ subdirectory — see internal/trash for the on-disk layout).
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		st, err := wstrash.GetStatus()
		if err != nil {
			return LoadedMsg{Err: err}
		}
		msg := LoadedMsg{Status: st}
		msg.Items, err = wstrash.Items()
		if err != nil {
			msg.Err = err
		}
		return msg
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
		if st == stepPurgeDays {
			return m.purgeDays(msg.(prompt.ResultMsg))
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
		_ = st // both steps reload after completion
		m, cmd := m.reload()
		return m, tea.Batch(nav.Notify(level, confirm.Summary(done.Result)), cmd)
	}

	switch msg := msg.(type) {
	case nav.ResizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list = m.list.SetSize(msg.Width, max(msg.Height-headerLines-1, 0))
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
		m.list = m.list.SetItems(msg.Items)
		return m, nil
	case RequestEnableMsg:
		if !m.modal.Open() {
			m = m.openEnable()
		}
		return m, nil
	case RequestEmptyMsg:
		if !m.modal.Open() {
			m = m.openEmpty(m.list.Items())
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
	case key.Matches(k, m.keys.Refresh):
		return m.reload()
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
}

// actions is every trash key and the rows it acts on.
var actions = func() []reslist.Action[Model, Item] {
	k := keys.Trash()
	plain := func(f func(Model, []Item) Model) func(Model, []Item) (Model, tea.Cmd) {
		return func(m Model, rows []Item) (Model, tea.Cmd) { return f(m, rows), nil }
	}
	return []reslist.Action[Model, Item]{
		{Key: k.Enable, Scope: reslist.Global, Do: func(m Model, _ []Item) (Model, tea.Cmd) { return m.openEnable(), nil }},
		{Key: k.Disable, Scope: reslist.Global, Do: func(m Model, _ []Item) (Model, tea.Cmd) { return m.openDisable(), nil }},
		{Key: k.DeleteItem, Scope: reslist.Selected, Do: plain(Model.openDeleteItems)},
		{Key: k.Empty, Scope: reslist.All, Do: plain(Model.openEmpty)},
		{Key: k.Purge, Scope: reslist.All, Do: Model.promptPurge},
	}
}()

// names is rows as a name set, or nil when rows is every item.
func (m Model) names(rows []Item) map[string]bool {
	if len(rows) == len(m.list.Items()) {
		return nil
	}
	out := make(map[string]bool, len(rows))
	for _, it := range rows {
		out[it.Name] = true
	}
	return out
}

func (m Model) promptPurge(rows []Item) (Model, tea.Cmd) {
	m.purge = m.names(rows)
	title := "Prune items older than (age)"
	if m.purge != nil {
		title = "Prune " + format.Plural(len(rows), "item") + " older than (age)"
	}
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepPurgeDays, prompt.New(title,
		prompt.WithHint("An age such as 30d, 12h or 2w (a bare number is days). Permanently deletes items trashed longer ago."),
		prompt.WithValidate(parseAge)))
	return m, cmd
}

// openEnable mirrors `ws trash enable` with every integration that is off (use
// the CLI's --no-… flags for a subset).
func (m Model) openEnable() Model {
	actions, err := wstrash.EnableActions(wstrash.EnableOptions{})
	if err != nil {
		return m
	}
	pl := plan.Plan{Command: "trash.enable", Actions: actions}
	c := confirm.New("Enable soft-delete", pl).WithNote("For a subset of integrations use `ws trash enable --no-shell-rm|--no-vscode|--no-file-explorer`.")
	m.modal = m.modal.Confirm(stepChange, c)
	return m
}

func (m Model) openDisable() Model {
	pl := plan.Plan{Command: "trash.disable", Actions: wstrash.DisableActions()}
	m.modal = m.modal.Confirm(stepChange, confirm.New("Disable soft-delete", pl))
	return m
}

func (m Model) openDeleteItems(items []Item) Model {
	pl := plan.Plan{Command: "trash.rm"}
	for _, it := range items {
		it := it
		desc := it.Name
		if it.SizeBytes > 0 {
			desc += " (" + style.HumanBytes(it.SizeBytes) + ")"
		}
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "trash-rm-" + it.Name,
			Description: "Permanently delete " + desc,
			Execute:     func() error { return wstrash.Delete(it) },
		})
	}
	m.modal = m.modal.Confirm(stepDeleteItem, confirm.New("Delete "+format.Plural(len(items), "trash item"), pl))
	return m
}

// parseAge validates the prune prompt: an age in the shared format.
func parseAge(v string) error {
	_, err := age.Parse(v)
	return err
}

// purgeDays turns the submitted age into the checklist: one action per trashed
// item older than the cutoff, all pre-selected by the confirm view.
func (m Model) purgeDays(r prompt.ResultMsg) (nav.Component, tea.Cmd) {
	if r.Cancelled {
		return m, nil
	}
	d, err := age.Parse(r.Value)
	if err != nil {
		return m, nav.Notify(nav.LevelWarn, err.Error())
	}
	res, matched, err := wstrash.SelectOlderThan(d, m.env.Now())
	if err != nil {
		return m, nav.Notify(nav.LevelWarn, err.Error())
	}
	if len(matched) == 0 {
		return m, nav.Notify(nav.LevelInfo, fmt.Sprintf("Nothing older than %s.", strings.TrimSpace(r.Value)))
	}
	if m.purge != nil {
		matched = slices.DeleteFunc(matched, func(it wstrash.Item) bool { return !m.purge[it.Name] })
		if len(matched) == 0 {
			return m, nav.Notify(nav.LevelInfo, fmt.Sprintf("None of the %d items is older than %s.", len(m.purge), strings.TrimSpace(r.Value)))
		}
	}
	_ = res
	now := m.env.Now()
	pl := plan.Plan{Command: "trash.prune"}
	for _, it := range matched {
		it := it
		desc := fmt.Sprintf("%s (%s, %s ago)", it.Name, style.HumanBytes(it.SizeBytes), format.Age(now.Sub(it.DeletedAt)))
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "prune-" + it.Name,
			Description: "Permanently delete " + desc,
			Execute:     func() error { return wstrash.Delete(it) },
		})
	}
	title := fmt.Sprintf("Prune trash older than %s", strings.TrimSpace(r.Value))
	m.modal = m.modal.Confirm(stepPurge, confirm.New(title, pl))
	return m, nil
}

// openEmpty empties the whole trash when rows is every item, else deletes
// just rows.
func (m Model) openEmpty(rows []Item) Model {
	if m.names(rows) != nil {
		return m.openDeleteItems(rows)
	}
	st := m.state.Status
	pl := plan.Plan{Command: "trash.empty"}
	if st.Items > 0 {
		pl.Actions = []plan.Action{{
			ID:          "trash-empty",
			Description: fmt.Sprintf("Permanently delete %s (%s) in %s", format.Plural(st.Items, "item"), style.HumanBytes(st.SizeBytes), st.Root),
			Execute:     wstrash.Empty,
		}}
	}
	m.modal = m.modal.Confirm(stepChange, confirm.New("Empty trash", pl))
	return m
}

func (m Model) header() []string {
	if !m.loaded {
		return []string{theme.Muted.Render("Reading trash…"), "", "", "", ""}
	}
	st := m.state.Status
	size := style.HumanBytes(st.SizeBytes) + " · " + format.Plural(st.Files, "file")
	if st.WarnSizeBytes > 0 {
		size += theme.Muted.Render(fmt.Sprintf(" (warns at %s)", style.HumanBytes(st.WarnSizeBytes)))
	}
	if st.OverLimit {
		size = theme.Warn.Render(style.HumanBytes(st.SizeBytes)+" — over the limit") + " · " + format.Plural(st.Files, "file")
	}
	on := func(ok bool, label, detail string) string {
		label = fmt.Sprintf("%-15s", label)
		if ok {
			return "  " + theme.OK.Render("✔ "+label) + theme.Muted.Render(detail)
		}
		return "  " + theme.Muted.Render("✘ "+label+detail)
	}
	return []string{
		theme.Bold.Render(theme.Icon(nav.ScreenTrash.Icon())+m.env.Rel(st.Root)) + "  " + size,
		"",
		on(st.Enabled(wstrash.ShellRM), "shell rm", "rm moves files here"),
		on(st.Enabled(wstrash.VSCode), "VS Code", "editor deletes go here"),
		on(st.Enabled(wstrash.FileExplorer), "file explorer", "desktop trash points here"),
	}
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
