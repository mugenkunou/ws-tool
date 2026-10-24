package symlink

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
	for _, d := range []string{"symlinks", "agent"} {
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
	l, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: system})
	if err != nil {
		t.Fatalf("add %s: %v", system, err)
	}
	return l
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

// overwrite replaces whatever stands at system (a link) with a real file, as a
// tool that rewrites its config does.
func overwrite(t *testing.T, system, content string) {
	t.Helper()
	if err := os.Remove(system); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(system, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
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

// ── MirrorPath storage ─────────────────────────────────────────────

func TestAddMovesTheFileAndLinksBackAtTheMirrorPath(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "export A=1\n")
	link := e.add(t, sys)

	if link.Original != "symlinks/home/.bashrc" || link.Dir || link.Sudo {
		t.Fatalf("link = %+v", link)
	}
	if !isSymlink(sys) {
		t.Fatal("the host path should be a symlink")
	}
	if got := mustRead(t, filepath.Join(e.workspace, "symlinks", "home", ".bashrc")); got != "export A=1\n" {
		t.Fatalf("workspace copy = %q", got)
	}
	if links := e.links(t); len(links) != 1 || links[0].System != sys {
		t.Fatalf("links = %+v", links)
	}
}

func TestAddNeedsNoSecretDecision(t *testing.T) {
	e := setup(t)
	link := e.add(t, e.write(t, ".ssh/id_ed25519", "-----BEGIN OPENSSH PRIVATE KEY-----\n"))
	if link.Original != "symlinks/home/.ssh/id_ed25519" {
		t.Fatalf("a private key lives in the same tree as everything else, got %s", link.Original)
	}
}

// ── add: one test per row of the case table (plan 0003 §3) ─────────

func plan(e env, host, to string, dir bool) (AddPlan, error) {
	return PlanAdd(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: host, To: to, Dir: dir})
}

func TestAddCaseRealAndFreeMovesAndLinks(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".gitconfig", "[user]\n")
	p, err := plan(e, sys, "", false)
	if err != nil || p.Case != CaseMove || p.Workspace != filepath.Join(e.workspace, "symlinks", "home", ".gitconfig") {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	// PlanAdd moves nothing.
	if isSymlink(sys) || exists(p.Workspace) {
		t.Fatal("planning must change nothing")
	}
	to := filepath.Join(e.workspace, "agent", "gitconfig")
	l, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: sys, To: to})
	if err != nil || l.Original != "agent/gitconfig" || mustRead(t, to) != "[user]\n" || !isSymlink(sys) {
		t.Fatalf("a second path chooses the storage place: %+v %v", l, err)
	}
}

func TestAddCaseDriftedTakesTheLiveFile(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "old\n")
	link := e.add(t, sys)
	overwrite(t, sys, "live\n")

	p, err := plan(e, sys, "", false)
	if err != nil || p.Case != CaseTake {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	if _, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: sys}); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, WorkspaceCopy(e.workspace, link)) != "live\n" || !isSymlink(sys) || mustRead(t, sys) != "live\n" {
		t.Fatal("the live file must become the workspace copy, linked back")
	}
	if got := trashed(t); len(got) != 1 || got[0] != ".bashrc" {
		t.Fatalf("the old copy goes to the trash, got %v", got)
	}
	if links := e.links(t); len(links) != 1 || links[0].Original != link.Original {
		t.Fatalf("the entry keeps its original: %+v", links)
	}
	// The entry's own workspace path is accepted as the second path.
	overwrite(t, sys, "again\n")
	if _, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: sys, To: WorkspaceCopy(e.workspace, link)}); err != nil {
		t.Fatal(err)
	}
	// Another one is not.
	overwrite(t, sys, "third\n")
	if _, err := plan(e, sys, filepath.Join(e.workspace, "agent", "x"), false); err == nil {
		t.Fatal("a drifted entry keeps its own path")
	}
}

func TestAddDriftedHealthyAndBrokenEntries(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "precious\n")
	link := e.add(t, sys)
	// Healthy: nothing to take, and the only copy survives (plan 0002 bug B1).
	if p, err := plan(e, sys, "", false); err != nil || p.Case != CaseManaged {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	if _, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: sys}); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, WorkspaceCopy(e.workspace, link)) != "precious\n" || !isSymlink(sys) || len(trashed(t)) != 0 {
		t.Fatal("the only copy must be untouched")
	}
	// Host missing: nothing to take; link restores it.
	if err := os.Remove(sys); err != nil {
		t.Fatal(err)
	}
	if p, err := plan(e, sys, "", false); err != nil || p.Case != CaseNothing || !strings.Contains(p.Note, "ws symlink link") {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	// A link elsewhere: nothing to take.
	other := e.write(t, "other", "o")
	if err := os.Symlink(other, sys); err != nil {
		t.Fatal(err)
	}
	if p, err := plan(e, sys, "", false); err != nil || p.Case != CaseNothing {
		t.Fatalf("plan = %+v, %v", p, err)
	}
}

func TestAddCaseDestinationExists(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".taken", "x")
	dest := filepath.Join(e.workspace, "symlinks", "home", ".taken")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: sys})
	if err == nil || !strings.Contains(err.Error(), "destination exists") || !strings.Contains(err.Error(), "remove it or choose another") {
		t.Fatalf("got %v", err)
	}
	if mustRead(t, sys) != "x" || isSymlink(sys) || mustRead(t, dest) != "old" {
		t.Fatal("a refused add changes nothing")
	}
	// Another path for the same host is fine.
	if _, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: sys, To: filepath.Join(e.workspace, "agent", "taken")}); err != nil {
		t.Fatal(err)
	}
}

func TestAddCaseUntrackedLinkRegistersAndMovesNothing(t *testing.T) {
	e := setup(t)
	target := filepath.Join(e.workspace, "data", "bruno", "env.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(e.home, "tools", "env.json")
	if err := os.MkdirAll(filepath.Dir(host), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, host); err != nil {
		t.Fatal(err)
	}
	p, err := plan(e, host, "", false)
	if err != nil || p.Case != CaseRegister || p.Workspace != target || p.Original != "data/bruno/env.json" {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	// The same target as the second path is fine; another one is an error.
	if _, err := plan(e, host, target, false); err != nil {
		t.Fatalf("the link's own target: %v", err)
	}
	if _, err := plan(e, host, filepath.Join(e.workspace, "agent", "other"), false); err == nil {
		t.Fatal("a second path that differs from the link target is an error")
	}
	l, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: host})
	if err != nil || l.Original != "data/bruno/env.json" {
		t.Fatalf("%+v %v", l, err)
	}
	if mustRead(t, target) != "{}" || !isSymlink(host) || exists(filepath.Join(e.workspace, "symlinks")) && len(trashed(t)) != 0 {
		t.Fatal("nothing moves")
	}
	if links := e.links(t); len(links) != 1 || links[0].System != host {
		t.Fatalf("links = %+v", links)
	}
}

func TestAddCaseLinkOutsideTheWorkspaceIsAnError(t *testing.T) {
	e := setup(t)
	real := e.write(t, "elsewhere", "x")
	host := filepath.Join(e.home, ".alias")
	if err := os.Symlink(real, host); err != nil {
		t.Fatal(err)
	}
	for _, to := range []string{"", filepath.Join(e.workspace, "agent", "x")} {
		if _, err := plan(e, host, to, false); err == nil || !strings.Contains(err.Error(), "link to somewhere else") {
			t.Fatalf("to=%q: %v", to, err)
		}
	}
}

func TestAddCaseMissingHostLinksOutAndRegisters(t *testing.T) {
	e := setup(t)
	agent := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(agent, []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(e.home, ".claude", "CLAUDE.md")
	p, err := plan(e, host, agent, false)
	if err != nil || p.Case != CaseLinkOut {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	l, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: host, To: agent})
	if err != nil || l.Original != "agent/AGENTS.md" {
		t.Fatalf("%+v %v", l, err)
	}
	if !isSymlink(host) || mustRead(t, host) != "rules\n" || mustRead(t, agent) != "rules\n" {
		t.Fatal("the host links to the workspace file, which stays where it is")
	}
	// A missing host whose mirror path exists links out too.
	e.write(t, ".zshrc", "z\n")
	mirror := e.add(t, filepath.Join(e.home, ".zshrc"))
	if _, err := wsconfig.RemoveLink(e.config, mirror.System); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(mirror.System); err != nil {
		t.Fatal(err)
	}
	l, err = Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: mirror.System})
	if err != nil || l.Original != "symlinks/home/.zshrc" {
		t.Fatalf("%+v %v", l, err)
	}
	if !isSymlink(mirror.System) || len(e.links(t)) != 2 {
		t.Fatal("the host is linked out and the entry registered")
	}
}

func TestAddCaseMissingAndMissing(t *testing.T) {
	e := setup(t)
	_, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: filepath.Join(e.home, ".nothing")})
	if err == nil || !strings.Contains(err.Error(), "nothing to add") {
		t.Fatalf("got %v", err)
	}
}

func TestAddRefusals(t *testing.T) {
	e := setup(t)
	if _, err := plan(e, e.home, "", false); err == nil || !strings.Contains(err.Error(), "home directory") {
		t.Fatalf("$HOME itself is refused, got %v", err)
	}
	f := e.write(t, ".vimrc", "set nu\n")
	if _, err := plan(e, f, "", true); err == nil || !strings.Contains(err.Error(), "--dir needs a directory") {
		t.Fatalf("Dir on a file: %v", err)
	}
	dir := filepath.Join(e.home, ".config", "x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := plan(e, dir, "", false); err == nil || !strings.Contains(err.Error(), "--dir") {
		t.Fatalf("a folder without Dir: %v", err)
	}
	inside := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(inside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := plan(e, inside, "", false); err == nil || !strings.Contains(err.Error(), "inside the workspace") {
		t.Fatalf("a real file inside the workspace: %v", err)
	}
	// The workspace path: inside the workspace, not in .git, not an entry's original.
	if _, err := plan(e, f, filepath.Join(t.TempDir(), "x"), false); err == nil || !strings.Contains(err.Error(), "inside the workspace") {
		t.Fatalf("outside the workspace: %v", err)
	}
	if _, err := plan(e, f, filepath.Join(e.workspace, ".git", "x"), false); err == nil || !strings.Contains(err.Error(), ".git") {
		t.Fatalf("inside .git: %v", err)
	}
	taken := e.add(t, e.write(t, ".other", "o"))
	if _, err := plan(e, f, WorkspaceCopy(e.workspace, taken), false); err == nil || !strings.Contains(err.Error(), "already the original") {
		t.Fatalf("an entry's original: %v", err)
	}
	if _, err := plan(e, f, filepath.Join(WorkspaceCopy(e.workspace, taken), "inner"), false); err == nil {
		t.Fatal("inside an entry's original")
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
	l, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: dir, Dir: true})
	if err != nil {
		t.Fatal(err)
	}
	if !l.Dir || l.Original != "symlinks/home/.password-store" {
		t.Fatalf("link = %+v", l)
	}
	if !isSymlink(dir) || !exists(filepath.Join(e.workspace, "symlinks", "home", ".password-store", "site.gpg")) {
		t.Fatal("the folder moved whole and was linked back")
	}
}

func TestAddRollsBackAFailedMove(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".keep", "content\n")
	// A file where a parent directory of the destination must go: the move cannot start.
	blocker := filepath.Join(e.workspace, "symlinks", "home")
	if err := os.MkdirAll(filepath.Dir(blocker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: sys}); err == nil {
		t.Fatal("the add must fail")
	}
	if isSymlink(sys) || mustRead(t, sys) != "content\n" {
		t.Fatal("a failed add leaves the live file where it was")
	}
	if len(e.links(t)) != 0 {
		t.Fatal("and registers nothing")
	}
	// A failed registration after the move puts the file back.
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(e.config, 0o444); err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Dir(e.config)
	if err := os.Chmod(cfgDir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(cfgDir, 0o755)
	defer os.Chmod(e.config, 0o644)
	if os.Geteuid() != 0 {
		_, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: sys})
		if err == nil {
			t.Skip("ws.json was writable anyway")
		}
		if isSymlink(sys) || mustRead(t, sys) != "content\n" {
			t.Fatal("a failed registration puts the file back")
		}
	}
}

// ── Take refusals ──────────────────────────────────────────────────

func TestTakeLiveRefusals(t *testing.T) {
	e := setup(t)
	// A shared copy (registered with a workspace path of its own) is never replaced.
	agent := filepath.Join(e.workspace, "agent", "AGENTS.md")
	if err := os.WriteFile(agent, []byte("shared\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(e.home, ".claude", "CLAUDE.md")
	if _, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: shared, To: agent}); err != nil {
		t.Fatal(err)
	}
	overwrite(t, shared, "live\n")
	if _, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: shared}); err == nil || !strings.Contains(err.Error(), "shared") {
		t.Fatalf("a shared copy must be refused, got %v", err)
	}
	if mustRead(t, agent) != "shared\n" {
		t.Fatal("the shared copy must be untouched")
	}
	// A folder entry whose host was replaced by a real folder.
	dir := filepath.Join(e.home, ".d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: dir, Dir: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: dir, Dir: true}); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("a directory entry cannot take the live content, got %v", err)
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
	if exists(filepath.Join(e.workspace, "symlinks", "home", ".config")) {
		t.Fatal("empty directories under symlinks/ must be removed")
	}
	if !exists(filepath.Join(e.workspace, "symlinks")) {
		t.Fatal("symlinks/ itself must stay")
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
	if !exists(filepath.Join(e.workspace, "symlinks", "home", ".config", "app", "b.conf")) {
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
	if _, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: sys, To: agent}); err != nil {
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
	if err := os.Symlink(WorkspaceCopy(e.workspace, link), second); err != nil {
		t.Fatal(err)
	}
	if err := wsconfig.AddLink(e.config, wsconfig.Link{System: second, Original: link.Original}); err != nil {
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
	to := filepath.Join(e.workspace, "symlinks", "home", ".password-store")
	if _, err := Add(AddOptions{WorkspacePath: e.workspace, ConfigPath: e.config, Host: dir, Dir: true}); err != nil {
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

// ── Link ───────────────────────────────────────────────────────────

func TestLinkCreatesTheSymlink(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".profile", "x\n")
	e.add(t, sys)
	if err := os.Remove(sys); err != nil {
		t.Fatal(err)
	}
	if err := Link(LinkOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys}); err != nil {
		t.Fatal(err)
	}
	if !isSymlink(sys) || mustRead(t, sys) != "x\n" {
		t.Fatal("the symlink must be back")
	}
}

func TestLinkCreatesMissingParents(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".config/app/app.conf", "x\n")
	e.add(t, sys)
	if err := os.RemoveAll(filepath.Join(e.home, ".config")); err != nil {
		t.Fatal(err)
	}
	if err := Link(LinkOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys}); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, sys) != "x\n" {
		t.Fatal("parents must be created on a new machine")
	}
}

func TestLinkBacksUpWhateverIsThere(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "workspace\n")
	e.add(t, sys)
	overwrite(t, sys, "live\n")

	p, err := PlanLink(e.workspace, e.config, sys)
	if err != nil || p.Backup != sys+".ws-backup" || p.OK || p.Missing || !p.Drifted {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	if err := Link(LinkOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys}); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, sys) != "workspace\n" || mustRead(t, sys+".ws-backup") != "live\n" {
		t.Fatal("the workspace copy wins; the live file is kept as .ws-backup")
	}
	overwrite(t, sys, "live again\n")
	if err := Link(LinkOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys}); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, sys+".ws-backup") != "live\n" || mustRead(t, sys+".ws-backup.1") != "live again\n" {
		t.Fatal("never overwrite an earlier backup")
	}
}

func TestLinkAMissingCopyIsAnError(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "x\n")
	link := e.add(t, sys)
	if err := os.Remove(WorkspaceCopy(e.workspace, link)); err != nil {
		t.Fatal(err)
	}
	if p, err := PlanLink(e.workspace, e.config, sys); err != nil || !p.Missing {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	if err := Link(LinkOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("a missing copy cannot be linked, got %v", err)
	}
}

func TestLinkOfAnOkEntryIsANoOp(t *testing.T) {
	e := setup(t)
	sys := e.write(t, ".bashrc", "x\n")
	e.add(t, sys)
	if p, err := PlanLink(e.workspace, e.config, sys); err != nil || !p.OK {
		t.Fatalf("an ok entry has nothing to do: %+v %v", p, err)
	}
	if err := Link(LinkOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: sys}); err != nil {
		t.Fatal(err)
	}
	if exists(sys + ".ws-backup") {
		t.Fatal("an ok entry must not be backed up")
	}
}

// TestLinkRecognisesARelativeSymlink is plan 0002 bug B5.
func TestLinkRecognisesARelativeSymlink(t *testing.T) {
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
	if p, err := PlanLink(e.workspace, e.config, sys); err != nil || !p.OK {
		t.Fatalf("a relative symlink to the copy is already linked: %+v %v", p, err)
	}
}

// TestLinkRegistersAnOrphan: a path with no entry whose mirror exists in the workspace
// is registered and linked.
func TestLinkRegistersAnOrphan(t *testing.T) {
	e := setup(t)
	copyPath := filepath.Join(e.workspace, "symlinks", "home", ".zshrc")
	if err := os.MkdirAll(filepath.Dir(copyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyPath, []byte("z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(e.home, ".zshrc")
	p, err := PlanLink(e.workspace, e.config, host)
	if err != nil || !p.Register || p.Link.Original != "symlinks/home/.zshrc" {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	if len(e.links(t)) != 0 {
		t.Fatal("planning registers nothing")
	}
	if err := Link(LinkOptions{WorkspacePath: e.workspace, ConfigPath: e.config, SystemPath: host}); err != nil {
		t.Fatal(err)
	}
	if !isSymlink(host) || mustRead(t, host) != "z\n" || len(e.links(t)) != 1 {
		t.Fatal("linked and registered")
	}
	// No entry and no mirror: an error.
	if _, err := PlanLink(e.workspace, e.config, filepath.Join(e.home, ".nothing")); err == nil || !strings.Contains(err.Error(), "not a managed symlink") {
		t.Fatalf("got %v", err)
	}
}
