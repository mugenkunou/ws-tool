package skill

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// env is a fake machine: a home and a workspace with agent/skills/.
func env(t *testing.T) (home, ws string) {
	t.Helper()
	home, ws = t.TempDir(), t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	if _, faked := BuiltinFS.(fstest.MapFS); !faked {
		// Only the skills a test writes, not the ones embedded in the binary.
		old := BuiltinFS
		BuiltinFS = fstest.MapFS{}
		t.Cleanup(func() { BuiltinFS = old })
	}
	if err := os.MkdirAll(Root(ws), 0o755); err != nil {
		t.Fatal(err)
	}
	return home, ws
}

func addSkill(t *testing.T, ws, name string) {
	t.Helper()
	dir := filepath.Join(Root(ws), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func claude(t *testing.T) Tool {
	t.Helper()
	tool, ok := FindTool("claude")
	if !ok {
		t.Fatal("claude is not in the matrix")
	}
	return tool
}

func TestMatrixIsSortedAndComplete(t *testing.T) {
	for i, tool := range Tools {
		if tool.Name == "" || tool.Config == "" || tool.Dir == "" {
			t.Errorf("row %d is incomplete: %+v", i, tool)
		}
		if i > 0 && Tools[i-1].Name >= tool.Name {
			t.Errorf("%q is out of order", tool.Name)
		}
		if filepath.Base(tool.Dir) != "skills" {
			t.Errorf("%s: dir %q does not end in skills", tool.Name, tool.Dir)
		}
	}
}

func TestListFindsFoldersAndSymlinksOnly(t *testing.T) {
	_, ws := env(t)
	addSkill(t, ws, "real")
	// A symlink to a folder elsewhere is a skill too.
	elsewhere := t.TempDir()
	if err := os.WriteFile(filepath.Join(elsewhere, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(Root(ws), "linked")); err != nil {
		t.Fatal(err)
	}
	// A dangling link is listed as broken; a loose file and an empty folder are not skills.
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone"), filepath.Join(Root(ws), "dangling")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(Root(ws), "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(Root(ws), "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := List(ws)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"dangling": true, "linked": false, "real": false}
	if len(got) != len(want) {
		t.Fatalf("skills = %+v", got)
	}
	for _, s := range got {
		broken, ok := want[s.Name]
		if !ok || broken != s.Broken {
			t.Errorf("skill %+v unexpected", s)
		}
	}
}

func TestListWithoutSkillsFolder(t *testing.T) {
	got, err := List(t.TempDir())
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestDetectedUsesConfigFolder(t *testing.T) {
	home, _ := env(t)
	tool := claude(t)
	if tool.Detected(home) {
		t.Fatal("detected before ~/.claude exists")
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !tool.Detected(home) {
		t.Fatal("not detected with ~/.claude present and no skills folder")
	}
}

func TestInstallScanUninstall(t *testing.T) {
	home, ws := env(t)
	addSkill(t, ws, "review")
	addSkill(t, ws, "notes")
	tool := claude(t)
	s := Skill{Name: "review", Path: filepath.Join(Root(ws), "review")}

	if err := Install(home, ws, tool, s); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(home, ".claude", "skills", "review"))
	if err != nil || target != s.Path {
		t.Fatalf("link = %q, %v", target, err)
	}
	if err := Install(home, ws, tool, s); err == nil {
		t.Fatal("installed over an existing link")
	}

	res, err := Scan(home, ws, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != len(Tools) || len(res.Rows) != 2 {
		t.Fatalf("scan = %+v", res)
	}
	for _, r := range res.Rows {
		for _, c := range r.Tools {
			want := r.Skill == "review" && c.Tool == "claude"
			if c.Installed != want {
				t.Errorf("%s in %s: installed=%v", r.Skill, c.Tool, c.Installed)
			}
		}
	}

	if got := InstalledOurs(home, ws, tool); len(got) != 1 || got[0] != "review" {
		t.Fatalf("ours = %v", got)
	}
	if err := Uninstall(home, ws, tool, "review"); err != nil {
		t.Fatal(err)
	}
	if Installed(home, tool, "review") {
		t.Fatal("link survived uninstall")
	}
	if _, err := os.Stat(s.Path); err != nil {
		t.Fatalf("the workspace skill was touched: %v", err)
	}
}

func TestUninstallLeavesForeignThingsAlone(t *testing.T) {
	home, ws := env(t)
	tool := claude(t)
	dir := tool.DirPath(home)
	if err := os.MkdirAll(filepath.Join(dir, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "other")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"real", "other", "missing"} {
		if err := Uninstall(home, ws, tool, name); err == nil {
			t.Errorf("uninstall removed or accepted %q", name)
		}
	}
	if !Installed(home, tool, "real") || !Installed(home, tool, "other") {
		t.Fatal("a foreign entry was removed")
	}
}

func TestInstallRefusesBrokenSkillAndRealFolder(t *testing.T) {
	home, ws := env(t)
	tool := claude(t)
	if err := Install(home, ws, tool, Skill{Name: "x", Broken: true}); err == nil {
		t.Fatal("installed a broken skill")
	}
	addSkill(t, ws, "taken")
	if err := os.MkdirAll(filepath.Join(tool.DirPath(home), "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(home, ws, tool, Skill{Name: "taken", Path: filepath.Join(Root(ws), "taken")}); err == nil {
		t.Fatal("replaced a real folder")
	}
}

func TestOursFollowsOneHopOnly(t *testing.T) {
	home, ws := env(t)
	tool := claude(t)
	// The workspace skill is itself a symlink: the link in the tool still points
	// into agent/skills/, and that is all that matters.
	real := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(Root(ws), "chained")); err != nil {
		t.Fatal(err)
	}
	skills, _ := List(ws)
	if len(skills) != 1 {
		t.Fatalf("skills = %v", skills)
	}
	if err := Install(home, ws, tool, skills[0]); err != nil {
		t.Fatal(err)
	}
	if !Ours(home, ws, tool, "chained") {
		t.Fatal("a link to a symlinked skill is not ours")
	}
}
