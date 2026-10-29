package skills

import (
	"fmt"

	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Card implements nav.Summary: skills on both shelves, how many are installed
// in some tool, and the broken links and name clashes.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	installed, broken, shadowed := 0, 0, 0
	for _, e := range m.Entries {
		if len(e.Tools) > 0 {
			installed++
		}
		if e.Broken {
			broken++
		}
		if e.Shadowed {
			shadowed++
		}
	}
	c := nav.Card{Err: m.Err, Lines: []string{fmt.Sprintf("%s · %d installed", format.Plural(len(m.Entries), "skill"), installed)}}
	switch {
	case broken > 0:
		c.Lines = append(c.Lines, theme.Warn.Render(format.Plural(broken, "broken link")))
	case shadowed > 0:
		c.Lines = append(c.Lines, theme.Warn.Render(format.Plural(shadowed, "name clash")))
	case len(m.Entries) > 0:
		c.Lines = append(c.Lines, theme.OK.Render("all skills usable"))
	}
	if broken > 0 {
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenSkills,
			Text:   format.Plural(broken, "skill") + " link to a folder that is gone",
			Filter: "broken=true"})
	}
	if shadowed > 0 {
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenSkills,
			Text:   format.Plural(shadowed, "skill") + " of yours share a built-in's name; the built-in wins",
			Filter: "note=built-in"})
	}
	return nav.ScreenSkills, c, true
}
