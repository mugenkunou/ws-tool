// Package symlink manages host files whose originals live in the workspace
// and are symlinked back (spec §5 symlink).
//
// Two directions decide which copy wins when both exist:
//
//   - Add: the live file goes into the workspace (a drifted entry's live file
//     replaces its copy). Adding a path whose workspace copy already exists, or a
//     link into the workspace, moves nothing.
//   - Link: the workspace copy goes out to the host path.
//   - Unlink reverses both.
//
// A workspace copy under symlinks/ that no other entry points at is owned: ws made it and may move or replace it. Any other
// workspace copy is shared, and ws never moves or deletes it.
package symlink

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/fsutil"
	"github.com/mugenkunou/ws-tool/internal/trash"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// SymlinksRoot is the workspace-relative directory of the mirrored storage tree
// (symlinks/home/… and symlinks/system/…).
const SymlinksRoot = "symlinks"

// ownedRoots are the directories whose files ws may move and replace.
var ownedRoots = []string{SymlinksRoot}

// ── Options and results ────────────────────────────────────────────

// AddOptions configures PlanAdd and Add.
type AddOptions struct {
	WorkspacePath string
	ConfigPath    string
	// Host is the host path to add (~ is expanded).
	Host string
	// To is the absolute path inside the workspace to store the copy at. Empty
	// means MirrorPath(Host).
	To string
	// Dir takes a directory whole, as one entry (a catalog folder, ~/.password-store).
	Dir bool
}

// ── Paths ──────────────────────────────────────────────────────────

// LinkOriginalPath returns the workspace-relative path of a link's original.
func LinkOriginalPath(link wsconfig.Link) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(link.Original)))
}

// WorkspaceCopy is the absolute path of an entry's workspace copy.
func WorkspaceCopy(workspacePath string, link wsconfig.Link) string {
	return filepath.Clean(filepath.Join(workspacePath, filepath.FromSlash(LinkOriginalPath(link))))
}

// RelInWorkspace returns abs relative to the workspace, slash-separated, and
// whether it lies strictly inside it (the workspace itself is not inside).
func RelInWorkspace(workspacePath, abs string) (string, bool) {
	rel, err := filepath.Rel(filepath.Clean(workspacePath), filepath.Clean(abs))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// ownedRoot returns the owned root that the workspace-relative path lies under.
func ownedRoot(rel string) (string, bool) {
	rel = filepath.ToSlash(filepath.Clean(rel))
	for _, root := range ownedRoots {
		if strings.HasPrefix(rel, root+"/") {
			return root, true
		}
	}
	return "", false
}

// IsShared reports whether the entry's workspace copy is shared: it lies
// outside symlinks/ (a workspace path chosen with `add <host> <ws-path>`, such
// as agent/AGENTS.md), or another entry points at the same copy. ws never moves
// or deletes a shared copy.
func IsShared(cfg wsconfig.WsConfig, link wsconfig.Link) bool {
	rel := LinkOriginalPath(link)
	if _, owned := ownedRoot(rel); !owned {
		return true
	}
	for _, other := range cfg.Symlinks {
		if filepath.Clean(other.System) != filepath.Clean(link.System) && LinkOriginalPath(other) == rel {
			return true
		}
	}
	return false
}

// FindLink returns the entry whose system path is abs.
func FindLink(cfg wsconfig.WsConfig, abs string) (wsconfig.Link, bool) {
	abs = filepath.Clean(abs)
	for _, l := range cfg.Symlinks {
		if filepath.Clean(l.System) == abs {
			return l, true
		}
	}
	return wsconfig.Link{}, false
}

// ownLink reports whether the system path is a symlink to expected.
func ownLink(system, expected string) bool {
	fi, err := os.Lstat(system)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return false
	}
	target, err := os.Readlink(system)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(system), target)
	}
	return filepath.Clean(target) == filepath.Clean(expected)
}

// freeBackupPath returns <path>.ws-backup, or <path>.ws-backup.N with the
// lowest N that is free, so ws never overwrites an earlier backup.
func freeBackupPath(path string) string {
	candidate := path + ".ws-backup"
	for n := 1; ; n++ {
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s.ws-backup.%d", path, n)
	}
}

// ── Add ────────────────────────────────────────────────────────────

// AddCase says what Add will do, decided by what exists (plan 0003 §3).
type AddCase string

const (
	// CaseMove: real content at the host moves into the workspace, the host path
	// becomes a link, and the entry is registered.
	CaseMove AddCase = "move"
	// CaseTake: the entry is drifted (a real file stands where the link was). The
	// live content replaces the workspace copy, which goes to the trash, and the
	// host path is linked again.
	CaseTake AddCase = "take"
	// CaseRegister: the host path is already a link into the workspace. Only the
	// entry is recorded; nothing moves.
	CaseRegister AddCase = "register"
	// CaseLinkOut: the host path is missing and the workspace path exists. The host
	// path is linked out and the entry recorded.
	CaseLinkOut AddCase = "link"
	// CaseManaged: the entry exists and is linked to its copy; nothing to do.
	CaseManaged AddCase = "managed"
	// CaseNothing: the entry exists but there is nothing to take (Note says why).
	CaseNothing AddCase = "nothing"
)

// AddPlan says what Add will do to one host path, so the plan line can name both
// paths before anything moves. PlanAdd changes nothing.
type AddPlan struct {
	Case      AddCase
	Host      string // absolute host path
	Workspace string // absolute path of the workspace copy
	Original  string // workspace-relative, as stored
	Dir       bool
	Sudo      bool
	// Note explains CaseManaged and CaseNothing.
	Note string
}

// insideGit reports whether rel (workspace-relative, slash-separated) is .git or
// lies inside one.
func insideGit(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if part == ".git" {
			return true
		}
	}
	return false
}

// wsTarget validates a workspace path: inside the workspace, not inside .git, and
// not an entry's original or inside one (except the entry named own, whose host
// path is its System).
func wsTarget(workspacePath string, cfg wsconfig.WsConfig, abs, ownSystem string) (string, error) {
	rel, in := RelInWorkspace(workspacePath, abs)
	if !in {
		return "", fmt.Errorf("%s must lie inside the workspace %s", abs, workspacePath)
	}
	if insideGit(rel) {
		return "", fmt.Errorf("%s lies inside .git; choose another workspace path", abs)
	}
	for _, l := range cfg.Symlinks {
		if ownSystem != "" && filepath.Clean(l.System) == filepath.Clean(ownSystem) {
			continue
		}
		o := LinkOriginalPath(l)
		if rel == o || strings.HasPrefix(rel, o+"/") {
			return "", fmt.Errorf("%s is already the original of %s; unlink that first or choose another workspace path", abs, l.System)
		}
	}
	return rel, nil
}

// resolveLinkTarget is where a symlink points, absolute.
func resolveLinkTarget(link string) (string, error) {
	target, err := os.Readlink(link)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	return filepath.Clean(target), nil
}

// PlanAdd decides what adding opts.Host does, and refuses what cannot work. It
// changes nothing. The cases are the table in plan 0003 §3.
func PlanAdd(opts AddOptions) (AddPlan, error) {
	p := AddPlan{Dir: opts.Dir}
	host, err := expandPath(opts.Host)
	if err != nil {
		return p, err
	}
	p.Host = host
	home, _ := os.UserHomeDir()
	if home != "" && filepath.Clean(home) == host {
		return p, errors.New("cannot register the home directory as a symlink")
	}
	cfg, err := wsconfig.Load(opts.ConfigPath)
	if err != nil {
		return p, err
	}
	to := ""
	if opts.To != "" {
		to = filepath.Clean(opts.To)
	}
	p.Sudo = NeedsSudo(host)
	li, lerr := os.Lstat(host)

	// An entry exists: the live file wins (take), or there is nothing to do.
	if entry, managed := FindLink(cfg, host); managed {
		p.Workspace = WorkspaceCopy(opts.WorkspacePath, entry)
		p.Original = LinkOriginalPath(entry)
		p.Dir, p.Sudo = entry.Dir, entry.Sudo
		if to != "" && to != p.Workspace {
			return p, fmt.Errorf("%s is already managed and stored at %s; unlink it first to store it elsewhere", host, p.Workspace)
		}
		switch {
		case lerr != nil:
			p.Case, p.Note = CaseNothing, "nothing there to take; ws symlink link "+host+" restores the link"
		case li.Mode()&os.ModeSymlink != 0:
			if ownLink(host, p.Workspace) {
				if _, err := os.Stat(p.Workspace); err == nil {
					p.Case, p.Note = CaseManaged, "already managed"
					return p, nil
				}
				p.Case, p.Note = CaseNothing, "the workspace copy is missing; nothing to take"
				return p, nil
			}
			p.Case, p.Note = CaseNothing, "a link to somewhere else; ws symlink link "+host+" replaces it"
		case entry.Dir || li.IsDir():
			return p, fmt.Errorf("%s is a directory entry; taking the live content works on files only", host)
		case IsShared(cfg, entry):
			return p, fmt.Errorf("%s is a shared copy (stored outside the owned symlinks/ tree); copy the live file over it yourself, then run ws symlink link", p.Workspace)
		default:
			p.Case = CaseTake
		}
		return p, nil
	}

	// The workspace path: the given one, or the mirror of the host path.
	var rel string
	if to != "" {
		if rel, err = wsTarget(opts.WorkspacePath, cfg, to, ""); err != nil {
			return p, err
		}
	} else {
		mirror := MirrorPath(host, home)
		if mirror == "" {
			return p, fmt.Errorf("%s has no storage path (is it relative, or does it contain ..?)", host)
		}
		rel = mirror
		if rel, err = wsTarget(opts.WorkspacePath, cfg, filepath.Join(opts.WorkspacePath, filepath.FromSlash(mirror)), ""); err != nil {
			return p, err
		}
	}
	p.Original = rel
	p.Workspace = filepath.Join(opts.WorkspacePath, filepath.FromSlash(rel))

	switch {
	case lerr != nil: // missing
		info, serr := os.Stat(p.Workspace)
		if serr != nil {
			return p, fmt.Errorf("nothing to add: %s does not exist, and neither does %s", host, p.Workspace)
		}
		p.Case, p.Dir = CaseLinkOut, info.IsDir()
	case li.Mode()&os.ModeSymlink != 0:
		target, err := resolveLinkTarget(host)
		if err != nil {
			return p, err
		}
		trel, in := RelInWorkspace(opts.WorkspacePath, target)
		if !in {
			return p, fmt.Errorf("%s is a link to somewhere else (%s); nothing to add", host, target)
		}
		if to != "" && to != target {
			return p, fmt.Errorf("%s is a link to %s, not to %s", host, target, to)
		}
		info, serr := os.Stat(target)
		if serr != nil {
			return p, fmt.Errorf("%s is a link to %s, which does not exist", host, target)
		}
		if _, err := wsTarget(opts.WorkspacePath, cfg, target, ""); err != nil {
			return p, err
		}
		p.Case, p.Workspace, p.Original, p.Dir = CaseRegister, target, trel, info.IsDir()
	default: // real file or folder
		if _, in := RelInWorkspace(opts.WorkspacePath, host); in {
			return p, fmt.Errorf("%s lies inside the workspace; nothing to move out of it", host)
		}
		if li.IsDir() && !opts.Dir {
			return p, errors.New("a folder is added whole with --dir, or one entry per file with --files")
		}
		if opts.Dir && !li.IsDir() {
			return p, fmt.Errorf("--dir needs a directory: %s", host)
		}
		if _, err := os.Lstat(p.Workspace); err == nil {
			return p, fmt.Errorf("the destination exists: %s; remove it or choose another workspace path", p.Workspace)
		}
		p.Case, p.Dir = CaseMove, li.IsDir()
	}
	return p, nil
}

// Add does what PlanAdd decided and records the entry. A failed step puts back what
// it moved, so the host path is never left empty.
func Add(opts AddOptions) (wsconfig.Link, error) {
	p, err := PlanAdd(opts)
	if err != nil {
		return wsconfig.Link{}, err
	}
	link := wsconfig.Link{System: p.Host, Original: p.Original, Dir: p.Dir, Sudo: p.Sudo}
	switch p.Case {
	case CaseManaged:
		return link, nil
	case CaseNothing:
		return link, errors.New(p.Note)
	case CaseTake:
		return link, takeLive(p.Host, p.Workspace, p.Sudo)
	case CaseRegister:
		return link, wsconfig.AddLink(opts.ConfigPath, link)
	case CaseLinkOut:
		if err := linkOut(p.Host, p.Workspace, "", p.Sudo); err != nil {
			return link, err
		}
		if err := wsconfig.AddLink(opts.ConfigPath, link); err != nil {
			_ = removePath(p.Host, p.Sudo)
			return link, err
		}
		return link, nil
	}

	// CaseMove
	if err := os.MkdirAll(filepath.Dir(p.Workspace), 0o755); err != nil {
		return link, err
	}
	if p.Sudo {
		if err := sudoRename(p.Host, p.Workspace); err != nil {
			return link, err
		}
		if err := sudoSymlink(p.Workspace, p.Host); err != nil {
			_ = sudoRename(p.Workspace, p.Host)
			return link, err
		}
	} else {
		if err := movePath(p.Host, p.Workspace); err != nil {
			return link, err
		}
		if err := os.Symlink(p.Workspace, p.Host); err != nil {
			_ = movePath(p.Workspace, p.Host)
			return link, err
		}
	}
	if err := wsconfig.AddLink(opts.ConfigPath, link); err != nil {
		_ = removePath(p.Host, p.Sudo)
		if p.Sudo {
			_ = sudoRename(p.Workspace, p.Host)
		} else {
			_ = movePath(p.Workspace, p.Host)
		}
		return link, err
	}
	return link, nil
}

// takeLive keeps the live file of a drifted entry: the old workspace copy goes to
// the trash, the live file moves into its place, and the host path becomes a link
// to it again. PlanAdd has already refused a directory, a shared copy and a link.
func takeLive(host, workspaceAbs string, sudo bool) error {
	// The old copy goes to the trash, never deleted. Without one, the live
	// file only needs its parent directories.
	if _, err := os.Lstat(workspaceAbs); err == nil {
		if err := trash.SoftDelete(workspaceAbs); err != nil {
			return fmt.Errorf("trash the old copy %s: %w", workspaceAbs, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(workspaceAbs), 0o755); err != nil {
		return err
	}
	if sudo {
		if err := sudoRename(host, workspaceAbs); err != nil {
			return err
		}
		return sudoSymlink(workspaceAbs, host)
	}
	if err := movePath(host, workspaceAbs); err != nil {
		return err
	}
	if err := os.Symlink(workspaceAbs, host); err != nil {
		// Put the live file back, so the host path is not left empty.
		_ = movePath(workspaceAbs, host)
		return err
	}
	return nil
}

// ── Unlink ─────────────────────────────────────────────────────────

// Unlink modes: how the entry's content goes back to the system path.
const (
	ModeMove    = "move"    // an owned copy moves back
	ModeCopy    = "copy"    // a shared copy is copied; the original stays
	ModeMissing = "missing" // the copy is gone; nothing to restore
)

// UnlinkPlan says what Unlink will do to one entry, so the plan line can name
// it before anything changes.
type UnlinkPlan struct {
	Link      wsconfig.Link
	Workspace string // absolute path of the workspace copy
	Mode      string // ModeMove, ModeCopy or ModeMissing
	// Backup is where the live file at the system path moves first, when
	// something other than the entry's own symlink stands there. Empty when not.
	Backup string
}

// PlanUnlink inspects an entry. It changes nothing.
func PlanUnlink(workspacePath string, cfg wsconfig.WsConfig, link wsconfig.Link) UnlinkPlan {
	p := UnlinkPlan{Link: link, Workspace: WorkspaceCopy(workspacePath, link)}
	switch _, err := os.Lstat(p.Workspace); {
	case err != nil:
		p.Mode = ModeMissing
	case IsShared(cfg, link):
		p.Mode = ModeCopy
	default:
		p.Mode = ModeMove
	}
	if p.Mode != ModeMissing {
		if _, err := os.Lstat(link.System); err == nil && !ownLink(link.System, p.Workspace) {
			p.Backup = freeBackupPath(link.System)
		}
	}
	return p
}

// UnlinkOptions configures Unlink.
type UnlinkOptions struct {
	WorkspacePath string
	ConfigPath    string
	SystemPath    string
}

// Unlink puts a real file back at the system path and stops managing it:
//
//  1. Anything at the system path other than the entry's own symlink (a real
//     file that replaced the link, or a symlink elsewhere) is renamed to
//     .ws-backup first, never deleted.
//  2. The entry's symlink is removed.
//  3. An owned copy moves back (directories it leaves empty under
//     symlinks/ are removed). A shared copy is
//     copied, and the original stays in the workspace. With no copy, nothing is
//     restored and only a dangling symlink of the entry is removed.
//  4. The entry leaves ws.json.
func Unlink(opts UnlinkOptions) error {
	abs, err := expandPath(opts.SystemPath)
	if err != nil {
		return err
	}
	cfg, err := wsconfig.Load(opts.ConfigPath)
	if err != nil {
		return err
	}
	link, ok := FindLink(cfg, abs)
	if !ok {
		return fmt.Errorf("symlink not managed: %s", abs)
	}
	p := PlanUnlink(opts.WorkspacePath, cfg, link)

	if p.Mode == ModeMissing {
		if ownLink(abs, p.Workspace) {
			if err := removePath(abs, link.Sudo); err != nil {
				return err
			}
		}
		_, err := wsconfig.RemoveLink(opts.ConfigPath, abs)
		return err
	}

	if _, err := os.Lstat(abs); err == nil {
		if ownLink(abs, p.Workspace) {
			if err := removePath(abs, link.Sudo); err != nil {
				return err
			}
		} else if err := renamePath(abs, p.Backup, link.Sudo); err != nil {
			return fmt.Errorf("back up %s: %w", abs, err)
		}
	}

	if link.Sudo {
		if err := sudoMkdirAll(filepath.Dir(abs)); err != nil {
			return err
		}
	} else if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}

	if p.Mode == ModeCopy {
		var cerr error
		if link.Sudo {
			cerr = sudoCopy(p.Workspace, abs)
		} else {
			cerr = copyAny(p.Workspace, abs)
		}
		if cerr != nil {
			return fmt.Errorf("copy %s: %w", p.Workspace, cerr)
		}
	} else {
		var merr error
		if link.Sudo {
			merr = sudoRename(p.Workspace, abs)
		} else {
			merr = movePath(p.Workspace, abs)
		}
		if merr != nil {
			return fmt.Errorf("move %s back: %w", p.Workspace, merr)
		}
		pruneEmptyDirs(opts.WorkspacePath, filepath.Dir(p.Workspace))
	}

	_, err = wsconfig.RemoveLink(opts.ConfigPath, abs)
	return err
}

// pruneEmptyDirs removes empty directories from dir upwards, stopping at the
// owned root (symlinks), which stays.
func pruneEmptyDirs(workspacePath, dir string) {
	for {
		dir = filepath.Clean(dir)
		rel, ok := RelInWorkspace(workspacePath, dir)
		if !ok {
			return
		}
		root, owned := ownedRoot(rel + "/x")
		if !owned || rel == root {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// ── Link ───────────────────────────────────────────────────────────

// LinkPlan says what linking one host path will do. The workspace copy wins.
type LinkPlan struct {
	Link      wsconfig.Link
	Workspace string // absolute path of the workspace copy
	// Register is true for an orphan: the host path has no entry, but its mirror
	// path exists in the workspace, so the entry is recorded too.
	Register bool
	// Missing is true when the workspace copy is gone: nothing can be linked, and ws
	// leaves the host path alone.
	Missing bool
	// OK is true when the host path already is the entry's link.
	OK bool
	// Backup is where whatever stands at the host path moves first (a real file or
	// a link elsewhere). ws never overwrites it.
	Backup string
	// Drifted is true when a real file stands at the host path.
	Drifted bool
}

// PlanLink inspects one host path. It changes nothing. A path with an entry is
// repaired as recorded; one without is an orphan when its mirror path exists in the
// workspace, and is otherwise an error.
func PlanLink(workspacePath, configPath, hostPath string) (LinkPlan, error) {
	var p LinkPlan
	host, err := expandPath(hostPath)
	if err != nil {
		return p, err
	}
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		return p, err
	}
	link, ok := FindLink(cfg, host)
	if !ok {
		home, _ := os.UserHomeDir()
		mirror := MirrorPath(host, home)
		if mirror == "" {
			return p, fmt.Errorf("not a managed symlink: %s", host)
		}
		abs := filepath.Join(workspacePath, filepath.FromSlash(mirror))
		info, serr := os.Stat(abs)
		if serr != nil {
			return p, fmt.Errorf("not a managed symlink: %s (and %s is not in the workspace)", host, abs)
		}
		link = wsconfig.Link{System: host, Original: mirror, Dir: info.IsDir(), Sudo: NeedsSudo(host)}
		p.Register = true
	}
	p.Link = link
	p.Workspace = WorkspaceCopy(workspacePath, link)
	if _, err := os.Stat(p.Workspace); err != nil {
		p.Missing = true
		return p, nil
	}
	li, err := os.Lstat(link.System)
	if err != nil {
		return p, nil // nothing there: only the symlink is needed
	}
	if ownLink(link.System, p.Workspace) {
		p.OK = true
		return p, nil
	}
	p.Drifted = li.Mode()&os.ModeSymlink == 0
	p.Backup = freeBackupPath(link.System)
	return p, nil
}

// LinkOptions configures Link.
type LinkOptions struct {
	WorkspacePath string
	ConfigPath    string
	SystemPath    string
}

// Link points a host path at its workspace copy. The workspace copy wins: whatever
// else stands at the host path is kept as .ws-backup, never deleted. Missing parent
// directories are created. An orphan is registered as well. A missing workspace
// copy is an error.
func Link(opts LinkOptions) error {
	p, err := PlanLink(opts.WorkspacePath, opts.ConfigPath, opts.SystemPath)
	if err != nil {
		return err
	}
	if p.Missing {
		return fmt.Errorf("workspace copy %s is missing", p.Workspace)
	}
	if !p.OK {
		if err := linkOut(p.Link.System, p.Workspace, p.Backup, p.Link.Sudo); err != nil {
			return err
		}
	}
	if p.Register {
		if err := wsconfig.AddLink(opts.ConfigPath, p.Link); err != nil {
			return err
		}
	}
	return nil
}

// linkOut makes system a symlink to target, first renaming whatever is there
// to backup (when backup is not empty), and rolling that rename back when the
// symlink cannot be made.
func linkOut(system, target, backup string, sudo bool) error {
	if sudo {
		if err := sudoMkdirAll(filepath.Dir(system)); err != nil {
			return fmt.Errorf("create parent directory: %w", err)
		}
	} else if err := os.MkdirAll(filepath.Dir(system), 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	if backup != "" {
		if err := renamePath(system, backup, sudo); err != nil {
			return fmt.Errorf("back up %s: %w", system, err)
		}
	}
	var err error
	if sudo {
		err = sudoSymlink(target, system)
	} else {
		err = os.Symlink(target, system)
	}
	if err != nil {
		if backup != "" {
			_ = renamePath(backup, system, sudo)
		}
		return fmt.Errorf("create symlink: %w", err)
	}
	return nil
}

// ── Entries ────────────────────────────────────────────────────────

// List returns every entry in ws.json.
func List(configPath string) ([]wsconfig.Link, error) {
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		return nil, err
	}
	return cfg.Symlinks, nil
}

// ── File helpers ───────────────────────────────────────────────────

func expandPath(path string) (string, error) {
	return wsconfig.ExpandUserPath(path)
}

// removePath removes a path that is the entry's own symlink.
func removePath(path string, sudo bool) error {
	if sudo {
		return sudoRemoveAll(path)
	}
	return os.Remove(path)
}

// renamePath renames src to dst, with sudo for entries that need root.
func renamePath(src, dst string, sudo bool) error {
	if sudo {
		return sudoRename(src, dst)
	}
	return os.Rename(src, dst)
}

// movePath and copyAny are the shared mover (internal/fsutil).
func movePath(src, dst string) error { return fsutil.Move(src, dst) }

func copyAny(src, dst string) error { return fsutil.Copy(src, dst) }
