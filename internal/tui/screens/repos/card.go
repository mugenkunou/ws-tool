package repos

import (
	"fmt"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Card implements nav.Summary: the fleet totals, and the repos that failed to
// scan, are out of sync or have hygiene findings.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	s := m.Summarize()
	c := nav.Card{Err: m.Err, Lines: []string{
		format.Plural(s.Repos, "repo") + dirtyNote(s.Dirty),
		fmt.Sprintf("↑ %d ahead · ↓ %d behind", s.Ahead, s.Behind),
	}}
	var failed, outOfSync, withFindings []string
	for _, e := range m.Entries {
		st := e.Status
		switch {
		case st.Error != "":
			failed = append(failed, st.Path)
		case st.Dirty || st.Ahead > 0 || st.Behind > 0:
			outOfSync = append(outOfSync, st.Path)
		}
		if e.Issues() > 0 {
			withFindings = append(withFindings, st.Path)
		}
	}
	if len(failed) > 0 {
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevError, Screen: nav.ScreenRepos,
			Text:   format.Plural(len(failed), "repo") + " failed to scan",
			Filter: "state=error"})
	}
	if len(outOfSync) > 0 {
		var parts []string
		if s.Dirty > 0 {
			parts = append(parts, fmt.Sprintf("%d dirty", s.Dirty))
		}
		if s.Behind > 0 {
			parts = append(parts, fmt.Sprintf("%d behind", s.Behind))
		}
		if s.Ahead > 0 {
			parts = append(parts, fmt.Sprintf("%d ahead", s.Ahead))
		}
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenRepos,
			Text:  fmt.Sprintf("%s out of sync (%s)", format.Plural(len(outOfSync), "repo"), strings.Join(parts, ", ")),
			Focus: SelectMsg{Path: outOfSync[0]}})
	}
	if s.Findings > 0 {
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevInfo, Screen: nav.ScreenRepos,
			Text:  fmt.Sprintf("%s in %s", format.Plural(s.Findings, "hygiene finding"), format.Plural(len(withFindings), "repo")),
			Focus: SelectMsg{Path: withFindings[0]}})
	}
	return nav.ScreenRepos, c, true
}

func dirtyNote(n int) string {
	if n == 0 {
		return ""
	}
	return " · " + theme.Warn.Render(fmt.Sprintf("%d dirty", n))
}
