package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/secret"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/trash"
)

// ── violation printers ──
//
// Each printer accepts an "indent" flag:
//   indent=true  → used by aggregate views (2-space indent, section header)
//   indent=false → used by ws ignore scan / ws secret scan / etc. (no indent, no header)

func printIgnoreViolations(w io.Writer, violations []ignore.Violation, nc bool, indent bool) {
	if len(violations) == 0 {
		return
	}
	pfx := ""
	if indent {
		fmt.Fprintln(w)
		fmt.Fprintln(w, style.Boldf(nc, "  [Ignore]"))
		pfx = "  "
	}
	for _, v := range violations {
		sev := severityLabel(v.Severity, nc)
		detail := v.Path
		if v.Message != "" {
			detail = fmt.Sprintf("%s  %s", v.Path, style.Mutedf(nc, "(%s)", v.Message))
		}
		fmt.Fprintf(w, "%s%s  %-14s %s\n", pfx, sev, v.Type, detail)
	}
}

// printIgnoreViolationsSplit prints violations in two sections:
// actionable violations first, then a safe harbor summary. When expandHarbors
// is true, safe harbor items are listed individually; otherwise they are
// collapsed to a single summary line.
func printIgnoreViolationsSplit(w io.Writer, violations []ignore.Violation, nc bool, expandHarbors bool) {
	var actionable, harbors []ignore.Violation
	var harborBytes int64
	for _, v := range violations {
		if v.InSafeHarbor {
			harbors = append(harbors, v)
			harborBytes += v.SizeBytes
		} else {
			actionable = append(actionable, v)
		}
	}

	if len(actionable) > 0 {
		fmt.Fprintf(w, "%s (%d)\n", style.Boldf(nc, "Violations"), len(actionable))
		for _, v := range actionable {
			sev := severityLabel(v.Severity, nc)
			detail := v.Path
			if v.Message != "" {
				detail = fmt.Sprintf("%s  %s", v.Path, style.Mutedf(nc, "(%s)", v.Message))
			}
			fmt.Fprintf(w, "  %s  %-14s %s\n", sev, v.Type, detail)
		}
	}

	if len(harbors) > 0 {
		if len(actionable) > 0 {
			fmt.Fprintln(w)
		}
		sizePart := ""
		if harborBytes > 0 {
			sizePart = fmt.Sprintf(", %s", style.HumanBytes(harborBytes))
		}
		fmt.Fprintf(w, "%s (%d items%s)\n",
			style.Mutedf(nc, "Safe harbors"), len(harbors), sizePart)
		if expandHarbors {
			for _, v := range harbors {
				detail := v.Path
				if v.Message != "" {
					detail = fmt.Sprintf("%s  %s", v.Path, style.Mutedf(nc, "(%s)", v.Message))
				}
				fmt.Fprintf(w, "  %s  %-14s %s\n", severityLabel(v.Severity, nc), v.Type, detail)
			}
		} else {
			fmt.Fprintf(w, "  %s\n", style.Mutedf(nc, "Use --expand-harbors to see details."))
		}
	}
}

func printSecretViolations(w io.Writer, violations []secret.Violation, nc bool, indent bool) {
	if len(violations) == 0 {
		return
	}
	pfx := ""
	if indent {
		fmt.Fprintln(w)
		fmt.Fprintln(w, style.Boldf(nc, "  [Secret]"))
		pfx = "  "
	}
	for _, v := range violations {
		sev := severityLabel(v.Severity, nc)
		detail := v.Path
		if v.Line > 0 {
			detail = fmt.Sprintf("%s:%d: %s", v.Path, v.Line, strings.TrimSpace(v.Snippet))
		}
		fmt.Fprintf(w, "%s%s  %s\n", pfx, sev, detail)
	}
}

func printDotfileIssues(w io.Writer, issues []dotfile.Issue, nc bool, indent bool) {
	if len(issues) == 0 {
		return
	}
	pfx := ""
	if indent {
		fmt.Fprintln(w)
		fmt.Fprintln(w, style.Boldf(nc, "  [Dotfiles]"))
		pfx = "  "
	}
	for _, d := range issues {
		sev := "WARNING"
		if d.Status == dotfile.StatusBroken {
			sev = "CRITICAL"
		}
		label := severityLabel(sev, nc)
		fmt.Fprintf(w, "%s%s  %-12s %s  →  %s  [%s]\n", pfx, label, d.Status, d.SystemPath, d.WorkspacePath, d.Message)
	}
}

func printTrashWarnings(w io.Writer, status trash.Status, scanResult trash.ScanResult, nc bool, indent bool) {
	hasSetupWarnings := status.WarningCount() > 0
	hasSizeWarning := scanResult.OverLimit
	if !hasSetupWarnings && !hasSizeWarning {
		return
	}
	pfx := ""
	if indent {
		fmt.Fprintln(w)
		fmt.Fprintln(w, style.Boldf(nc, "  [Trash]"))
		pfx = "  "
	}
	warn := severityLabel("WARNING", nc)
	if !status.ShellRMConfigured {
		fmt.Fprintf(w, "%s%s  machine-setup  shell-rm integration not configured\n", pfx, warn)
	}
	if !status.VSCodeConfigured {
		fmt.Fprintf(w, "%s%s  machine-setup  vscode-delete integration not configured\n", pfx, warn)
	}
	if !status.FileExplorerConfigured {
		fmt.Fprintf(w, "%s%s  machine-setup  file-explorer integration not configured\n", pfx, warn)
	}
	if hasSizeWarning {
		fmt.Fprintf(w, "%s%s  trash-size     %s exceeds threshold %s (%d files)\n", pfx, warn, style.HumanBytes(scanResult.SizeBytes), style.HumanBytes(int64(scanResult.WarnSizeMB)*1024*1024), scanResult.FileCount)
	}
}

func severityLabel(sev string, nc bool) string {
	switch sev {
	case "CRITICAL":
		return style.Errorf(nc, "%-8s", "CRITICAL")
	case "INFO":
		return style.Mutedf(nc, "%-8s", "INFO")
	default:
		return style.Warningf(nc, "%-8s", "WARNING")
	}
}
