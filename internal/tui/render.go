package tui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mugenkunou/ws-tool/internal/style"
)

// RenderDashboard renders the full-screen TUI dashboard into w.
// It respects the given terminal size and noColor flag.
func RenderDashboard(w io.Writer, d DashboardData, size TermSize, noColor bool) {
	cols := int(size.Cols)
	rows := int(size.Rows)

	// Graceful degradation for tiny terminals.
	if cols < 60 || rows < 15 {
		renderCompact(w, d, noColor)
		return
	}

	nc := noColor
	hr := func() { fmt.Fprintln(w, style.Paint(style.Dim, strings.Repeat("─", cols), nc)) }

	// Title bar
	title := style.Boldf(nc, " ws") + style.Mutedf(nc, " — %s", d.Workspace)
	fmt.Fprintln(w, title)
	hr()

	// Top panels: HEALTH | DOTFILES | LOG
	renderTopPanels(w, d, nc)
	hr()

	// Violations panel
	renderViolations(w, d, cols, nc)
	hr()

	// Recent sessions
	renderSessions(w, d, cols, nc)
	hr()

	// Dotfiles
	renderDotfiles(w, d, cols, nc)
	hr()

	// Key bar
	renderKeyBar(w, nc)
}

func renderTopPanels(w io.Writer, d DashboardData, nc bool) {
	totalCrit := d.IgnoreCritical + d.SecretCritical + d.DotfileCritical
	totalWarn := d.IgnoreWarning + d.SecretWarning + d.DotfileWarning

	// HEALTH column
	healthLabel := style.Boldf(nc, "  HEALTH")
	healthCrit := fmt.Sprintf("  %s %d critical", style.IconCheck(nc), totalCrit)
	if totalCrit > 0 {
		healthCrit = fmt.Sprintf("  %s %s", style.IconCross(nc), style.Errorf(nc, "%d critical", totalCrit))
	}
	healthWarn := fmt.Sprintf("  %s %d warning", style.IconCheck(nc), totalWarn)
	if totalWarn > 0 {
		healthWarn = fmt.Sprintf("  %s %s", style.IconWarning(nc), style.Warningf(nc, "%d warning", totalWarn))
	}

	// DOTFILES column
	dotLabel := style.Boldf(nc, "DOTFILES")
	registered := len(d.DotfileIssues)
	okCount := 0
	brokenCount := 0
	for _, iss := range d.DotfileIssues {
		if iss.Status == "BROKEN" {
			brokenCount++
		} else {
			okCount++
		}
	}
	dotLine1 := fmt.Sprintf("%d registered", registered)
	dotLine2 := fmt.Sprintf("%d ok / %d broken", okCount, brokenCount)

	// LOG column
	logLabel := style.Boldf(nc, "LOG")
	logLine1 := style.Mutedf(nc, "no active session")
	logLine2 := ""
	if d.LogActive {
		logLine1 = fmt.Sprintf("%s recording", style.IconRecord(nc))
		logLine2 = fmt.Sprintf("tag: %s", d.ActiveTag)
	}

	fmt.Fprintf(w, "%-28s %-24s %s\n", healthLabel, dotLabel, logLabel)
	fmt.Fprintf(w, "%-28s %-24s %s\n", healthCrit, dotLine1, logLine1)
	fmt.Fprintf(w, "%-28s %-24s %s\n", healthWarn, dotLine2, logLine2)
}

func renderViolations(w io.Writer, d DashboardData, cols int, nc bool) {
	count := len(d.Violations)
	fmt.Fprintf(w, " %s Violations (%d)\n", style.Boldf(nc, ""), count)
	if count == 0 {
		fmt.Fprintf(w, "  %s\n", style.Successf(nc, "No violations"))
		return
	}
	maxShow := 10
	if count < maxShow {
		maxShow = count
	}
	for i := 0; i < maxShow; i++ {
		v := d.Violations[i]
		sev := style.Badge(v.Severity, nc)
		sizePart := ""
		if v.SizeMB > 0 {
			sizePart = fmt.Sprintf("  %d MB", v.SizeMB)
		}
		path := v.Path
		maxPath := cols - 40
		if maxPath > 0 && len(path) > maxPath {
			path = "…" + path[len(path)-maxPath:]
		}
		fmt.Fprintf(w, "  %-10s %-8s%s  %s\n", sev, v.Type, sizePart, path)
	}
	if count > maxShow {
		fmt.Fprintf(w, "  %s\n", style.Mutedf(nc, "(+ %d more)", count-maxShow))
	}
}

func renderSessions(w io.Writer, d DashboardData, cols int, nc bool) {
	fmt.Fprintf(w, " %s\n", style.Boldf(nc, " Recent sessions"))
	if len(d.Sessions) == 0 {
		fmt.Fprintf(w, "  %s\n", style.Mutedf(nc, "No sessions"))
		return
	}
	maxShow := 5
	if len(d.Sessions) < maxShow {
		maxShow = len(d.Sessions)
	}
	for i := 0; i < maxShow; i++ {
		s := d.Sessions[i]
		status := style.Mutedf(nc, s.StartedAt.Format("2006-01-02"))
		dur := formatDuration(s.DurationSec)
		if s.Active {
			status = style.Successf(nc, "%s active", style.IconRecord(nc))
			dur = formatDuration(int64(time.Since(s.StartedAt).Seconds()))
		}
		fmt.Fprintf(w, "  %-16s %-18s %-10s %d commands\n", s.Tag, status, dur, s.Commands)
	}
}

func renderDotfiles(w io.Writer, d DashboardData, cols int, nc bool) {
	fmt.Fprintf(w, " %s\n", style.Boldf(nc, " Dotfiles"))
	if len(d.DotfileIssues) == 0 {
		fmt.Fprintf(w, "  %s\n", style.Mutedf(nc, "None registered"))
		return
	}
	maxShow := 8
	if len(d.DotfileIssues) < maxShow {
		maxShow = len(d.DotfileIssues)
	}
	for i := 0; i < maxShow; i++ {
		iss := d.DotfileIssues[i]
		icon := style.IconCheck(nc)
		if iss.Status == "BROKEN" {
			icon = style.IconCross(nc)
		} else if iss.Status == "OVERWRITTEN" {
			icon = style.IconWarning(nc)
		}
		sys := iss.SystemPath
		if len(sys) > 30 {
			sys = "…" + sys[len(sys)-29:]
		}
		ws := iss.WorkspacePath
		if len(ws) > 25 {
			ws = "…" + ws[len(ws)-24:]
		}
		fmt.Fprintf(w, "  %s %-32s %s %s\n", icon, sys, style.IconArrow(nc), ws)
	}
	if len(d.DotfileIssues) > maxShow {
		fmt.Fprintf(w, "  %s\n", style.Mutedf(nc, "(+ %d more)", len(d.DotfileIssues)-maxShow))
	}
}

func renderKeyBar(w io.Writer, nc bool) {
	keys := []string{
		style.Boldf(nc, "q") + " quit",
		style.Boldf(nc, "r") + " refresh",
		style.Boldf(nc, "s") + " scan",
		style.Boldf(nc, "d") + " dotfiles",
		style.Boldf(nc, "l") + " logs",
		style.Boldf(nc, "?") + " help",
	}
	fmt.Fprintf(w, "  %s\n", strings.Join(keys, "   "))
}

func renderCompact(w io.Writer, d DashboardData, nc bool) {
	totalViolations := len(d.Violations)
	style.Header(w, style.Boldf(nc, "ws")+" "+style.Mutedf(nc, "dashboard"), nc)
	style.KV(w, "Workspace", style.Infof(nc, "%s", d.Workspace), nc)
	style.KV(w, "Violations", style.Boldf(nc, "%d", totalViolations), nc)
	style.KV(w, "Dotfiles", fmt.Sprintf("%d registered", len(d.DotfileIssues)), nc)
	style.KV(w, "Sessions", fmt.Sprintf("%d", len(d.Sessions)), nc)
	if d.LogActive {
		style.KV(w, "Active log", style.Successf(nc, "%s", d.ActiveTag), nc)
	}
	fmt.Fprintln(w, style.Divider(nc))
	fmt.Fprintln(w, "  Terminal too small for full TUI. Resize to at least 60x15.")
}

func formatDuration(sec int64) string {
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	if sec < 3600 {
		return fmt.Sprintf("%dm", sec/60)
	}
	return fmt.Sprintf("%dh %dm", sec/3600, (sec%3600)/60)
}
