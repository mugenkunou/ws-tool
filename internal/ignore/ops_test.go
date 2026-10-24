package ignore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rulesWith(exclude ...string) UserRules {
	r := DefaultUserRules()
	for _, p := range exclude {
		r.Exclude = append(r.Exclude, RuleEntry{Pattern: p})
	}
	return r
}

// TestUserExcludesWinOverAlwaysSyncDirectories: user excludes are the last
// rules, so an ignored path inside configs/ (a built-in always-sync directory)
// or inside a user always-sync directory stops syncing. This is the change that
// activates existing ignore_rules.exclude entries that used to be no-ops.
func TestUserExcludesWinOverAlwaysSyncDirectories(t *testing.T) {
	rules := rulesWith(
		"configs/dotfiles/.bashrc",
		"secrets/old.key",
		"archive/*.log",
		"experiments/ml/dump.bin",
	)
	rules.SafeHarbors = []RuleEntry{{Pattern: "experiments/ml/**"}}
	engine := BuildEngine(rules)

	for _, p := range []string{"configs/dotfiles/.bashrc", "secrets/old.key", "archive/run.log", "experiments/ml/dump.bin"} {
		res := engine.Evaluate(p, false)
		if res.Included {
			t.Errorf("%s: want ignored by the user exclude, got included (rule %s)", p, res.Rule)
		}
		if res.RuleSource != SourceUser {
			t.Errorf("%s: rule source %q, want %q", p, res.RuleSource, SourceUser)
		}
	}
	// Neighbours in the same always-sync directories still sync.
	for _, p := range []string{"configs/dotfiles/.zshrc", "secrets/new.key", "experiments/ml/model.bin", "archive/data.tar.gz"} {
		if res := engine.Evaluate(p, false); !res.Included {
			t.Errorf("%s: want synced, got ignored by %s", p, res.Rule)
		}
	}
}

// TestActivatedExcludeRulesTakeEffectInMegaignore: the generated .megaignore
// puts user excludes after every always-sync rule, and reading that file back
// gives the same verdicts as the engine built from ws.json.
func TestActivatedExcludeRulesTakeEffectInMegaignore(t *testing.T) {
	rules := rulesWith("configs/dotfiles/.bashrc", "artifacts/big.bin")
	rules.SafeHarbors = []RuleEntry{{Pattern: "experiments/ml/**"}}
	content := GenerateMegaignore(rules)

	idx := func(s string) int { return strings.Index(content, s) }
	for _, harbor := range []string{"+g:artifacts/**", "+g:configs/**", "+g:experiments/ml/**"} {
		for _, excl := range []string{"-:configs/dotfiles/.bashrc", "-:artifacts/big.bin"} {
			if idx(harbor) < 0 || idx(excl) < 0 || idx(excl) < idx(harbor) {
				t.Fatalf("%s must come before %s in:\n%s", harbor, excl, content)
			}
		}
	}
	if idx("-:artifacts/big.bin") > idx("-s:*") {
		t.Fatalf("the symlink rule must come last:\n%s", content)
	}

	path := filepath.Join(t.TempDir(), ".megaignore")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	fromFile, err := LoadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	fromRules := BuildEngine(rules)
	for _, p := range []string{"configs/dotfiles/.bashrc", "configs/dotfiles/.zshrc", "artifacts/big.bin", "artifacts/ok.bin", "experiments/ml/x", "notes/a.csv"} {
		a, b := fromFile.Evaluate(p, false), fromRules.Evaluate(p, false)
		if a.Included != b.Included {
			t.Errorf("%s: file says included=%v, rules say %v", p, a.Included, b.Included)
		}
	}
	if fromFile.Evaluate("configs/dotfiles/.bashrc", false).Included {
		t.Error("the written .megaignore must ignore configs/dotfiles/.bashrc")
	}
}

func TestRuleSources(t *testing.T) {
	rules := rulesWith("experiments/run")
	rules.SafeHarbors = []RuleEntry{{Pattern: "labs/**"}}
	engine := BuildEngine(rules)

	cases := []struct {
		path, rule, source string
		included           bool
	}{
		{"a/b.csv", "-g:*.csv", SourceBuiltin, false},
		{"archive/x.tar.gz", "+g:archive/**", SourceBuiltin, true},
		{"labs/x.csv", "+g:labs/**", SourceUser, true},
		{"experiments/run/x.txt", "-:experiments/run", SourceUser, false},
		{"notes/a.md", "<default>", "", true},
	}
	for _, c := range cases {
		res := engine.Evaluate(c.path, false)
		if res.Rule != c.rule || res.RuleSource != c.source || res.Included != c.included {
			t.Errorf("%s: got rule=%s source=%q included=%v, want %s %q %v", c.path, res.Rule, res.RuleSource, res.Included, c.rule, c.source, c.included)
		}
	}
}

func TestMegaignoreSectionOrderAndStale(t *testing.T) {
	rules := rulesWith("a/b")
	rules.SafeHarbors = []RuleEntry{{Pattern: "labs/**"}}
	content := GenerateMegaignore(rules)
	order := []string{"# Built-in rules", "# Built-in always-sync directories", "# Always-sync directories", "# Ignored paths (ws ignore add)", "-s:*"}
	last := -1
	for _, s := range order {
		i := strings.Index(content, s)
		if i < 0 || i < last {
			t.Fatalf("%q out of order in:\n%s", s, content)
		}
		last = i
	}

	path := filepath.Join(t.TempDir(), ".megaignore")
	if stale, err := Stale(path, rules); err != nil || !stale {
		t.Fatalf("a missing .megaignore is stale, got %v %v", stale, err)
	}
	if err := WriteMegaignore(path, rules); err != nil {
		t.Fatal(err)
	}
	if stale, _ := Stale(path, rules); stale {
		t.Fatal("a freshly written .megaignore is not stale")
	}
	if stale, _ := Stale(path, DefaultUserRules()); !stale {
		t.Fatal("rules that changed make it stale")
	}
	if err := os.WriteFile(path, []byte("-:hand-edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if stale, _ := Stale(path, rules); !stale {
		t.Fatal("a hand edit makes it stale")
	}
}

func TestNormalizeMutedAndMutedBy(t *testing.T) {
	ws := "/home/u/Workspace"
	got := NormalizeMuted(ws, []string{"a/b/", "./c", "/home/u/Workspace/d", "/elsewhere/x", "", "..", "../x", "/home/u/Workspace"})
	want := []string{"a/b", "c", "d"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("NormalizeMuted = %v, want %v", got, want)
	}

	muted := []string{"a/b", "a"}
	if e, ok := MutedBy(muted, "a/b/c.txt"); !ok || e != "a" {
		t.Fatalf("MutedBy picks the widest entry, got %q %v", e, ok)
	}
	if e, ok := MutedBy(muted, "a/b"); !ok || e != "a" {
		t.Fatalf("got %q %v", e, ok)
	}
	if _, ok := MutedBy(muted, "ab/c"); ok {
		t.Fatal("a sibling that shares a prefix is not muted")
	}
	if _, ok := MutedBy(muted, ""); ok {
		t.Fatal("the workspace root is never muted")
	}
}

func TestMuteStateOf(t *testing.T) {
	engine := BuildEngine(DefaultUserRules())
	muted := []string{"exp/ml"}

	if st := MuteStateOf(engine, muted, "exp/ml", true); !st.Muted || st.Entry != "exp/ml" {
		t.Fatalf("own entry: %+v", st)
	}
	if st := MuteStateOf(engine, muted, "exp/ml/x/y.bin", false); !st.Muted || st.Entry != "exp/ml" {
		t.Fatalf("parent entry: %+v", st)
	}
	st := MuteStateOf(engine, muted, "archive/a.bin", false)
	if !st.Muted || st.Entry != "" || st.Rule != "+g:archive/**" || st.RuleSource != SourceBuiltin {
		t.Fatalf("always-sync directory: %+v", st)
	}
	if st := MuteStateOf(engine, muted, "notes/a.md", false); st.Muted {
		t.Fatalf("plain path: %+v", st)
	}
}

func TestClassifyRm(t *testing.T) {
	rules := rulesWith("experiments/run/dump.bin", "tmp/**", "build/")
	cases := []struct {
		rel   string
		kind  RmKind
		rule  string
		entry string
	}{
		{"experiments/run/dump.bin", RmExact, "", "experiments/run/dump.bin"},
		{"build", RmExact, "", "build/"}, // trailing slash is the same path
		{"tmp/x/y.txt", RmWider, "-g:tmp/**", ""},
		{"notes/data.csv", RmBuiltin, "-g:*.csv", ""},
		{"notes/a.md", RmNotIgnored, "", ""},
	}
	for _, c := range cases {
		v := ClassifyRm(rules, c.rel, false)
		if v.Kind != c.kind || v.Rule != c.rule || v.Entry != c.entry {
			t.Errorf("%s: got %+v, want kind=%s rule=%q entry=%q", c.rel, v, c.kind, c.rule, c.entry)
		}
	}

	// A parent entry that also covers the path: removing the exact entry still
	// leaves the path ignored, and the verdict says by what.
	both := rulesWith("exp", "exp/dump.bin")
	v := ClassifyRm(both, "exp/dump.bin", false)
	if v.Kind != RmExact || v.Entry != "exp/dump.bin" || v.Rule != "-:exp" {
		t.Fatalf("got %+v", v)
	}
	// A path ignored by an exact entry and a built-in rule.
	csv := rulesWith("a/x.csv")
	v = ClassifyRm(csv, "a/x.csv", false)
	if v.Kind != RmExact || v.Rule != "-g:*.csv" {
		t.Fatalf("got %+v", v)
	}
}

func TestListIgnored(t *testing.T) {
	ws := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(ws, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("notes/a.md", "x")
	write("notes/data.csv", "123456")
	write("exp/run/one.txt", "1")
	write("exp/run/sub/two.txt", "22")
	write("exp/keep.txt", "k")
	write("archive/old.csv", "synced: always-sync directory")

	engine := BuildEngine(rulesWith("exp/run"))
	rows := ListIgnored(engine, ws, nil)
	var paths []string
	for _, r := range rows {
		paths = append(paths, r.Path)
	}
	want := "exp/run/one.txt,exp/run/sub/two.txt,notes/data.csv"
	if strings.Join(paths, ",") != want {
		t.Fatalf("ignored = %v, want %s", paths, want)
	}
	if rows[0].Rule != "-:exp/run" || rows[0].RuleSource != SourceUser || rows[2].Rule != "-g:*.csv" || rows[2].RuleSource != SourceBuiltin || rows[2].SizeBytes != 6 {
		t.Fatalf("rows = %+v", rows)
	}

	only := ListIgnored(engine, ws, []string{filepath.Join(ws, "exp")})
	if len(only) != 2 {
		t.Fatalf("under exp: %+v", only)
	}
}

func TestCheckOutsideWorkspaceIsAnError(t *testing.T) {
	ws := t.TempDir()
	if _, _, err := Check(BuildEngine(DefaultUserRules()), ws, filepath.Join(filepath.Dir(ws), "elsewhere"), false); err == nil {
		t.Fatal("a path outside the workspace must be an error")
	}
	res, rel, err := Check(BuildEngine(DefaultUserRules()), ws, filepath.Join(ws, "a", "b.csv"), false)
	if err != nil || rel != "a/b.csv" || res.Included {
		t.Fatalf("got %+v %q %v", res, rel, err)
	}
}

// TestScanMutedPaths: ignore.muted entries mute their warnings (INFO, muted
// true), project-meta findings included; always-sync directories mute too.
func TestScanMutedPaths(t *testing.T) {
	ws := t.TempDir()
	big := make([]byte, 2*1024*1024)
	mk := func(rel string, data []byte) {
		p := filepath.Join(ws, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("live/big.bin", big)
	mk("quiet/big.bin", big)
	mk("archive/big.bin", big)
	mk("quiet/app/go.mod", []byte("module x"))
	mk("quiet/app/bin/tool", []byte("x"))
	mk("live/app/go.mod", []byte("module y"))
	mk("live/app/bin/tool", []byte("x"))

	vs, err := Scan(ScanOptions{WorkspacePath: ws, WarnSizeMB: 1, CritSizeMB: 10, MaxDepth: 6,
		Engine: BuildEngine(DefaultUserRules()), Muted: []string{"quiet"}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Violation{}
	for _, v := range vs {
		got[v.Type+":"+v.Path] = v
	}
	for key, wantMuted := range map[string]bool{
		"bloat:live/big.bin":         false,
		"bloat:quiet/big.bin":        true,
		"bloat:archive/big.bin":      true,
		"project-meta:live/app/bin":  false,
		"project-meta:quiet/app/bin": true,
	} {
		v, ok := got[key]
		if !ok {
			t.Errorf("missing %s in %v", key, got)
			continue
		}
		if v.Muted != wantMuted {
			t.Errorf("%s: muted=%v, want %v", key, v.Muted, wantMuted)
		}
		if wantMuted && v.Severity != "INFO" {
			t.Errorf("%s: muted severity %s, want INFO", key, v.Severity)
		}
		if !wantMuted && v.Severity == "INFO" {
			t.Errorf("%s: live warning must not be INFO", key)
		}
	}
}
