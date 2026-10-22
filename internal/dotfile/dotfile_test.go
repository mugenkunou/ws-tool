package dotfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// env is one isolated machine: a home directory, an XDG trash (every
// TakeLive goes through it, so it must never be the real one) and a workspace.
type env struct {
	home, workspace, config string
}

func setup(t *testing.T) env {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	root := filepath.Join(t.TempDir(), "Workspace")
	for _, d := range []string{"configs/dotfiles", "secrets/dotfiles", "agent"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := wsconfig.ConfigPath(root)
	if err := wsconfig.Save(cfg, wsconfig.Default()); err != nil {
		t.Fatal(err)
	}
	return env{home: home, workspace: root, config: cfg}
}

func (e env) write(t *testing.T, rel, content string) string {
	t.Helper()
	p := filepath.Join(e.home, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func (e env) add(t *testing.T, system string) wsconfig.Link {
	t.Helper()
	res, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: system})
	if err != nil {
		t.Fatalf("add %s: %v", system, err)
	}
	return res.Link
}

func (e env) links(t *testing.T) []wsconfig.Link {
	t.Helper()
	l, err := List(e.config)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func (e env) unlink(system string) error {
	return Unlink(UnlinkOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: system})
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func isSymlink(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// trashed lists the names in the isolated XDG trash.
func trashed(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(os.Getenv("XDG_DATA_HOME"), "Trash", "files"))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// ── Add ────────────────────────────────────────────────────────────

func TestAddMovesTheFileAndLinksBack(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "export A=1\n")
	link := e.add(t, sys)

	if link.Original != "configs/dotfiles/bashrc" || link.Dir || link.Sudo {
		t.Fatalf("link = %+v", link)
	}
	if !isSymlink(sys) {
		t.Fatal("the system path should be a symlink")
	}
	copyPath := filepath.Join(e.workspace, "configs", "dotfiles", "bashrc")
	if got := mustRead(t, copyPath); got != "export A=1\n" {
		t.Fatalf("workspace copy = %q", got)
	}
	if links := e.links(t); len(links) != 1 || links[0].System != sys {
		t.Fatalf("links = %+v", links)
	}
}

func TestAddPutsSecretsUnderSecrets(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".ssh/id_ed25519", "-----BEGIN OPENSSH PRIVATE KEY-----\n")
	link := e.add(t, sys)
	if link.Original != "secrets/dotfiles/ssh/id_ed25519" {
		t.Fatalf("a private key belongs under secrets/dotfiles, got %s", link.Original)
	}
}

func TestAddTo(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".gitconfig", "[user]\n")
	to := filepath.Join(e.workspace, "agent", "gitconfig")
	res, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys, To: to})
	if err != nil {
		t.Fatal(err)
	}
	if res.Link.Original != "agent/gitconfig" || mustRead(t, to) != "[user]\n" || !isSymlink(sys) {
		t.Fatalf("--to did not store the copy at the given path: %+v", res.Link)
	}

	other := e.write(t, ".other", "x")
	if _, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: other, To: to}); err == nil {
		t.Fatal("--to must refuse a path that exists")
	}
	if _, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: other, To: filepath.Join(t.TempDir(), "x")}); err == nil {
		t.Fatal("--to must lie inside the workspace")
	}
}

func TestAddRefusals(t *testing.T) {
	e := setup(t)
	add := func(sys string) error {
		_, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys})
		return err
	}

	if err := add(filepath.Join(e.home, ".missing")); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("a missing path must fail, got %v", err)
	}
	if err := add(e.home); err == nil || !strings.Contains(err.Error(), "home directory") {
		t.Fatalf("$HOME itself must be refused, got %v", err)
	}

	target := e.write(t, ".real", "x")
	link := filepath.Join(e.home, ".alias")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := add(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("a symlink must be refused (add the file it points to), got %v", err)
	}

	inside := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(inside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := add(inside); err == nil || !strings.Contains(err.Error(), "ws dotfile link") {
		t.Fatalf("a path inside the workspace must be refused with the link hint, got %v", err)
	}

	sys := e.write(t, ".taken", "x")
	if err := os.WriteFile(filepath.Join(e.workspace, "configs", "dotfiles", "taken"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := add(sys); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("a taken storage path must fail, got %v", err)
	}
	if mustRead(t, sys) != "x" {
		t.Fatal("a failed add must leave the live file alone")
	}
}

func TestAddDirWhole(t *testing.T) {
	e := setup(t)
	dir := filepath.Join(e.home, ".password-store")
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "site.gpg"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: dir}); err == nil {
		t.Fatal("a directory without Dir must be refused")
	}
	to := filepath.Join(e.workspace, "secrets", "dotfiles", "password-store")
	res, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: dir, Dir: true, To: to})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Link.Dir || res.Link.Original != "secrets/dotfiles/password-store" {
		t.Fatalf("link = %+v", res.Link)
	}
	if !exists(filepath.Join(to, "site.gpg")) || !exists(filepath.Join(to, ".git")) {
		t.Fatal("the directory and its history must move whole")
	}
	if dirs, _ := FindDirEntries(e.workspace, e.config); len(dirs) != 0 {
		t.Fatalf("migrate must leave whole-dir links alone: %+v", dirs)
	}

	file := e.write(t, ".vimrc", "set nu\n")
	if _, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: file, Dir: true}); err == nil {
		t.Fatal("Dir on a file must be refused")
	}
}

func TestStoragePath(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := []struct {
		system string
		secret bool
		want   string
	}{
		{filepath.Join(home, ".bashrc"), false, "configs/dotfiles/bashrc"},
		{filepath.Join(home, ".config/Code/User/settings.json"), false, "configs/dotfiles/config/Code/User/settings.json"},
		{filepath.Join(home, ".kube/config"), true, "secrets/dotfiles/kube/config"},
		{"/etc/docker/daemon.json", false, "configs/dotfiles/etc/docker/daemon.json"},
		{filepath.Join(home, "bin/tool"), false, "configs/dotfiles/bin/tool"},
	}
	t.Setenv("HOME", home)
	for _, c := range cases {
		got, err := StoragePath(c.system, c.secret)
		if err != nil || got != c.want {
			t.Errorf("StoragePath(%q, %v) = %q, %v; want %q", c.system, c.secret, got, err, c.want)
		}
	}
	if _, err := StoragePath(home, false); err == nil {
		t.Error("the home directory itself has no storage path")
	}
}

// ── Scan ───────────────────────────────────────────────────────────

func TestScanReportsBrokenAndOverwritten(t *testing.T) {
	e := setup(t)
	a := e.write(t, ".a", "a")
	b := e.write(t, ".b", "b")
	c := e.write(t, ".c", "c")
	d := e.write(t, ".d", "d")
	for _, p := range []string{a, b, c, d} {
		e.add(t, p)
	}
	if err := os.Remove(a); err != nil { // nothing at the system path
		t.Fatal(err)
	}
	if err := os.Remove(b); err != nil { // a real file replaced the link
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(e.workspace, "configs", "dotfiles", "c")); err != nil { // the copy is gone
		t.Fatal(err)
	}

	issues, err := Scan(ScanOptions{WorkspacePath: e.workspace, ConfigPath: e.config})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Issue{}
	for _, is := range issues {
		got[is.SystemPath] = is
	}
	if len(got) != 3 {
		t.Fatalf("issues = %+v", issues)
	}
	if got[a].Status != StatusBroken || got[a].Message != "system path is missing" {
		t.Errorf("a: %+v", got[a])
	}
	if got[b].Status != StatusOverwritten || got[b].Message != "system path is no longer a symlink" {
		t.Errorf("b: %+v", got[b])
	}
	if got[c].Status != StatusBroken || got[c].Message != "symlink target is missing" {
		t.Errorf("c: %+v", got[c])
	}
	if _, bad := got[d]; bad {
		t.Errorf("d is healthy: %+v", got[d])
	}
}

// ── TakeLive (add on an overwritten entry) ─────────────────────────

func overwrite(t *testing.T, system, content string) {
	t.Helper()
	if err := os.Remove(system); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(system, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTakeLiveKeepsTheLiveFile(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "old\n")
	link := e.add(t, sys)
	overwrite(t, sys, "live\n")

	if err := TakeLive(TakeLiveOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys}); err != nil {
		t.Fatal(err)
	}
	copyPath := WorkspaceCopy(e.workspace, link)
	if mustRead(t, copyPath) != "live\n" || !isSymlink(sys) || mustRead(t, sys) != "live\n" {
		t.Fatal("the live file must become the workspace copy, linked back")
	}
	if links := e.links(t); len(links) != 1 || links[0].Original != link.Original {
		t.Fatalf("the entry must keep its original: %+v", links)
	}
	if got := trashed(t); len(got) != 1 || got[0] != "bashrc" {
		t.Fatalf("the old copy goes to the trash, got %v", got)
	}
}

// TestTakeLiveOnAHealthyEntryKeepsTheOnlyCopy is plan 0002 bug B1: dotfile
// sync on a healthy link deleted the workspace copy and then moved the dangling
// symlink into the workspace.
func TestTakeLiveOnAHealthyEntryKeepsTheOnlyCopy(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "precious\n")
	link := e.add(t, sys)

	err := TakeLive(TakeLiveOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys})
	if err == nil {
		t.Fatal("taking the live file of an ok entry must fail")
	}
	if mustRead(t, WorkspaceCopy(e.workspace, link)) != "precious\n" || !isSymlink(sys) {
		t.Fatal("the only copy must be untouched")
	}
	if len(trashed(t)) != 0 {
		t.Fatalf("nothing must reach the trash, got %v", trashed(t))
	}
}

func TestTakeLiveKeepsTheSecretCopyUnderSecrets(t *testing.T) {
	// Plan 0002 bug B4: the location was detected again, so a secret with an
	// ordinary name moved from secrets/dotfiles/ into the pushed repository.
	e := setup(t)
	sys := e.write(t, ".netrc", "machine x password y\n")
	link, err := LinkTo(LinkToOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: filepath.Join(e.home, ".cfg"),
		To: filepath.Join(e.workspace, "secrets", "dotfiles", "token")})
	if err == nil {
		t.Fatalf("LinkTo needs an existing file, got link %+v", link)
	}

	if err := os.WriteFile(filepath.Join(e.workspace, "secrets", "dotfiles", "netrc"), []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sys); err != nil {
		t.Fatal(err)
	}
	if _, err := LinkTo(LinkToOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys,
		To: filepath.Join(e.workspace, "secrets", "dotfiles", "netrc")}); err != nil {
		t.Fatal(err)
	}
	overwrite(t, sys, "live\n")
	if err := TakeLive(TakeLiveOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys}); err != nil {
		t.Fatal(err)
	}
	if links := e.links(t); links[0].Original != "secrets/dotfiles/netrc" {
		t.Fatalf("the original must stay under secrets/dotfiles: %+v", links)
	}
	if mustRead(t, filepath.Join(e.workspace, "secrets", "dotfiles", "netrc")) != "live\n" {
		t.Fatal("the live file did not arrive")
	}
	if exists(filepath.Join(e.workspace, "configs", "dotfiles", "netrc")) {
		t.Fatal("a secret must never land in configs/dotfiles")
	}
}

func TestTakeLiveRefusals(t *testing.T) {
	e := setup(t)
	take := func(sys string) error {
		return TakeLive(TakeLiveOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys})
	}

	// A shared copy, made by link --to, is never replaced.
	agent := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(agent, []byte("shared\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(e.home, ".claude", "CLAUDE.md")
	if _, err := LinkTo(LinkToOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: shared, To: agent}); err != nil {
		t.Fatal(err)
	}
	overwrite(t, shared, "live\n")
	if err := take(shared); err == nil || !strings.Contains(err.Error(), "shared") {
		t.Fatalf("a shared copy must be refused, got %v", err)
	}
	if mustRead(t, agent) != "shared\n" {
		t.Fatal("the shared copy must be untouched")
	}

	// A symlink to somewhere else.
	sys := e.write(t, ".x", "x")
	e.add(t, sys)
	if err := os.Remove(sys); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, sys); err != nil {
		t.Fatal(err)
	}
	if err := take(sys); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("a foreign symlink must be refused, got %v", err)
	}

	// A directory at the system path.
	if err := os.Remove(sys); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sys, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := take(sys); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("a directory must be refused, got %v", err)
	}

	// Nothing at the system path.
	if err := os.Remove(sys); err != nil {
		t.Fatal(err)
	}
	if err := take(sys); err == nil || !strings.Contains(err.Error(), "ws dotfile link") {
		t.Fatalf("a missing system path hints at link, got %v", err)
	}
	if err := take(filepath.Join(e.home, ".unmanaged")); err == nil {
		t.Fatal("an unmanaged path must fail")
	}
}

// ── Unlink ─────────────────────────────────────────────────────────

func TestUnlinkMovesTheOwnedCopyBack(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".config/app/app.conf", "x=1\n")
	link := e.add(t, sys)

	if err := e.unlink(sys); err != nil {
		t.Fatal(err)
	}
	if isSymlink(sys) || mustRead(t, sys) != "x=1\n" {
		t.Fatal("a real file must be back at the system path")
	}
	if exists(WorkspaceCopy(e.workspace, link)) {
		t.Fatal("an owned copy moves back; it must not stay in the workspace")
	}
	if len(e.links(t)) != 0 {
		t.Fatal("the entry must leave ws.json")
	}
	// The directories the move left empty are pruned, the root stays.
	if exists(filepath.Join(e.workspace, "configs", "dotfiles", "config")) {
		t.Fatal("empty directories under configs/dotfiles must be removed")
	}
	if !exists(filepath.Join(e.workspace, "configs", "dotfiles")) {
		t.Fatal("configs/dotfiles itself must stay")
	}
}

func TestUnlinkPrunesOnlyEmptyDirs(t *testing.T) {
	e := setup(t)
	a := e.write(t, ".config/app/a.conf", "a")
	b := e.write(t, ".config/app/b.conf", "b")
	e.add(t, a)
	e.add(t, b)
	if err := e.unlink(a); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(e.workspace, "configs", "dotfiles", "config", "app", "b.conf")) {
		t.Fatal("the directory still holds b.conf and must stay")
	}
}

// TestUnlinkCopiesASharedOriginal is plan 0002 bug B2: rm and reset moved a
// link-made original (agent/AGENTS.md) out of the workspace.
func TestUnlinkCopiesASharedOriginal(t *testing.T) {
	e := setup(t)
	agent := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(agent, []byte("rules\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	sys := filepath.Join(e.home, ".claude", "CLAUDE.md")
	if _, err := LinkTo(LinkToOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys, To: agent}); err != nil {
		t.Fatal(err)
	}

	if err := e.unlink(sys); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, agent) != "rules\n" {
		t.Fatal("the shared original must stay in the workspace")
	}
	if isSymlink(sys) || mustRead(t, sys) != "rules\n" {
		t.Fatal("a copy must be put at the system path")
	}
	fi, _ := os.Stat(sys)
	if fi.Mode().Perm() != 0o640 {
		t.Fatalf("the copy keeps its mode, got %v", fi.Mode().Perm())
	}
	if len(e.links(t)) != 0 {
		t.Fatal("the entry must leave ws.json")
	}
}

func TestUnlinkCopiesWhenTwoEntriesShareACopy(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "x\n")
	link := e.add(t, sys)
	second := filepath.Join(e.home, ".profile")
	if _, err := LinkTo(LinkToOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: second, To: WorkspaceCopy(e.workspace, link)}); err != nil {
		t.Fatal(err)
	}
	if err := e.unlink(sys); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, WorkspaceCopy(e.workspace, link)) != "x\n" || mustRead(t, second) != "x\n" {
		t.Fatal("the other entry still points at the copy; it must stay")
	}
}

// TestUnlinkKeepsAnOverwritingFile is plan 0002 bug B3: rm and reset deleted
// the live file that replaced a link.
func TestUnlinkKeepsAnOverwritingFile(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "old\n")
	link := e.add(t, sys)
	overwrite(t, sys, "live work\n")

	if err := e.unlink(sys); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, sys) != "old\n" {
		t.Fatal("the workspace copy goes back to the system path")
	}
	if mustRead(t, sys+".ws-backup") != "live work\n" {
		t.Fatal("the live file must be kept as .ws-backup, never deleted")
	}
	if exists(WorkspaceCopy(e.workspace, link)) {
		t.Fatal("the copy moved back")
	}

	// A second unlink never overwrites an earlier backup.
	sys2 := e.write(t, ".zshrc", "old2\n")
	e.add(t, sys2)
	overwrite(t, sys2, "live2\n")
	if err := os.WriteFile(sys2+".ws-backup", []byte("earlier"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.unlink(sys2); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, sys2+".ws-backup") != "earlier" || mustRead(t, sys2+".ws-backup.1") != "live2\n" {
		t.Fatal("an earlier backup must survive; the new one takes the next free name")
	}
}

func TestUnlinkWithAMissingCopy(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "x\n")
	link := e.add(t, sys)
	if err := os.Remove(WorkspaceCopy(e.workspace, link)); err != nil {
		t.Fatal(err)
	}
	if err := e.unlink(sys); err != nil {
		t.Fatal(err)
	}
	if exists(sys) {
		t.Fatal("the dangling symlink of the entry must be removed")
	}
	if len(e.links(t)) != 0 {
		t.Fatal("the entry must be unregistered")
	}

	// A live file at the system path is never touched when the copy is gone.
	sys2 := e.write(t, ".zshrc", "y\n")
	link2 := e.add(t, sys2)
	if err := os.Remove(WorkspaceCopy(e.workspace, link2)); err != nil {
		t.Fatal(err)
	}
	overwrite(t, sys2, "live\n")
	if err := e.unlink(sys2); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, sys2) != "live\n" || exists(sys2+".ws-backup") {
		t.Fatal("with no copy there is nothing to restore; the live file stays where it is")
	}
}

func TestUnlinkDirectoryEntry(t *testing.T) {
	e := setup(t)
	dir := filepath.Join(e.home, ".password-store")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.gpg"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(e.workspace, "secrets", "dotfiles", "password-store")
	if _, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: dir, Dir: true, To: to}); err != nil {
		t.Fatal(err)
	}
	if err := e.unlink(dir); err != nil {
		t.Fatal(err)
	}
	if isSymlink(dir) || !exists(filepath.Join(dir, "a.gpg")) || exists(to) {
		t.Fatal("the directory must be back, whole")
	}
}

func TestPlanUnlinkNamesWhatWillHappen(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "x\n")
	link := e.add(t, sys)
	cfg, _ := wsconfig.Load(e.config)

	if p := PlanUnlink(e.workspace, cfg, link); p.Mode != ModeMove || p.Backup != "" {
		t.Fatalf("healthy owned entry: %+v", p)
	}
	overwrite(t, sys, "live\n")
	if p := PlanUnlink(e.workspace, cfg, link); p.Mode != ModeMove || p.Backup != sys+".ws-backup" {
		t.Fatalf("overwritten entry: %+v", p)
	}
	if err := os.Remove(WorkspaceCopy(e.workspace, link)); err != nil {
		t.Fatal(err)
	}
	if p := PlanUnlink(e.workspace, cfg, link); p.Mode != ModeMissing {
		t.Fatalf("missing copy: %+v", p)
	}
}

func TestReset(t *testing.T) {
	e := setup(t)
	a := e.write(t, ".a", "a")
	b := e.write(t, ".b", "b")
	e.add(t, a)
	e.add(t, b)
	res, err := Reset(ResetOptions{WorkspacePath: e.workspace, ConfigPath: e.config})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entries) != 2 || len(e.links(t)) != 0 || isSymlink(a) || isSymlink(b) {
		t.Fatalf("reset must unlink every entry: %+v", res)
	}
}

// ── Relink (link without --to) ─────────────────────────────────────

func TestRelinkCreatesTheSymlink(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".profile", "x\n")
	e.add(t, sys)
	if err := os.Remove(sys); err != nil {
		t.Fatal(err)
	}
	if err := Relink(RelinkOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys}); err != nil {
		t.Fatal(err)
	}
	if !isSymlink(sys) || mustRead(t, sys) != "x\n" {
		t.Fatal("the symlink must be back")
	}
}

func TestRelinkCreatesMissingParents(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".config/app/app.conf", "x\n")
	e.add(t, sys)
	if err := os.RemoveAll(filepath.Join(e.home, ".config")); err != nil {
		t.Fatal(err)
	}
	if err := Relink(RelinkOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys}); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, sys) != "x\n" {
		t.Fatal("parents must be created on a new machine")
	}
}

func TestRelinkBacksUpWhateverIsThere(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "workspace\n")
	e.add(t, sys)
	overwrite(t, sys, "live\n")

	p := PlanRelink(e.workspace, e.links(t)[0])
	if p.Backup != sys+".ws-backup" || p.OK || p.Missing {
		t.Fatalf("plan = %+v", p)
	}
	if err := Relink(RelinkOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys}); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, sys) != "workspace\n" || mustRead(t, sys+".ws-backup") != "live\n" {
		t.Fatal("the workspace copy wins; the live file is kept as .ws-backup")
	}

	// An earlier backup is kept.
	overwrite(t, sys, "live again\n")
	if err := Relink(RelinkOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys}); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, sys+".ws-backup") != "live\n" || mustRead(t, sys+".ws-backup.1") != "live again\n" {
		t.Fatal("never overwrite an earlier backup")
	}
}

func TestRelinkAMissingCopyIsAnError(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "x\n")
	link := e.add(t, sys)
	if err := os.Remove(WorkspaceCopy(e.workspace, link)); err != nil {
		t.Fatal(err)
	}
	if p := PlanRelink(e.workspace, link); !p.Missing {
		t.Fatalf("plan = %+v", p)
	}
	if err := Relink(RelinkOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("a missing copy cannot be linked, got %v", err)
	}
}

func TestRelinkOfAnOkEntryIsANoOp(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "x\n")
	link := e.add(t, sys)
	if p := PlanRelink(e.workspace, link); !p.OK {
		t.Fatalf("an ok entry has nothing to do: %+v", p)
	}
	if err := Relink(RelinkOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys}); err != nil {
		t.Fatal(err)
	}
	if exists(sys + ".ws-backup") {
		t.Fatal("an ok entry must not be backed up")
	}
}

// TestRelinkRecognisesARelativeSymlink is plan 0002 bug B5: the already-linked
// check compared the raw readlink target, so a relative target never matched.
func TestRelinkRecognisesARelativeSymlink(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "x\n")
	link := e.add(t, sys)
	if err := os.Remove(sys); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(filepath.Dir(sys), WorkspaceCopy(e.workspace, link))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rel, sys); err != nil {
		t.Fatal(err)
	}
	if p := PlanRelink(e.workspace, link); !p.OK {
		t.Fatalf("a relative symlink to the copy is already linked: %+v", p)
	}
}

// ── LinkTo (link --to) ─────────────────────────────────────────────

func TestLinkToManagesAnExistingWorkspaceFile(t *testing.T) {
	e := setup(t)
	agent := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(agent, []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sys := filepath.Join(e.home, ".claude", "CLAUDE.md")
	link, err := LinkTo(LinkToOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys, To: agent})
	if err != nil {
		t.Fatal(err)
	}
	if link.Original != "agent/AGENTS.md" || link.Dir {
		t.Fatalf("link = %+v", link)
	}
	if !isSymlink(sys) || mustRead(t, sys) != "rules\n" || mustRead(t, agent) != "rules\n" {
		t.Fatal("the symlink must point at the workspace file, which stays where it is")
	}
	if links := e.links(t); len(links) != 1 || links[0].Original != "agent/AGENTS.md" {
		t.Fatalf("links = %+v", links)
	}
}

func TestLinkToRenamesWhatIsThere(t *testing.T) {
	e := setup(t)
	agent := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(agent, []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sys := e.write(t, ".claude/CLAUDE.md", "my old notes\n")
	if _, err := LinkTo(LinkToOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys, To: agent}); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, sys) != "rules\n" || mustRead(t, sys+".ws-backup") != "my old notes\n" {
		t.Fatal("the workspace copy wins, and what stood there is kept as .ws-backup")
	}
}

func TestLinkToIsIdempotentWhenAlreadyLinked(t *testing.T) {
	e := setup(t)
	agent := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(agent, []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sys := filepath.Join(e.home, "CLAUDE.md")
	if err := os.Symlink(agent, sys); err != nil {
		t.Fatal(err)
	}
	p, err := PlanLinkTo(LinkToOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys, To: agent})
	if err != nil || !p.AlreadyLinked || p.Backup != "" {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	if _, err := LinkTo(LinkToOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys, To: agent}); err != nil {
		t.Fatal(err)
	}
	if exists(sys+".ws-backup") || len(e.links(t)) != 1 {
		t.Fatal("an already-correct symlink is only recorded")
	}
}

func TestLinkToRecordsADirectory(t *testing.T) {
	e := setup(t)
	if err := os.MkdirAll(filepath.Join(e.workspace, "secrets", "pass"), 0o700); err != nil {
		t.Fatal(err)
	}
	link, err := LinkTo(LinkToOptions{WorkspacePath: e.workspace, ConfigPath: e.config,
		SystemPath: filepath.Join(e.home, ".password-store"), To: filepath.Join(e.workspace, "secrets", "pass")})
	if err != nil {
		t.Fatal(err)
	}
	if !link.Dir {
		t.Fatalf("a directory original makes a directory entry: %+v", link)
	}
}

func TestLinkToRefusals(t *testing.T) {
	e := setup(t)
	agent := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(agent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sys := filepath.Join(e.home, "CLAUDE.md")
	plan := func(system, to string) error {
		_, err := PlanLinkTo(LinkToOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: system, To: to})
		return err
	}
	if err := plan(sys, filepath.Join(e.workspace, "agent", "nope.md")); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("a missing workspace file, got %v", err)
	}
	outside := e.write(t, "elsewhere.md", "x")
	if err := plan(sys, outside); err == nil || !strings.Contains(err.Error(), "inside the workspace") {
		t.Fatalf("a file outside the workspace, got %v", err)
	}
	if _, err := LinkTo(LinkToOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys, To: agent}); err != nil {
		t.Fatal(err)
	}
	if err := plan(sys, agent); err == nil || !strings.Contains(err.Error(), "already managed") {
		t.Fatalf("an existing entry, got %v", err)
	}
}

// ── classification ─────────────────────────────────────────────────

func TestClassifyGPGAsSecret(t *testing.T) {
	f := filepath.Join(t.TempDir(), "site.gpg")
	if err := os.WriteFile(f, []byte{0x85, 0x02, 0x0c, 0x00}, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if got := ClassifyFile(f); got != FileClassSecret {
		t.Fatalf("expected a .gpg file to be a secret, got %s", got)
	}
}

func TestLooksLikeSecretJudgesDirectoriesByName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "id_rsa")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !LooksLikeSecret(dir, true) {
		t.Fatal("a directory is judged by its name")
	}
	plain := filepath.Join(t.TempDir(), "themes")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if LooksLikeSecret(plain, true) {
		t.Fatal("themes is not a secret")
	}
}
