package format

import (
	"testing"
	"time"
)

func TestPlural(t *testing.T) {
	for _, c := range []struct {
		n          int
		noun, want string
	}{
		{0, "repo", "0 repos"}, {1, "repo", "1 repo"}, {2, "repo", "2 repos"},
		{42, "entry", "42 entries"}, {1, "entry", "1 entry"}, {3, "directory", "3 directories"},
		{2, "key", "2 keys"}, {2, "cron job", "2 cron jobs"},
	} {
		if got := Plural(c.n, c.noun); got != c.want {
			t.Errorf("Plural(%d, %q) = %q, want %q", c.n, c.noun, got, c.want)
		}
	}
}

func TestAge(t *testing.T) {
	for d, want := range map[time.Duration]string{
		9 * time.Second:     "9s",
		12 * time.Minute:    "12m",
		47 * time.Hour:      "47h",
		30 * 24 * time.Hour: "30d",
	} {
		if got := Age(d); got != want {
			t.Errorf("Age(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestDuration(t *testing.T) {
	for s, want := range map[int64]string{42: "42s", 185: "3m 05s", 3720: "1h 02m"} {
		if got := Duration(s); got != want {
			t.Errorf("Duration(%d) = %q, want %q", s, got, want)
		}
	}
}
