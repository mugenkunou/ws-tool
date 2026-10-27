package app

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// helpColumn is one titled column of the full help.
type helpColumn struct {
	title string
	rows  [][2]string // key, description
}

func rowOf(b key.Binding) [2]string { return [2]string{b.Help().Key, b.Help().Desc} }

func rowsOf(bs ...key.Binding) [][2]string {
	var out [][2]string
	for _, b := range bs {
		if b.Enabled() {
			out = append(out, rowOf(b))
		}
	}
	return out
}

// helpColumns are the four columns of the full help (spec §6.3): this screen's
// keys, the list keys, the general keys, and the `:` commands.
func (m Model) helpColumns() []helpColumn {
	g, l, t := m.keys, keys.List(), keys.Table()

	listKeys := rowsOf(t.LineUp, t.LineDown, t.GotoTop, t.GotoBottom, t.PageUp, t.PageDown)
	listKeys = append(listKeys, rowsOf(l.Bindings()...)...)
	general := rowsOf(g.NextTab, g.PrevTab, g.Refresh, g.Command, g.Help, g.Quit)
	general = append(general, [2]string{"esc", "back"})

	known := map[string]bool{}
	for _, cols := range [][][2]string{listKeys, general} {
		for _, r := range cols {
			known[r[0]] = true
		}
	}
	// The screen's own keys: its help minus the list and general keys above.
	var screen [][2]string
	if cur := m.current(); cur != nil {
		for _, group := range cur.KeyMap().FullHelp() {
			for _, b := range group {
				if r := rowOf(b); b.Enabled() && !known[r[0]] && r[0] != "" {
					known[r[0]] = true
					screen = append(screen, r)
				}
			}
		}
	}

	cols := []helpColumn{
		{title: "This screen", rows: screen},
		{title: "Lists", rows: listKeys},
		{title: "General", rows: general},
	}
	if cur := m.current(); cur != nil {
		var cmds [][2]string
		for _, c := range commander(cur) {
			name := ":" + c.Name
			if c.Args != "" {
				name += " " + c.Args
			}
			cmds = append(cmds, [2]string{name, c.Help})
		}
		if m.screens != nil {
			cmds = append(cmds, [2]string{":config show", "print the config"}, [2]string{":config edit", "edit ws.json"}, [2]string{":help  :q", "this help; quit"})
		}
		cols = append(cols, helpColumn{title: "Commands", rows: cmds})
	}
	return cols
}

// fullHelpView renders the columns side by side, each titled. When they do not
// fit the width, the widest columns have their descriptions shortened (with an
// ellipsis) down to a floor, and if that is not enough the columns that fit are
// kept in the order this screen, commands, lists, general, so the screen's own
// keys and commands are never the ones lost.
func (m Model) fullHelpView(width int) string {
	const gap, floor = 2, 16
	cols := m.helpColumns()
	natural := make([]int, len(cols))
	keyW := make([]int, len(cols))
	for i, c := range cols {
		natural[i] = lipgloss.Width(c.title)
		for _, r := range c.rows {
			keyW[i] = max(keyW[i], lipgloss.Width(r[0]))
		}
		for _, r := range c.rows {
			natural[i] = max(natural[i], keyW[i]+2+lipgloss.Width(r[1]))
		}
	}
	limit := slices.Clone(natural)
	sum := func() int {
		n := 0
		for _, w := range limit {
			n += w + gap
		}
		return n
	}
	for sum() > width {
		widest := 0
		for i := range limit {
			if limit[i] > limit[widest] {
				widest = i
			}
		}
		if limit[widest] <= floor {
			break
		}
		limit[widest]--
	}

	rendered := make([]string, len(cols))
	widths := make([]int, len(cols))
	for i, c := range cols {
		lines := []string{theme.Title.Render(c.title)}
		for _, r := range c.rows {
			line := theme.Bold.Render(r[0]) + strings.Repeat(" ", keyW[i]-lipgloss.Width(r[0])+2) + theme.Muted.Render(r[1])
			lines = append(lines, ansi.Truncate(line, limit[i], "…"))
		}
		rendered[i] = lipgloss.NewStyle().MarginRight(gap).Render(strings.Join(lines, "\n"))
		widths[i] = lipgloss.Width(rendered[i])
	}
	keep := make([]bool, len(cols))
	used := 0
	for _, i := range []int{0, 3, 1, 2} {
		if i >= len(cols) {
			continue
		}
		if used+widths[i] <= width || used == 0 {
			keep[i], used = true, used+widths[i]
		}
	}
	var out []string
	for i := range cols {
		if keep[i] {
			out = append(out, rendered[i])
		}
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(lipgloss.JoinHorizontal(lipgloss.Top, out...))
}

// helpPopup is the full help in a bordered box, sized to its content and the
// terminal; render centers it over the frame.
func (m Model) helpPopup() string {
	inner := max(m.width-8, 10)
	body := m.fullHelpView(inner) + "\n\n" + theme.Muted.Render("esc or ? to close")
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.Title.GetForeground()).
		Padding(0, 2).MaxWidth(m.width).MaxHeight(m.height).Render(body)
}

// overlay draws box centered over base, which is width×height.
func overlay(base, box string, width, height int) string {
	lines := strings.Split(base, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	x, y := max((width-bw)/2, 0), max((height-bh)/2, 0)
	for i, bl := range strings.Split(box, "\n") {
		if y+i >= len(lines) {
			break
		}
		l := lines[y+i]
		if w := lipgloss.Width(l); w < x {
			l += strings.Repeat(" ", x-w)
		}
		lines[y+i] = ansi.Truncate(l, x, "") + bl + ansi.TruncateLeft(l, x+bw, "")
	}
	return strings.Join(lines, "\n")
}
