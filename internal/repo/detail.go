package repo

import (
	"fmt"
	"strings"
)

// Detail is one repo in full, as `ws repo show` prints it: the status fields,
// then every hygiene finding.
func Detail(e Entry) string {
	var b strings.Builder
	row := func(label, value string) { fmt.Fprintf(&b, "%-10s%s\n", label, value) }

	st := e.Status
	row("Path", st.Path)
	branch := st.Branch
	if st.Detached {
		branch = "detached HEAD"
	}
	row("Branch", branch)
	row("State", e.State())
	upstream := "none"
	if st.HasUpstream {
		upstream = fmt.Sprintf("↑%d ↓%d", st.Ahead, st.Behind)
	}
	row("Upstream", upstream)
	if st.Error != "" {
		row("Error", st.Error)
	}
	if len(e.Findings) == 0 {
		b.WriteString("Hygiene  none\n")
		return b.String()
	}
	b.WriteString("Hygiene\n")
	width := 0
	for _, f := range e.Findings {
		width = max(width, len(f.Check))
	}
	for _, f := range e.Findings {
		fmt.Fprintf(&b, "  %-4s  %-*s  %s\n", f.Severity, width, f.Check, f.Detail)
	}
	return b.String()
}
