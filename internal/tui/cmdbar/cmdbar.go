// Package cmdbar is the one-line command bar (spec §6.6): `:` and the input,
// drawn in place of the footer, with the matching completions after the cursor,
// dimmed and clipped to the width. tab completes the first match, enter submits
// the line, esc cancels. The owner (the app) builds the candidates and
// parses the line; the bar only edits text.
package cmdbar

import (
	"strings"
	"sync/atomic"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

var nextID atomic.Int64

// ResultMsg reports a submitted line, or Cancelled. The line is trimmed and
// has no leading `:`.
type ResultMsg struct {
	ID        int64
	Line      string
	Cancelled bool
}

// Model is the command bar state.
type Model struct {
	id    int64
	input []rune
	cands []string
	keys  keys.PromptMap
	width int
}

// New opens a bar with prefill typed in and candidates to complete from, in
// the order they should be offered.
func New(prefill string, candidates []string) Model {
	return Model{id: nextID.Add(1), input: []rune(prefill), cands: candidates, keys: keys.Prompt()}
}

// ID identifies this bar; the owner matches it against ResultMsg.ID.
func (m Model) ID() int64 { return m.id }

// SetWidth sets the width the bar may draw into.
func (m Model) SetWidth(w int) Model {
	m.width = w
	return m
}

// Value is the text typed so far.
func (m Model) Value() string { return string(m.input) }

// Matches are the candidates the typed text is a proper prefix of, ignoring
// case, in the order given. Candidates equal to the text are left out, since
// there is nothing to complete.
func (m Model) Matches() []string {
	typed := strings.ToLower(strings.TrimLeft(string(m.input), " "))
	var out []string
	seen := map[string]bool{}
	for _, c := range m.cands {
		lc := strings.ToLower(c)
		if lc == typed || !strings.HasPrefix(lc, typed) || seen[lc] {
			continue
		}
		seen[lc] = true
		out = append(out, c)
	}
	return out
}

// Update handles typing, tab, enter and esc.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	id := m.id
	switch {
	case key.Matches(k, m.keys.Cancel):
		return m, func() tea.Msg { return ResultMsg{ID: id, Cancelled: true} }
	case key.Matches(k, m.keys.Submit):
		line := strings.TrimSpace(string(m.input))
		return m, func() tea.Msg { return ResultMsg{ID: id, Line: line} }
	case key.Matches(k, m.keys.Complete):
		if ms := m.Matches(); len(ms) > 0 {
			m.input = []rune(ms[0])
		}
	case k.Code == tea.KeyBackspace:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	case k.Code == 'u' && k.Mod == tea.ModCtrl:
		m.input = nil
	case k.Text != "":
		m.input = append(m.input, []rune(k.Text)...)
	}
	return m, nil
}

// View draws the bar on one line: `:` and the text, then the first match's
// rest and the other matches, dimmed, clipped to the width.
func (m Model) View() string {
	line := theme.Bold.Render(":") + string(m.input) + theme.Muted.Render("▏")
	if ms := m.Matches(); len(ms) > 0 {
		typed := len([]rune(strings.TrimLeft(string(m.input), " ")))
		rest := string([]rune(ms[0])[min(typed, len([]rune(ms[0]))):])
		tail := rest
		if len(ms) > 1 {
			tail += "   " + strings.Join(ms[1:], " · ")
		}
		line += theme.Muted.Render(tail)
	}
	if m.width <= 0 {
		return line
	}
	return lipgloss.NewStyle().MaxWidth(m.width).Render(line)
}

// Closest returns the candidate nearest to typed by edit distance, for the
// "did you mean" notice. It returns "" when nothing is close (more than half of
// the typed word would have to change).
func Closest(typed string, candidates []string) string {
	typed = strings.ToLower(strings.TrimSpace(typed))
	best, bestD := "", 1<<30
	for _, c := range candidates {
		d := distance(typed, strings.ToLower(c))
		if strings.HasPrefix(strings.ToLower(c), typed) {
			d = 0 // a longer command that starts with what was typed
		}
		if d < bestD {
			best, bestD = c, d
		}
	}
	if bestD > max(len(typed)/2, 1) {
		return ""
	}
	return best
}

// distance is the Levenshtein distance between a and b.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
