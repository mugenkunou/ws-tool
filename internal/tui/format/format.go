// Package format renders values compactly for table cells.
package format

import (
	"fmt"
	"strings"
	"time"

	"github.com/mugenkunou/ws-tool/internal/style"
)

// Age renders a duration as its largest whole unit: "45s", "12m", "5h", "3d".
func Age(d time.Duration) string { return style.Age(d) }

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
