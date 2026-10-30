package credential

import (
	"github.com/mugenkunou/ws-tool/internal/credential"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Card implements nav.Summary: whether git's credential helper points at ws.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	c := nav.Card{Err: m.Err}
	if m.Err != nil {
		return nav.ScreenCredential, c, true
	}
	switch {
	case m.Connected:
		c.Lines = []string{theme.OK.Render("helper connected")}
	case m.State == credential.HelperStale:
		c.Lines = []string{theme.Warn.Render("helper stale")}
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenCredential,
			Text: "The git credential helper is stale (" + m.Detail + "): press . then e to repair it"})
	case m.Helper != "":
		c.Lines = []string{theme.Warn.Render("helper is not ws")}
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenCredential,
			Text: "Git's credential helper is " + m.Helper + ", not ws: press . then e to switch to ws"})
	default:
		c.Lines = []string{theme.Warn.Render("helper not connected")}
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenCredential,
			Text: "The git credential helper is not set up: press . then e"})
	}
	return nav.ScreenCredential, c, true
}
