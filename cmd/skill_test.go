package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mugenkunou/ws-tool/internal/skill"
)

// skEnv is a workspace plus a fake home, with one skill and Claude "installed".
type skEnv struct{ home, ws string }

func newSkEnv(t *testing.T) skEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	ws := initWorkspace(t)
	dir := filepath.Join(ws, "agent", "skills", "review-pr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: review-pr\ndescription: x\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	return skEnv{home: home, ws: ws}
}

func (e skEnv) run(stdin string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	full := append([]string{"--no-color", "--workspace", e.ws}, args...)
	code := Execute(full, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestSkillScanGrid(t *testing.T) {
	e := newSkEnv(t)
	code, out, errOut := e.run("", "skill", "scan")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"review-pr", "claude", "codex", "(*) detected: claude", "( ) not detected:", "codex*"} {
		if !strings.Contains(out, want) {
			t.Errorf("scan lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "✔") {
		t.Errorf("nothing is installed yet:\n%s", out)
	}
}

func TestSkillInstallScanUninstall(t *testing.T) {
	e := newSkEnv(t)
	link := filepath.Join(e.home, ".claude", "skills", "review-pr")

	if code, out, errOut := e.run("y\n", "skill", "install", "review-pr", "--tool", "claude"); code != 0 {
		t.Fatalf("install exit %d: %s%s", code, out, errOut)
	}
	if _, err := os.Readlink(link); err != nil {
		t.Fatalf("no link: %v", err)
	}

	code, out, _ := e.run("", "--json", "skill", "scan", "--tool", "claude")
	if code != 0 {
		t.Fatalf("scan exit %d", code)
	}
	var env struct {
		Data struct {
			Skills []struct {
				Skill string `json:"skill"`
				Tools []struct {
					Tool      string `json:"tool"`
					Installed bool   `json:"installed"`
				} `json:"tools"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(env.Data.Skills) != 1 || len(env.Data.Skills[0].Tools) != 1 || !env.Data.Skills[0].Tools[0].Installed {
		t.Fatalf("scan json = %s", out)
	}

	if code, out, errOut := e.run("y\n", "skill", "uninstall", "review-pr", "--tool", "claude"); code != 0 {
		t.Fatalf("uninstall exit %d: %s%s", code, out, errOut)
	}
	if _, err := os.Lstat(link); err == nil {
		t.Fatal("link survived uninstall")
	}
}

func TestSkillInstallPicklists(t *testing.T) {
	e := newSkEnv(t)
	// Detected tools come first, so claude is 1; then the only skill is 1.
	code, out, errOut := e.run("1\n1\ny\n", "skill", "install")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if _, err := os.Readlink(filepath.Join(e.home, ".claude", "skills", "review-pr")); err != nil {
		t.Fatalf("no link: %v\n%s", err, out)
	}
	if code, out, _ := e.run("1\n1\ny\n", "skill", "uninstall"); code != 0 {
		t.Fatalf("uninstall picklist exit %d\n%s", code, out)
	}
}

func TestSkillInstallNeverOverwrites(t *testing.T) {
	e := newSkEnv(t)
	if err := os.MkdirAll(filepath.Join(e.home, ".claude", "skills", "review-pr"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, _ := e.run("y\n", "skill", "install", "review-pr", "--tool", "claude")
	if code == 0 {
		t.Fatal("install over a real folder reported success")
	}
	if info, err := os.Lstat(filepath.Join(e.home, ".claude", "skills", "review-pr")); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("the real folder was replaced")
	}
}

func TestSkillUnknownToolAndVerb(t *testing.T) {
	e := newSkEnv(t)
	if code, _, errOut := e.run("", "skill", "scan", "--tool", "vim"); code == 0 || !strings.Contains(errOut, "unknown tool") {
		t.Errorf("unknown tool: %d %q", code, errOut)
	}
	if code, _, _ := e.run("", "skill", "bogus"); code == 0 {
		t.Error("unknown verb succeeded")
	}
}

// withBuiltins replaces the embedded skills with workspace-write (and its
// write-rules.md) and workspace-run, as ws v0.16 would ship them.
func withBuiltins(t *testing.T) {
	t.Helper()
	oldFS, oldVersion := skill.BuiltinFS, skill.Version
	skill.BuiltinFS = fstest.MapFS{
		"README.md":                      {Data: []byte("not a skill")},
		"workspace-write/SKILL.md":       {Data: []byte("---\nname: workspace-write\n---\nSee `ws skill show workspace-write --path`.\n")},
		"workspace-write/write-rules.md": {Data: []byte("rules")},
		"workspace-run/SKILL.md":         {Data: []byte("---\nname: workspace-run\n---\n")},
	}
	skill.Version = "v0.16"
	t.Cleanup(func() { skill.BuiltinFS, skill.Version = oldFS, oldVersion })
}

func (e skEnv) managed(name string) string {
	return filepath.Join(e.home, ".local", "share", "ws", "skills", name)
}

func TestSkillScanShowsBothShelves(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	// A skill of yours with a built-in's name: the built-in wins.
	clash := filepath.Join(e.ws, "agent", "skills", "workspace-run")
	if err := os.MkdirAll(clash, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clash, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := e.run("", "skill", "scan", "--tool", "claude")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{
		"Built-in (ws v0.16)", "workspace-write  v0.16", "Yours (", "review-pr",
		"workspace-run  [warn] same name as a built-in; the built-in wins",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("scan lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "Built-in") > strings.Index(out, "Yours") {
		t.Errorf("built-ins must come first:\n%s", out)
	}

	_, out, _ = e.run("", "--json", "skill", "scan")
	var env struct {
		Data struct {
			Version string `json:"version"`
			Skills  []struct {
				Skill    string `json:"skill"`
				Shelf    string `json:"shelf"`
				Version  string `json:"version"`
				Shadowed bool   `json:"shadowed"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	shelves := map[string]string{}
	for _, r := range env.Data.Skills {
		shelves[r.Skill+"/"+r.Shelf] = r.Version
		if r.Shelf == "yours" && r.Skill == "workspace-run" && !r.Shadowed {
			t.Error("the clash is not marked shadowed")
		}
	}
	if shelves["workspace-write/built-in"] != "v0.16" || shelves["review-pr/yours"] != "" || env.Data.Version != "v0.16" {
		t.Fatalf("scan json = %s", out)
	}
}

func TestSkillShowBothShelves(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	code, out, _ := e.run("", "skill", "show", "workspace-write", "--path")
	if code != 0 || strings.TrimSpace(out) != e.managed("workspace-write") {
		t.Fatalf("show --path = %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(strings.TrimSpace(out), "write-rules.md")); err != nil {
		t.Fatalf("the sibling file is not there: %v", err)
	}
	if _, out, _ := e.run("", "skill", "show", "review-pr", "--path"); strings.TrimSpace(out) != filepath.Join(e.ws, "agent", "skills", "review-pr") {
		t.Fatalf("show --path for yours = %q", out)
	}
	code, out, _ = e.run("", "skill", "show", "workspace-write")
	for _, want := range []string{"shelf", "built-in", "v0.16", "~/.local/share/ws/skills/workspace-write", "tools", "none"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("show lacks %q:\n%s", want, out)
		}
	}
	if code, _, errOut := e.run("", "skill", "show", "nope"); code != 1 || !strings.Contains(errOut, "no such skill") {
		t.Errorf("unknown skill: %d %q", code, errOut)
	}
}

func TestSkillInstallBuiltinDevAndBack(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	link := filepath.Join(e.home, ".claude", "skills", "workspace-write")
	if code, out, errOut := e.run("y\n", "skill", "install", "workspace-write", "--tool", "claude"); code != 0 {
		t.Fatalf("install exit %d: %s%s", code, out, errOut)
	}
	if target, _ := os.Readlink(link); target != e.managed("workspace-write") {
		t.Fatalf("link = %q, want the managed copy", target)
	}

	src := t.TempDir()
	checkout := filepath.Join(src, "skills", "workspace-write")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "SKILL.md"), []byte("live"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := e.run("y\n", "skill", "install", "--dev="+src, "workspace-write"); code != 0 {
		t.Fatalf("install --dev exit %d: %s%s", code, out, errOut)
	}
	if b, _ := os.ReadFile(filepath.Join(link, "SKILL.md")); string(b) != "live" {
		t.Fatalf("the tool does not see the checkout: %q", b)
	}
	if _, out, _ := e.run("", "skill", "scan", "--tool", "claude"); !strings.Contains(out, "workspace-write  dev") {
		t.Errorf("scan does not show dev mode:\n%s", out)
	}
	if code, _, errOut := e.run("y\n", "skill", "install", "--dev", "review-pr"); code != 1 || !strings.Contains(errOut, "built-in") {
		t.Errorf("--dev on a skill of yours: %d %q", code, errOut)
	}

	// A plain install switches it back; the link stays as it is.
	code, out, errOut := e.run("y\n", "skill", "install", "workspace-write", "--tool", "claude")
	if code != 0 || !strings.Contains(out, "Switch built-in workspace-write back") {
		t.Fatalf("plain install exit %d: %s%s", code, out, errOut)
	}
	if b, _ := os.ReadFile(filepath.Join(link, "SKILL.md")); string(b) == "live" {
		t.Fatal("still linked to the checkout")
	}

	if code, out, errOut := e.run("y\n", "skill", "uninstall", "workspace-write", "--tool", "claude"); code != 0 {
		t.Fatalf("uninstall exit %d: %s%s", code, out, errOut)
	}
	if _, err := os.Lstat(link); err == nil {
		t.Fatal("link survived uninstall")
	}
}

func TestSkillInstallPicklistLabelsShelves(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	_, out, _ := e.run("", "skill", "install", "--tool", "claude")
	for _, want := range []string{"workspace-run    built-in", "workspace-write  built-in", "review-pr        yours"} {
		if !strings.Contains(out, want) {
			t.Errorf("picklist lacks %q:\n%s", want, out)
		}
	}
}

func TestSkillEject(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	if code, _, errOut := e.run("y\n", "skill", "install", "workspace-write", "--tool", "claude"); code != 0 {
		t.Fatal(errOut)
	}
	code, out, errOut := e.run("y\n", "skill", "eject", "workspace-write")
	if code != 0 || !strings.Contains(out, "repoint claude") {
		t.Fatalf("eject exit %d: %s%s", code, out, errOut)
	}
	copyDir := filepath.Join(e.ws, "agent", "skills", "workspace-write")
	if b, _ := os.ReadFile(filepath.Join(copyDir, ".ws-ejected")); string(b) != "v0.16\n" {
		t.Fatalf(".ws-ejected = %q", b)
	}
	if target, _ := os.Readlink(filepath.Join(e.home, ".claude", "skills", "workspace-write")); target != copyDir {
		t.Fatalf("link = %q", target)
	}
	if _, out, _ := e.run("", "skill", "scan", "--tool", "claude"); !strings.Contains(out, "workspace-write  ejected from v0.16") {
		t.Errorf("scan does not show the ejected copy under Yours:\n%s", out)
	}
	if code, _, errOut := e.run("y\n", "skill", "eject", "workspace-write"); code != 1 || !strings.Contains(errOut, "already yours") {
		t.Errorf("second eject: %d %q", code, errOut)
	}
	if code, _, _ := e.run("y\n", "skill", "eject", "review-pr"); code != 1 {
		t.Error("ejected a skill of yours")
	}
}

func TestSkillCheck(t *testing.T) {
	withBuiltins(t)
	e := newSkEnv(t)
	if code, out, errOut := e.run("", "skill", "check"); code != 0 || !strings.Contains(out, "No drift") {
		t.Fatalf("clean check: %d %s%s", code, out, errOut)
	}

	// A warning only: still exit 0.
	skillMD := filepath.Join(e.ws, "agent", "skills", "review-pr", "SKILL.md")
	if err := os.WriteFile(skillMD, []byte("---\nname: review-pr\nmetadata:\n  ws-layout: \"9\"\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := e.run("", "skill", "check")
	if code != 0 || !strings.Contains(out, "ws-layout") || !strings.Contains(out, "0 errors, 1 warning") {
		t.Fatalf("warning check: %d\n%s", code, out)
	}

	// A command that does not exist is an error.
	if err := os.WriteFile(skillMD, []byte("Run `ws skill frobnicate` first.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ = e.run("", "--json", "skill", "check")
	var env struct {
		Data struct {
			Errors   int `json:"errors"`
			Findings []struct {
				Level, File, Message string
				Line                 int
			} `json:"findings"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if code != 1 || env.Data.Errors != 1 || env.Data.Findings[0].File != skillMD || env.Data.Findings[0].Line != 1 {
		t.Fatalf("error check: %d\n%s", code, out)
	}
}

func TestInitOffersBuiltinSkills(t *testing.T) {
	withBuiltins(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".claude", "skills", "workspace-write")
	initIn := func(stdin string) string {
		testSetXDG(t)
		var out, errOut bytes.Buffer
		ws := filepath.Join(t.TempDir(), "Workspace")
		if code := Execute([]string{"--no-color", "init", "--workspace", ws}, strings.NewReader(stdin), &out, &errOut); code != 0 {
			t.Fatalf("init exit %d: %s", code, errOut.String())
		}
		return out.String()
	}

	// No terminal: a hint, nothing linked.
	if out := initIn("a\ny\n"); !strings.Contains(out, "Built-in skills (workspace-run, workspace-write): `ws skill install`") {
		t.Errorf("no hint:\n%s", out)
	}
	if _, err := os.Lstat(link); err == nil {
		t.Fatal("linked without asking")
	}

	old := initStdinIsTerminal
	initStdinIsTerminal = func(io.Reader) bool { return true }
	t.Cleanup(func() { initStdinIsTerminal = old })

	// Enter is no.
	if out := initIn("a\n\n"); !strings.Contains(out, "into claude? [y/N]") {
		t.Errorf("no offer:\n%s", out)
	}
	if _, err := os.Lstat(link); err == nil {
		t.Fatal("Enter installed the skills")
	}
	initIn("a\ny\n")
	if target, _ := os.Readlink(link); target != filepath.Join(home, ".local", "share", "ws", "skills", "workspace-write") {
		t.Fatalf("link = %q", target)
	}
}
