// Package trash is the soft-delete screen: which integrations route deletes
// to the trash, how full it is, the most recently deleted items, and
// enable / disable / empty.
package trash

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/style"
	wstrash "github.com/mugenkunou/ws-tool/internal/trash"
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

const maxItems = 200

// Item is one top-level entry in the trash root.
type Item struct {
	Name      string
	SizeBytes int64
	Deleted   time.Time // modification time of the entry
	Dir       bool
}

// LoadedMsg carries trash state. Other components (the dashboard) may
// observe it.
type LoadedMsg struct {
	Status wstrash.Status
	Scan   wstrash.ScanResult
	Items  []Item // newest first, at most maxItems
	Err    error
}

// RequestEnableMsg asks the screen to open the enable-soft-delete checklist.
type RequestEnableMsg struct{}

// RequestEmptyMsg asks the screen to open the empty-trash checklist.
type RequestEmptyMsg struct{}

type step int

const stepChange step = iota

// Model is the trash screen state.
type Model struct {
	env    env.Env
	keys   keys.TrashMap
	list   listview.Model
	modal  modal.Model
	state  LoadedMsg
	loaded bool
	width  int
	height int
}

const headerLines = 5 // root+size, blank, 3 integrations… see header()

// New creates the screen.
func New(e env.Env) Model {
	return Model{
		env:  e,
		keys: keys.Trash(),
		list: listview.New([]layout.Col{
			{Title: "Recently deleted", Min: 20, Weight: 1},
			{Title: "Size", Min: 9},
			{Title: "Deleted", Min: 9},
		}, theme.Icon(theme.IconSparkles)+"Trash is empty."),
	}
}

func root(e env.Env) string {
	r := config.WithAbsPaths(e.Config, e.Paths.Workspace).Trash.RootDir
	if r == "" {
		r, _ = config.ExpandUserPath("~/.Trash")
	}
	return r
}

// Load reads trash status, size, and top-level entries.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		st, err := wstrash.GetStatus(root(e))
		if err != nil {
			return LoadedMsg{Err: err}
		}
		msg := LoadedMsg{Status: st}
		msg.Scan, _ = wstrash.Scan(wstrash.ScanOptions{RootDir: st.RootDir, WarnSizeMB: e.Config.Trash.WarnSizeMB})
		entries, err := os.ReadDir(st.RootDir)
		if err != nil {
			return msg // a missing trash root just means nothing deleted yet
		}
		for _, de := range entries {
			info, err := de.Info()
			if err != nil {
				continue
			}
			it := Item{Name: de.Name(), Deleted: info.ModTime(), Dir: de.IsDir(), SizeBytes: info.Size()}
			if de.IsDir() {
				it.SizeBytes = dirSize(filepath.Join(st.RootDir, de.Name()))
			}
			msg.Items = append(msg.Items, it)
		}
		sort.Slice(msg.Items, func(i, j int) bool { return msg.Items[i].Deleted.After(msg.Items[j].Deleted) })
		if len(msg.Items) > maxItems {
			msg.Items = msg.Items[:maxItems]
		}
		return msg
	}
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
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
		m.list = m.list.SetRows(m.rows())
		return m, nil
	case RequestEnableMsg:
		if !m.modal.Open() {
			m = m.openEnable()
		}
		return m, nil
	case RequestEmptyMsg:
		if !m.modal.Open() {
			m = m.openEmpty()
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
	switch {
	case key.Matches(k, m.keys.Back):
		return m, nav.Up
	case key.Matches(k, m.keys.Refresh):
		return m.reload()
	case key.Matches(k, m.keys.Enable):
		return m.openEnable(), nil
	case key.Matches(k, m.keys.Disable):
		return m.openDisable(), nil
	case key.Matches(k, m.keys.Empty):
		return m.openEmpty(), nil
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
}

// openEnable mirrors `ws trash enable` with all integrations (use the CLI's
// --no-… flags for a subset: the state file records one combination).
func (m Model) openEnable() Model {
	r, mp := root(m.env), m.env.Paths.Manifest
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
			_, err := wstrash.Reset(wstrash.ResetOptions{WorkspacePath: p.Workspace, ManifestPath: p.Manifest})
			return err
		},
	}}}
	m.modal = m.modal.Confirm(stepChange, confirm.New("Disable soft-delete", pl))
	return m
}

func (m Model) openEmpty() Model {
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

func (m Model) rows() []table.Row {
	out := make([]table.Row, 0, len(m.state.Items))
	now := m.env.Now()
	for _, it := range m.state.Items {
		name := it.Name
		if it.Dir {
			name += "/"
		}
		out = append(out, table.Row{name, style.HumanBytes(it.SizeBytes), format.Age(now.Sub(it.Deleted)) + " ago"})
	}
	return out
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
	return m.keys
}
