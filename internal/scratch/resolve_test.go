package scratch

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

func mkdirs(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(root, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResolve(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "dns-debug.2026-09", "proxy-auth.2026-10", "foo", "foo-2", "myfoo")

	cases := []struct {
		name string
		want string // "" = an error
		err  string
	}{
		{"dns", "dns-debug.2026-09", ""}, // a unique substring
		{"DNS", "dns-debug.2026-09", ""}, // case-insensitive
		{"foo", "foo", ""},               // an exact name wins over foo-2 and myfoo
		{"proxy-auth.2026-10", "proxy-auth.2026-10", ""},
		{"nope", "", "not found"},
	}
	for _, c := range cases {
		got, err := Resolve(root, c.name)
		if c.want == "" {
			if err == nil {
				t.Errorf("Resolve(%q) = %q, want an error", c.name, got.Name)
			}
			continue
		}
		if err != nil || got.Name != c.want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", c.name, got.Name, err, c.want)
		}
	}

	_, err := Resolve(root, "fo")
	if err == nil || !strings.Contains(err.Error(), "matches several") || !strings.Contains(err.Error(), "foo-2") || !strings.Contains(err.Error(), "myfoo") {
		t.Fatalf("an ambiguous substring lists the candidates, got %v", err)
	}
	if _, err := Resolve(root, "  "); err == nil {
		t.Fatal("an empty name is an error")
	}
}

// TestDeleteTouchesOnlyTheNamedDirectory: deleting `foo` must not also delete
// `foo-2` and `myfoo`.
func TestDeleteTouchesOnlyTheNamedDirectory(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	root := t.TempDir()
	mkdirs(t, root, "foo", "foo-2", "myfoo")

	entry, err := Resolve(root, "foo")
	if err != nil || entry.Name != "foo" {
		t.Fatalf("Resolve = %+v, %v", entry, err)
	}
	if err := Remove(entry); err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{"foo-2", "myfoo"} {
		if _, err := os.Stat(filepath.Join(root, keep)); err != nil {
			t.Errorf("%s must survive deleting foo", keep)
		}
	}

	// An ambiguous substring deletes nothing.
	if _, err := Resolve(root, "o"); err == nil {
		t.Fatal("a substring that matches several must delete nothing")
	}
	for _, keep := range []string{"foo-2", "myfoo"} {
		if _, err := os.Stat(filepath.Join(root, keep)); err != nil {
			t.Errorf("%s must survive an ambiguous delete", keep)
		}
	}
}

func TestPlanAddNamesTheDestination(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Scratch")
	ws := filepath.Join(t.TempDir(), "Workspace")
	mkdirs(t, root, "capture-2")
	if err := os.MkdirAll(filepath.Join(ws, "experiments", "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	pcap := filepath.Join(ws, "experiments", "net", "capture.pcap")
	envFile := filepath.Join(ws, "experiments", ".env")
	dir := filepath.Join(ws, "experiments", "net2")
	for _, f := range []string{pcap, envFile} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mkdirs(t, filepath.Join(ws, "experiments"), "net2")

	plans, err := PlanAdd(AddOptions{RootDir: root, WorkspacePath: ws, Paths: []string{pcap, envFile, dir}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, "capture", "capture.pcap"), // a file is wrapped in a directory named after it
		filepath.Join(root, ".env", ".env"),            // a name that would be empty keeps the full name
		filepath.Join(root, "net2"),                    // a directory keeps its name
	}
	for i, p := range plans {
		if p.Dest != want[i] {
			t.Errorf("plan %d: dest %s, want %s", i, p.Dest, want[i])
		}
	}
}

func TestPlanAddNumbersCollisions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Scratch")
	ws := filepath.Join(t.TempDir(), "Workspace")
	mkdirs(t, root, "capture")
	for _, d := range []string{"a", "b"} {
		mkdirs(t, ws, d)
		if err := os.WriteFile(filepath.Join(ws, d, "capture.pcap"), []byte(d), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plans, err := PlanAdd(AddOptions{RootDir: root, WorkspacePath: ws, Paths: []string{filepath.Join(ws, "a", "capture.pcap"), filepath.Join(ws, "b", "capture.pcap")}})
	if err != nil {
		t.Fatal(err)
	}
	// capture exists, so -2; the second path of the same command gets -3, never the same dir.
	if plans[0].Dir != filepath.Join(root, "capture-2") || plans[1].Dir != filepath.Join(root, "capture-3") {
		t.Fatalf("dirs = %s, %s", plans[0].Dir, plans[1].Dir)
	}
	// An existing scratch directory is never merged into or overwritten.
	if err := Add(plans[0]); err != nil {
		t.Fatal(err)
	}
	if err := Add(plans[0]); err == nil {
		t.Fatal("Add must refuse a destination that exists")
	}
}

func TestPlanAddRefusals(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Scratch")
	ws := filepath.Join(t.TempDir(), "Workspace")
	mkdirs(t, root, "existing")
	mkdirs(t, ws, "agent", "symlinks/home")
	cfg := filepath.Join(ws, "ws.json")
	if err := os.WriteFile(cfg, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(ws, "agent", "AGENTS.md")
	if err := os.WriteFile(original, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirOriginal := filepath.Join(ws, "symlinks", "home", "password-store")
	mkdirs(t, filepath.Join(ws, "symlinks", "home"), "password-store")
	if err := os.WriteFile(filepath.Join(dirOriginal, "a.gpg"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	links := []wsconfig.Link{
		{System: "/home/u/.claude/CLAUDE.md", Original: "agent/AGENTS.md"},
		{System: "/home/u/.password-store", Original: "symlinks/home/password-store", Dir: true},
	}
	plan := func(p string) error {
		_, err := PlanAdd(AddOptions{RootDir: root, WorkspacePath: ws, ConfigPath: cfg, Links: links, Paths: []string{p}})
		return err
	}
	cases := map[string]string{
		filepath.Join(ws, "missing.txt"):    "not found",
		root:                                "scratch root",
		filepath.Join(root, "existing"):     "scratch root",
		filepath.Dir(root):                  "contains the scratch root",
		ws:                                  "contains the workspace",
		cfg:                                 "ws.json",
		original:                            "symlink original",
		filepath.Join(dirOriginal, "a.gpg"): "symlink original",
		filepath.Join(ws, "symlinks"):       "symlink original",
	}
	for p, want := range cases {
		if err := plan(p); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("PlanAdd(%s) = %v, want an error with %q", p, err, want)
		}
	}
}

func TestAddMovesASymlinkAsALink(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Scratch")
	ws := filepath.Join(t.TempDir(), "Workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ws, "latest")
	if err := os.Symlink("/nonexistent/build", link); err != nil {
		t.Fatal(err)
	}
	plans, err := PlanAdd(AddOptions{RootDir: root, WorkspacePath: ws, Paths: []string{link}})
	if err != nil {
		t.Fatal(err)
	}
	if err := Add(plans[0]); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(plans[0].Dest); err != nil || got != "/nonexistent/build" {
		t.Fatalf("the symlink moves as the link: %q %v", got, err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatal("the source must be gone")
	}
	// No .ws-meta.json: tags are written by the first scratch tag.
	if _, err := os.Stat(filepath.Join(plans[0].Dir, ".ws-meta.json")); !os.IsNotExist(err) {
		t.Fatal("add writes no metadata")
	}
}

func TestApplyTags(t *testing.T) {
	tags, added, removed := ApplyTags([]string{"dns", "old"}, []string{" Network, DNS ", "k8s"}, []string{"old", "absent"})
	if !reflect.DeepEqual(tags, []string{"dns", "network", "k8s"}) ||
		!reflect.DeepEqual(added, []string{"network", "k8s"}) ||
		!reflect.DeepEqual(removed, []string{"old"}) {
		t.Fatalf("tags=%v added=%v removed=%v", tags, added, removed)
	}
	// Nothing to do.
	tags, added, removed = ApplyTags([]string{"a"}, []string{"a"}, []string{"b"})
	if len(added) != 0 || len(removed) != 0 || !reflect.DeepEqual(tags, []string{"a"}) {
		t.Fatalf("tags=%v added=%v removed=%v", tags, added, removed)
	}
	if got := SplitTags(" a, ,B,,c "); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("SplitTags = %v", got)
	}
}
