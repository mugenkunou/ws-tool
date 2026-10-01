// Package format renders values compactly for table cells.
package format

import (
	"fmt"
	"strings"
	"time"
)

// Age renders a duration as its largest whole unit: "45s", "12m", "5h", "3d".
func Age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// Duration renders seconds as "42s", "3m 05s" or "1h 02m".
func Duration(seconds int64) string {
	switch {
	case seconds < 60:
		return fmt.Sprintf("%ds", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%dm %02ds", seconds/60, seconds%60)
	default:
		return fmt.Sprintf("%dh %02dm", seconds/3600, (seconds%3600)/60)
	}
}

// Plural renders a count with its noun: "1 issue", "2 issues",
// "2 entries". The last word is pluralized ("2 cron jobs").
func Plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %s", n, pluralize(noun))
}

func pluralize(noun string) string {
	if l := len(noun); l >= 2 && noun[l-1] == 'y' && !strings.ContainsRune("aeiou", rune(noun[l-2])) {
		return noun[:l-1] + "ies"
	}
	return noun + "s"
}
