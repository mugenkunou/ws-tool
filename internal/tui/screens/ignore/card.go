package ignore

import (
	"fmt"

	wsignore "github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Card implements nav.Summary: actionable violations, those in included
// paths, and stale mutes.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	n := m.Actionable()
	included := len(m.Violations) - n
	c := nav.Card{Err: m.Err, Lines: []string{theme.OK.Render("nothing bloating sync")}}
	if n > 0 {
		c.Lines = []string{theme.Warn.Render(format.Plural(n, "violation"))}
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenIgnore,
			Text: format.Plural(n, "sync hygiene violation") + " (bloat, depth, build artifacts)"})
	}
	if included > 0 {
		c.Lines = append(c.Lines, theme.Muted.Render(fmt.Sprintf("%d in included paths", included)))
	}
	if n := len(m.Stale); n > 0 {
		c.Lines = append(c.Lines, theme.Muted.Render(format.Plural(n, "stale mute")))
		text := format.Plural(n, "muted path") + " no longer exist"
		if n == 1 {
			text += "s"
		}
		// Opening the issue filters the Muted view to the stale rows, so p
		// (prune stale) sweeps exactly them.
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevInfo, Screen: nav.ScreenIgnore,
			Text: text, Filter: "type=" + wsignore.StaleType})
	}
	return nav.ScreenIgnore, c, true
}
