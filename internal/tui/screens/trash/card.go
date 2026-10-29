package trash

import (
	"fmt"

	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/tui/format"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/theme"
)

// Card implements nav.Summary: size, soft-delete integrations, and a trash
// over its size limit.
func (m LoadedMsg) Card() (nav.Screen, nav.Card, bool) {
	st := m.Status
	off := st.WarningCount()
	c := nav.Card{Err: m.Err, Lines: []string{fmt.Sprintf("%s · %s", style.HumanBytes(st.SizeBytes), format.Plural(st.Files, "file"))}}
	if off == 0 {
		c.Lines = append(c.Lines, theme.OK.Render("soft-delete on"))
	} else {
		c.Lines = append(c.Lines, theme.Warn.Render(fmt.Sprintf("soft-delete off (%d of 3)", off)))
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenTrash,
			Text: fmt.Sprintf("Soft-delete is off for %d of 3 integrations", off)})
	}
	if st.OverLimit {
		c.Issues = append(c.Issues, nav.Issue{Sev: nav.SevWarn, Screen: nav.ScreenTrash,
			Text: fmt.Sprintf("Trash holds %s (over %s)", style.HumanBytes(st.SizeBytes), style.HumanBytes(st.WarnSizeBytes))})
	}
	return nav.ScreenTrash, c, true
}
