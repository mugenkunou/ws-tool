package trash

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry is one trashed top-level item with its recorded deletion time.
type Entry struct {
	Name      string    `json:"name"`
	Dir       bool      `json:"dir"`
	Files     int       `json:"files"`
	SizeBytes int64     `json:"size_bytes"`
	DeletedAt time.Time `json:"deleted_at"`
}

// PurgeOptions configures removing trash items older than a cutoff age.
type PurgeOptions struct {
	RootDir   string
	OlderThan time.Duration
	Now       time.Time // zero means time.Now()
	DryRun    bool
}

// PurgeResult describes the outcome of a purge. Matched lists the items
// older than the cutoff (oldest first). Kept counts items newer than the
// cutoff; Skipped counts items with no readable DeletionDate, which are
// never deleted.
type PurgeResult struct {
	RootDir   string  `json:"root_dir"`
	Exists    bool    `json:"exists"`
	DryRun    bool    `json:"dry_run"`
	Matched   []Entry `json:"matched"`
	Kept      int     `json:"kept"`
	Skipped   int     `json:"skipped"`
	FileCount int     `json:"file_count"`
	SizeBytes int64   `json:"size_bytes"`
}

// ListOlderThan returns the trashed items whose recorded DeletionDate is
// strictly more than olderThan before now, oldest first. Age comes from the
// .trashinfo sidecar, not the file's mtime.
func ListOlderThan(rootHint string, olderThan time.Duration, now time.Time) (PurgeResult, error) {
	root, err := resolveRoot(rootHint)
	if err != nil {
		return PurgeResult{}, err
	}
	res := PurgeResult{RootDir: root}
	if now.IsZero() {
		now = time.Now()
	}
	entries, err := os.ReadDir(filesDir(root))
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return PurgeResult{}, err
	}
	res.Exists = true
	cutoff := now.Add(-olderThan)
	for _, de := range entries {
		deleted, ok := readDeletionDate(filepath.Join(infoDir(root), de.Name()+".trashinfo"))
		if !ok {
			res.Skipped++
			continue
		}
		if !deleted.Before(cutoff) {
			res.Kept++
			continue
		}
		e := Entry{Name: de.Name(), Dir: de.IsDir(), DeletedAt: deleted}
		e.Files, e.SizeBytes = measure(filepath.Join(filesDir(root), de.Name()))
		res.Matched = append(res.Matched, e)
		res.FileCount += e.Files
		res.SizeBytes += e.SizeBytes
	}
	sort.Slice(res.Matched, func(i, j int) bool { return res.Matched[i].DeletedAt.Before(res.Matched[j].DeletedAt) })
	return res, nil
}

// Purge permanently removes the items ListOlderThan reports (files and their
// .trashinfo). With DryRun it only reports them.
func Purge(opts PurgeOptions) (PurgeResult, error) {
	res, err := ListOlderThan(opts.RootDir, opts.OlderThan, opts.Now)
	if err != nil {
		return PurgeResult{}, err
	}
	res.DryRun = opts.DryRun
	if opts.DryRun {
		return res, nil
	}
	for _, e := range res.Matched {
		if err := os.RemoveAll(filepath.Join(filesDir(res.RootDir), e.Name)); err != nil {
			return PurgeResult{}, err
		}
		_ = os.Remove(filepath.Join(infoDir(res.RootDir), e.Name+".trashinfo"))
	}
	return res, nil
}

// readDeletionDate parses DeletionDate (local time, per the trash spec) from
// a .trashinfo file.
func readDeletionDate(path string) (time.Time, bool) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "DeletionDate=")
		if !ok {
			continue
		}
		t, err := time.ParseInLocation("2006-01-02T15:04:05", v, time.Local)
		return t, err == nil
	}
	return time.Time{}, false
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
