package app

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/cmdbar"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
)

// commander is the screen's `.` menu commands, if it has any.
func commander(c nav.Component) []nav.CommandInfo {
	if cm, ok := c.(nav.Commander); ok {
		return cm.Commands()
	}
	return nil
}

// screenNames are what `:` takes to switch to a screen: its noun and its title,
// lower case. The Dashboard has only its title.
func screenNames(s nav.Screen) []string {
	names := []string{strings.ToLower(s.Title())}
	if noun := s.Noun(); noun != "" {
		names = append([]string{noun}, names...)
	}
	return names
}

// candidates are what the command bar completes (spec §6.6): the screens,
// help and quit. Verbs are not typed here: the `.` menu offers them.
func (m Model) candidates() []string {
	var out []string
	for _, s := range nav.Screens() {
		out = append(out, screenNames(s)...)
	}
	return append(out, "help", "q", "quit")
}

// openBar opens the command bar with prefill typed in.
func (m Model) openBar(prefill string) Model {
	if m.screens == nil {
		return m
	}
	bar := cmdbar.New(prefill, m.candidates()).SetWidth(m.width)
	m.bar = &bar
	return m
}

// resolveScreen finds the screen a word names: an exact noun or title, else the
// only screen one of whose names starts with it. When several do, they are
// returned so the notice can list them.
func resolveScreen(word string) (nav.Screen, []string, bool) {
	word = strings.ToLower(word)
	if word == "workspace" { // the Settings screen's old name keeps working
		return nav.ScreenSettings, nil, true
	}
	var prefixed []nav.Screen
	for _, s := range nav.Screens() {
		for _, n := range screenNames(s) {
			if n == word {
				return s, nil, true
			}
		}
		if slices.ContainsFunc(screenNames(s), func(n string) bool { return strings.HasPrefix(n, word) }) {
			prefixed = append(prefixed, s)
		}
	}
	switch len(prefixed) {
	case 0:
		return 0, nil, false
	case 1:
		return prefixed[0], nil, true
	}
	var names []string
	for _, s := range prefixed {
		names = append(names, screenNames(s)[0])
	}
	return 0, names, false
}

// unknown is the notice for a line that is no command, with the nearest one.
func (m Model) unknown(line string) (Model, tea.Cmd) {
	text := "Unknown command: " + line
	if near := cmdbar.Closest(line, m.candidates()); near != "" {
		text += " — did you mean " + near + "?"
	}
	return m.update(nav.NoticeMsg{Text: text, Level: nav.LevelWarn})
}

// runLine runs a line typed in the command bar (spec §6.6), as k9s does:
//
//	q, quit      quit
//	help         full help
//	<screen>     switch (a unique prefix of the noun or title works)
//
// Verbs are not typed here: the `.` menu lists the active screen's.
func (m Model) runLine(line string) (Model, tea.Cmd) {
	w := strings.Fields(line)
	if len(w) == 0 {
		return m.resize()
	}
	switch strings.Join(w, " ") {
	case "q", "quit":
		return m, tea.Quit
	case "help":
		m.fullHelp = true
		return m.resize()
	}
	if m.screens == nil || len(w) > 1 {
		return m.unknown(line)
	}
	s, ambiguous, ok := resolveScreen(w[0])
	if !ok {
		if len(ambiguous) > 0 {
			return m.update(nav.NoticeMsg{Text: fmt.Sprintf("%q matches %s", w[0], strings.Join(ambiguous, ", ")), Level: nav.LevelWarn})
		}
		return m.unknown(line)
	}
	m.active = s
	return m.resize()
}
