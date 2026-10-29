package symlinks

import (
	"github.com/mugenkunou/ws-tool/internal/symlink"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Card implements nav.Summary. Broken, missing and drifted entries are the
// problems; untracked links and orphans are in the screen's list but are not
// alarms. The second load (Walked) adds the $HOME walk, which the Dashboard
// does not need, so it leaves the card alone.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	if m.Walked {
		return nav.ScreenSymlinks, nav.Card{}, false
	}
	c := nav.Card{Err: m.Err, Lines: []string{format.Plural(m.Registered(), "symlink") + " managed"}}
	var bad []string
	for _, e := range m.Entries {
		switch e.Status {
		case symlink.StatusBroken, symlink.StatusMissing, symlink.StatusDrifted:
			bad = append(bad, e.System)
		}
	}
	if n := len(bad); n > 0 {
		c.Lines = append(c.Lines, theme.Error.Render(format.Plural(n, "link")+" need fixing"))
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevError, Screen: nav.ScreenSymlinks,
			Text:  format.Plural(n, "symlink") + " broken, missing or drifted",
			Focus: SelectMsg{System: bad[0]}})
	} else if m.Registered() > 0 {
		c.Lines = append(c.Lines, theme.OK.Render("all links healthy"))
	}
	return nav.ScreenSymlinks, c, true
}
