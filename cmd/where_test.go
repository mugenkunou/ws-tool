package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runWhereCmd(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = Execute(args, strings.NewReader(""), &o, &e)
	return code, o.String(), e.String()
}

func TestWhereText(t *testing.T) {
	ws := initWorkspace(t)
	scriptEnvFixture(t)
	code, out, errOut := runWhereCmd(t, "--workspace", ws, "--no-color", "where", filepath.Join(t.TempDir(), "report.pdf"))
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
	if out != "artifacts/\n  raw blob (.pdf)\n" {
		t.Fatalf("got %q", out)
	}

	code, out, _ = runWhereCmd(t, "--workspace", ws, "--no-color", "where", filepath.Join(ws, "artifacts", "x.pdf"))
	if code != 0 || !strings.HasPrefix(out, "artifacts/ Inside\n") {
		t.Fatalf("exit %d got %q", code, out)
	}
}

func TestWhereJSON(t *testing.T) {
	ws := initWorkspace(t)
	scriptEnvFixture(t)
	path := filepath.Join(t.TempDir(), "app.yaml")
	code, out, errOut := runWhereCmd(t, "--workspace", ws, "--json", "where", path)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
	var env struct {
		Command string `json:"command"`
		Data    struct {
			Path    string `json:"path"`
			Folder  string `json:"folder"`
			Reason  string `json:"reason"`
			Matched bool   `json:"matched"`
			Inside  bool   `json:"inside"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	d := env.Data
	if env.Command != "where" || d.Path != path || d.Folder != "configs/<system>-<instance>/" || !d.Matched || d.Inside || d.Reason == "" {
		t.Fatalf("%+v", env)
	}
}

func TestWhereNoFolderFits(t *testing.T) {
	ws := initWorkspace(t)
	scriptEnvFixture(t)
	path := filepath.Join(t.TempDir(), "thoughts.txt")
	code, out, errOut := runWhereCmd(t, "--workspace", ws, "where", path)
	if code != 1 || out != "" || !strings.Contains(errOut, "no folder fits "+path+": ask the user") {
		t.Fatalf("exit %d out=%q stderr=%q", code, out, errOut)
	}

	code, out, errOut = runWhereCmd(t, "--workspace", ws, "--json", "where", path)
	if code != 1 || !strings.Contains(errOut, "ask the user") {
		t.Fatalf("exit %d stderr=%q", code, errOut)
	}
	var env struct {
		Data struct {
			Matched bool   `json:"matched"`
			Folder  string `json:"folder"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || env.Data.Matched || env.Data.Folder != "" {
		t.Fatalf("err=%v out=%s", err, out)
	}
}

func TestWhereUsage(t *testing.T) {
	testSetXDG(t)
	for _, args := range [][]string{{"where"}, {"where", "a", "b"}} {
		code, _, errOut := runWhereCmd(t, args...)
		if code != 1 || !strings.Contains(errOut, "usage: ws where <path>") {
			t.Errorf("%v: exit %d stderr=%q", args, code, errOut)
		}
	}
}

// TestWhereNeedsNoWorkspace: without ws.json the answer is the same, and
// nothing is created.
func TestWhereNeedsNoWorkspace(t *testing.T) {
	testSetXDG(t)
	home := scriptEnvFixture(t)
	dir := filepath.Join(t.TempDir(), "none")
	code, out, errOut := runWhereCmd(t, "--workspace", dir, "--no-color", "where", "dump.har")
	if code != 0 || !strings.HasPrefix(out, "$WS_SCRATCH/dump.") {
		t.Fatalf("exit %d out=%q stderr=%q", code, out, errOut)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("where created %s", dir)
	}
	if _, err := os.Stat(filepath.Join(home, "Scratch")); err == nil {
		t.Fatal("where created the scratch root")
	}
}
