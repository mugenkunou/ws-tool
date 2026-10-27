package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// maxHistory is how many distinct command lines History keeps; the least
// recently run go first.
const maxHistory = 200

// HistoryEntry is one command line run in repos, how often, and when last.
type HistoryEntry struct {
	Line  string    `json:"line"`
	Count int       `json:"count"`
	Last  time.Time `json:"last"`
}

// History is the command lines the TUI ran in repos. It is machine-local
// (wsconfig.StateDir), so its suggestions follow what you run on this machine.
type History struct {
	Entries []HistoryEntry `json:"commands"`
}

// HistoryFile is where History lives: repo-history.json in the state dir.
func HistoryFile(stateDir string) string { return filepath.Join(stateDir, "repo-history.json") }

// LoadHistory reads the history at path. A missing or unreadable file is an
// empty history: suggestions are a convenience, never an error.
func LoadHistory(path string) History {
	var h History
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &h)
	}
	return h
}

// Save writes the history to path, creating its directory.
func (h History) Save(path string) error {
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Add records one run of line at now.
func (h History) Add(line string, now time.Time) History {
	line = strings.TrimSpace(line)
	if line == "" {
		return h
	}
	entries := slices.Clone(h.Entries)
	i := slices.IndexFunc(entries, func(e HistoryEntry) bool { return e.Line == line })
	if i < 0 {
		entries = append(entries, HistoryEntry{Line: line})
		i = len(entries) - 1
	}
	entries[i].Count++
	entries[i].Last = now
	if len(entries) > maxHistory {
		slices.SortStableFunc(entries, func(a, b HistoryEntry) int { return b.Last.Compare(a.Last) })
		entries = entries[:maxHistory]
	}
	return History{Entries: entries}
}

// Recent is every line, the most recently run first.
func (h History) Recent() []string {
	entries := slices.Clone(h.Entries)
	slices.SortStableFunc(entries, func(a, b HistoryEntry) int { return b.Last.Compare(a.Last) })
	return lines(entries)
}

// Ranked is the lines that start with prefix and are longer than it, the most
// run first, ties broken by the most recent.
func (h History) Ranked(prefix string) []string {
	var entries []HistoryEntry
	for _, e := range h.Entries {
		if strings.HasPrefix(e.Line, prefix) && e.Line != prefix {
			entries = append(entries, e)
		}
	}
	slices.SortStableFunc(entries, func(a, b HistoryEntry) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return b.Last.Compare(a.Last)
	})
	return lines(entries)
}

func lines(entries []HistoryEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Line
	}
	return out
}
