package symlink

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// Status is where one symlink stands (ADR 0003 §4). Detect keeps no state: it
// compares ws.json, the files under symlinks/, the catalog paths on the host and,
// when asked, a bounded walk of $HOME.
type Status string

const (
	StatusOK        Status = "ok"        // in ws.json, linked and healthy
	StatusFound     Status = "found"     // a catalog item exists here and is not managed: add
	StatusUntracked Status = "untracked" // a host link points into the workspace but is not in ws.json: add <host-path>
	StatusOrphan    Status = "orphan"    // content under symlinks/ with no entry in ws.json: link <host-path>
	StatusMissing   Status = "missing"   // in ws.json, nothing at the host path (a new laptop): link
	StatusBroken    Status = "broken"    // the host link points at something that is gone, or elsewhere: link
	StatusDrifted   Status = "drifted"   // a tool replaced the link with a real file: add <host-path> takes it
)

// DefaultWalkCap is the number of nodes the $HOME walk visits before it stops.
const DefaultWalkCap = 200_000

// walkDepth is how deep the $HOME walk goes (home's children are depth 1).
const walkDepth = 4

// walkSkip are folders the $HOME walk never enters: a name matches at any depth, a
// path with a slash matches relative to $HOME.
var walkSkip = []string{
	".cache", ".git", "node_modules", ".local/share", ".npm", ".cargo/registry",
	".rustup", ".var", "snap", ".Trash", ".local/share/Trash",
}

// Options are the inputs of Detect. Everything the result depends on is here, so a
// test sets HOME and the OS to whatever it needs.
type Options struct {
	// Home is $HOME, Workspace the workspace root. GOOS is "linux" or "darwin"
	// (runtime.GOOS in real use); the catalog's "mac" rows match "darwin".
	Home, Workspace, GOOS string
	// Root is put in front of the catalog's absolute host paths outside Home (the
	// /etc rows) when ws reads the filesystem ("" is the real /). It lets a test
	// fake /etc. A registered entry is always read where it is.
	Root string
	// Entries are the ws.json symlinks, with System expanded.
	Entries []wsconfig.Link
	// Walk turns the $HOME walk for untracked links on. The Dashboard leaves it off.
	Walk bool
	// WalkCap overrides DefaultWalkCap (0 means the default).
	WalkCap int
}

// Record is one line of the result: a registered entry, or a thing Detect found.
type Record struct {
	Status  Status
	App     string // catalog label, "" for a custom path
	Secret  bool   // a catalog secret row
	Sudo    bool
	Kind    Kind
	Message string
	// System is the host path; Original the workspace-relative storage path and
	// WorkspacePath the same, absolute. Dir is true for a folder entry.
	System        string
	Original      string
	WorkspacePath string
	Dir           bool
	// Registered is true when the record is a ws.json entry.
	Registered bool
	// Group ties a private key to its .pub so the checklist shows one row.
	Group string
}

// Result is what Detect found.
type Result struct {
	Records []Record
	// Notes are things worth saying once, such as a walk that stopped at its cap.
	Notes []string
	// Truncated is true when the $HOME walk hit its cap.
	Truncated bool
}

// Detect is a pure function of its inputs: it reads the filesystem and writes
// nothing. Registered entries come first, in ws.json order, then found items (in
// catalog order), untracked links and orphans (both sorted by path).
func Detect(opts Options) Result {
	d := detector{opts: opts, seen: map[string]bool{}}
	d.home = filepath.Clean(opts.Home)
	d.ws = filepath.Clean(opts.Workspace)
	d.items = catalogFor(opts.GOOS)

	d.registered()
	d.catalog()
	if opts.Walk {
		d.walkHome()
	}
	d.orphans()
	return d.res
}

type detector struct {
	opts  Options
	home  string
	ws    string
	items []Item
	seen  map[string]bool // host paths that already have a record
	res   Result
}

// catalogFor is the catalog rows that apply to goos.
func catalogFor(goos string) []Item {
	var out []Item
	for _, it := range Catalog {
		switch it.OS {
		case OSAny:
		case OSLinux:
			if goos != "linux" {
				continue
			}
		case OSMac:
			if goos != "darwin" {
				continue
			}
		}
		out = append(out, it)
	}
	return out
}

// real is the path to read on disk for a host path.
func (d *detector) real(host string) string {
	if d.opts.Root == "" || d.inHome(host) {
		return host
	}
	return filepath.Join(d.opts.Root, host)
}

func (d *detector) inHome(p string) bool {
	_, ok := within(d.home, filepath.Clean(p))
	return ok
}

// expand turns a catalog path into a host path pattern.
func (d *detector) expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(d.home, filepath.FromSlash(p[2:]))
	}
	return filepath.FromSlash(p)
}

func (d *detector) abs(original string) string {
	return filepath.Join(d.ws, filepath.FromSlash(original))
}

// appOf is the catalog label of a host path, "" when no catalog row covers it.
func (d *detector) appOf(host string) (app string, secret bool) {
	for _, it := range d.items {
		if ok, _ := filepath.Match(d.expand(it.Path), host); ok {
			return it.App, it.Secret
		}
	}
	return "", false
}

// resolve is where a host symlink points, absolute.
func resolveLink(host string, real string) (string, error) {
	target, err := os.Readlink(real)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(host), target)
	}
	return filepath.Clean(target), nil
}

// registered is rule 1: every ws.json entry.
func (d *detector) registered() {
	for _, l := range d.opts.Entries {
		host := filepath.Clean(l.System)
		orig := d.abs(l.Original)
		app, secret := d.appOf(host)
		rec := Record{
			App: app, Secret: secret, Sudo: l.Sudo, Kind: KindFile, System: host,
			Original: filepath.ToSlash(filepath.Clean(filepath.FromSlash(l.Original))), WorkspacePath: orig,
			Dir: l.Dir, Registered: true,
		}
		if info, err := os.Stat(orig); err == nil && info.IsDir() {
			rec.Kind = KindFolder
			rec.Dir = true
		}
		// A registered entry is read where it is: Root only fakes the catalog's own
		// /etc paths, and a link whose host path is outside $HOME is real.
		real := host
		li, err := os.Lstat(real)
		switch {
		case err != nil:
			rec.Status, rec.Message = StatusMissing, "nothing at the host path"
			if _, serr := os.Lstat(orig); serr != nil {
				rec.Message = "nothing at the host path, and the workspace copy is missing too"
			}
		case li.Mode()&os.ModeSymlink != 0:
			target, rerr := resolveLink(host, real)
			switch {
			case rerr != nil:
				rec.Status, rec.Message = StatusBroken, "cannot read the link"
			case target != filepath.Clean(orig):
				rec.Status, rec.Message = StatusBroken, "the link points to "+target+", not the workspace copy"
			default:
				if _, serr := os.Stat(orig); serr != nil {
					rec.Status, rec.Message = StatusBroken, "the link target is missing"
				} else {
					rec.Status = StatusOK
				}
			}
		default:
			rec.Status, rec.Message = StatusDrifted, "a real file stands where the link should be"
		}
		d.seen[host] = true
		d.res.Records = append(d.res.Records, rec)
	}
}

// managed reports whether host is an entry's host path, lies inside a folder
// entry, or contains an entry.
func (d *detector) managed(host string) bool {
	for _, l := range d.opts.Entries {
		s := filepath.Clean(l.System)
		if s == host {
			return true
		}
		if _, in := within(s, host); in && (l.Dir || d.isDirOriginal(l)) {
			return true
		}
		if _, in := within(host, s); in {
			return true
		}
	}
	return false
}

func (d *detector) isDirOriginal(l wsconfig.Link) bool {
	info, err := os.Stat(d.abs(l.Original))
	return err == nil && info.IsDir()
}

func (d *detector) isEntrySystem(host string) bool {
	for _, l := range d.opts.Entries {
		if filepath.Clean(l.System) == host {
			return true
		}
	}
	return false
}

// insideWorkspace reports whether target lies in the workspace, and its path
// relative to it.
func (d *detector) insideWorkspace(target string) (string, bool) {
	rel, ok := within(d.ws, target)
	return filepath.ToSlash(rel), ok
}

// catalog is rule 2, and the catalog half of rule 3: each row's host paths.
func (d *detector) catalog() {
	for _, it := range d.items {
		pattern := d.expand(it.Path)
		var hosts []string
		if strings.ContainsAny(pattern, "*?[") {
			ms, _ := filepath.Glob(d.real(pattern))
			for _, m := range ms {
				host := m
				if d.opts.Root != "" && !d.inHome(pattern) {
					host = strings.TrimPrefix(m, filepath.Clean(d.opts.Root))
				}
				hosts = append(hosts, host)
			}
			sort.Strings(hosts)
		} else {
			hosts = []string{pattern}
		}
		for _, host := range hosts {
			base := filepath.Base(host)
			if it.App == "ssh" && strings.HasPrefix(it.Path, "~/.ssh/id_") {
				if strings.HasSuffix(base, ".ws-backup") || (!strings.HasSuffix(it.Path, ".pub") && strings.HasSuffix(base, ".pub")) {
					continue
				}
			}
			d.consider(it, host)
		}
	}
}

// consider decides what one catalog host path is.
func (d *detector) consider(it Item, host string) {
	if d.seen[host] || d.isEntrySystem(host) {
		return
	}
	real := d.real(host)
	li, err := os.Lstat(real)
	if err != nil {
		return
	}
	if li.Mode()&os.ModeSymlink != 0 {
		target, err := resolveLink(host, real)
		if err != nil {
			return
		}
		if rel, in := d.insideWorkspace(target); in && !d.managed(host) {
			d.seen[host] = true
			d.res.Records = append(d.res.Records, Record{
				Status: StatusUntracked, App: it.App, Secret: it.Secret, Sudo: it.Sudo, Kind: it.Kind, System: host,
				Original: rel, WorkspacePath: target, Dir: it.Kind == KindFolder,
				Message: "a link into the workspace that ws.json does not know",
			})
		}
		return // a link to somewhere else is none of our business
	}
	if d.managed(host) {
		return
	}
	if li.IsDir() != (it.Kind == KindFolder) {
		return
	}
	original := MirrorPath(host, d.home)
	if original == "" {
		return
	}
	group := host
	if it.App == "ssh" && strings.HasSuffix(host, ".pub") {
		group = strings.TrimSuffix(host, ".pub")
		if _, err := os.Lstat(d.real(group)); err != nil {
			group = host
		}
	}
	d.seen[host] = true
	d.res.Records = append(d.res.Records, Record{
		Status: StatusFound, App: it.App, Secret: it.Secret, Sudo: it.Sudo, Kind: it.Kind, System: host,
		Original: original, WorkspacePath: d.abs(original), Dir: it.Kind == KindFolder,
		Message: "a known config that is not symlinked yet", Group: group,
	})
}

// skipped reports whether the walk leaves a $HOME-relative folder alone.
func skipped(rel string) bool {
	for _, s := range walkSkip {
		if strings.Contains(s, "/") {
			if rel == s || strings.HasPrefix(rel, s+"/") {
				return true
			}
		} else if path.Base(rel) == s {
			return true
		}
	}
	return false
}

// walkHome is rule 3: links under $HOME that point into the workspace and are not
// entries. It never follows links, stops at the cap, and never enters the
// workspace itself (links inside it are out of scope: rsync keeps them).
func (d *detector) walkHome() {
	limit := d.opts.WalkCap
	if limit <= 0 {
		limit = DefaultWalkCap
	}
	visited := 0
	var found []Record
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if d.res.Truncated || depth > walkDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if d.res.Truncated {
				return
			}
			if visited++; visited > limit {
				d.res.Truncated = true
				d.res.Notes = append(d.res.Notes, "the $HOME walk stopped after "+itoa(limit)+" entries; some links may not be listed")
				return
			}
			p := filepath.Join(dir, e.Name())
			if e.Type()&fs.ModeSymlink != 0 {
				target, err := resolveLink(p, p)
				if err != nil {
					continue
				}
				if rel, in := d.insideWorkspace(target); in && !d.seen[p] && !d.isEntrySystem(p) && !d.managed(p) {
					app, secret := d.appOf(p)
					d.seen[p] = true
					kind := KindFile
					if info, err := os.Stat(target); err == nil && info.IsDir() {
						kind = KindFolder
					}
					found = append(found, Record{
						Status: StatusUntracked, App: app, Secret: secret, Kind: kind, System: p, Original: rel,
						WorkspacePath: target, Dir: kind == KindFolder, Sudo: !d.inHome(p),
						Message: "a link into the workspace that ws.json does not know",
					})
				}
				continue
			}
			if !e.IsDir() {
				continue
			}
			if p == d.ws {
				continue // the workspace itself, or a mount of it
			}
			rel, _ := within(d.home, p)
			if skipped(filepath.ToSlash(rel)) {
				continue
			}
			walk(p, depth+1)
		}
	}
	walk(d.home, 1)
	sort.Slice(found, func(i, j int) bool { return found[i].System < found[j].System })
	d.res.Records = append(d.res.Records, found...)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// orphans is rule 4: content under symlinks/ that no entry covers. A node that
// is the mirror of a catalog folder is one orphan and is not entered.
func (d *detector) orphans() {
	root := d.abs(SymlinksRoot)
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return
	}
	folderMirrors := d.folderMirrors()
	var found []Record
	_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := within(d.ws, p)
		rel = filepath.ToSlash(rel)
		if p == root || rel == MirrorHome || rel == MirrorSystem {
			return nil
		}
		covered, containsEntry := d.coverage(rel)
		if covered {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if e.IsDir() {
			if !containsEntry && matchesAny(folderMirrors, rel) {
				if rec, ok := d.orphan(rel, p, KindFolder); ok {
					found = append(found, rec)
				}
				return filepath.SkipDir
			}
			return nil
		}
		if rec, ok := d.orphan(rel, p, KindFile); ok {
			found = append(found, rec)
		}
		return nil
	})
	sort.Slice(found, func(i, j int) bool { return found[i].Original < found[j].Original })
	d.res.Records = append(d.res.Records, found...)
}

// coverage says whether rel is an entry's original or inside one (covered), and
// whether an entry's original lies inside rel (containsEntry).
func (d *detector) coverage(rel string) (covered, containsEntry bool) {
	for _, l := range d.opts.Entries {
		o := filepath.ToSlash(filepath.Clean(filepath.FromSlash(l.Original)))
		if rel == o || strings.HasPrefix(rel, o+"/") {
			covered = true
		}
		if strings.HasPrefix(o, rel+"/") {
			containsEntry = true
		}
	}
	return covered, containsEntry
}

// orphan builds the record of an orphan, and false when its host path has an entry
// already or cannot be derived.
func (d *detector) orphan(rel, abs string, kind Kind) (Record, bool) {
	host := HostPath(rel, d.home)
	if host == "" || d.isEntrySystem(host) {
		return Record{}, false
	}
	app, secret := d.appOf(host)
	rec := Record{
		Status: StatusOrphan, App: app, Secret: secret, Kind: kind, System: host, Original: rel, WorkspacePath: abs,
		Dir: kind == KindFolder, Sudo: !d.inHome(host), Message: "content under symlinks/ that ws.json does not know",
	}
	return rec, true
}

// folderMirrors are the mirror paths (as path.Match patterns) of the catalog's
// folder rows.
func (d *detector) folderMirrors() []string {
	var out []string
	for _, it := range d.items {
		if it.Kind != KindFolder {
			continue
		}
		if m := MirrorPath(d.expand(it.Path), d.home); m != "" {
			out = append(out, m)
		}
	}
	return out
}

func matchesAny(patterns []string, rel string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, rel); ok {
			return true
		}
	}
	return false
}
