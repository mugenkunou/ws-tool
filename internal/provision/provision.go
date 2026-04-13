// Package provision tracks external side-effects created by ws commands.
//
// Every RW command that creates or modifies files outside <workspace>/ws/
// records an entry in the provisioning ledger (ws/provisions.json). This
// enables ws reset to reverse all side-effects cleanly.
//
// Entry types:
//
//	file         — a file created by ws (undo: delete)
//	dir          — a directory created by ws (undo: delete if empty/ws-owned)
//	symlink      — a symlink at a system path (undo: remove symlink, restore original)
//	config_line  — a line appended to a config file (undo: remove line)
//	git_exclude  — a line added to .git/info/exclude (undo: remove line)
package provision

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const CurrentSchema = 1

// Type classifies what a provision entry represents.
type Type string

const (
	TypeFile       Type = "file"
	TypeDir        Type = "dir"
	TypeSymlink    Type = "symlink"
	TypeConfigLine Type = "config_line"
	TypeGitExclude Type = "git_exclude"
)

// Entry records a single external side-effect.
type Entry struct {
	Type    Type   `json:"type"`
	Path    string `json:"path"`             // absolute path of the affected file/dir/symlink
	Target  string `json:"target,omitempty"` // symlink target (workspace-relative) for TypeSymlink
	Line    string `json:"line,omitempty"`   // exact line content for TypeConfigLine / TypeGitExclude
	Command string `json:"command"`          // ws command that created this entry
	Time    string `json:"ts"`               // RFC 3339 timestamp
}

// Ledger is the on-disk representation of ws/provisions.json.
type Ledger struct {
	Schema  int     `json:"provisions_schema"`
	Entries []Entry `json:"entries"`
}

// LedgerPath returns the default provisions.json path for a workspace.
func LedgerPath(workspacePath string) string {
	return filepath.Join(workspacePath, "ws", "provisions.json")
}

// Load reads the ledger from disk. Returns an empty ledger if the file
// does not exist.
func Load(path string) (Ledger, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Ledger{Schema: CurrentSchema, Entries: []Entry{}}, nil
		}
		return Ledger{}, err
	}

	var l Ledger
	if err := json.Unmarshal(content, &l); err != nil {
		return Ledger{}, err
	}

	if l.Entries == nil {
		l.Entries = []Entry{}
	}
	return l, nil
}

// Save writes the ledger to disk, creating parent directories as needed.
func Save(path string, l Ledger) error {
	if l.Schema == 0 {
		l.Schema = CurrentSchema
	}
	if l.Entries == nil {
		l.Entries = []Entry{}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	encoded, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}

	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o644)
}

// Record appends an entry to the ledger on disk. Duplicate entries
// (same type + path) are replaced rather than duplicated.
func Record(ledgerPath string, e Entry) error {
	l, err := Load(ledgerPath)
	if err != nil {
		return err
	}

	if e.Time == "" {
		e.Time = time.Now().UTC().Format(time.RFC3339)
	}

	// Replace existing entry with same type+path, or append.
	replaced := false
	for i, existing := range l.Entries {
		if existing.Type == e.Type && existing.Path == e.Path {
			l.Entries[i] = e
			replaced = true
			break
		}
	}
	if !replaced {
		l.Entries = append(l.Entries, e)
	}

	return Save(ledgerPath, l)
}

// Remove deletes entries matching type+path from the ledger.
func Remove(ledgerPath string, typ Type, path string) error {
	l, err := Load(ledgerPath)
	if err != nil {
		return err
	}

	filtered := make([]Entry, 0, len(l.Entries))
	for _, e := range l.Entries {
		if e.Type == typ && e.Path == path {
			continue
		}
		filtered = append(filtered, e)
	}

	l.Entries = filtered
	return Save(ledgerPath, l)
}

// RecordAll appends multiple entries atomically (single read-write cycle).
func RecordAll(ledgerPath string, entries []Entry) error {
	l, err := Load(ledgerPath)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, e := range entries {
		if e.Time == "" {
			e.Time = now
		}

		replaced := false
		for i, existing := range l.Entries {
			if existing.Type == e.Type && existing.Path == e.Path {
				l.Entries[i] = e
				replaced = true
				break
			}
		}
		if !replaced {
			l.Entries = append(l.Entries, e)
		}
	}

	return Save(ledgerPath, l)
}

// Reversed returns entries in reverse order (LIFO) for undo operations.
func Reversed(entries []Entry) []Entry {
	n := len(entries)
	rev := make([]Entry, n)
	for i, e := range entries {
		rev[n-1-i] = e
	}
	return rev
}
