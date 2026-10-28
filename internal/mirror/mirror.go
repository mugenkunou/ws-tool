// Package mirror copies the workspace one way into a folder a sync client
// watches (spec Factor XII, §5 `ws sync`, plan 0004). rsync does the copying;
// ws's own ignore engine decides what goes out, so the mirror holds exactly
// the files `ws ignore check` says sync.
package mirror

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mugenkunou/ws-tool/internal/ignore"
)

// StampName is the marker file in the mirror root. rsync never deletes it.
const StampName = ".ws-sync-stamp"

// GuardMinFiles: the delete guard never trips for this many deletions or fewer.
const GuardMinFiles = 10

// Stamp identifies the push that last wrote a mirror.
type Stamp struct {
	PushID string    `json:"push_id"`
	Host   string    `json:"host"`
	Time   time.Time `json:"time"`
}

// LastPush is the machine-local record of this machine's last push.
type LastPush struct {
	Stamp
	Target  string `json:"target"`
	Files   int    `json:"files"`
	Bytes   int64  `json:"bytes"`
	Changed int    `json:"changed"`
	Deleted int    `json:"deleted"`
}

// StampState says how the mirror's stamp relates to this machine's last push.
type StampState string

const (
	StampFirst   StampState = "first"   // no stamp, no last push: first push
	StampOK      StampState = "ok"      // the stamp is this machine's last push
	StampWiped   StampState = "wiped"   // a last push exists but the stamp is gone
	StampForeign StampState = "foreign" // the stamp was written by another push
)

// Options configure one push, status or restore.
type Options struct {
	Workspace    string
	Target       string
	Rules        ignore.UserRules
	Exclude      []ignore.RuleEntry
	MaxDeletePct int
	StateDir     string // the ws-tool state dir; ws sync uses its sync/ subdir
}

func (o Options) syncDir() string { return filepath.Join(o.StateDir, "sync") }
func (o Options) lastPushPath() string {
	return filepath.Join(o.syncDir(), "last-push.json")
}

// Check validates the options without touching anything.
func Check(o Options) error {
	if strings.TrimSpace(o.Target) == "" {
		return errors.New("sync.mirror.target is not set: add it to ws.json (ws config edit)")
	}
	if _, err := exec.LookPath("rsync"); err != nil {
		return errors.New("rsync is not installed: ws sync needs it on PATH")
	}
	if !filepath.IsAbs(o.Target) {
		return fmt.Errorf("sync.mirror.target must be an absolute path or start with ~/: %s", o.Target)
	}
	ws, tg := filepath.Clean(o.Workspace), filepath.Clean(o.Target)
	if within(tg, ws) {
		return fmt.Errorf("sync.mirror.target %s is inside the workspace %s", tg, ws)
	}
	if within(ws, tg) {
		return fmt.Errorf("the workspace %s is inside sync.mirror.target %s", ws, tg)
	}
	parent := filepath.Dir(tg)
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return fmt.Errorf("the mirror's parent folder %s does not exist (is the sync client installed, or was the folder moved?)", parent)
	}
	if info, err := os.Lstat(tg); err == nil && !info.IsDir() {
		return fmt.Errorf("sync.mirror.target %s exists and is not a folder", tg)
	}
	return nil
}

// within reports whether p is base or below it.
func within(p, base string) bool {
	rel, err := filepath.Rel(base, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// Filter is the rsync filter for one push: an include rule for every synced
// directory and file, then "- *".
type Filter struct {
	Rules    []byte   // NUL-separated, for --from0
	Files    int      // regular files admitted
	Bytes    int64    // their total size
	Symlinks []string // workspace-relative symlinks that would sync; skipped
}

// BuildFilter walks the workspace with ws's ignore engine plus the extra
// excludes. Symbolic links are never admitted (spec: ws never syncs links).
func BuildFilter(workspace string, rules ignore.UserRules, exclude []ignore.RuleEntry) (Filter, error) {
	engine := ignore.BuildEngine(rules)
	extra := ignore.ExcludeEngine(exclude)
	var f Filter
	var buf bytes.Buffer
	err := filepath.WalkDir(workspace, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == workspace {
				return err
			}
			return nil // vanished or unreadable: skip
		}
		if path == workspace {
			return nil
		}
		rel, err := filepath.Rel(workspace, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		isDir := d.IsDir()
		if !engine.Evaluate(rel, isDir).Included || !extra.Evaluate(rel, isDir).Included {
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			f.Symlinks = append(f.Symlinks, rel)
		case isDir:
			fmt.Fprintf(&buf, "+ /%s/\x00", escape(rel))
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				return nil
			}
			fmt.Fprintf(&buf, "+ /%s\x00", escape(rel))
			f.Files++
			f.Bytes += info.Size()
		}
		return nil
	})
	if err != nil {
		return Filter{}, err
	}
	buf.WriteString("- *\x00")
	f.Rules = buf.Bytes()
	return f, nil
}

// escape makes a path literal in an rsync filter rule. rsync reads a backslash
// as an escape only when the pattern holds a wildcard, so a path without one
// is already literal.
func escape(rel string) string {
	if !strings.ContainsAny(rel, "*?[") {
		return rel
	}
	var b strings.Builder
	for _, r := range rel {
		if strings.ContainsRune(`\*?[`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Prepared is a push that has been planned (filter written, dry run done) but
// not run. Close removes its temporary filter file.
type Prepared struct {
	Options     Options
	Filter      Filter
	State       StampState
	Last        *LastPush // nil when there is none for this target
	Changed     []string  // files the push would copy
	Deleted     []string  // mirror paths the push would delete
	MirrorFiles int       // regular files in the mirror now
	filterPath  string
}

// Prepare checks the options, builds the filter and dry-runs rsync.
func Prepare(o Options) (*Prepared, error) {
	if err := Check(o); err != nil {
		return nil, err
	}
	f, err := BuildFilter(o.Workspace, o.Rules, o.Exclude)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(o.syncDir(), "tmp"), 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(o.syncDir(), "filter-*")
	if err != nil {
		return nil, err
	}
	if _, err := tmp.Write(f.Rules); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, err
	}
	tmp.Close()
	p := &Prepared{Options: o, Filter: f, filterPath: tmp.Name()}

	last, err := ReadLastPush(o)
	if err != nil {
		p.Close()
		return nil, err
	}
	p.Last = last
	stamp, err := ReadStamp(o.Target)
	if err != nil {
		p.Close()
		return nil, err
	}
	p.State = stampState(stamp, last)
	p.MirrorFiles = countFiles(o.Target)

	out, err := p.rsync(true)
	if err != nil {
		p.Close()
		return nil, err
	}
	p.Changed, p.Deleted = parseItemized(out)
	return p, nil
}

// Close removes the temporary filter file.
func (p *Prepared) Close() {
	if p.filterPath != "" {
		os.Remove(p.filterPath)
		p.filterPath = ""
	}
}

// GuardTripped reports whether the push would delete more of the mirror than
// max_delete_pct allows (and more than GuardMinFiles files).
func (p *Prepared) GuardTripped() bool {
	n := len(p.Deleted)
	if n <= GuardMinFiles {
		return false
	}
	pct := p.Options.MaxDeletePct
	if pct <= 0 {
		pct = 20
	}
	return n*100 > pct*p.MirrorFiles
}

func (p *Prepared) rsync(dryRun bool) ([]byte, error) {
	o := p.Options
	// --update: skip a file whose copy in the mirror is newer than the workspace
	// file. Virtual-drive clients (Zoho TrueSync) ignore the mtime rsync -t sets
	// and stamp the copy with the upload time, so the times never match again
	// and plain -rt would re-send every file on every push. An upload time is
	// always later than the source's mtime, and an edit made after it is newer
	// again, so a real change is still copied.
	args := []string{"-rt", "--update", "--delete", "--delete-excluded", "--from0",
		"--filter=P /" + StampName,
		"--filter=merge " + p.filterPath,
		"--temp-dir=" + filepath.Join(o.syncDir(), "tmp"),
		"--out-format=%i|%n",
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, withSlash(o.Workspace), withSlash(o.Target))
	return runRsync(args)
}

func withSlash(p string) string { return strings.TrimSuffix(p, "/") + "/" }

func runRsync(args []string) ([]byte, error) {
	cmd := exec.Command("rsync", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("rsync failed: %s", msg)
	}
	return out, nil
}

// parseItemized reads rsync's "%i|%n" lines: transferred files and deletions.
func parseItemized(out []byte) (changed, deleted []string) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		item, name, ok := strings.Cut(sc.Text(), "|")
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(item, "*deleting"):
			if !strings.HasSuffix(name, "/") {
				deleted = append(deleted, name)
			}
		case len(item) > 1 && item[1] == 'f' && (item[0] == '>' || item[0] == 'c'):
			changed = append(changed, name)
		}
	}
	return changed, deleted
}

// Result is what a push did.
type Result struct {
	Target   string    `json:"target"`
	Files    int       `json:"files"`
	Bytes    int64     `json:"bytes"`
	Changed  int       `json:"changed"`
	Deleted  int       `json:"deleted"`
	Symlinks []string  `json:"skipped_symlinks"`
	Time     time.Time `json:"time"`
}

// Run copies for real, then writes the stamp and the last-push record.
func (p *Prepared) Run() (Result, error) {
	out, err := p.rsync(false)
	if err != nil {
		return Result{}, err
	}
	changed, deleted := parseItemized(out)
	host, _ := os.Hostname()
	now := time.Now().UTC().Truncate(time.Second)
	stamp := Stamp{PushID: newID(), Host: host, Time: now}
	if err := writeJSON(filepath.Join(p.Options.Target, StampName), stamp); err != nil {
		return Result{}, fmt.Errorf("writing the stamp: %w", err)
	}
	last := LastPush{Stamp: stamp, Target: p.Options.Target, Files: p.Filter.Files, Bytes: p.Filter.Bytes,
		Changed: len(changed), Deleted: len(deleted)}
	if err := writeJSON(p.Options.lastPushPath(), last); err != nil {
		return Result{}, fmt.Errorf("writing the last-push record: %w", err)
	}
	return Result{Target: p.Options.Target, Files: p.Filter.Files, Bytes: p.Filter.Bytes,
		Changed: len(changed), Deleted: len(deleted), Symlinks: p.Filter.Symlinks, Time: now}, nil
}

func stampState(stamp *Stamp, last *LastPush) StampState {
	switch {
	case stamp == nil && last == nil:
		return StampFirst
	case stamp == nil:
		return StampWiped
	case last == nil || stamp.PushID != last.PushID:
		return StampForeign
	}
	return StampOK
}

// ReadStamp reads the mirror's stamp; nil when there is none.
func ReadStamp(target string) (*Stamp, error) {
	var s Stamp
	ok, err := readJSON(filepath.Join(target, StampName), &s)
	if !ok || err != nil {
		return nil, err
	}
	return &s, nil
}

// ReadLastPush reads this machine's last push to o.Target; nil when there is
// none, or when it went to another target.
func ReadLastPush(o Options) (*LastPush, error) {
	var l LastPush
	ok, err := readJSON(o.lastPushPath(), &l)
	if !ok || err != nil {
		return nil, err
	}
	if filepath.Clean(l.Target) != filepath.Clean(o.Target) {
		return nil, nil
	}
	return &l, nil
}

// countFiles counts regular files under dir, without the stamp.
func countFiles(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() && path != filepath.Join(dir, StampName) {
			n++
		}
		return nil
	})
	return n
}

// Lock takes the push lock. busy is true when another push holds it; unlock
// is then nil.
func Lock(o Options) (unlock func(), busy bool, err error) {
	if err := os.MkdirAll(o.syncDir(), 0o755); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(filepath.Join(o.syncDir(), "lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, false, nil
}

// RestoreResult is what a restore did.
type RestoreResult struct {
	Source  string `json:"source"`
	Copied  int    `json:"copied"`
	Adopted bool   `json:"adopted_stamp"`
}

// Restore copies src into o.Workspace. It never deletes anything in the
// workspace and skips the stamp. When src has a stamp, this machine adopts it
// (as its last push to o.Target) so its next push is not taken as foreign.
func Restore(o Options, src string, dryRun bool) (RestoreResult, error) {
	if _, err := exec.LookPath("rsync"); err != nil {
		return RestoreResult{}, errors.New("rsync is not installed: ws sync needs it on PATH")
	}
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		return RestoreResult{}, fmt.Errorf("the mirror folder %s does not exist", src)
	}
	if !dryRun {
		if err := os.MkdirAll(o.Workspace, 0o755); err != nil {
			return RestoreResult{}, err
		}
	}
	args := []string{"-rt", "--exclude=/" + StampName, "--out-format=%i|%n"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, withSlash(src), withSlash(o.Workspace))
	out, err := runRsync(args)
	if err != nil {
		return RestoreResult{}, err
	}
	changed, _ := parseItemized(out)
	res := RestoreResult{Source: src, Copied: len(changed)}
	if dryRun {
		return res, nil
	}
	stamp, err := ReadStamp(src)
	if err != nil || stamp == nil {
		return res, err
	}
	target := o.Target
	if target == "" {
		target = src
	}
	last := LastPush{Stamp: *stamp, Target: target}
	o.Target = target
	if err := os.MkdirAll(o.syncDir(), 0o755); err != nil {
		return res, err
	}
	if err := writeJSON(o.lastPushPath(), last); err != nil {
		return res, err
	}
	res.Adopted = true
	return res, nil
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readJSON reads path into v. ok is false when the file does not exist.
func readJSON(path string, v any) (ok bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, fmt.Errorf("parsing %s: %w", path, err)
	}
	return true, nil
}
