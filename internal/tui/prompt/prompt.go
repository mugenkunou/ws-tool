// Package prompt is a one-line text input with a ghost panel of matching
// suggestions (the TUI counterpart of internal/tui.GhostInput).
//
// Suggestions are either a fixed list or produced by a Completer, which runs
// in a tea.Cmd (it may read the filesystem) whenever the value changes.
// enter validates and submits; tab completes to the first match; esc cancels.
// The owner receives a ResultMsg.
package prompt

import (
	"strconv"
	"strings"
	"sync/atomic"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

const maxGhostRows = 6

var nextID atomic.Int64

// ResultMsg reports the submitted value, or Cancelled.
type ResultMsg struct {
	ID        int64
	Value     string
	Cancelled bool
}

// Completer returns suggestions for the current value. It runs off the
// update loop and may do IO.
type Completer func(value string) []string

type suggestionsMsg struct {
	id    int64
	value string
	list  []string
}

// Model is the prompt state.
type Model struct {
	id          int64
	title       string
	hint        string
	input       textinput.Model
	suggestions []string // fixed list (filtered by substring) or completer output
	completer   Completer
	validate    func(string) error
	allowEmpty  bool
	separator   string // when set, suggestions apply to the last segment
	err         string
	keys        keys.PromptMap
	width       int
	height      int
}

// Option configures a prompt.
type Option func(*Model)

// WithValue pre-fills the input.
func WithValue(v string) Option { return func(m *Model) { m.input.SetValue(v); m.input.CursorEnd() } }

// WithPlaceholder sets the placeholder text.
func WithPlaceholder(p string) Option { return func(m *Model) { m.input.Placeholder = p } }

// WithHint adds a muted explanation under the title.
func WithHint(h string) Option { return func(m *Model) { m.hint = h } }

// WithSuggestions shows matching entries from a fixed list.
func WithSuggestions(s []string) Option { return func(m *Model) { m.suggestions = s } }

// WithCompleter computes suggestions from the value (e.g. path completion).
func WithCompleter(c Completer) Option { return func(m *Model) { m.completer = c } }

// WithValidate rejects a submission when v returns an error.
func WithValidate(v func(string) error) Option { return func(m *Model) { m.validate = v } }

// WithSeparator makes the input a list (e.g. ","): suggestions match and
// tab completes the last segment only.
func WithSeparator(sep string) Option { return func(m *Model) { m.separator = sep } }

// AllowEmpty lets an empty value be submitted (default: rejected).
func AllowEmpty() Option { return func(m *Model) { m.allowEmpty = true } }

// New creates a focused prompt.
func New(title string, opts ...Option) Model {
	in := textinput.New()
	in.Prompt = "› "
	in.KeyMap.AcceptSuggestion.SetEnabled(false) // tab is handled here
	in.Focus()
	m := Model{id: nextID.Add(1), title: title, input: in, keys: keys.Prompt()}
	for _, o := range opts {
		o(&m)
	}
	return m
}

// ID identifies this prompt; owners match it against ResultMsg.ID.
func (m Model) ID() int64 { return m.id }

// Init starts the first completion, if any.
func (m Model) Init() tea.Cmd { return m.complete() }

// SetSize fits the prompt into width×height cells.
func (m Model) SetSize(width, height int) Model {
	m.width, m.height = width, height
	m.input.SetWidth(max(width-lipgloss.Width(m.input.Prompt)-1, 1))
	return m
}

// Update handles typing, completion results, submit and cancel.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case suggestionsMsg:
		if msg.id == m.id && msg.value == m.input.Value() {
			m.suggestions = msg.list
		}
		return m, nil
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Cancel):
			id := m.id
			return m, func() tea.Msg { return ResultMsg{ID: id, Cancelled: true} }
		case key.Matches(msg, m.keys.Submit):
			v := strings.TrimSpace(m.input.Value())
			if v == "" && !m.allowEmpty {
				m.err = "a value is required"
				return m, nil
			}
			if m.validate != nil {
				if err := m.validate(v); err != nil {
					m.err = err.Error()
					return m, nil
				}
			}
			id := m.id
			return m, func() tea.Msg { return ResultMsg{ID: id, Value: v} }
		case key.Matches(msg, m.keys.Complete):
			if match := m.matches(); len(match) > 0 {
				head, _ := m.split()
				m.input.SetValue(head + match[0])
				m.input.CursorEnd()
				return m, m.complete()
			}
			return m, nil
		}
		before := m.input.Value()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.input.Value() != before {
			m.err = ""
			return m, tea.Batch(cmd, m.complete())
		}
		return m, cmd
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) complete() tea.Cmd {
	if m.completer == nil {
		return nil
	}
	id, value, c := m.id, m.input.Value(), m.completer
	return func() tea.Msg { return suggestionsMsg{id: id, value: value, list: c(value)} }
}

// matches returns suggestions relevant to the current value.
func (m Model) matches() []string {
	if m.completer != nil {
		return m.suggestions // the completer already filtered
	}
	_, last := m.split()
	q := strings.ToLower(strings.TrimSpace(last))
	var out []string
	for _, s := range m.suggestions {
		if q == "" || strings.Contains(strings.ToLower(s), q) {
			out = append(out, s)
		}
	}
	return out
}

// split returns the input up to and including the last separator (with a
// trailing space), and the segment after it.
func (m Model) split() (head, last string) {
	v := m.input.Value()
	if m.separator == "" {
		return "", v
	}
	i := strings.LastIndex(v, m.separator)
	if i < 0 {
		return "", v
	}
	return strings.TrimRight(v[:i+len(m.separator)], " ") + " ", v[i+len(m.separator):]
}

// View renders the prompt.
func (m Model) View() string {
	lines := []string{theme.Title.Render(m.title)}
	if m.hint != "" {
		lines = append(lines, theme.Muted.Render(m.hint))
	}
	lines = append(lines, "", m.input.View())
	if m.err != "" {
		lines = append(lines, theme.Error.Render("✘ "+m.err))
	}
	if match := m.matches(); len(match) > 0 {
		lines = append(lines, "")
		for i, s := range match {
			if i == maxGhostRows {
				lines = append(lines, theme.Muted.Render("  … "+strconv.Itoa(len(match)-maxGhostRows)+" more"))
				break
			}
			lines = append(lines, theme.Muted.Render("  "+s))
		}
	}
	return lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(strings.Join(lines, "\n"))
}

// KeyMap describes the bindings for the help bar.
func (m Model) KeyMap() help.KeyMap { return m.keys }
