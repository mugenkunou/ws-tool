package app

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/cmdbar"
	"github.com/mugenkunou/ws-tool/internal/tui/handover"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
)

// configDone tags the handover of `config edit` and `config show`, so the app
// refreshes the screens when they return.
type configDone struct{}

// commander is the screen's `:` commands, if it has any.
func commander(c nav.Component) []nav.CommandInfo {
	if cm, ok := c.(nav.Commander); ok {
		return cm.Commands()
	}
	return nil
}

// words are a command's name split into words.
func words(name string) []string { return strings.Fields(name) }

// matchesCommand reports whether typed starts with one of commands' names, as
// whole words.
func matchesCommand(commands []nav.CommandInfo, typed []string) bool {
	return slices.ContainsFunc(commands, func(c nav.CommandInfo) bool {
		n := words(c.Name)
		return len(typed) >= len(n) && slices.Equal(typed[:len(n)], n)
	})
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

// hostedNouns are CLI nouns that have no screen: the screen that hosts them.
var hostedNouns = map[string]nav.Screen{"pass": nav.ScreenSecrets, "credential": nav.ScreenSecrets}

// candidates are what the command bar completes (spec §6.6): the active
// screen's commands, the screens, `<noun> <command>` for every screen, and the
// workspace commands.
func (m Model) candidates() []string {
	var out []string
	if cur := m.current(); cur != nil {
		for _, c := range commander(cur) {
			out = append(out, c.Name)
		}
	}
	for _, s := range nav.Screens() {
		out = append(out, screenNames(s)...)
	}
	for _, s := range nav.Screens() {
		if int(s) >= len(m.screens) {
			continue
		}
		for _, c := range commander(m.screens[s]) {
			out = append(out, s.Noun()+" "+c.Name)
			if noun, _, _ := strings.Cut(c.Name, " "); hostedNouns[noun] == s {
				out = append(out, c.Name) // `pass audit` works from any screen
			}
		}
	}
	return append(out, "config show", "config edit", "help", "q", "quit")
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

// runLine runs a line typed in the command bar (spec §6.6):
//
//	q, quit                          quit
//	help                             full help
//	config show, config edit         the workspace config
//	<screen>                         switch (a unique prefix of the noun or title works)
//	<command> [args]                 the active screen's command
//	<noun> <command> [args]          switch to the noun's screen, then run it there
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
	case "config show":
		return m, handover.Run(handover.Paged(handover.WS(m.env.Paths, "config", "show")), configDone{})
	case "config edit":
		return m, handover.Run(handover.WS(m.env.Paths, "config", "edit"), configDone{})
	}
	if m.screens == nil {
		return m.unknown(line)
	}

	// The active screen's own command wins over a screen of the same name.
	if cur := m.current(); matchesCommand(commander(cur), w) {
		m, resizeCmd := m.resize()
		m, cmd := m.updateCurrent(nav.CommandMsg{Words: w, Line: line}, false)
		return m, tea.Batch(resizeCmd, cmd)
	}

	// `pass` and `credential` have no screen of their own: Secrets hosts them,
	// and its commands are named `pass audit`, `credential status`.
	if s, ok := hostedNouns[w[0]]; ok && matchesCommand(commander(m.screens[s]), w) {
		m.active = s
		m, resizeCmd := m.resize()
		m, cmd := m.updateCurrent(nav.CommandMsg{Words: w, Line: line}, false)
		return m, tea.Batch(resizeCmd, cmd)
	}

	s, ambiguous, ok := resolveScreen(w[0])
	if !ok {
		if len(ambiguous) > 0 {
			return m.update(nav.NoticeMsg{Text: fmt.Sprintf("%q matches %s", w[0], strings.Join(ambiguous, ", ")), Level: nav.LevelWarn})
		}
		return m.unknown(line)
	}
	if len(w) == 1 {
		m.active = s
		return m.resize()
	}
	if !matchesCommand(commander(m.screens[s]), w[1:]) {
		return m.unknown(line)
	}
	m.active = s
	m, resizeCmd := m.resize()
	m, cmd := m.updateCurrent(nav.CommandMsg{Words: w[1:], Line: line}, false)
	return m, tea.Batch(resizeCmd, cmd)
}
