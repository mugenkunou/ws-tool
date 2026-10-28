package cmd

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/workspace"
)

// scriptEnvFixture sets the variables `ws env` reads, with a quote in one of
// them so the quoting is exercised.
func scriptEnvFixture(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TMPDIR", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(home, "it's run"))
	return home
}

func TestEnvPrintsQuotedExports(t *testing.T) {
	workspace := initWorkspace(t)
	home := scriptEnvFixture(t)
	var out, errOut bytes.Buffer
	if code := Execute([]string{"--workspace", workspace, "env"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("env: exit %d stderr=%s", code, errOut.String())
	}
	want := "export WS_ROOT='" + workspace + "'\n" +
		"export WS_TMP='" + home + "/tmp'\n" +
		"export WS_CACHE='" + home + "/.cache/ws'\n" +
		"export WS_RUN='" + home + `/it'\''s run'` + "\n" +
		"export WS_SCRATCH='" + home + "/Scratch'\n"
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out.String(), want)
	}

	// The shell reads the values back unchanged.
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	got, err := exec.Command(sh, "-c", out.String()+`printf '%s' "$WS_RUN"`).Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != home+"/it's run" {
		t.Fatalf("sh read WS_RUN as %q", got)
	}
}

func TestEnvPrefersTheEnvironment(t *testing.T) {
	workspace := initWorkspace(t)
	scriptEnvFixture(t)
	t.Setenv("TMPDIR", "/x/tmp")
	t.Setenv("XDG_CACHE_HOME", "/x/cache")
	var out, errOut bytes.Buffer
	if code := Execute([]string{"--workspace", workspace, "--json", "env"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("env --json: exit %d stderr=%s", code, errOut.String())
	}
	var env struct {
		Command string            `json:"command"`
		Data    map[string]string `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Command != "env" || env.Data["WS_TMP"] != "/x/tmp" || env.Data["WS_CACHE"] != "/x/cache/ws" || env.Data["WS_ROOT"] != workspace {
		t.Fatalf("%+v", env)
	}
}

// TestEnvNeedsNoWorkspace: a host without ws.json still gets every path, with
// the default scratch root.
func TestEnvNeedsNoWorkspace(t *testing.T) {
	testSetXDG(t)
	home := scriptEnvFixture(t)
	dir := filepath.Join(t.TempDir(), "none")
	var out, errOut bytes.Buffer
	if code := Execute([]string{"--workspace", dir, "env"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("env: exit %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "export WS_SCRATCH='"+home+"/Scratch'\n") {
		t.Fatalf("got:\n%s", out.String())
	}
}

func TestEnvAndLayoutTakeNoArguments(t *testing.T) {
	testSetXDG(t)
	for _, c := range []string{"env", "layout"} {
		var out, errOut bytes.Buffer
		if code := Execute([]string{c, "extra"}, strings.NewReader(""), &out, &errOut); code == 0 {
			t.Errorf("ws %s extra should fail", c)
		}
	}
}

func TestLayoutListsEveryFolderAndHome(t *testing.T) {
	ws := initWorkspace(t)
	home := scriptEnvFixture(t)
	var out, errOut bytes.Buffer
	if code := Execute([]string{"--workspace", ws, "--no-color", "layout"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("layout: exit %d stderr=%s", code, errOut.String())
	}
	for _, d := range workspace.Layout {
		line := filepath.Join(ws, filepath.FromSlash(d.Path))
		if !strings.Contains(out.String(), line+" ") || !strings.Contains(out.String(), d.Purpose) {
			t.Errorf("layout lacks %s: %s", line, d.Purpose)
		}
	}
	for _, v := range []string{"WS_TMP", "WS_CACHE", "WS_RUN", "WS_SCRATCH", home + "/tmp", home + "/Scratch"} {
		if !strings.Contains(out.String(), v) {
			t.Errorf("layout lacks %s", v)
		}
	}
	if strings.Contains(out.String(), "missing") {
		t.Errorf("an initialized workspace has no missing folder:\n%s", out.String())
	}

	out.Reset()
	if code := Execute([]string{"--workspace", ws, "--json", "layout"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("layout --json: exit %d stderr=%s", code, errOut.String())
	}
	var env struct {
		Command string `json:"command"`
		Data    struct {
			Workspace string           `json:"workspace"`
			Folders   []layoutFolder   `json:"folders"`
			Homes     []workspace.Home `json:"homes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Command != "layout" || env.Data.Workspace != ws || len(env.Data.Folders) != len(workspace.Layout) || len(env.Data.Homes) != 4 {
		t.Fatalf("%+v", env)
	}
	for _, f := range env.Data.Folders {
		if !filepath.IsAbs(f.Path) || !f.Exists {
			t.Errorf("folder %+v: want an absolute path that exists", f)
		}
	}
}
