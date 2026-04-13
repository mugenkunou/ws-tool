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

type PruneOptions struct {
	RootDir    string
	OlderThan  time.Duration
	All        bool
	NameFilter string
	DryRun     bool
}

type PruneResult struct {
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

func Prune(opts PruneOptions) (PruneResult, error) {
	list, err := List(ListOptions{RootDir: opts.RootDir, SortBy: "name"})
	if err != nil {
		return PruneResult{}, err
	}
	res := PruneResult{Removed: []string{}, DryRun: opts.DryRun}
	for _, e := range list {
		remove := opts.All
		if !remove && opts.OlderThan > 0 {
			remove = e.Age >= opts.OlderThan
		}
		if remove && strings.TrimSpace(opts.NameFilter) != "" {
			remove = strings.Contains(strings.ToLower(e.Name), strings.ToLower(opts.NameFilter))
		}
		if !remove {
			continue
		}
		if !opts.DryRun {
			if err := os.RemoveAll(e.Path); err != nil {
				return res, err
			}
		}
		res.Removed = append(res.Removed, e.Name)
		res.FreedBytes += e.SizeBytes
	}
	return res, nil
}

func ParseOlderThan(input string) (time.Duration, error) {
	v := strings.TrimSpace(strings.ToLower(input))
	if v == "" {
		return 0, nil
	}
	if strings.HasSuffix(v, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(v, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid duration: %s", input)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %s", input)
	}
	return d, nil
}

func Delete(opts DeleteOptions) (PruneResult, error) {
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return PruneResult{}, errors.New("scratch name is required")
	}

	list, err := List(ListOptions{RootDir: opts.RootDir, SortBy: "name"})
	if err != nil {
		return PruneResult{}, err
	}

	res := PruneResult{Removed: []string{}, DryRun: opts.DryRun}
	for _, e := range list {
		if e.Name != name && !strings.Contains(strings.ToLower(e.Name), strings.ToLower(name)) {
			continue
		}
		if !opts.DryRun {
			if err := os.RemoveAll(e.Path); err != nil {
				return res, err
			}
		}
		res.Removed = append(res.Removed, e.Name)
		res.FreedBytes += e.SizeBytes
	}
	if len(res.Removed) == 0 {
		return res, fmt.Errorf("scratch entry not found: %s", name)
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
