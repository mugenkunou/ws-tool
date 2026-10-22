// Package trash is the soft-delete screen: which integrations route deletes
// to the trash, how full it is, the most recently deleted items, and
// enable / disable / empty.
package trash

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
	Status wstrash.Status
	Scan   wstrash.ScanResult
	Items  []Item // newest first
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

func root(e env.Env) string {
	// The new layout does not have a Trash field in wsconfig;
	// use the standard XDG trash location unconditionally.
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "Trash")
}

// Load reads trash status, size, and the trashed items (under the trash
// root's files/ subdirectory — see internal/trash for the on-disk layout).
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		st, err := wstrash.GetStatus(root(e))
		if err != nil {
			return LoadedMsg{Err: err}
		}
		msg := LoadedMsg{Status: st}
		msg.Scan, _ = wstrash.Scan(wstrash.ScanOptions{RootDir: st.RootDir, WarnSizeMB: 1024})
		msg.Items, err = wstrash.Items(st.RootDir)
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
	title := "Purge items older than (days)"
	if m.purge != nil {
		title = "Purge " + format.Plural(len(rows), "item") + " older than (days)"
	}
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Prompt(stepPurgeDays, prompt.New(title,
		prompt.WithHint("Permanently deletes trash items deleted more than this many days ago."),
		prompt.WithValidate(parseDays)))
	return m, cmd
}

// openEnable mirrors `ws trash enable` with all integrations (use the CLI's
// --no-… flags for a subset: the state file records one combination).
func (m Model) openEnable() Model {
	r, mp := root(m.env), m.env.Paths.Config
	pl := plan.Plan{Command: "trash.enable", Actions: []plan.Action{
		{
			ID:          "trash-setup",
			Description: "Enable soft-delete: shell rm, VS Code, file explorer → " + r,
			Execute: func() error {
				_, err := wstrash.Setup(wstrash.SetupOptions{RootDir: r, ShellRM: true, VSCodeDelete: true, FileExplorer: true})
				return err
			},
		},
		{
			ID:          "trash-record-provisions",
			Description: "Record trash provisions (so `ws reset` can undo them)",
			Execute: func() error {
				if err := wstrash.RecordShellProvisions(mp); err != nil {
					return err
				}
				return wstrash.RecordExplorerProvision(mp, r)
			},
		},
	}}
	c := confirm.New("Enable soft-delete", pl).WithNote("For a subset of integrations use `ws trash enable --no-shell-rm|--no-vscode|--no-file-explorer`.")
	m.modal = m.modal.Confirm(stepChange, c)
	return m
}

func (m Model) openDisable() Model {
	p := m.env.Paths
	pl := plan.Plan{Command: "trash.reset", Actions: []plan.Action{{
		ID:          "trash-reset",
		Description: "Remove soft-delete integrations (script, aliases, symlink)",
		Execute: func() error {
			_, err := wstrash.Reset(wstrash.ResetOptions{WorkspacePath: p.Workspace, ConfigPath: p.Config})
			return err
		},
	}}}
	m.modal = m.modal.Confirm(stepChange, confirm.New("Disable soft-delete", pl))
	return m
}

func (m Model) openDeleteItems(items []Item) Model {
	r := m.state.Status.RootDir
	if r == "" {
		r = root(m.env)
	}
	pl := plan.Plan{Command: "trash.rm"}
	for _, it := range items {
		itemPath := filepath.Join(r, "files", it.Name)
		infoPath := filepath.Join(r, "info", it.Name+".trashinfo")
		desc := it.Name
		if it.SizeBytes > 0 {
			desc += " (" + style.HumanBytes(it.SizeBytes) + ")"
		}
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "trash-rm-" + it.Name,
			Description: "Permanently delete " + desc,
			Execute: func() error {
				if err := os.RemoveAll(itemPath); err != nil {
					return err
				}
				_ = os.Remove(infoPath) // best-effort: remove .trashinfo
				return nil
			},
		})
	}
	m.modal = m.modal.Confirm(stepDeleteItem, confirm.New("Delete "+format.Plural(len(items), "trash item"), pl))
	return m
}

// parseDays validates the purge prompt: a positive whole number of days.
func parseDays(v string) error {
	if _, err := daysFrom(v); err != nil {
		return err
	}
	return nil
}

func daysFrom(v string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("enter a positive whole number of days")
	}
	return n, nil
}

// purgeDays turns the submitted day count into the checklist: one action per
// trashed item older than the cutoff, all pre-selected by the confirm view.
func (m Model) purgeDays(r prompt.ResultMsg) (nav.Component, tea.Cmd) {
	if r.Cancelled {
		return m, nil
	}
	days, err := daysFrom(r.Value)
	if err != nil {
		return m, nav.Notify(nav.LevelWarn, err.Error())
	}
	rt := m.state.Status.RootDir
	if rt == "" {
		rt = root(m.env)
	}
	age := time.Duration(days) * 24 * time.Hour
	res, err := wstrash.ListOlderThan(rt, age, m.env.Now())
	if err != nil {
		return m, nav.Notify(nav.LevelWarn, err.Error())
	}
	if len(res.Matched) == 0 {
		return m, nav.Notify(nav.LevelInfo, fmt.Sprintf("Nothing older than %d days.", days))
	}
	if m.purge != nil {
		res.Matched = slices.DeleteFunc(res.Matched, func(e wstrash.Entry) bool { return !m.purge[e.Name] })
		if len(res.Matched) == 0 {
			return m, nav.Notify(nav.LevelInfo, fmt.Sprintf("None of the %d items is older than %d days.", len(m.purge), days))
		}
	}
	now := m.env.Now()
	pl := plan.Plan{Command: "trash.purge"}
	for _, e := range res.Matched {
		name := e.Name
		desc := fmt.Sprintf("%s (%s, %s ago)", name, style.HumanBytes(e.SizeBytes), format.Age(now.Sub(e.DeletedAt)))
		pl.Actions = append(pl.Actions, plan.Action{
			ID:          "purge-" + name,
			Description: "Permanently delete " + desc,
			Execute: func() error {
				if err := os.RemoveAll(filepath.Join(rt, "files", name)); err != nil {
					return err
				}
				_ = os.Remove(filepath.Join(rt, "info", name+".trashinfo")) // best-effort
				return nil
			},
		})
	}
	title := fmt.Sprintf("Purge trash older than %d days", days)
	m.modal = m.modal.Confirm(stepPurge, confirm.New(title, pl))
	return m, nil
}

// openEmpty empties the whole trash when rows is every item, else deletes
// just rows.
func (m Model) openEmpty(rows []Item) Model {
	if m.names(rows) != nil {
		return m.openDeleteItems(rows)
	}
	r := m.state.Status.RootDir
	if r == "" {
		r = root(m.env)
	}
	sc := m.state.Scan
	pl := plan.Plan{Command: "trash.empty"}
	if sc.FileCount > 0 || len(m.state.Items) > 0 {
		pl.Actions = []plan.Action{{
			ID:          "trash-empty",
			Description: fmt.Sprintf("Permanently delete %s (%s) in %s", format.Plural(sc.FileCount, "file"), style.HumanBytes(sc.SizeBytes), r),
			Execute: func() error {
				_, err := wstrash.Empty(wstrash.EmptyOptions{RootDir: r})
				return err
			},
		}}
	}
	m.modal = m.modal.Confirm(stepChange, confirm.New("Empty trash", pl))
	return m
}

func (m Model) header() []string {
	if !m.loaded {
		return []string{theme.Muted.Render("Reading trash…"), "", "", "", ""}
	}
	st, sc := m.state.Status, m.state.Scan
	size := style.HumanBytes(sc.SizeBytes) + " · " + format.Plural(sc.FileCount, "file")
	if sc.WarnSizeMB > 0 {
		size += theme.Muted.Render(fmt.Sprintf(" (warns at %d MB)", sc.WarnSizeMB))
	}
	if sc.OverLimit {
		size = theme.Warn.Render(style.HumanBytes(sc.SizeBytes)+" — over the limit") + " · " + format.Plural(sc.FileCount, "file")
	}
	on := func(ok bool, label, detail string) string {
		label = fmt.Sprintf("%-15s", label)
		if ok {
			return "  " + theme.OK.Render("✔ "+label) + theme.Muted.Render(detail)
		}
		return "  " + theme.Muted.Render("✘ "+label+detail)
	}
	return []string{
		theme.Bold.Render(theme.Icon(nav.ScreenTrash.Icon())+m.env.Rel(st.RootDir)) + "  " + size,
		"",
		on(st.ShellRMConfigured, "shell rm", "rm moves files here"),
		on(st.VSCodeConfigured, "VS Code", "editor deletes go here"),
		on(st.FileExplorerConfigured, "file explorer", "desktop trash points here"),
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
