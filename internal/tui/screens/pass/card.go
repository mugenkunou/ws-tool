package pass

import (
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Card implements nav.Summary: the pass store's health, one line and one
// warning per missing piece. A store that cannot be read at all is Card.Err.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	c := nav.Card{Err: m.Err}
	if m.Err != nil {
		return nav.ScreenPass, c, true
	}
	h := m.Pass
	switch {
	case !h.GPGAvailable:
		c.Lines = []string{theme.Warn.Render("gpg not installed")}
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenPass, Text: "gpg is not installed; pass cannot encrypt"})
	case !h.Installed:
		c.Lines = []string{theme.Warn.Render("pass not installed")}
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenPass, Text: "pass is not installed"})
	case !h.Initialized:
		c.Lines = []string{theme.Warn.Render("store not initialized")}
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenPass, Text: "The pass store is not initialized: run :init"})
	default:
		c.Lines = []string{theme.OK.Render(format.Plural(h.EntryCount, "entry"))}
	}
	if h.GitBacked && !h.GitRemote {
		c.Lines[0] += theme.Muted.Render(" · no remote")
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenPass,
			Text: "The pass store has no git remote; secrets live only on this machine"})
	}
	return nav.ScreenPass, c, true
}
