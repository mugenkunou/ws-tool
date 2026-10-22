package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/fsutil"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// AddPlan says where one path will go.
type AddPlan struct {
	Source string // absolute path being moved
	Dest   string // absolute destination: the directory, or the file inside its new directory
	Dir    string // the scratch directory that holds Dest
}

// AddOptions configures PlanAdd and Add.
type AddOptions struct {
	RootDir       string // the scratch root
	WorkspacePath string
	ConfigPath    string // ws.json, which Add must never move
	Links         []wsconfig.Link
	Paths         []string // absolute, cleaned
}

// PlanAdd decides where every path goes and checks that all of them may move
// (spec §5 scratch add). It changes nothing. A directory keeps its name:
// <root>/<base>. A file goes into a new directory named after the file without
// its last extension, because scratch rows are directories:
// capture.pcap -> <root>/capture/capture.pcap. A name that would be empty
// (.env) keeps the full file name. A destination that already exists gets -2,
// -3, … and an existing scratch directory is never merged into.
//
// Any one of these stops the whole command before anything moves: the path does
// not exist; it is the scratch root or inside it; it is a directory that
// contains the scratch root or the workspace root; it is ws.json; it is a
// dotfile original that ws.json links[] points to (or contains one, or is
// inside a directory original).
func PlanAdd(opts AddOptions) ([]AddPlan, error) {
	root := filepath.Clean(opts.RootDir)
	workspace := filepath.Clean(opts.WorkspacePath)
	var originals []string
	for _, l := range opts.Links {
		originals = append(originals, filepath.Join(workspace, filepath.FromSlash(l.Original)))
	}

	reserved := map[string]bool{}
	var plans []AddPlan
	for _, src := range opts.Paths {
		src = filepath.Clean(src)
		info, err := os.Lstat(src)
		if err != nil {
			return nil, fmt.Errorf("path not found: %s", src)
		}
		switch {
		case within(root, src):
			return nil, fmt.Errorf("%s is the scratch root or inside it", src)
		case info.IsDir() && within(src, root):
			return nil, fmt.Errorf("%s contains the scratch root %s", src, root)
		case info.IsDir() && within(src, workspace):
			return nil, fmt.Errorf("%s contains the workspace %s", src, workspace)
		case opts.ConfigPath != "" && src == filepath.Clean(opts.ConfigPath):
			return nil, fmt.Errorf("%s is ws.json and cannot be moved", src)
		}
		for i, orig := range originals {
			if within(orig, src) || within(src, orig) {
				return nil, fmt.Errorf("%s is a dotfile original (%s links to it); run ws dotfile unlink %s first", src, opts.Links[i].System, opts.Links[i].System)
			}
		}

		base := filepath.Base(src)
		var dir, dest string
		if info.IsDir() {
			dir = uniqueIn(root, base, reserved)
			dest = filepath.Join(root, dir)
		} else {
			stem := strings.TrimSuffix(base, filepath.Ext(base))
			if stem == "" {
				stem = base
			}
			dir = uniqueIn(root, stem, reserved)
			dest = filepath.Join(root, dir, base)
		}
		reserved[dir] = true
		plans = append(plans, AddPlan{Source: src, Dest: dest, Dir: filepath.Join(root, dir)})
	}
	return plans, nil
}

// within reports whether p is dir or lies under it.
func within(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// uniqueIn returns base, or base-2, base-3, … : the first name that is neither
// taken in root nor already reserved by an earlier path of the same command.
func uniqueIn(root, base string, reserved map[string]bool) string {
	candidate := base
	for n := 2; ; n++ {
		if _, err := os.Lstat(filepath.Join(root, candidate)); os.IsNotExist(err) && !reserved[candidate] {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, n)
	}
}

// Add moves one planned path into the scratch root. No date suffix is added and
// no .ws-meta.json is written: tags are written by the first `scratch tag`.
func Add(p AddPlan) error {
	if _, err := os.Lstat(p.Dest); err == nil {
		return fmt.Errorf("%s already exists", p.Dest)
	}
	if err := os.MkdirAll(filepath.Dir(p.Dest), 0o755); err != nil {
		return err
	}
	if err := fsutil.Move(p.Source, p.Dest); err != nil {
		return fmt.Errorf("move %s: %w", p.Source, err)
	}
	return nil
}

// ApplyTags adds and removes tags. Tags are trimmed and lowercased, and a
// comma-separated value counts as several. It returns the new tag list and the
// tags that were really added and removed.
func ApplyTags(current, add, remove []string) (tags, added, removed []string) {
	tags = append([]string{}, current...)
	has := func(list []string, t string) int {
		for i, x := range list {
			if x == t {
				return i
			}
		}
		return -1
	}
	for _, raw := range add {
		for _, t := range SplitTags(raw) {
			if has(tags, t) < 0 {
				tags = append(tags, t)
				added = append(added, t)
			}
		}
	}
	for _, raw := range remove {
		for _, t := range SplitTags(raw) {
			if i := has(tags, t); i >= 0 {
				tags = append(tags[:i], tags[i+1:]...)
				removed = append(removed, t)
			}
		}
	}
	return tags, added, removed
}

// SplitTags splits a comma-separated tag argument into normalized tags.
func SplitTags(arg string) []string {
	var out []string
	for _, part := range strings.Split(arg, ",") {
		if t := NormalizeTag(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}
