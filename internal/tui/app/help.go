package app

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

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
	general := rowsOf(g.NextTab, g.PrevTab, g.Navigate, g.Refresh, g.Command, g.Help, g.Quit)
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

// fullHelpView renders the columns side by side, each titled, clipped to the
// width. It is as tall as its tallest column plus the titles.
func (m Model) fullHelpView() string {
	cols := m.helpColumns()
	rendered := make([]string, 0, len(cols))
	for _, c := range cols {
		kw := 0
		for _, r := range c.rows {
			kw = max(kw, lipgloss.Width(r[0]))
		}
		lines := []string{theme.Title.Render(c.title)}
		for _, r := range c.rows {
			lines = append(lines, theme.Bold.Render(r[0])+strings.Repeat(" ", kw-lipgloss.Width(r[0])+2)+theme.Muted.Render(r[1]))
		}
		rendered = append(rendered, lipgloss.NewStyle().MarginRight(3).Render(strings.Join(lines, "\n")))
	}
	return lipgloss.NewStyle().MaxWidth(m.width).Render(lipgloss.JoinHorizontal(lipgloss.Top, rendered...))
}
