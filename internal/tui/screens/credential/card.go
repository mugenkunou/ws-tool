package credential

import (
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Card implements nav.Summary: whether git's credential helper points at ws.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	c := nav.Card{Err: m.Err}
	if m.Err != nil {
		return nav.ScreenCredential, c, true
	}
	if m.Connected {
		c.Lines = []string{theme.OK.Render("helper connected")}
	} else {
		c.Lines = []string{theme.Warn.Render("helper not connected")}
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenCredential,
			Text: "The git credential helper is not set up: run :enable"})
	}
	return nav.ScreenCredential, c, true
}
