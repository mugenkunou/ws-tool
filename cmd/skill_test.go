package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skEnv is a workspace plus a fake home, with one skill and Claude "installed".
type skEnv struct{ home, ws string }

func newSkEnv(t *testing.T) skEnv {
	t.Helper()
	ws := initWorkspace(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
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
