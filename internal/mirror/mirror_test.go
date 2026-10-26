package mirror

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/ignore"
)

func needRsync(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not installed")
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setup makes a workspace with synced, ignored and symlinked entries, and an
// empty parent folder for the mirror.
func setup(t *testing.T) Options {
	t.Helper()
	root := t.TempDir()
	ws := filepath.Join(root, "Workspace")
	write(t, filepath.Join(ws, "ws.json"), "{}")
	write(t, filepath.Join(ws, "notes", "a.md"), "a")
	write(t, filepath.Join(ws, "notes", "we*ird [x].md"), "w")
	write(t, filepath.Join(ws, "proj", "node_modules", "x.js"), "x") // built-in ignore
	write(t, filepath.Join(ws, "proj", "debug.log"), "log")          // built-in ignore
	write(t, filepath.Join(ws, "private", "key"), "k")               // sync.exclude
	write(t, filepath.Join(ws, "symlinks", "home", ".gitconfig"), "g")
	if err := os.Symlink("notes", filepath.Join(ws, "lnk")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Cloud"), 0o755); err != nil {
		t.Fatal(err)
	}
	return Options{
		Workspace:    ws,
		Target:       filepath.Join(root, "Cloud", "Workspace"),
		Rules:        ignore.DefaultUserRules(),
		Exclude:      []ignore.RuleEntry{{Pattern: "private"}},
		MaxDeletePct: 20,
		StateDir:     filepath.Join(root, "state"),
	}
}

func tree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || path == dir || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out
}

func push(t *testing.T, o Options) (*Prepared, Result) {
	t.Helper()
	p, err := Prepare(o)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer p.Close()
	res, err := p.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return p, res
}

func TestCheck(t *testing.T) {
	needRsync(t)
	o := setup(t)
	if err := Check(o); err != nil {
		t.Fatalf("valid options: %v", err)
	}
	cases := map[string]func(*Options){
		"unset":            func(o *Options) { o.Target = "" },
		"relative":         func(o *Options) { o.Target = "Cloud/Workspace" },
		"inside workspace": func(o *Options) { o.Target = filepath.Join(o.Workspace, "mirror") },
		"contains ws":      func(o *Options) { o.Target = filepath.Dir(o.Workspace) },
		"no parent":        func(o *Options) { o.Target = filepath.Join(filepath.Dir(o.Target), "gone", "Workspace") },
	}
	for name, mod := range cases {
		c := o
		mod(&c)
		if err := Check(c); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestBuildFilterMatchesEngine(t *testing.T) {
	o := setup(t)
	f, err := BuildFilter(o.Workspace, o.Rules, o.Exclude)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.Symlinks, []string{"lnk"}) {
		t.Fatalf("symlinks %v, want [lnk]", f.Symlinks)
	}
	rules := strings.Split(strings.TrimSuffix(string(f.Rules), "\x00"), "\x00")
	if rules[len(rules)-1] != "- *" {
		t.Fatalf("last rule %q, want \"- *\"", rules[len(rules)-1])
	}
	want := []string{`+ /notes/we\*ird \[x].md`, "+ /notes/a.md", "+ /ws.json", "+ /symlinks/home/.gitconfig"}
	for _, w := range want {
		if !slices.Contains(rules, w) {
			t.Errorf("filter lacks %q: %q", w, rules)
		}
	}
	for _, r := range rules {
		for _, bad := range []string{"node_modules", "debug.log", "private", "lnk"} {
			if strings.Contains(r, bad) {
				t.Errorf("filter admits %q", r)
			}
		}
	}
	if f.Files != 4 {
		t.Errorf("files %d, want 4", f.Files)
	}
}

func TestPushMirrorsExactlyAndDeletes(t *testing.T) {
	needRsync(t)
	o := setup(t)
	p, res := push(t, o)
	if p.State != StampFirst {
		t.Errorf("first push state %s", p.State)
	}
	want := []string{StampName, "notes/a.md", "notes/we*ird [x].md", "symlinks/home/.gitconfig", "ws.json"}
	if got := tree(t, o.Target); !slices.Equal(got, want) {
		t.Fatalf("mirror %v, want %v", got, want)
	}
	if res.Changed != 4 || res.Files != 4 {
		t.Errorf("result %+v", res)
	}

	// Delete a file and ignore a folder: both leave the mirror. A file put in
	// the mirror by hand is removed too (the mirror is never edited).
	os.Remove(filepath.Join(o.Workspace, "notes", "a.md"))
	o.Exclude = append(o.Exclude, ignore.RuleEntry{Pattern: "symlinks"})
	write(t, filepath.Join(o.Target, "edited-in-mirror.txt"), "x")
	p, res = push(t, o)
	if p.State != StampOK {
		t.Errorf("second push state %s", p.State)
	}
	want = []string{StampName, "notes/we*ird [x].md", "ws.json"}
	if got := tree(t, o.Target); !slices.Equal(got, want) {
		t.Fatalf("mirror %v, want %v", got, want)
	}
	if res.Deleted != 3 {
		t.Errorf("deleted %d, want 3", res.Deleted)
	}
}

func TestStampStates(t *testing.T) {
	needRsync(t)
	o := setup(t)
	push(t, o)

	// Wiped: the mirror is emptied (locally, or by the client after a web delete).
	os.RemoveAll(o.Target)
	os.MkdirAll(o.Target, 0o755)
	p, err := Prepare(o)
	if err != nil {
		t.Fatal(err)
	}
	p.Close()
	if p.State != StampWiped {
		t.Fatalf("state %s, want wiped", p.State)
	}
	push(t, o)

	// Foreign: another machine pushed (its own stamp).
	write(t, filepath.Join(o.Target, StampName), `{"push_id":"other","host":"laptop2"}`)
	p, err = Prepare(o)
	if err != nil {
		t.Fatal(err)
	}
	p.Close()
	if p.State != StampForeign {
		t.Fatalf("state %s, want foreign", p.State)
	}
}

func TestDeleteGuard(t *testing.T) {
	needRsync(t)
	o := setup(t)
	for i := 0; i < 30; i++ {
		write(t, filepath.Join(o.Workspace, "bulk", strings.Repeat("f", i+1)), "x")
	}
	push(t, o)
	os.RemoveAll(filepath.Join(o.Workspace, "bulk"))
	p, err := Prepare(o)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if len(p.Deleted) != 30 || !p.GuardTripped() {
		t.Fatalf("deleted %d, tripped %v: want 30, true", len(p.Deleted), p.GuardTripped())
	}
	// Few deletions never trip it.
	p.Deleted = p.Deleted[:GuardMinFiles]
	if p.GuardTripped() {
		t.Fatal("guard tripped at GuardMinFiles deletions")
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	needRsync(t)
	o := setup(t)
	push(t, o)

	fresh := Options{Workspace: filepath.Join(t.TempDir(), "Workspace"), StateDir: filepath.Join(t.TempDir(), "state")}
	res, err := Restore(fresh, o.Target, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"notes/a.md", "notes/we*ird [x].md", "symlinks/home/.gitconfig", "ws.json"} // no stamp
	if got := tree(t, fresh.Workspace); !slices.Equal(got, want) {
		t.Fatalf("restored %v, want %v", got, want)
	}
	if !res.Adopted {
		t.Fatal("restore did not adopt the stamp")
	}
	// The new machine's first push is recognised, not foreign.
	fresh.Target, fresh.Rules = o.Target, ignore.DefaultUserRules()
	p, err := Prepare(fresh)
	if err != nil {
		t.Fatal(err)
	}
	p.Close()
	if p.State != StampOK {
		t.Fatalf("after restore state %s, want ok", p.State)
	}
}

func TestLockBusy(t *testing.T) {
	o := setup(t)
	unlock, busy, err := Lock(o)
	if err != nil || busy {
		t.Fatalf("first lock: busy=%v err=%v", busy, err)
	}
	defer unlock()
	if _, busy, _ := Lock(o); !busy {
		t.Fatal("second lock should be busy")
	}
}
