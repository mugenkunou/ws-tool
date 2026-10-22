package trash

import (
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mugenkunou/ws-tool/internal/fsutil"
	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// WarnSizeBytes is the trash size above which `trash status` warns. It is fixed
// at 1 GB.
const WarnSizeBytes int64 = 1 << 30

// Resolve finds the trash items that args name (spec §5 trash, "Item
// arguments"). An argument is an item's name (the name column), or the path it
// was deleted from: one that contains / or starts with ~ is a path. A path is
// resolved by the path rules (absolute, ~, or relative to the current directory)
// and matches the most recently deleted item with that original path. An
// argument that matches nothing is an error that names every such argument, and
// nothing runs. A repeated item counts once.
func Resolve(args []string) ([]Item, error) {
	items, err := Items()
	if err != nil {
		return nil, err
	}
	var out []Item
	var missing []string
	for _, arg := range args {
		var found *Item
		if strings.ContainsRune(arg, '/') || strings.HasPrefix(arg, "~") {
			abs, err := wsconfig.ExpandUserPath(arg)
			if err != nil {
				return nil, fmt.Errorf("invalid path %q: %w", arg, err)
			}
			// Items are newest first, so the first match is the most recent.
			for i := range items {
				if filepath.Clean(items[i].OriginalPath) == abs {
					found = &items[i]
					break
				}
			}
		} else {
			for i := range items {
				if items[i].Name == arg {
					found = &items[i]
					break
				}
			}
		}
		if found == nil {
			missing = append(missing, arg)
			continue
		}
		dup := false
		for _, o := range out {
			dup = dup || o.Name == found.Name
		}
		if !dup {
			out = append(out, *found)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("no such trash item: %s", strings.Join(missing, ", "))
	}
	return out, nil
}

// Skip is a restore target that cannot be restored, and why.
type Skip struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// RestorePlan builds one action per item that can be put back, and a Skip for
// each one that cannot (spec §5 trash restore). ws never overwrites: if anything
// exists at the original path, a dangling symlink included, the item is skipped.
// An item with no .trashinfo or no Path= cannot be restored. When two targets
// share an original path, the first one named is restored and the other skipped.
// Missing parent directories are created with mode 0755, and the action names
// them.
func RestorePlan(items []Item) (actions []plan.Action, skipped []Skip) {
	root, err := Root()
	if err != nil {
		for _, it := range items {
			skipped = append(skipped, Skip{Name: it.Name, Path: it.OriginalPath, Reason: err.Error()})
		}
		return nil, skipped
	}
	claimed := map[string]string{} // original path -> the item that will restore there
	for _, it := range items {
		it := it
		switch {
		case it.OriginalPath == "":
			skipped = append(skipped, Skip{Name: it.Name, Reason: "no original path recorded"})
			continue
		}
		dest := filepath.Clean(it.OriginalPath)
		if _, err := os.Lstat(dest); err == nil {
			skipped = append(skipped, Skip{Name: it.Name, Path: dest, Reason: dest + " already exists"})
			continue
		}
		if first, taken := claimed[dest]; taken {
			skipped = append(skipped, Skip{Name: it.Name, Path: dest, Reason: dest + " is restored by " + first})
			continue
		}
		claimed[dest] = it.Name

		desc := fmt.Sprintf("Restore %s to %s", it.Name, dest)
		if _, err := os.Stat(filepath.Dir(dest)); os.IsNotExist(err) {
			desc += " (creates " + missingParent(dest) + ")"
		}
		actions = append(actions, plan.Action{
			ID:          "trash-restore-" + it.Name,
			Description: desc,
			Execute:     func() error { return restore(root, it, dest) },
		})
	}
	return actions, skipped
}

// missingParent is the topmost directory of dest's parents that does not exist.
func missingParent(dest string) string {
	dir := filepath.Dir(dest)
	top := dir
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return top
		}
		if _, err := os.Stat(parent); err == nil {
			return top
		}
		top, dir = parent, parent
	}
}

// restore moves files/<name> to dest, then removes its .trashinfo and its line in
// directorysizes. If the move fails, both stay.
func restore(root string, it Item, dest string) error {
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := fsutil.Move(filepath.Join(filesDir(root), it.Name), dest); err != nil {
		return err
	}
	forget(root, it.Name)
	return nil
}

// Delete deletes one item for good: files/<name>, its .trashinfo and its line in
// directorysizes. Unlike a delete anywhere else in ws, this cannot be undone.
func Delete(it Item) error {
	root, err := Root()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(filesDir(root), it.Name)); err != nil {
		return err
	}
	forget(root, it.Name)
	return nil
}

// forget removes an item's .trashinfo and its line in directorysizes.
func forget(root, name string) {
	_ = os.Remove(filepath.Join(infoDir(root), name+".trashinfo"))
	path := filepath.Join(root, "directorysizes")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var kept []string
	changed := false
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		// "<size> <mtime> <percent-encoded name>"
		fields := strings.SplitN(line, " ", 3)
		if len(fields) == 3 && unescapeName(fields[2]) == name {
			changed = true
			continue
		}
		if line != "" {
			kept = append(kept, line)
		}
	}
	if !changed {
		return
	}
	out := strings.Join(kept, "\n")
	if out != "" {
		out += "\n"
	}
	_ = os.WriteFile(path, []byte(out), 0o644)
}

func unescapeName(s string) string {
	if v, err := url.PathUnescape(s); err == nil {
		return v
	}
	return s
}

// ── empty ──────────────────────────────────────────────────────────

// Summary is the size of the trash.
type Summary struct {
	Root      string `json:"root"`
	Items     int    `json:"items"`
	Files     int    `json:"files"`
	SizeBytes int64  `json:"size_bytes"`
}

// Summarize counts the items, files and bytes in the trash.
func Summarize() (Summary, error) {
	root, err := Root()
	if err != nil {
		return Summary{}, err
	}
	s := Summary{Root: root}
	entries, err := os.ReadDir(filesDir(root))
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return Summary{}, err
	}
	s.Items = len(entries)
	for _, de := range entries {
		n, size := measure(filepath.Join(filesDir(root), de.Name()))
		s.Files += n
		s.SizeBytes += size
	}
	return s, nil
}

// Empty deletes every item for good: every entry in files/ and info/ (orphan
// .trashinfo files included) and the directorysizes file. It keeps files/ and
// info/ and does not touch other directories such as expunged/. An empty or
// missing trash is a successful no-op.
func Empty() error {
	root, err := Root()
	if err != nil {
		return err
	}
	for _, dir := range []string{filesDir(root), infoDir(root)} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		for _, e := range entries {
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	if err := os.Remove(filepath.Join(root, "directorysizes")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ── prune ──────────────────────────────────────────────────────────

// Entry is one trashed item that a prune matched.
type Entry struct {
	Name      string    `json:"name"`
	Dir       bool      `json:"dir"`
	Files     int       `json:"files"`
	SizeBytes int64     `json:"size_bytes"`
	DeletedAt time.Time `json:"deleted_at"`
}

// PruneResult describes what a prune matches. Matched lists the items deleted
// more than olderThan ago, oldest first. Kept counts the newer ones. Skipped
// counts items with no DeletionDate in their .trashinfo, which are never pruned.
type PruneResult struct {
	Matched   []Entry `json:"matched"`
	Kept      int     `json:"kept"`
	Skipped   int     `json:"skipped"`
	SizeBytes int64   `json:"size_bytes"`
}

// SelectOlderThan chooses the items whose DeletionDate is more than olderThan
// before now. The age is measured from the .trashinfo, not the file's mtime.
func SelectOlderThan(olderThan time.Duration, now time.Time) (PruneResult, []Item, error) {
	var res PruneResult
	items, err := Items()
	if err != nil {
		return res, nil, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-olderThan)
	var matched []Item
	for _, it := range items {
		switch {
		case !it.HasDate:
			res.Skipped++
		case !it.DeletedAt.Before(cutoff):
			res.Kept++
		default:
			matched = append(matched, it)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].DeletedAt.Before(matched[j].DeletedAt) })
	for _, it := range matched {
		res.Matched = append(res.Matched, Entry{Name: it.Name, Dir: it.Dir, Files: it.Files, SizeBytes: it.SizeBytes, DeletedAt: it.DeletedAt})
		res.SizeBytes += it.SizeBytes
	}
	return res, matched, nil
}

// measure returns the file count and total size under path (a file counts as 1).
func measure(path string) (int, int64) {
	var n int
	var size int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			size += info.Size()
		}
		n++
		return nil
	})
	return n, size
}
