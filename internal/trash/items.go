package trash

import (
	"bufio"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// Item is one top-level entry in the trash: the record `ws trash ls` prints
// and the TUI trash screen lists.
type Item struct {
	Name         string    // entry name under files/ (what `ws trash rm` takes)
	Path         string    // absolute path of the entry under files/
	OriginalPath string    // where it was deleted from (.trashinfo Path, absolute), if recorded
	Dir          bool      // the entry is a directory
	Files        int       // files inside (a file counts as 1)
	SizeBytes    int64     // total size
	DeletedAt    time.Time // .trashinfo DeletionDate, else the entry's mtime
	// HasDate is true when DeletedAt came from the .trashinfo, not the mtime.
	HasDate bool
}

// Items lists every trashed entry, newest first. A missing trash directory has
// no items. An entry without a .trashinfo has no original path, and its date
// falls back to the file's mtime. A relative Path= is resolved against the
// directory that holds the trash directory, as the freedesktop spec says.
func Items() ([]Item, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filesDir(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Item, 0, len(entries))
	for _, de := range entries {
		path := filepath.Join(filesDir(root), de.Name())
		it := Item{Name: de.Name(), Path: path, Dir: de.IsDir()}
		info := readTrashInfo(filepath.Join(infoDir(root), de.Name()+".trashinfo"))
		it.OriginalPath, it.DeletedAt = info.path, info.deleted
		if it.OriginalPath != "" && !filepath.IsAbs(it.OriginalPath) {
			it.OriginalPath = filepath.Join(filepath.Dir(root), it.OriginalPath)
		}
		it.HasDate = !it.DeletedAt.IsZero()
		if it.DeletedAt.IsZero() {
			if fi, err := de.Info(); err == nil {
				it.DeletedAt = fi.ModTime()
			}
		}
		it.Files, it.SizeBytes = measure(path)
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DeletedAt.After(out[j].DeletedAt) })
	return out, nil
}

type trashInfo struct {
	path    string
	deleted time.Time
}

// readTrashInfo reads the original path and deletion date from a .trashinfo
// sidecar; missing or unreadable values stay zero.
func readTrashInfo(path string) trashInfo {
	var ti trashInfo
	f, err := os.Open(path)
	if err != nil {
		return ti
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(line, "Path="); ok {
			if p, err := url.PathUnescape(v); err == nil {
				ti.path = p
			} else {
				ti.path = v
			}
		}
		if v, ok := strings.CutPrefix(line, "DeletionDate="); ok {
			if t, err := time.ParseInLocation("2006-01-02T15:04:05", v, time.Local); err == nil {
				ti.deleted = t
			}
		}
	}
	return ti
}

// Schema describes trash items for the CLI and the TUI.
func Schema(v resource.View) resource.Schema[Item] {
	return resource.Schema[Item]{
		Name: "trash item",
		ID:   func(it Item) string { return it.Name },
		Fields: []resource.Field[Item]{
			{Key: "name", Title: "Name", Value: func(it Item) any { return it.Name },
				Text: func(it Item) string {
					if it.Dir {
						return it.Name + "/"
					}
					return it.Name
				}},
			{Key: "original_path", Title: "Deleted from", Value: func(it Item) any { return it.OriginalPath },
				Text: func(it Item) string { return v.Path(it.OriginalPath) }},
			{Key: "size_bytes", Title: "Size", Value: func(it Item) any { return it.SizeBytes },
				Text: func(it Item) string { return style.HumanBytes(it.SizeBytes) }},
			{Key: "files", Title: "Files", Value: func(it Item) any { return it.Files }},
			{Key: "deleted_at", Title: "Deleted", Value: func(it Item) any { return it.DeletedAt },
				Text: func(it Item) string {
					if it.DeletedAt.IsZero() {
						return ""
					}
					return style.Age(v.Now().Sub(it.DeletedAt)) + " ago"
				}},
			{Key: "dir", Title: "Directory", Value: func(it Item) any { return it.Dir }},
			{Key: "path", Title: "Trash path", Value: func(it Item) any { return it.Path },
				Text: func(it Item) string { return v.Path(it.Path) }},
		},
		Columns: []string{"name", "original_path", "size_bytes", "deleted_at"},
	}
}
