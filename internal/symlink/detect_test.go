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

// ── a known config that is not symlinked is never reported ─────────

func TestDetectNeverSuggests(t *testing.T) {
	m := newMachine(t)
	m.file(t, m.home_(".gitconfig"), "[user]")
	m.file(t, m.home_(".config/nvim/init.lua"), "x")
	m.file(t, m.home_(".ssh/id_rsa"), "-----BEGIN")
	m.file(t, "/etc/hosts", "127.0.0.1")
	if r := m.detect("linux", true); len(r.Records) != 0 {
		t.Fatalf("a real file ws.json does not know is none of ws's business: %+v", r.Records)
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

func TestDetectEntryIsOneRecord(t *testing.T) {
	m := newMachine(t)
	m.file(t, m.home_(".gitconfig"), "[user]") // drifted: a real file
	r := m.detect("linux", false, wsconfig.Link{System: m.home_(".gitconfig"), Original: "symlinks/home/.gitconfig"})
	if n := len(r.Records); n != 1 || r.Records[0].Status != StatusDrifted {
		t.Fatalf("one record, drifted: %+v", r.Records)
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
