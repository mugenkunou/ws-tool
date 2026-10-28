package skill

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// builtins replaces the embedded skills with a fake shelf and points the
// managed folder into a temp dir.
func builtins(t *testing.T, version string, files map[string]string) string {
	t.Helper()
	fsys := fstest.MapFS{
		"README.md":         {Data: []byte("not a skill")},
		"embed.go":          {Data: []byte("package skills")},
		"no-skill/notes.md": {Data: []byte("a folder without a SKILL.md")},
	}
	for p, body := range files {
		fsys[p] = &fstest.MapFile{Data: []byte(body)}
	}
	oldFS, oldVersion := BuiltinFS, Version
	BuiltinFS, Version = fsys, version
	t.Cleanup(func() { BuiltinFS, Version = oldFS, oldVersion })
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	return filepath.Join(data, "ws", "skills")
}

func writeShelf(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"workspace-write/SKILL.md":       "---\nname: workspace-write\n---\n",
		"workspace-write/write-rules.md": "rules",
		"workspace-run/SKILL.md":         "---\nname: workspace-run\n---\n",
	}
}

func TestBuiltinNamesSkipNonSkills(t *testing.T) {
	builtins(t, "v1", writeShelf(t))
	got := BuiltinNames()
	if len(got) != 2 || got[0] != "workspace-run" || got[1] != "workspace-write" {
		t.Fatalf("names = %v", got)
	}
}

func TestEmbeddedShelfExtracts(t *testing.T) {
	// The real embedded folder, which may hold no skill yet.
	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	if err := Refresh(home); err != nil {
		t.Fatal(err)
	}
	for _, s := range Builtins(home) {
		if s.Broken {
			t.Errorf("built-in %s was not extracted", s.Name)
		}
	}
}

func TestRefreshExtractsReadOnlyAndUpdates(t *testing.T) {
	managed := builtins(t, "v1", writeShelf(t))
	home := t.TempDir()
	if err := Refresh(home); err != nil {
		t.Fatal(err)
	}
	rules := filepath.Join(managed, "workspace-write", "write-rules.md")
	info, err := os.Stat(rules)
	if err != nil || info.Mode().Perm() != 0o444 {
		t.Fatalf("rules = %v, %v", info, err)
	}
	if info, _ := os.Stat(filepath.Join(managed, "workspace-write")); info.Mode().Perm() != 0o755 {
		t.Fatalf("folder mode = %v", info.Mode())
	}

	// New content in the binary: the copy follows.
	BuiltinFS.(fstest.MapFS)["workspace-write/write-rules.md"] = &fstest.MapFile{Data: []byte("new rules")}
	if err := Refresh(home); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(rules); string(b) != "new rules" {
		t.Fatalf("rules = %q", b)
	}

	// A new version alone rewrites the stamp.
	Version = "v2"
	if err := Refresh(home); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(managed, ".workspace-write.stamp")); string(b[:3]) != "v2 " {
		t.Fatalf("stamp = %q", b)
	}
}

func TestDevLinksTheCheckoutAndInstallSwitchesBack(t *testing.T) {
	managed := builtins(t, "v1", writeShelf(t))
	home := t.TempDir()
	if err := Refresh(home); err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	checkout := filepath.Join(src, "skills", "workspace-write")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "SKILL.md"), []byte("live"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Dev(home, src, "workspace-write"); err != nil {
		t.Fatal(err)
	}
	if err := Dev(home, src, "workspace-run"); err == nil {
		t.Fatal("dev mode for a skill missing from the checkout")
	}
	if err := Refresh(home); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(managed, "workspace-write", "SKILL.md")); string(b) != "live" {
		t.Fatalf("refresh replaced the dev link: %q", b)
	}
	var s Skill
	for _, b := range Builtins(home) {
		if b.Name == "workspace-write" {
			s = b
		}
	}
	if s.Dev != checkout || s.VersionOf() != "dev" {
		t.Fatalf("built-in = %+v", s)
	}

	// The tool link points at the managed folder, so it is ours in dev mode too.
	tool := claude(t)
	if err := Install(home, t.TempDir(), tool, s); err != nil {
		t.Fatal(err)
	}
	if !Ours(home, t.TempDir(), tool, "workspace-write") {
		t.Fatal("a link to the managed folder is not ours")
	}

	if err := Undev(home, "workspace-write"); err != nil {
		t.Fatal(err)
	}
	if _, dev := DevTarget(home, "workspace-write"); dev {
		t.Fatal("still in dev mode")
	}
	if b, _ := os.ReadFile(filepath.Join(managed, "workspace-write", "SKILL.md")); string(b) == "live" {
		t.Fatal("undev kept the checkout")
	}
}

func TestOursKnowsADirectDevLink(t *testing.T) {
	builtins(t, "v1", writeShelf(t))
	home, ws := env(t)
	src := t.TempDir()
	checkout := filepath.Join(src, "skills", "workspace-run")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Dev(home, src, "workspace-run"); err != nil {
		t.Fatal(err)
	}
	tool := claude(t)
	if err := os.MkdirAll(tool.DirPath(home), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(checkout, filepath.Join(tool.DirPath(home), "workspace-run")); err != nil {
		t.Fatal(err)
	}
	if !Ours(home, ws, tool, "workspace-run") {
		t.Fatal("a link straight at the dev checkout is not ours")
	}
}

func TestEjectMakesItYoursAndRepointsLinks(t *testing.T) {
	builtins(t, "v1", writeShelf(t))
	home, ws := env(t)
	if err := Refresh(home); err != nil {
		t.Fatal(err)
	}
	tool := claude(t)
	all, _ := All(home, ws)
	s, _ := Find(all, "workspace-write")
	if err := Install(home, ws, tool, s); err != nil {
		t.Fatal(err)
	}
	if err := Eject(home, ws, "workspace-write"); err != nil {
		t.Fatal(err)
	}
	copyDir := filepath.Join(Root(ws), "workspace-write")
	if b, _ := os.ReadFile(filepath.Join(copyDir, EjectedFile)); string(b) != "v1\n" {
		t.Fatalf("%s = %q", EjectedFile, b)
	}
	if info, err := os.Stat(filepath.Join(copyDir, "write-rules.md")); err != nil || info.Mode().Perm()&0o200 == 0 {
		t.Fatalf("the copy is not writable: %v %v", info, err)
	}
	if target, _ := os.Readlink(filepath.Join(tool.DirPath(home), "workspace-write")); target != copyDir {
		t.Fatalf("link = %q, want the workspace copy", target)
	}
	all, _ = All(home, ws)
	got, _ := Find(all, "workspace-write")
	if got.Shelf != Yours || got.Ejected != "v1" || got.Shadowed {
		t.Fatalf("after eject = %+v", got)
	}
	for _, s := range all {
		if s.Name == "workspace-write" && s.Shelf == Builtin {
			t.Fatal("the built-in is still listed beside its ejected copy")
		}
	}
	if err := Eject(home, ws, "workspace-write"); err == nil {
		t.Fatal("ejected over an existing copy")
	}
}

func TestNameClashTheBuiltinWins(t *testing.T) {
	builtins(t, "v1", writeShelf(t))
	home, ws := env(t)
	if err := Refresh(home); err != nil {
		t.Fatal(err)
	}
	addSkill(t, ws, "workspace-run")
	all, err := All(home, ws)
	if err != nil {
		t.Fatal(err)
	}
	var shadowed Skill
	for _, s := range all {
		if s.Name == "workspace-run" && s.Shelf == Yours {
			shadowed = s
		}
	}
	if !shadowed.Shadowed {
		t.Fatalf("the clash is not marked: %+v", all)
	}
	if s, _ := Find(all, "workspace-run"); s.Shelf != Builtin {
		t.Fatalf("Find = %+v, want the built-in", s)
	}
	if err := Install(home, ws, claude(t), shadowed); err == nil {
		t.Fatal("installed a shadowed skill")
	}
}
