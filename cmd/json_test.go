package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestJSONExitCodeFollowsActions: a write command run with --json must exit
// with its plan's code. It used to exit 0 whenever the JSON had been written,
// so scripts could not see a failed action (plan 0002 A3).
func TestJSONExitCodeFollowsActions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := initWorkspace(t)

	system := filepath.Join(home, ".bashrc")
	if err := os.WriteFile(system, []byte("export A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The storage path is taken, so the add action fails.
	taken := filepath.Join(workspace, "symlinks", "home", ".bashrc")
	if err := os.MkdirAll(filepath.Dir(taken), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(taken, []byte("already here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "--json", "symlink", "add", system}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("every action failed: exit %d, want 1\nstdout: %s\nstderr: %s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), `"status": "failed"`) {
		t.Fatalf("the JSON should report the failed action:\n%s", out.String())
	}
}

func TestJSONExitHelper(t *testing.T) {
	cases := []struct {
		actions []ActionStatus
		want    int
	}{
		{nil, 0},
		{[]ActionStatus{{Status: "executed"}, {Status: "skipped"}}, 0},
		{[]ActionStatus{{Status: "failed"}}, 1},
		{[]ActionStatus{{Status: "executed"}, {Status: "failed"}}, 3},
	}
	for _, c := range cases {
		var out, errOut bytes.Buffer
		got := writeJSON(&out, &errOut, "x.y", map[string]any{"actions": c.actions})
		if got != c.want {
			t.Errorf("actions %v: exit %d, want %d", c.actions, got, c.want)
		}
	}
}

// jsonMigrated lists the nouns whose JSON command names follow the grammar.
// Each noun is added in the commit that moves it (plan 0002 phase B).
var jsonMigrated = []string{"version", "symlink", "session", "scratch", "trash", "cron", "ignore", "repo", "secret", "pass", "credential"}

// TestJSONCommandNames: every read-only command that needs no target, run with
// --json, reports <noun>.<verb> as its command (spec §4.4).
func TestJSONCommandNames(t *testing.T) {
	workspace := initWorkspace(t)
	for _, c := range commands {
		if !c.RO || c.Args != "" && !strings.HasPrefix(c.Args, "[") {
			continue // needs a target
		}
		if c.Path[0] == "help" || c.Path[0] == "tui" || c.Path[0] == "completion" {
			continue // no JSON envelope of their own, or a terminal
		}
		if !slices.Contains(jsonMigrated, c.Path[0]) {
			continue
		}
		env := runJSON(t, workspace, c.Path)
		if env.Schema != 3 {
			t.Errorf("ws %s --json: schema %d, want 3", c.name(), env.Schema)
		}
		if want := strings.Join(c.Path, "."); env.Command != want {
			t.Errorf("ws %s --json: command %q, want %q", c.name(), env.Command, want)
		}
	}
}

// TestFlagsMayFollowPositionals: a flag after a positional argument is parsed
// (`ws symlink add ~/.x --dir` used to fail because the standard flag package
// stops at the first positional).
func TestFlagsMayFollowPositionals(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := initWorkspace(t)
	dir := filepath.Join(home, ".config", "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.conf"), []byte("x=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "symlink", "add", dir, "--dir", "--dry-run"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("a flag after the positional should parse: exit %d stderr=%s", code, errOut.String())
	}
}

// TestJSONStdoutIsOnlyJSON: with --json, a write command's stdout is the
// envelope and nothing else; the plan's "✔ …" lines used to precede it, so
// `ws --json … | jq` failed.
func TestJSONStdoutIsOnlyJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := initWorkspace(t)
	system := filepath.Join(home, ".bashrc")
	if err := os.WriteFile(system, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "--json", "symlink", "add", system}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	var env jsonEnvelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("stdout is not a single JSON document: %v\n%s", err, out.String())
	}
}
