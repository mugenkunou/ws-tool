package scratch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mugenkunou/ws-tool/internal/trash"
)

type NewOptions struct {
	RootDir      string
	Name         string
	NoDateSuffix bool
	SuffixMode   string
	DryRun       bool
}

type NewResult struct {
	Path    string `json:"path"`
	Created bool   `json:"created"`
	DryRun  bool   `json:"dry_run"`
}

type Entry struct {
	Name      string        `json:"name"`
	Path      string        `json:"path"`
	Age       time.Duration `json:"age"`
	SizeBytes int64         `json:"size_bytes"`
	Items     int           `json:"items"`
	Tags      []string      `json:"tags"`
}

type ListOptions struct {
	RootDir string
	SortBy  string
}

type DeleteResult struct {
	Removed    []string `json:"removed"`
	FreedBytes int64    `json:"freed_bytes"`
	DryRun     bool     `json:"dry_run"`
}

type DeleteOptions struct {
	RootDir string
	Name    string
	DryRun  bool
}

func New(opts NewOptions) (NewResult, error) {
	root := strings.TrimSpace(opts.RootDir)
	if root == "" {
		return NewResult{}, errors.New("scratch root dir is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return NewResult{}, err
	}

	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "scratch-" + strconv.FormatInt(time.Now().Unix()%100000, 10)
	}
	if opts.SuffixMode == "auto" && !opts.NoDateSuffix {
		name = name + "." + time.Now().Format("2006-01")
	}
	finalName := uniqueName(root, name)
	full := filepath.Join(root, finalName)

	if opts.DryRun {
		return NewResult{Path: full, Created: false, DryRun: true}, nil
	}
	if err := os.MkdirAll(full, 0o755); err != nil {
		return NewResult{}, err
	}
	_ = SaveMeta(full, Meta{Tags: []string{}, Created: time.Now().UTC()})
	return NewResult{Path: full, Created: true}, nil
}

func List(opts ListOptions) ([]Entry, error) {
	root := strings.TrimSpace(opts.RootDir)
	if root == "" {
		return nil, errors.New("scratch root dir is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0)
	now := time.Now()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name())
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		size, items := dirSizeAndItems(p)
		meta, _ := LoadMeta(p)
		out = append(out, Entry{Name: e.Name(), Path: p, Age: now.Sub(st.ModTime()), SizeBytes: size, Items: items, Tags: meta.Tags})
	}
	sortBy := strings.ToLower(strings.TrimSpace(opts.SortBy))
	if sortBy == "" {
		sortBy = "age"
	}
	sort.Slice(out, func(i, j int) bool {
		switch sortBy {
		case "name":
			return out[i].Name < out[j].Name
		case "size":
			return out[i].SizeBytes > out[j].SizeBytes
		default:
			return out[i].Age < out[j].Age
		}
	})
	return out, nil
}

// Resolve finds the one scratch directory a name stands for (spec §5 scratch,
// "Names"). An exact name wins. Otherwise a case-insensitive substring that
// matches exactly one directory is used, so `dns` finds `dns-debug.2026-09`. A
// name that matches nothing, or several directories, is an error that lists the
// candidates.
func Resolve(rootDir, name string) (Entry, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Entry{}, errors.New("scratch name is required")
	}
	list, err := List(ListOptions{RootDir: rootDir, SortBy: "name"})
	if err != nil {
		return Entry{}, err
	}
	for _, e := range list {
		if e.Name == name {
			return e, nil
		}
	}
	var matches []Entry
	for _, e := range list {
		if strings.Contains(strings.ToLower(e.Name), strings.ToLower(name)) {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 0:
		return Entry{}, fmt.Errorf("scratch entry not found: %s", name)
	case 1:
		return matches[0], nil
	}
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = m.Name
	}
	return Entry{}, fmt.Errorf("%q matches several scratch directories: %s", name, strings.Join(names, ", "))
}

// Remove moves one scratch directory to the XDG trash, where `ws trash
// restore` can bring it back. It takes the directory itself, never a name: a
// substring must not decide what is deleted.
func Remove(entry Entry) error {
	return trash.SoftDelete(entry.Path)
}

// Delete resolves name to exactly one directory (see Resolve) and moves it to
// the trash. It used to delete every directory whose name contained the
// argument, so deleting `foo` also deleted `foo-2` and `myfoo`.
func Delete(opts DeleteOptions) (DeleteResult, error) {
	entry, err := Resolve(opts.RootDir, opts.Name)
	if err != nil {
		return DeleteResult{Removed: []string{}, DryRun: opts.DryRun}, err
	}
	res := DeleteResult{Removed: []string{entry.Name}, FreedBytes: entry.SizeBytes, DryRun: opts.DryRun}
	if opts.DryRun {
		return res, nil
	}
	if err := Remove(entry); err != nil {
		return DeleteResult{Removed: []string{}}, err
	}
	return res, nil
}

// stripDateSuffix removes a trailing .YYYY-MM date suffix for matching purposes.
func stripDateSuffix(name string) string {
	if idx := strings.LastIndex(name, "."); idx > 0 {
		suffix := name[idx+1:]
		if len(suffix) == 7 && suffix[4] == '-' {
			return name[:idx]
		}
	}
	return name
}

func uniqueName(root, base string) string {
	candidate := base
	idx := 2
	for {
		if _, err := os.Stat(filepath.Join(root, candidate)); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, idx)
		idx++
	}
}

func dirSizeAndItems(path string) (int64, int) {
	var size int64
	items := 0
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != path {
				items++
			}
			return nil
		}
		st, err := d.Info()
		if err == nil {
			size += st.Size()
		}
		items++
		return nil
	})
	return size, items
}
