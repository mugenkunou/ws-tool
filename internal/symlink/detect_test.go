package symlink

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// A fake machine: a temp HOME, a temp workspace, and a temp "root" standing in for /.
type machine struct{ home, ws, root string }

func newMachine(t *testing.T) machine {
	t.Helper()
	base := t.TempDir()
	m := machine{home: filepath.Join(base, "home"), ws: filepath.Join(base, "Workspace"), root: filepath.Join(base, "root")}
	for _, d := range []string{m.home, m.ws, m.root} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", m.home)
	return m
}

func (m machine) file(t *testing.T, host, content string) string {
	t.Helper()
	real := host
	if !strings.HasPrefix(host, m.home) {
		real = filepath.Join(m.root, host)
	}
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return host
}

func (m machine) home_(rel string) string { return filepath.Join(m.home, rel) }

func (m machine) wsFile(t *testing.T, rel, content string) string {
	t.Helper()
	p := filepath.Join(m.ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func (m machine) link(t *testing.T, target, at string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, at); err != nil {
		t.Fatal(err)
	}
}

func (m machine) detect(goos string, walk bool, entries ...wsconfig.Link) Result {
	return Detect(Options{Home: m.home, Workspace: m.ws, GOOS: goos, Root: m.root, Entries: entries, Walk: walk})
}

func by(r Result, status Status) []Record {
	var out []Record
	for _, rec := range r.Records {
		if rec.Status == status {
			out = append(out, rec)
		}
	}
	return out
}

func find(t *testing.T, r Result, host string) Record {
	t.Helper()
	for _, rec := range r.Records {
		if rec.System == host {
			return rec
		}
	}
	t.Fatalf("no record for %s in %+v", host, r.Records)
	return Record{}
}

func has(r Result, host string) bool {
	for _, rec := range r.Records {
		if rec.System == host {
			return true
		}
	}
	return false
}

// ── rule 1: registered entries ─────────────────────────────────────

func TestDetectRegisteredStatuses(t *testing.T) {
	m := newMachine(t)
	mk := func(name string) wsconfig.Link {
		return wsconfig.Link{System: m.home_(name), Original: "symlinks/home/" + name}
	}
	// ok
	orig := m.wsFile(t, "symlinks/home/.ok", "x")
	m.link(t, orig, m.home_(".ok"))
	// missing (a new laptop): the workspace copy is there, the host is not
	m.wsFile(t, "symlinks/home/.missing", "x")
	// broken: the link points at something that is gone
	m.wsFile(t, "symlinks/home/.broken", "x")
	m.link(t, filepath.Join(m.ws, "symlinks/home/.gone"), m.home_(".broken"))
	// broken: the link points somewhere else that exists
	m.wsFile(t, "symlinks/home/.elsewhere", "x")
	other := m.file(t, m.home_("other"), "o")
	m.link(t, other, m.home_(".elsewhere"))
	// broken: the right link, the workspace copy is gone
	m.link(t, filepath.Join(m.ws, "symlinks/home/.nocopy"), m.home_(".nocopy"))
	// drifted: a tool replaced the link with a real file
	m.wsFile(t, "symlinks/home/.drift", "old")
	m.file(t, m.home_(".drift"), "new")

	r := m.detect("linux", false, mk(".ok"), mk(".missing"), mk(".broken"), mk(".elsewhere"), mk(".nocopy"), mk(".drift"))
	want := map[string]Status{".ok": StatusOK, ".missing": StatusMissing, ".broken": StatusBroken, ".elsewhere": StatusBroken, ".nocopy": StatusBroken, ".drift": StatusDrifted}
	for name, st := range want {
		if got := find(t, r, m.home_(name)); got.Status != st || !got.Registered {
			t.Errorf("%s: %+v, want %s", name, got, st)
		}
	}
	if rec := find(t, r, m.home_(".elsewhere")); !strings.Contains(rec.Message, "points to") {
		t.Errorf("the message says where it points: %q", rec.Message)
	}
	// Registered entries come first, in ws.json order.
	for i, name := range []string{".ok", ".missing", ".broken"} {
		if r.Records[i].System != m.home_(name) {
			t.Errorf("order: record %d is %s", i, r.Records[i].System)
		}
	}
}

func TestDetectRelativeLinkCountsAsOK(t *testing.T) {
	m := newMachine(t)
	m.wsFile(t, "symlinks/home/.rel", "x")
	rel, _ := filepath.Rel(m.home, filepath.Join(m.ws, "symlinks/home/.rel"))
	m.link(t, rel, m.home_(".rel"))
	r := m.detect("linux", false, wsconfig.Link{System: m.home_(".rel"), Original: "symlinks/home/.rel"})
	if got := find(t, r, m.home_(".rel")); got.Status != StatusOK {
		t.Fatalf("%+v", got)
	}
}

// ── rule 2: catalog items ──────────────────────────────────────────

func TestDetectFoundItems(t *testing.T) {
	m := newMachine(t)
	m.file(t, m.home_(".gitconfig"), "[user]")
	m.file(t, m.home_(".config/Code/User/settings.json"), "{}")
	m.file(t, m.home_(".config/Code/User/globalStorage/state.vscdb"), "db") // not a catalog item
	m.file(t, m.home_(".bash_history"), "secret")                           // never suggested
	m.file(t, m.home_(".ssh/known_hosts"), "x")                             // never suggested
	r := m.detect("linux", false)
	g := find(t, r, m.home_(".gitconfig"))
	if g.Status != StatusFound || g.App != "git" || g.Original != "symlinks/home/.gitconfig" || g.Secret || g.Sudo {
		t.Errorf("%+v", g)
	}
	if v := find(t, r, m.home_(".config/Code/User/settings.json")); v.App != "vscode" || v.Status != StatusFound {
		t.Errorf("%+v", v)
	}
	for _, no := range []string{".config/Code/User/globalStorage/state.vscdb", ".bash_history", ".ssh/known_hosts", ".config/Code"} {
		if has(r, m.home_(no)) {
			t.Errorf("%s must not be suggested", no)
		}
	}
}

func TestDetectLinkElsewhereIsSilent(t *testing.T) {
	m := newMachine(t)
	target := m.file(t, filepath.Join(filepath.Dir(m.home), "configs-repo", "gitconfig"), "x")
	m.link(t, target, m.home_(".gitconfig"))
	if r := m.detect("linux", true); has(r, m.home_(".gitconfig")) || len(r.Records) != 0 {
		t.Fatalf("a link outside the workspace says nothing: %+v", r.Records)
	}
}

func TestDetectCatalogFolderFoundOnce(t *testing.T) {
	m := newMachine(t)
	m.file(t, m.home_(".config/nvim/init.lua"), "x")
	m.file(t, m.home_(".config/nvim/lua/a.lua"), "x")
	r := m.detect("linux", false)
	var nvim []Record
	for _, rec := range r.Records {
		if strings.HasPrefix(rec.System, m.home_(".config/nvim")) {
			nvim = append(nvim, rec)
		}
	}
	if len(nvim) != 1 || nvim[0].System != m.home_(".config/nvim") || !nvim[0].Dir || nvim[0].Kind != KindFolder || nvim[0].App != "neovim" {
		t.Fatalf("the folder is one item: %+v", nvim)
	}
}

func TestDetectFileInsideAFolderEntryIsNotFound(t *testing.T) {
	m := newMachine(t)
	m.file(t, m.home_(".claude/commands/a.md"), "x")
	m.file(t, m.home_(".claude/CLAUDE.md"), "x")
	entry := wsconfig.Link{System: m.home_(".claude"), Original: "symlinks/home/.claude", Dir: true}
	r := m.detect("linux", false, entry)
	if has(r, m.home_(".claude/CLAUDE.md")) || has(r, m.home_(".claude/commands")) {
		t.Fatalf("inside a folder entry nothing is suggested: %+v", r.Records)
	}
	// An entry for the folder itself is not suggested again either.
	m = newMachine(t)
	m.file(t, m.home_(".config/nvim/init.lua"), "x")
	r = m.detect("linux", false, wsconfig.Link{System: m.home_(".config/nvim"), Original: "symlinks/home/.config/nvim", Dir: true})
	if len(by(r, StatusFound)) != 0 {
		t.Fatalf("%+v", by(r, StatusFound))
	}
}

func TestDetectEntryIsNotFoundAgain(t *testing.T) {
	m := newMachine(t)
	m.file(t, m.home_(".gitconfig"), "[user]") // drifted: a real file
	r := m.detect("linux", false, wsconfig.Link{System: m.home_(".gitconfig"), Original: "symlinks/home/.gitconfig"})
	if n := len(r.Records); n != 1 || r.Records[0].Status != StatusDrifted {
		t.Fatalf("one record, drifted: %+v", r.Records)
	}
}

func TestDetectSSHKeyAndPubPair(t *testing.T) {
	m := newMachine(t)
	m.file(t, m.home_(".ssh/id_rsa"), "-----BEGIN")
	m.file(t, m.home_(".ssh/id_rsa.pub"), "ssh-rsa AAA")
	m.file(t, m.home_(".ssh/id_rsa.ws-backup"), "old")
	m.file(t, m.home_(".ssh/id_ed25519.pub"), "ssh-ed25519 AAA") // a lone .pub
	m.file(t, m.home_(".ssh/config"), "Host *")
	r := m.detect("linux", false)
	key := find(t, r, m.home_(".ssh/id_rsa"))
	pub := find(t, r, m.home_(".ssh/id_rsa.pub"))
	if !key.Secret || pub.Secret {
		t.Errorf("the private key is secret, its .pub is not: %+v %+v", key, pub)
	}
	if key.Group != m.home_(".ssh/id_rsa") || pub.Group != key.Group {
		t.Errorf("a private key and its .pub share one group: %q %q", key.Group, pub.Group)
	}
	if lone := find(t, r, m.home_(".ssh/id_ed25519.pub")); lone.Group != lone.System {
		t.Errorf("a lone .pub is its own group: %+v", lone)
	}
	if has(r, m.home_(".ssh/id_rsa.ws-backup")) {
		t.Error("*.ws-backup is never matched")
	}
	count := 0
	for _, rec := range r.Records {
		if rec.System == m.home_(".ssh/id_rsa") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("id_* must not match the .pub as a key: %d records for id_rsa", count)
	}
}

func TestDetectSudoAndOS(t *testing.T) {
	m := newMachine(t)
	m.file(t, "/etc/hosts", "127.0.0.1")
	m.file(t, m.home_(".config/lazygit/config.yml"), "x")                     // linux row
	m.file(t, m.home_("Library/Application Support/lazygit/config.yml"), "x") // mac row
	linux := m.detect("linux", false)
	hosts := find(t, linux, "/etc/hosts")
	if hosts.Status != StatusFound || !hosts.Sudo || hosts.App != "hosts" || hosts.Original != "symlinks/system/etc/hosts" {
		t.Errorf("%+v", hosts)
	}
	if !has(linux, m.home_(".config/lazygit/config.yml")) || has(linux, m.home_("Library/Application Support/lazygit/config.yml")) {
		t.Errorf("linux sees the linux row only: %+v", linux.Records)
	}
	mac := m.detect("darwin", false)
	if has(mac, m.home_(".config/lazygit/config.yml")) || !has(mac, m.home_("Library/Application Support/lazygit/config.yml")) {
		t.Errorf("mac sees the mac row only: %+v", mac.Records)
	}
	if !has(mac, "/etc/hosts") {
		t.Error("an `any` row matches every OS")
	}
}

func TestDetectGlobFolders(t *testing.T) {
	m := newMachine(t)
	m.file(t, m.home_(".config/JetBrains/IntelliJIdea2025.1/keymaps/mine.xml"), "x")
	m.file(t, m.home_(".config/JetBrains/GoLand2025.1/keymaps/mine.xml"), "x")
	r := m.detect("linux", false)
	got := by(r, StatusFound)
	if len(got) != 2 || got[0].App != "jetbrains" || got[0].Kind != KindFolder {
		t.Fatalf("one item per match: %+v", got)
	}
}

// ── rule 3: untracked links ────────────────────────────────────────

func TestDetectUntrackedNeedsWalk(t *testing.T) {
	m := newMachine(t)
	orig := m.wsFile(t, "data/bruno/temp/environments/e.json", "x")
	m.link(t, orig, m.home_("projects/tools/env.json"))                          // depth 3, not a catalog path
	m.link(t, m.wsFile(t, "agent/AGENTS.md", "x"), m.home_(".claude/CLAUDE.md")) // a catalog path
	if r := m.detect("linux", false); len(by(r, StatusUntracked)) != 1 {
		t.Fatalf("without the walk only the catalog path is checked: %+v", r.Records)
	}
	r := m.detect("linux", true)
	got := by(r, StatusUntracked)
	if len(got) != 2 {
		t.Fatalf("%+v", r.Records)
	}
	u := find(t, r, m.home_("projects/tools/env.json"))
	if u.Original != "data/bruno/temp/environments/e.json" || u.WorkspacePath != orig {
		t.Errorf("%+v", u)
	}
	if c := find(t, r, m.home_(".claude/CLAUDE.md")); c.App != "claude" {
		t.Errorf("an untracked catalog path keeps its app: %+v", c)
	}
	// A registered link is not untracked.
	r = m.detect("linux", true, wsconfig.Link{System: m.home_("projects/tools/env.json"), Original: "data/bruno/temp/environments/e.json"})
	if len(by(r, StatusUntracked)) != 1 {
		t.Fatalf("%+v", r.Records)
	}
}

func TestDetectWalkIgnoresSkillLinks(t *testing.T) {
	m := newMachine(t)
	skill := m.wsFile(t, "agent/skills/review/SKILL.md", "x")
	m.link(t, filepath.Dir(skill), m.home_(".claude/skills/review")) // ws skill install made this
	if r := m.detect("linux", true); len(by(r, StatusUntracked)) != 0 {
		t.Fatalf("a skill link is ws's own: %+v", r.Records)
	}
}

func TestDetectWalkSkipsHeavyFoldersAndDepth(t *testing.T) {
	m := newMachine(t)
	orig := m.wsFile(t, "notes/a.md", "x")
	for _, d := range []string{".cache/x", "node_modules/x", ".local/share/x", ".npm/x", ".cargo/registry/x", ".git/x", ".var/x", "snap/x"} {
		m.link(t, orig, m.home_(d+"/link"))
	}
	m.link(t, orig, m.home_("a/b/c/link"))   // depth 4 (a, b, c, link): visited
	m.link(t, orig, m.home_("a/b/c/d/link")) // depth 5: too deep
	m.link(t, orig, m.home_("keep/link"))
	r := m.detect("linux", true)
	got := map[string]bool{}
	for _, rec := range by(r, StatusUntracked) {
		got[rec.System] = true
	}
	if len(got) != 2 || !got[m.home_("a/b/c/link")] || !got[m.home_("keep/link")] {
		t.Fatalf("only the two reachable links: %v", got)
	}
}

func TestDetectWalkDoesNotEnterTheWorkspaceOrFollowLinks(t *testing.T) {
	m := newMachine(t)
	inside := filepath.Join(m.home, "Workspace")
	// The workspace lives under HOME here.
	m2 := m
	m2.ws = inside
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(inside, "notes", "a.md")
	if err := os.MkdirAll(filepath.Dir(orig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orig, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m2.link(t, orig, filepath.Join(inside, "notes", "inside-link")) // inside the workspace: rsync keeps it
	// A link to a folder holding a link into the workspace: never followed.
	dir := filepath.Join(filepath.Dir(m.home), "elsewhere")
	m2.link(t, orig, filepath.Join(dir, "deep"))
	m2.link(t, dir, filepath.Join(m.home, "to-elsewhere"))
	r := m2.detect("linux", true)
	if len(r.Records) != 0 {
		t.Fatalf("nothing: links inside the workspace are out of scope, and links are not followed: %+v", r.Records)
	}
}

func TestDetectWalkCapStopsAndSaysSo(t *testing.T) {
	m := newMachine(t)
	for i := 0; i < 30; i++ {
		m.file(t, m.home_(filepath.Join("d", string(rune('a'+i%26))+string(rune('a'+i/26)), "f")), "x")
	}
	r := Detect(Options{Home: m.home, Workspace: m.ws, GOOS: "linux", Root: m.root, Walk: true, WalkCap: 10})
	if !r.Truncated || len(r.Notes) != 1 || !strings.Contains(r.Notes[0], "stopped after 10") {
		t.Fatalf("the cap is a note, never a hang: %+v", r)
	}
	if r := Detect(Options{Home: m.home, Workspace: m.ws, GOOS: "linux", Root: m.root, Walk: true}); r.Truncated {
		t.Fatal("the default cap is far above this tree")
	}
}

// ── rule 4: orphans ────────────────────────────────────────────────

func TestDetectOrphans(t *testing.T) {
	m := newMachine(t)
	m.wsFile(t, "symlinks/home/.zshrc", "x")
	m.wsFile(t, "symlinks/home/.config/starship.toml", "x")
	m.wsFile(t, "symlinks/system/etc/hosts", "x")
	m.wsFile(t, "symlinks/home/.registered", "x")
	m.wsFile(t, "symlinks/home/.inside/a", "x") // inside a folder entry
	m.wsFile(t, "symlinks/home/.inside/b", "x")
	entries := []wsconfig.Link{
		{System: m.home_(".registered"), Original: "symlinks/home/.registered"},
		{System: m.home_(".inside"), Original: "symlinks/home/.inside", Dir: true},
	}
	r := m.detect("linux", false, entries...)
	var got []string
	for _, rec := range by(r, StatusOrphan) {
		got = append(got, rec.Original)
	}
	want := []string{"symlinks/home/.config/starship.toml", "symlinks/home/.zshrc", "symlinks/system/etc/hosts"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("orphans = %v, want %v", got, want)
	}
	z := find(t, r, m.home_(".zshrc"))
	if z.App != "zsh" || z.Sudo || z.WorkspacePath != filepath.Join(m.ws, "symlinks/home/.zshrc") {
		t.Errorf("%+v", z)
	}
	if h := find(t, r, "/etc/hosts"); !h.Sudo || h.App != "hosts" {
		t.Errorf("%+v", h)
	}
}

func TestDetectOrphanCatalogFolderIsOneOrphan(t *testing.T) {
	m := newMachine(t)
	m.wsFile(t, "symlinks/home/.config/nvim/init.lua", "x")
	m.wsFile(t, "symlinks/home/.config/nvim/lua/a.lua", "x")
	m.wsFile(t, "symlinks/home/.config/other.toml", "x")
	r := m.detect("linux", false)
	got := by(r, StatusOrphan)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	nv := find(t, r, m.home_(".config/nvim"))
	if nv.Kind != KindFolder || !nv.Dir || nv.Original != "symlinks/home/.config/nvim" {
		t.Errorf("%+v", nv)
	}
}

func TestDetectReverseMappingWithSpaces(t *testing.T) {
	m := newMachine(t)
	m.wsFile(t, "symlinks/home/My Docs/a b.txt", "x")
	r := m.detect("linux", false)
	if rec := find(t, r, m.home_("My Docs/a b.txt")); rec.Status != StatusOrphan {
		t.Fatalf("%+v", rec)
	}
}

// ── purity ─────────────────────────────────────────────────────────

func TestDetectWritesNothingAndKeepsNoState(t *testing.T) {
	m := newMachine(t)
	m.file(t, m.home_(".gitconfig"), "x")
	m.wsFile(t, "symlinks/home/.zshrc", "x")
	snap := func() string {
		var b strings.Builder
		for _, root := range []string{m.home, m.ws} {
			_ = filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
				b.WriteString(p + "\n")
				return nil
			})
		}
		return b.String()
	}
	before := snap()
	a := m.detect("linux", true)
	b := m.detect("linux", true)
	if snap() != before {
		t.Fatal("Detect must not write")
	}
	if len(a.Records) != len(b.Records) {
		t.Fatalf("same inputs, same result: %d vs %d", len(a.Records), len(b.Records))
	}
}
