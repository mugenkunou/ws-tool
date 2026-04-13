// Package style provides zero-dependency ANSI color and formatting utilities
// for ws CLI output. All rendering respects the noColor flag and degrades
// gracefully to plain text when color is disabled (--no-color or NO_COLOR env).
//
// Design principles:
//   - Pure Go stdlib — no third-party color libraries.
//   - Single source of truth for all visual tokens (icons, colors, layout).
//   - Every public function accepts a noColor bool for testability.
//   - JSON output paths bypass this package entirely.
package style

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ──────────────────────────────────────────────────────────────────────────────
// ANSI escape sequences
// ──────────────────────────────────────────────────────────────────────────────

const esc = "\033["

// Reset
const Reset = esc + "0m"

// Styles
const (
	Bold      = esc + "1m"
	Dim       = esc + "2m"
	Italic    = esc + "3m"
	Underline = esc + "4m"
)

// Foreground colors — standard 16-color palette for maximum terminal compat.
const (
	FgBlack   = esc + "30m"
	FgRed     = esc + "31m"
	FgGreen   = esc + "32m"
	FgYellow  = esc + "33m"
	FgBlue    = esc + "34m"
	FgMagenta = esc + "35m"
	FgCyan    = esc + "36m"
	FgWhite   = esc + "37m"
)

// Bright foreground colors
const (
	FgBrightBlack   = esc + "90m"
	FgBrightRed     = esc + "91m"
	FgBrightGreen   = esc + "92m"
	FgBrightYellow  = esc + "93m"
	FgBrightBlue    = esc + "94m"
	FgBrightMagenta = esc + "95m"
	FgBrightCyan    = esc + "96m"
	FgBrightWhite   = esc + "97m"
)

// ──────────────────────────────────────────────────────────────────────────────
// Semantic color roles — the palette ws uses
// ──────────────────────────────────────────────────────────────────────────────

// Colors maps semantic roles to ANSI escape pairs.
// Every command uses these roles, never raw ANSI codes.
var Colors = struct {
	Success  string // green  — things that are OK, completed, healthy
	Error    string // red    — critical issues, failures, broken state
	Warning  string // yellow — warnings, attention needed, non-critical
	Info     string // cyan   — informational highlights, paths, values
	Muted    string // dim    — secondary info, hints, metadata
	Accent   string // bright blue — headers, commands, emphasis
	Bold     string // bold   — section titles, key labels
	Critical string // bold red — CRITICAL severity badge
}{
	Success:  FgGreen,
	Error:    FgRed,
	Warning:  FgYellow,
	Info:     FgCyan,
	Muted:    Dim,
	Accent:   FgBrightBlue,
	Bold:     Bold,
	Critical: Bold + FgRed,
}

// ──────────────────────────────────────────────────────────────────────────────
// Unicode icons — compact visual anchors
// ──────────────────────────────────────────────────────────────────────────────

// Icon constants used throughout ws output.
// Each has a plain-text fallback for --no-color mode.
type iconPair struct {
	Color string
	Plain string
}

var icons = struct {
	Check   iconPair // success / OK
	Cross   iconPair // error / failure
	Warning iconPair // warning / attention
	Dot     iconPair // bullet / list item
	Arrow   iconPair // direction / mapping
	Record  iconPair // recording active
	Info    iconPair // informational
	Lock    iconPair // secret / security
	Link    iconPair // symlink / dotfile
	Folder  iconPair // directory
	Git     iconPair // git repo
	Trash   iconPair // trash / delete
	Search  iconPair // search / grep
	Spark   iconPair // scratch / new
	Gear    iconPair // config / settings
	Shield  iconPair // scan / protection
	Wrench  iconPair // fix / repair
	Play    iconPair // start / begin
	Stop    iconPair // stop / end
	Eye     iconPair // context / watch
	Bell    iconPair // notification
	Clock   iconPair // time / duration
	Restore iconPair // restore / recover
}{
	Check:   iconPair{"✔", "[ok]"},
	Cross:   iconPair{"✖", "[err]"},
	Warning: iconPair{"▲", "[warn]"},
	Dot:     iconPair{"●", "(*)"},
	Arrow:   iconPair{"→", "->"},
	Record:  iconPair{"● ", "(rec)"},
	Info:    iconPair{"ℹ", "[i]"},
	Lock:    iconPair{"🔒", "[lock]"},
	Link:    iconPair{"🔗", "[link]"},
	Folder:  iconPair{"📁", "[dir]"},
	Git:     iconPair{"⎇ ", "[git]"},
	Trash:   iconPair{"🗑 ", "[trash]"},
	Search:  iconPair{"🔍", "[search]"},
	Spark:   iconPair{"⚡", "[new]"},
	Gear:    iconPair{"⚙ ", "[cfg]"},
	Shield:  iconPair{"🛡 ", "[scan]"},
	Wrench:  iconPair{"🔧", "[fix]"},
	Play:    iconPair{"▶", "[>]"},
	Stop:    iconPair{"■", "[stop]"},
	Eye:     iconPair{"👁 ", "[ctx]"},
	Bell:    iconPair{"🔔", "[bell]"},
	Clock:   iconPair{"⏱ ", "[time]"},
	Restore: iconPair{"♻ ", "[restore]"},
}

// Icon returns the appropriate icon string for the given icon pair.
func Icon(ip iconPair, noColor bool) string {
	if noColor {
		return ip.Plain
	}
	return ip.Color
}

// Convenience accessors
func IconCheck(noColor bool) string   { return Icon(icons.Check, noColor) }
func IconCross(noColor bool) string   { return Icon(icons.Cross, noColor) }
func IconWarning(noColor bool) string { return Icon(icons.Warning, noColor) }
func IconDot(noColor bool) string     { return Icon(icons.Dot, noColor) }
func IconArrow(noColor bool) string   { return Icon(icons.Arrow, noColor) }
func IconRecord(noColor bool) string  { return Icon(icons.Record, noColor) }
func IconInfo(noColor bool) string    { return Icon(icons.Info, noColor) }
func IconLock(noColor bool) string    { return Icon(icons.Lock, noColor) }
func IconLink(noColor bool) string    { return Icon(icons.Link, noColor) }
func IconFolder(noColor bool) string  { return Icon(icons.Folder, noColor) }
func IconGit(noColor bool) string     { return Icon(icons.Git, noColor) }
func IconTrash(noColor bool) string   { return Icon(icons.Trash, noColor) }
func IconSearch(noColor bool) string  { return Icon(icons.Search, noColor) }
func IconSpark(noColor bool) string   { return Icon(icons.Spark, noColor) }
func IconGear(noColor bool) string    { return Icon(icons.Gear, noColor) }
func IconShield(noColor bool) string  { return Icon(icons.Shield, noColor) }
func IconWrench(noColor bool) string  { return Icon(icons.Wrench, noColor) }
func IconPlay(noColor bool) string    { return Icon(icons.Play, noColor) }
func IconStop(noColor bool) string    { return Icon(icons.Stop, noColor) }
func IconEye(noColor bool) string     { return Icon(icons.Eye, noColor) }
func IconBell(noColor bool) string    { return Icon(icons.Bell, noColor) }
func IconClock(noColor bool) string   { return Icon(icons.Clock, noColor) }
func IconRestore(noColor bool) string { return Icon(icons.Restore, noColor) }

// ──────────────────────────────────────────────────────────────────────────────
// Core painting functions
// ──────────────────────────────────────────────────────────────────────────────

// Paint wraps text with an ANSI code and reset. Returns plain text if noColor.
func Paint(code, text string, noColor bool) string {
	if noColor || code == "" {
		return text
	}
	return code + text + Reset
}

// Successf formats and paints in success (green).
func Successf(noColor bool, format string, a ...any) string {
	return Paint(Colors.Success, fmt.Sprintf(format, a...), noColor)
}

// Errorf formats and paints in error (red).
func Errorf(noColor bool, format string, a ...any) string {
	return Paint(Colors.Error, fmt.Sprintf(format, a...), noColor)
}

// Warningf formats and paints in warning (yellow).
func Warningf(noColor bool, format string, a ...any) string {
	return Paint(Colors.Warning, fmt.Sprintf(format, a...), noColor)
}

// Infof formats and paints in info (cyan).
func Infof(noColor bool, format string, a ...any) string {
	return Paint(Colors.Info, fmt.Sprintf(format, a...), noColor)
}

// Mutedf formats and paints as muted (dim).
func Mutedf(noColor bool, format string, a ...any) string {
	return Paint(Colors.Muted, fmt.Sprintf(format, a...), noColor)
}

// Accentf formats and paints as accent (blue).
func Accentf(noColor bool, format string, a ...any) string {
	return Paint(Colors.Accent, fmt.Sprintf(format, a...), noColor)
}

// Boldf formats and paints as bold.
func Boldf(noColor bool, format string, a ...any) string {
	return Paint(Colors.Bold, fmt.Sprintf(format, a...), noColor)
}

// ──────────────────────────────────────────────────────────────────────────────
// Status badges — colored status labels
// ──────────────────────────────────────────────────────────────────────────────

// Badge returns a colored status badge string.
func Badge(label string, noColor bool) string {
	upper := strings.ToUpper(label)
	switch upper {
	case "OK", "CLEAN", "HEALTHY", "SYNCED", "CONFIGURED", "ENABLED", "ACTIVE":
		return Paint(Colors.Success, upper, noColor)
	case "CRITICAL", "BROKEN", "ERROR", "FAILED":
		return Paint(Colors.Critical, upper, noColor)
	case "WARNING", "OVERWRITTEN", "DIRTY", "ATTENTION", "IGNORED", "DISABLED", "SKIPPED":
		return Paint(Colors.Warning, upper, noColor)
	default:
		return Paint(Colors.Info, upper, noColor)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Severity counters — "0 critical · 2 warning" with color
// ──────────────────────────────────────────────────────────────────────────────

// Counts renders "N critical · M warning" with appropriate coloring.
func Counts(critical, warning int, noColor bool) string {
	var parts []string

	cStr := fmt.Sprintf("%d critical", critical)
	if critical > 0 {
		cStr = Paint(Colors.Critical, cStr, noColor)
	} else {
		cStr = Paint(Colors.Muted, cStr, noColor)
	}
	parts = append(parts, cStr)

	wStr := fmt.Sprintf("%d warning", warning)
	if warning > 0 {
		wStr = Paint(Colors.Warning, wStr, noColor)
	} else {
		wStr = Paint(Colors.Muted, wStr, noColor)
	}
	parts = append(parts, wStr)

	return strings.Join(parts, " · ")
}

// ──────────────────────────────────────────────────────────────────────────────
// Section headers — visual structure
// ──────────────────────────────────────────────────────────────────────────────

const dividerWidth = 56

// Divider returns a horizontal line of box-drawing characters.
func Divider(noColor bool) string {
	line := strings.Repeat("─", dividerWidth)
	return Paint(Colors.Muted, line, noColor)
}

// Header prints a styled section header with icon.
func Header(w io.Writer, title string, noColor bool) {
	fmt.Fprintln(w, Boldf(noColor, "%s", title))
	fmt.Fprintln(w, Divider(noColor))
}

// ──────────────────────────────────────────────────────────────────────────────
// Key-value rows — aligned label : value pairs
// ──────────────────────────────────────────────────────────────────────────────

// KV writes a left-aligned key-value pair with consistent column width.
func KV(w io.Writer, key string, value string, noColor bool) {
	label := Paint(Colors.Bold, fmt.Sprintf("  %-18s", key), noColor)
	fmt.Fprintf(w, "%s %s\n", label, value)
}

// HumanBytes returns a compact, human-readable string for bytes.
func HumanBytes(bytes int64) string {
	if bytes < 0 {
		return "-" + HumanBytes(-bytes)
	}

	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
		tb = 1024 * gb
	)

	switch {
	case bytes >= tb:
		return fmt.Sprintf("%.1f TB", float64(bytes)/float64(tb))
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Table rendering — simple column-aligned table
// ──────────────────────────────────────────────────────────────────────────────

// TableRow represents one row of a table.
type TableRow struct {
	Columns []string
}

// RenderTable writes rows with space-padded columns.
// widths[i] is the minimum width for column i.
func RenderTable(w io.Writer, rows []TableRow, widths []int) {
	for _, row := range rows {
		var sb strings.Builder
		for i, col := range row.Columns {
			if i < len(widths) {
				fmt.Fprintf(&sb, "%-*s", widths[i], col)
			} else {
				sb.WriteString(col)
			}
			if i < len(row.Columns)-1 {
				sb.WriteString("  ")
			}
		}
		fmt.Fprintln(w, strings.TrimRight(sb.String(), " "))
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Result line helpers — "✔ Moved ...", "✖ Failed ..."
// ──────────────────────────────────────────────────────────────────────────────

// ResultSuccess returns a green check + message line.
func ResultSuccess(noColor bool, format string, a ...any) string {
	icon := IconCheck(noColor)
	msg := fmt.Sprintf(format, a...)
	return Paint(Colors.Success, icon, noColor) + " " + msg
}

// ResultError returns a red cross + message line.
func ResultError(noColor bool, format string, a ...any) string {
	icon := IconCross(noColor)
	msg := fmt.Sprintf(format, a...)
	return Paint(Colors.Error, icon, noColor) + " " + msg
}

// ResultWarning returns a yellow triangle + message line.
func ResultWarning(noColor bool, format string, a ...any) string {
	icon := IconWarning(noColor)
	msg := fmt.Sprintf(format, a...)
	return Paint(Colors.Warning, icon, noColor) + " " + msg
}

// ResultInfo returns a cyan info + message line.
func ResultInfo(noColor bool, format string, a ...any) string {
	icon := IconInfo(noColor)
	msg := fmt.Sprintf(format, a...)
	return Paint(Colors.Info, icon, noColor) + " " + msg
}

// ──────────────────────────────────────────────────────────────────────────────
// Tree rendering helpers
// ──────────────────────────────────────────────────────────────────────────────

const (
	TreePipe   = "│   "
	TreeBranch = "├── "
	TreeCorner = "└── "
	TreeSpace  = "    "
)

// TreePrefix returns a dimmed tree prefix string.
func TreePrefix(prefix string, noColor bool) string {
	return Paint(Colors.Muted, prefix, noColor)
}

// ──────────────────────────────────────────────────────────────────────────────
// Environment detection
// ──────────────────────────────────────────────────────────────────────────────

// ShouldDisableColor returns true if color should be suppressed:
//   - NO_COLOR env is set (https://no-color.org)
//   - TERM=dumb
//   - stdout is not a terminal
func ShouldDisableColor() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return true
	}
	if os.Getenv("TERM") == "dumb" {
		return true
	}
	return false
}
