// Package capture is the knowledge-capture screen: configured capture
// locations with a preview of the selected captures file, plus pinning the
// clipboard (new entry or amend), writing a typed note, and editing the file.
package capture

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	wscapture "github.com/mugenkunou/ws-tool/internal/capture"
	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/tui/confirm"
	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/handover"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/layout"
	"github.com/mugenkunou/ws-tool/internal/tui/listview"
	"github.com/mugenkunou/ws-tool/internal/tui/modal"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/prompt"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

const previewLines = 200

// LoadedMsg carries the capture locations, default first then by name.
type LoadedMsg struct {
	Locations []wscapture.Location
	Err       error
}

type previewMsg struct {
	path string
	text string
}

type step int

const (
	stepPinTopic step = iota
	stepPin
	stepAmend
	stepNoteText
	stepNote
)

// region is which pane receives movement keys: enter moves into the
// preview, esc back to the list.
type region int

const (
	regionList region = iota
	regionPreview
)

type handoverTag int

const handoverEdit handoverTag = iota

// Model is the capture screen state.
type Model struct {
	env     env.Env
	keys    keys.CaptureMap
	list    listview.Model
	preview viewport.Model
	modal   modal.Model
	locs    []wscapture.Location
	shown   string // path currently in the preview
	focus   region
	width   int
	height  int
}

// New creates the screen.
func New(e env.Env) Model {
	k := keys.Capture()
	vp := viewport.New()
	vp.SoftWrap = true
	vp.KeyMap = k.Preview
	return Model{
		env:     e,
		keys:    k,
		preview: vp,
		list: listview.New([]layout.Col{
			{Title: "Location", Min: 10, Weight: 1},
			{Title: "File", Min: 20, Weight: 3},
			{Title: "State", Min: 11},
		}, "No capture locations."),
	}
}

func (m Model) wsDir() string { return filepath.Join(m.env.Paths.Workspace, "ws") }

// locations resolves configured capture dirs (relative → workspace).
func locations(e env.Env) map[string]string {
	return config.WithAbsPaths(e.Config, e.Paths.Workspace).Capture.Locations
}

// Load lists capture locations.
func Load(e env.Env) tea.Cmd {
	return func() tea.Msg {
		locs := wscapture.Locations(filepath.Join(e.Paths.Workspace, "ws"), locations(e))
		sort.SliceStable(locs, func(i, j int) bool {
			if (locs[i].Name == "default") != (locs[j].Name == "default") {
				return locs[i].Name == "default"
			}
			return locs[i].Name < locs[j].Name
		})
		return LoadedMsg{Locations: locs}
	}
}

func loadPreview(path string) tea.Cmd {
	return func() tea.Msg {
		data, err := os.ReadFile(path)
		if err != nil {
			return previewMsg{path: path, text: theme.Muted.Render("(no captures yet)")}
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		if len(lines) > previewLines {
			lines = lines[len(lines)-previewLines:]
		}
		return previewMsg{path: path, text: strings.Join(lines, "\n")}
	}
}

// Init starts the first load.
func (m Model) Init() tea.Cmd { return Load(m.env) }

// CapturesKeys implements nav.Component.
func (m Model) CapturesKeys() bool { return m.modal.Open() }

func (m Model) listHeight() int { return min(len(m.locs)+2, max(m.height/3, 3)) }

func (m Model) layout() Model {
	lh := m.listHeight()
	m.list = m.list.SetSize(m.width, lh)
	m.preview.SetWidth(m.width)
	m.preview.SetHeight(max(m.height-lh-1, 0))
	m.modal = m.modal.SetSize(m.width, m.height)
	return m
}

func (m Model) selected() (wscapture.Location, bool) {
	i := m.list.Cursor()
	if i < 0 || i >= len(m.locs) {
		return wscapture.Location{}, false
	}
	return m.locs[i], true
}

// syncPreview loads the preview when the selection changed.
func (m Model) syncPreview() (Model, tea.Cmd) {
	loc, ok := m.selected()
	if !ok || loc.Path == m.shown {
		return m, nil
	}
	m.shown = loc.Path
	m.preview.SetContent(theme.Muted.Render("Loading…"))
	return m, loadPreview(loc.Path)
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
		return m.layout(), nil
	case nav.RefreshMsg:
		m.shown = ""
		return m, Load(m.env)
	case LoadedMsg:
		m.locs = msg.Locations
		m.list = m.list.SetRows(m.rows())
		m = m.layout()
		return m.syncPreview()
	case previewMsg:
		if msg.path == m.shown {
			m.preview.SetContent(msg.text)
			m.preview.GotoBottom()
		}
		return m, nil
	case handover.DoneMsg:
		if _, ok := msg.Tag.(handoverTag); !ok {
			return m, nil
		}
		m.shown = ""
		return m, Load(m.env)
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
	if m.focus == regionPreview {
		if key.Matches(k, m.keys.Back) { // one level up: back to the list
			m.focus = regionList
			return m, nil
		}
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(k)
		return m, cmd
	}
	if key.Matches(k, m.keys.Focus) {
		m.focus = regionPreview
		return m, nil
	}
	loc, haveLoc := m.selected()
	switch {
	case key.Matches(k, m.keys.Back):
		return m, nav.Up
	case key.Matches(k, m.keys.Refresh):
		m.shown = ""
		return m, Load(m.env)
	case !haveLoc:
		return m, nil
	case key.Matches(k, m.keys.Pin):
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepPinTopic, prompt.New("Pin clipboard to "+loc.Name,
			prompt.WithHint("Entry topic (blank to derive it from the content)."), prompt.AllowEmpty()))
		return m, cmd
	case key.Matches(k, m.keys.Amend):
		return m.openPin(stepAmend, loc, "", true), nil
	case key.Matches(k, m.keys.Note):
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Prompt(stepNoteText, prompt.New("Write a note to "+loc.Name,
			prompt.WithHint("One line; edit the file (e) for longer notes.")))
		return m, cmd
	case key.Matches(k, m.keys.Edit):
		return m, m.edit(loc)
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	m, pcmd := m.syncPreview()
	return m, tea.Batch(cmd, pcmd)
}

// edit opens the captures file in the editor, creating it first as the CLI
// does. The file write happens inside the command.
func (m Model) edit(loc wscapture.Location) tea.Cmd {
	editor := m.env.Config.Scratch.EditorCmd
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}
	path := loc.Path
	return tea.Sequence(
		func() tea.Msg {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return nav.NoticeMsg{Level: nav.LevelError, Text: err.Error()}
			}
			if _, err := os.Stat(path); os.IsNotExist(err) {
				_ = os.WriteFile(path, []byte("# Captures\n\n"), 0o644)
			}
			return nil
		},
		handover.Editor(editor, path, handoverEdit),
	)
}

func (m Model) dialogResult(st step, msg tea.Msg) (nav.Component, tea.Cmd) {
	loc, ok := m.selected()
	switch st {
	case stepPinTopic:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled || !ok {
			return m, nil
		}
		return m.openPin(stepPin, loc, r.Value, false), nil
	case stepNoteText:
		r := msg.(prompt.ResultMsg)
		if r.Cancelled || !ok {
			return m, nil
		}
		return m.openNote(loc, r.Value), nil
	}
	done := msg.(confirm.DoneMsg)
	if done.Result.Aborted {
		return m, nil
	}
	level := nav.LevelSuccess
	if done.Result.HasFailures() {
		level = nav.LevelWarn
	}
	m.shown = ""
	return m, tea.Batch(nav.Notify(level, confirm.Summary(done.Result)), Load(m.env))
}

func (m Model) pinOptions(loc wscapture.Location) (wscapture.PinOptions, error) {
	file, assets, err := wscapture.ResolveLocation(m.wsDir(), locations(m.env), loc.Name)
	return wscapture.PinOptions{CapturesFile: file, AssetsDir: assets}, err
}

func (m Model) openPin(st step, loc wscapture.Location, topic string, amend bool) Model {
	opts, err := m.pinOptions(loc)
	desc := "Pin clipboard to " + m.env.Rel(loc.Path)
	if amend {
		desc = "Append clipboard to the last entry in " + m.env.Rel(loc.Path)
	}
	opts.Topic, opts.Amend = topic, amend
	p := plan.Plan{Command: "capture.pin", Actions: []plan.Action{{
		ID:          "capture-pin",
		Description: desc,
		Execute: func() error {
			if err != nil {
				return err
			}
			_, perr := wscapture.PinClipboard(opts)
			return perr
		},
	}}}
	m.modal = m.modal.Confirm(st, confirm.New("Capture", p))
	return m
}

func (m Model) openNote(loc wscapture.Location, text string) Model {
	opts, err := m.pinOptions(loc)
	p := plan.Plan{Command: "capture.pin", Actions: []plan.Action{{
		ID:          "capture-note",
		Description: "Add note to " + m.env.Rel(loc.Path),
		Execute: func() error {
			if err != nil {
				return err
			}
			_, perr := wscapture.PinText(text, opts)
			return perr
		},
	}}}
	m.modal = m.modal.Confirm(stepNote, confirm.New("Capture note", p))
	return m
}

func (m Model) rows() []table.Row {
	out := make([]table.Row, 0, len(m.locs))
	for _, l := range m.locs {
		state := theme.OK.Render("exists")
		if !l.Exists {
			state = theme.Muted.Render("not created")
		}
		out = append(out, table.Row{l.Name, m.env.Rel(l.Path), state})
	}
	return out
}

// View implements nav.Component.
func (m Model) View() string {
	if m.modal.Open() {
		return m.modal.View()
	}
	label := " preview · enter to scroll "
	st := theme.Rule
	if m.focus == regionPreview {
		label, st = " preview · esc back to list ", theme.Title
	}
	rule := st.Render(lipgloss.NewStyle().MaxWidth(m.width).Render("──" + label + strings.Repeat("─", max(m.width-lipgloss.Width(label)-2, 0))))
	return lipgloss.JoinVertical(lipgloss.Left, m.list.View(), rule, m.preview.View())
}

// KeyMap implements nav.Component.
func (m Model) KeyMap() help.KeyMap {
	if m.modal.Open() {
		return m.modal.KeyMap()
	}
	return m.keys
}
