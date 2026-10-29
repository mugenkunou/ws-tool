package secrets

import (
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Card implements nav.Summary: the secret findings.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	c := nav.Card{Err: m.Err, Lines: []string{theme.OK.Render("no secrets found")}}
	var n, muted int
	for _, v := range m.Violations {
		if v.Muted {
			muted++
		} else {
			n++
		}
	}
	if n > 0 {
		c.Lines = []string{theme.Warn.Render(format.Plural(n, "finding"))}
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenSecrets,
			Text: format.Plural(n, "possible secret") + " in workspace files"})
	}
	if muted > 0 {
		c.Lines[0] += theme.Muted.Render(" · " + format.Plural(muted, "muted finding"))
	}
	return nav.ScreenSecrets, c, true
}
