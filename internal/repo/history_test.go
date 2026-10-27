package repo

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHistoryRanksByUseThenRecency(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	var h History
	h = h.Add("git status -s", t0)
	h = h.Add("git pull", t0.Add(time.Minute))
	h = h.Add("git pull", t0.Add(2*time.Minute))
	h = h.Add("git push", t0.Add(3*time.Minute))
	h = h.Add("  ", t0) // blank lines are not history

	if got := strings.Join(h.Ranked("git "), ","); got != "git pull,git push,git status -s" {
		t.Fatalf("most run first, ties by the most recent: %q", got)
	}
	if got := strings.Join(h.Ranked("git pu"), ","); got != "git pull,git push" {
		t.Fatalf("prefix: %q", got)
	}
	if got := h.Ranked("git pull"); len(got) != 0 {
		t.Fatalf("the value itself is no suggestion: %q", got)
	}
	if got := strings.Join(h.Recent(), ","); got != "git push,git pull,git status -s" {
		t.Fatalf("recent first: %q", got)
	}
}

func TestHistoryDropsTheLeastRecentPastTheCap(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	var h History
	for i := 0; i <= maxHistory; i++ {
		h = h.Add("git log -"+strings.Repeat("n", i+1), t0.Add(time.Duration(i)*time.Second))
	}
	if len(h.Entries) != maxHistory {
		t.Fatalf("%d entries", len(h.Entries))
	}
	for _, e := range h.Entries {
		if e.Line == "git log -n" {
			t.Fatal("the oldest line should go")
		}
	}
}

func TestHistorySaveAndLoad(t *testing.T) {
	path := HistoryFile(filepath.Join(t.TempDir(), "state"))
	if h := LoadHistory(path); len(h.Entries) != 0 {
		t.Fatal("a missing file is an empty history")
	}
	h := History{}.Add("git fetch", time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC))
	if err := h.Save(path); err != nil {
		t.Fatal(err)
	}
	got := LoadHistory(path)
	if len(got.Entries) != 1 || got.Entries[0].Line != "git fetch" || got.Entries[0].Count != 1 {
		t.Fatalf("%+v", got)
	}
}
