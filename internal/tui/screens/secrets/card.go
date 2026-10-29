package secrets

import (
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Card implements nav.Summary: secret findings and the pass store's health.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	h := m.Health.Pass
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
	if h.Initialized {
		c.Lines = append(c.Lines, "pass: "+format.Plural(h.EntryCount, "entry"))
	} else {
		c.Lines = append(c.Lines, theme.Muted.Render("pass not set up"))
	}
	if h.Initialized && h.GitBacked && !h.GitRemote {
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenSecrets,
			Text: "Pass store has no git remote — secrets live only on this machine"})
	}
	if h.Initialized && m.Health.CopyCommand != "" {
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevInfo, Screen: nav.ScreenSecrets, Advice: true,
			Text:    "Your pass store syncs only through its own git remote. A copy in the workspace would sync with MEGA too.",
			Prefill: m.Health.CopyCommand})
	}
	return nav.ScreenSecrets, c, true
}
