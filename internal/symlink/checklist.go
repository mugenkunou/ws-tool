package symlink

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// CheckRow is one line of the `ws symlink add` checklist: one item on its own
// (a private key with its .pub is the one pair).
type CheckRow struct {
	App     string
	Items   []Entry
	Secret  bool
	Sudo    bool
	Checked bool
}

// ChecklistRows lists the found items, one row each, so each can be picked on its
// own. Ordinary items are checked; secret and sudo items are not. A private key
// and its .pub are one row.
func ChecklistRows(found []Entry) []CheckRow {
	var rows []CheckRow
	var apps []string
	for _, e := range found {
		if !slices.Contains(apps, e.App) {
			apps = append(apps, e.App)
		}
	}
	// Private keys first, so a .pub can join its key's row.
	keyRow := map[string]int{}
	for _, e := range found {
		if e.Secret || e.Sudo {
			keyRow[e.System] = len(rows)
			rows = append(rows, CheckRow{App: e.App, Items: []Entry{e}, Secret: e.Secret, Sudo: e.Sudo})
		}
	}
	for _, e := range found {
		if e.Secret || e.Sudo {
			continue
		}
		if i, paired := keyRow[e.Group]; paired && e.Group != e.System {
			rows[i].Items = append(rows[i].Items, e)
			continue
		}
		rows = append(rows, CheckRow{App: e.App, Items: []Entry{e}, Checked: true})
	}
	// Order: by app (catalog order), ordinary rows before the app's own secret rows.
	order := func(r CheckRow) int { return slices.Index(apps, r.App) }
	slices.SortStableFunc(rows, func(a, b CheckRow) int {
		if d := order(a) - order(b); d != 0 {
			return d
		}
		return boolInt(a.Secret || a.Sudo) - boolInt(b.Secret || b.Sudo)
	})
	return rows
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// label is what a row lists: the item paths, with ~ for $HOME.
func (r CheckRow) Label(home string) string {
	var names []string
	for _, e := range r.Items {
		names = append(names, TildePath(e.System, home))
	}
	// A private key and its .pub read as one name.
	if len(names) == 2 && strings.HasSuffix(names[1], ".pub") && strings.TrimSuffix(names[1], ".pub") == names[0] {
		return names[0] + " + .pub"
	}
	if len(names) > 3 {
		return strings.Join(names[:2], ", ") + fmt.Sprintf(", … (%d items)", len(names))
	}
	return strings.Join(names, ", ")
}

func TildePath(p, home string) string {
	if home != "" {
		if rel, err := filepath.Rel(home, p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return p
}
