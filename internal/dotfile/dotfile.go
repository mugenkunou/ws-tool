// Package dotfile manages system files whose originals live in the workspace
// and are symlinked back (spec §5 dotfile).
//
// Two directions decide which copy wins when both exist:
//
//   - Add and TakeLive: the live file goes into the workspace.
//   - Relink and LinkTo: the workspace copy goes out to the system path.
//   - Unlink reverses both.
//
// A workspace copy under configs/dotfiles/ or secrets/dotfiles/ that no other
// entry points at is owned: ws made it and may move or replace it. Any other
// workspace copy is shared, and ws never moves or deletes it.
package dotfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mugenkunou/ws-tool/internal/trash"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

const (
	// ConfigsRoot and SecretsRoot are the workspace-relative directories ws
	// stores the copies it makes in. A file that looks like a secret goes to
	// SecretsRoot, so it is not part of the pushed backup repository.
	ConfigsRoot = "configs/dotfiles"
	SecretsRoot = "secrets/dotfiles"

	StatusBroken      = "BROKEN"
	StatusOverwritten = "OVERWRITTEN"
)

// ownedRoots are the directories whose files ws may move and replace.
var ownedRoots = []string{ConfigsRoot, SecretsRoot}

// ── Options and results ────────────────────────────────────────────

// AddOptions configures Add.
type AddOptions struct {
	WorkspacePath string
	ConfigPath    string
	SystemPath    string
	// To is the absolute path inside the workspace to store the copy at. Empty
	// means the storage path (StoragePath).
	To string
	// Dir takes a directory whole, as one entry (e.g. ~/.password-store).
	Dir bool
}

// AddResult is returned by Add.
type AddResult struct {
	Link wsconfig.Link `json:"link"`
}

// Issue is one unhealthy entry, as Scan reports it.
type Issue struct {
	SystemPath    string `json:"system_path"`
	WorkspacePath string `json:"workspace_path"` // absolute path of the workspace copy
	Status        string `json:"status"`
	Message       string `json:"message"`
}

// ScanOptions configures Scan.
type ScanOptions struct {
	WorkspacePath string
	ConfigPath    string
}

// ── Paths ──────────────────────────────────────────────────────────

// LinkDotfilePath returns the workspace-relative path of a link's original.
func LinkDotfilePath(link wsconfig.Link) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(link.Original)))
}

// WorkspaceCopy is the absolute path of an entry's workspace copy.
func WorkspaceCopy(workspacePath string, link wsconfig.Link) string {
	return filepath.Clean(filepath.Join(workspacePath, filepath.FromSlash(LinkDotfilePath(link))))
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
// outside configs/dotfiles/ and secrets/dotfiles/ (a `link --to` target such as
// agent/AGENTS.md), or another entry points at the same copy. ws never moves
// or deletes a shared copy.
func IsShared(cfg wsconfig.WsConfig, link wsconfig.Link) bool {
	rel := LinkDotfilePath(link)
	if _, owned := ownedRoot(rel); !owned {
		return true
	}
	for _, other := range cfg.Links {
		if filepath.Clean(other.System) != filepath.Clean(link.System) && LinkDotfilePath(other) == rel {
			return true
		}
	}
	return false
}

// StoragePath is where Add puts a system path inside the workspace,
// workspace-relative. ws takes the path relative to $HOME (or to / outside
// home), drops the leading dot of its first component (a bare "." becomes
// "dot"), and puts it under secrets/dotfiles/ for a secret, else under
// configs/dotfiles/.
func StoragePath(systemPath string, secret bool) (string, error) {
	home, _ := os.UserHomeDir()
	cleanSystem := filepath.Clean(systemPath)

	var rel string
	if home != "" && (cleanSystem == home || strings.HasPrefix(cleanSystem, home+string(os.PathSeparator))) {
		rel = strings.TrimPrefix(cleanSystem, home+string(os.PathSeparator))
		if rel == "" || cleanSystem == home {
			return "", errors.New("cannot register the home directory as a dotfile")
		}
	} else {
		rel = strings.TrimPrefix(cleanSystem, string(os.PathSeparator))
	}

	parts := strings.Split(rel, string(os.PathSeparator))
	if after, ok := strings.CutPrefix(parts[0], "."); ok {
		if after == "" {
			after = "dot"
		}
		parts[0] = after
	}
	rel = filepath.Clean(filepath.Join(parts...))
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid relative path: %s", rel)
	}

	root := ConfigsRoot
	if secret {
		root = SecretsRoot
	}
	return filepath.ToSlash(filepath.Join(root, rel)), nil
}

// LooksLikeSecret reports whether a system path should be stored under
// secrets/dotfiles/. A file is judged by its name and its first bytes; a whole
// directory (--dir) by its name alone.
func LooksLikeSecret(abs string, isDir bool) bool {
	if isDir {
		return nameLooksSecret(strings.ToLower(filepath.Base(abs)))
	}
	return ClassifyFile(abs) == FileClassSecret
}

// FindLink returns the entry whose system path is abs.
func FindLink(cfg wsconfig.WsConfig, abs string) (wsconfig.Link, bool) {
	abs = filepath.Clean(abs)
	for _, l := range cfg.Links {
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

// Add takes the live file or directory at SystemPath into the workspace and
// leaves a symlink behind, then records the entry. It refuses a path that
// does not exist, that is $HOME, that is itself a symlink (add the file it
// points to), or that lies inside the workspace (to link a file the workspace
// already has, use LinkTo), and a destination that already exists.
func Add(opts AddOptions) (AddResult, error) {
	var result AddResult

	abs, err := expandPath(opts.SystemPath)
	if err != nil {
		return result, err
	}
	li, err := os.Lstat(abs)
	if err != nil {
		return result, fmt.Errorf("dotfile path not found: %s", abs)
	}
	if li.Mode()&os.ModeSymlink != 0 {
		return result, fmt.Errorf("%s is a symlink; add the file it points to instead", abs)
	}
	if home, herr := os.UserHomeDir(); herr == nil && filepath.Clean(home) == abs {
		return result, errors.New("cannot register the home directory as a dotfile")
	}
	if _, in := RelInWorkspace(opts.WorkspacePath, abs); in {
		return result, fmt.Errorf("%s lies inside the workspace; to link a file the workspace already has, use ws dotfile link %s --to <ws-path>", abs, abs)
	}

	isDir := li.IsDir()
	if isDir && !opts.Dir {
		return result, errors.New("cannot add a directory as a single file: pass --dir to link it whole, or let 'ws dotfile add <dir>' select files")
	}
	if opts.Dir && !isDir {
		return result, fmt.Errorf("not a directory: %s", abs)
	}

	cfg, err := wsconfig.Load(opts.ConfigPath)
	if err != nil {
		return result, err
	}
	if _, managed := FindLink(cfg, abs); managed {
		return result, fmt.Errorf("dotfile already managed: %s", abs)
	}

	var rel string
	if opts.To != "" {
		to := filepath.Clean(opts.To)
		var in bool
		if rel, in = RelInWorkspace(opts.WorkspacePath, to); !in {
			return result, fmt.Errorf("--to %s must lie inside the workspace %s", to, opts.WorkspacePath)
		}
	} else if rel, err = StoragePath(abs, LooksLikeSecret(abs, isDir)); err != nil {
		return result, err
	}
	workspaceAbs := filepath.Join(opts.WorkspacePath, filepath.FromSlash(rel))
	if _, err := os.Lstat(workspaceAbs); err == nil {
		return result, fmt.Errorf("workspace dotfile target already exists: %s", workspaceAbs)
	}

	link := wsconfig.Link{System: abs, Original: rel, Dir: opts.Dir, Sudo: NeedsSudo(abs)}
	result.Link = link

	if err := os.MkdirAll(filepath.Dir(workspaceAbs), 0o755); err != nil {
		return result, err
	}
	if link.Sudo {
		if err := sudoRename(abs, workspaceAbs); err != nil {
			return result, err
		}
		if err := sudoSymlink(workspaceAbs, abs); err != nil {
			return result, err
		}
	} else {
		if err := movePath(abs, workspaceAbs); err != nil {
			return result, err
		}
		if err := os.Symlink(workspaceAbs, abs); err != nil {
			return result, err
		}
	}
	if err := wsconfig.AddLink(opts.ConfigPath, link); err != nil {
		return result, err
	}
	return result, nil
}

// ── TakeLive (add on an overwritten entry) ─────────────────────────

// TakeLiveOptions configures TakeLive.
type TakeLiveOptions struct {
	WorkspacePath string
	ConfigPath    string
	SystemPath    string
}

// TakeLive keeps the live file of an overwritten entry: the old workspace copy
// goes to the trash, the live file moves into its place keeping the entry's
// original, and the system path becomes a symlink to it again.
//
// It fails, and changes nothing, when the system path is a symlink (to
// somewhere else, or already to the copy), a directory, or when the entry is a
// directory entry or its copy is shared (made by LinkTo, or pointed at by two
// entries): the copy would be lost, or someone else's file overwritten.
func TakeLive(opts TakeLiveOptions) error {
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
		return fmt.Errorf("dotfile not managed: %s", abs)
	}
	workspaceAbs := WorkspaceCopy(opts.WorkspacePath, link)

	if link.Dir {
		return fmt.Errorf("%s is a directory entry; taking the live file works on files only", abs)
	}
	li, err := os.Lstat(abs)
	switch {
	case err != nil:
		return fmt.Errorf("nothing at %s to take; ws dotfile link %s restores the link", abs, abs)
	case li.Mode()&os.ModeSymlink != 0 && ownLink(abs, workspaceAbs):
		return fmt.Errorf("%s is already linked to its workspace copy; nothing to take", abs)
	case li.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("%s is a symlink to somewhere else; ws dotfile link %s replaces it, or replace it with a file first", abs, abs)
	case li.IsDir():
		return fmt.Errorf("%s is a directory; taking the live file works on files only", abs)
	}
	if IsShared(cfg, link) {
		return fmt.Errorf("%s is a shared copy (made by ws dotfile link --to); copy the live file over it yourself, then run ws dotfile link", workspaceAbs)
	}

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
	if link.Sudo {
		if err := sudoRename(abs, workspaceAbs); err != nil {
			return err
		}
		return sudoSymlink(workspaceAbs, abs)
	}
	if err := movePath(abs, workspaceAbs); err != nil {
		return err
	}
	if err := os.Symlink(workspaceAbs, abs); err != nil {
		// Put the live file back, so the system path is not left empty.
		_ = movePath(workspaceAbs, abs)
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
//     configs/dotfiles/ and secrets/dotfiles/ are removed). A shared copy is
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
		return fmt.Errorf("dotfile not managed: %s", abs)
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
// owned root (configs/dotfiles, secrets/dotfiles), which stays.
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

// ── Relink (link without --to) ─────────────────────────────────────

// RelinkPlan says what repairing one entry will do.
type RelinkPlan struct {
	Link      wsconfig.Link
	Workspace string // absolute path of the workspace copy
	// Missing is true when the workspace copy is gone: the entry cannot be
	// linked, and ws leaves its system path alone.
	Missing bool
	// OK is true when the system path already is the entry's symlink.
	OK bool
	// Backup is where whatever stands at the system path moves first.
	Backup string
}

// PlanRelink inspects an entry. It changes nothing.
func PlanRelink(workspacePath string, link wsconfig.Link) RelinkPlan {
	p := RelinkPlan{Link: link, Workspace: WorkspaceCopy(workspacePath, link)}
	if _, err := os.Stat(p.Workspace); err != nil {
		p.Missing = true
		return p
	}
	if _, err := os.Lstat(link.System); err != nil {
		return p // nothing there: only the symlink is needed
	}
	if ownLink(link.System, p.Workspace) {
		p.OK = true
		return p
	}
	p.Backup = freeBackupPath(link.System)
	return p
}

// RelinkOptions configures Relink.
type RelinkOptions struct {
	WorkspacePath string
	ConfigPath    string
	SystemPath    string
}

// Relink points an entry's system path at its workspace copy. The workspace
// copy wins: whatever else stands at the system path is kept as .ws-backup,
// never deleted. Missing parent directories are created. An entry whose copy is
// missing is an error.
func Relink(opts RelinkOptions) error {
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
		return fmt.Errorf("dotfile not managed: %s", abs)
	}
	p := PlanRelink(opts.WorkspacePath, link)
	if p.Missing {
		return fmt.Errorf("workspace copy %s is missing", p.Workspace)
	}
	if p.OK {
		return nil
	}
	return linkOut(abs, p.Workspace, p.Backup, link.Sudo)
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

// ── LinkTo (link --to) ─────────────────────────────────────────────

// LinkToOptions configures PlanLinkTo and LinkTo.
type LinkToOptions struct {
	WorkspacePath string
	ConfigPath    string
	SystemPath    string
	// To is the absolute path of the file or directory in the workspace that
	// the system path becomes a link to. It must already exist.
	To string
}

// LinkToPlan says what LinkTo will do.
type LinkToPlan struct {
	System    string
	Workspace string // absolute path of the workspace file
	Original  string // workspace-relative, as stored
	Dir       bool
	Sudo      bool
	// AlreadyLinked is true when the system path already is a symlink to the
	// workspace file, so only the entry is recorded.
	AlreadyLinked bool
	// Backup is where whatever stands at the system path moves first.
	Backup string
}

// PlanLinkTo validates a `link --to` and says what it will do. It changes
// nothing.
func PlanLinkTo(opts LinkToOptions) (LinkToPlan, error) {
	var p LinkToPlan
	abs, err := expandPath(opts.SystemPath)
	if err != nil {
		return p, err
	}
	to := filepath.Clean(opts.To)
	rel, in := RelInWorkspace(opts.WorkspacePath, to)
	if !in {
		return p, fmt.Errorf("--to %s must lie inside the workspace %s", to, opts.WorkspacePath)
	}
	info, err := os.Stat(to)
	if err != nil {
		return p, fmt.Errorf("workspace original not found: %s", to)
	}
	cfg, err := wsconfig.Load(opts.ConfigPath)
	if err != nil {
		return p, err
	}
	if _, managed := FindLink(cfg, abs); managed {
		return p, fmt.Errorf("dotfile already managed: %s (ws dotfile link %s repairs it)", abs, abs)
	}

	p = LinkToPlan{System: abs, Workspace: to, Original: rel, Dir: info.IsDir(), Sudo: NeedsSudo(abs)}
	if _, err := os.Lstat(abs); err == nil {
		if ownLink(abs, to) {
			p.AlreadyLinked = true
		} else {
			p.Backup = freeBackupPath(abs)
		}
	}
	return p, nil
}

// LinkTo manages SystemPath as a symlink to a file already in the workspace
// and records the entry. Nothing moves into the workspace. Whatever else
// stands at the system path is kept as .ws-backup. A system path that needs
// root is linked with sudo.
func LinkTo(opts LinkToOptions) (wsconfig.Link, error) {
	p, err := PlanLinkTo(opts)
	if err != nil {
		return wsconfig.Link{}, err
	}
	link := wsconfig.Link{System: p.System, Original: p.Original, Dir: p.Dir, Sudo: p.Sudo}
	if !p.AlreadyLinked {
		if err := linkOut(p.System, p.Workspace, p.Backup, p.Sudo); err != nil {
			return link, err
		}
	}
	return link, wsconfig.AddLink(opts.ConfigPath, link)
}

// ── Scan ───────────────────────────────────────────────────────────

// List returns every entry in ws.json.
func List(configPath string) ([]wsconfig.Link, error) {
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		return nil, err
	}
	return cfg.Links, nil
}

// Scan checks every entry's symlink and reports the unhealthy ones. It reads
// only lstat and readlink, so it never needs sudo.
func Scan(opts ScanOptions) ([]Issue, error) {
	cfg, err := wsconfig.Load(opts.ConfigPath)
	if err != nil {
		return nil, err
	}

	issues := make([]Issue, 0)
	for _, link := range cfg.Links {
		expected := WorkspaceCopy(opts.WorkspacePath, link)
		issue := func(status, msg string) Issue {
			return Issue{SystemPath: link.System, WorkspacePath: expected, Status: status, Message: msg}
		}

		entry, err := os.Lstat(link.System)
		if err != nil {
			issues = append(issues, issue(StatusBroken, "system path is missing"))
			continue
		}
		if entry.Mode()&os.ModeSymlink == 0 {
			issues = append(issues, issue(StatusOverwritten, "system path is no longer a symlink"))
			continue
		}
		target, err := os.Readlink(link.System)
		if err != nil {
			issues = append(issues, issue(StatusBroken, "failed to read symlink target"))
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(link.System), target)
		}
		if filepath.Clean(target) != expected {
			issues = append(issues, issue(StatusOverwritten, "symlink points to an unexpected target"))
			continue
		}
		if _, err := os.Stat(expected); err != nil {
			issues = append(issues, issue(StatusBroken, "symlink target is missing"))
		}
	}
	return issues, nil
}

// FindDirEntries returns all link entries whose workspace copy is a directory
// and that are not a deliberate directory entry. These are candidates for
// migration to file-level entries.
func FindDirEntries(workspacePath, configPath string) ([]wsconfig.Link, error) {
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		return nil, err
	}
	var dirs []wsconfig.Link
	for _, l := range cfg.Links {
		info, err := os.Stat(WorkspaceCopy(workspacePath, l))
		// A deliberate directory link (Dir) stays whole; only legacy ones migrate.
		if err == nil && info.IsDir() && !l.Dir {
			dirs = append(dirs, l)
		}
	}
	return dirs, nil
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

func movePath(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) && errors.Is(linkErr.Err, syscall.EXDEV) {
		if err := copyAny(src, dst); err != nil {
			return err
		}
		return os.RemoveAll(src)
	}
	return err
}

func copyAny(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}

	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyAny(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
