package skill

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeCommands knows two nouns: skill (show, install) and repo (root add).
func fakeCommands(words []string) (real, known bool) {
	if words[0] != "skill" && words[0] != "repo" {
		return false, false
	}
	line := strings.Join(words, " ")
	for _, c := range []string{"skill show", "skill install", "repo root add", "repo root"} {
		if line == c || strings.HasPrefix(line, c+" ") {
			return true, true
		}
	}
	return len(words) == 1, true
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func checkIn(home, ws string) CheckInput {
	return CheckInput{
		Home: home, Workspace: ws, Command: fakeCommands,
		Anchors: []string{"notes", "scripts", "agent"}, Schema: 1,
		Env:    map[string]string{"WS_TMP": filepath.Join(home, "tmp"), "WS_CACHE": filepath.Join(home, ".cache", "ws")},
		Getenv: func(k string) string { return map[string]string{"HOME": home}[k] },
	}
}

// messages are the findings as "level: message", for matching.
func messages(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Level+": "+f.Message)
	}
	return out
}

func hasMessage(got []string, want string) bool {
	return slices.ContainsFunc(got, func(m string) bool { return strings.Contains(m, want) })
}

func TestCheckFindsDrift(t *testing.T) {
	builtins(t, "v1", map[string]string{
		"workspace-write/SKILL.md": "Run `ws skill show workspace-write --path` and `ws skill frobnicate`.\n",
	})
	home, ws := env(t)
	if err := Refresh(home); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(Root(ws), "mine", "SKILL.md"), strings.Join([]string{
		"---", "name: mine", "metadata:", `  ws-layout: "2"`, "---",
		"Notes go in `notes/kept.md`, never `notes/gone.md` or `notes/<topic>.md`. Run `scripts/own.py`.",
		"```sh", "ws repo root add ~/src", "ws repo rot", "```",
		"The ws tool stops here; `ws.json` is not a path to check.", "",
	}, "\n"))
	writeFile(t, filepath.Join(ws, "notes", "kept.md"), "x")
	writeFile(t, filepath.Join(Root(ws), "mine", "scripts", "own.py"), "x") // in the skill's own folder
	writeFile(t, filepath.Join(ws, "scripts", "good.sh"), "#!/bin/sh\n# Prints hello.\necho \"$HOME/tmp\" ~/tmp\n")
	writeFile(t, filepath.Join(ws, "scripts", "bad.sh"), "#!/bin/sh\ncd /tmp && ls\n")
	writeFile(t, filepath.Join(ws, "scripts", "README.md"), "not a script, /tmp is fine here")
	writeFile(t, filepath.Join(ws, "scripts", "lib", "common.sh"), strings.Join([]string{
		`WS_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)`,
		`WS_TMP=${TMPDIR:-$HOME/tmp}`,
		`WS_CACHE=${XDG_CACHE_HOME:-$HOME/.cache}/old-ws`, "",
	}, "\n"))

	got, err := Check(checkIn(home, ws))
	if err != nil {
		t.Fatal(err)
	}
	msgs := messages(got)
	for _, want := range []string{
		"error: `ws skill frobnicate` is not a ws command",
		"error: `ws repo rot` is not a ws command",
		"error: `notes/gone.md` exists neither",
		`warning: ws-layout is "2"; ws.json's schema is 1`,
		"error: no header comment",
		"error: mentions /tmp",
		"warning: WS_CACHE falls back to " + filepath.Join(home, ".cache", "old-ws"),
	} {
		if !hasMessage(msgs, want) {
			t.Errorf("no %q in:\n%s", want, strings.Join(msgs, "\n"))
		}
	}
	for _, not := range []string{"skill show", "repo root add", "kept.md", "own.py", "<topic>", "WS_TMP falls", "WS_ROOT", "good.sh"} {
		if hasMessage(msgs, not) {
			t.Errorf("%q reported:\n%s", not, strings.Join(msgs, "\n"))
		}
	}
	for _, f := range got {
		if strings.HasPrefix(f.Message, "mentions /tmp") && filepath.Base(f.File) != "bad.sh" {
			t.Errorf("/tmp reported in %s", f.File)
		}
	}
}

func TestCheckCleanWorkspace(t *testing.T) {
	builtins(t, "v1", nil)
	home, ws := env(t)
	addSkill(t, ws, "plain")
	got, err := Check(checkIn(home, ws))
	if err != nil || len(got) != 0 {
		t.Fatalf("findings = %v, %v", got, err)
	}
}

func TestShellValue(t *testing.T) {
	env := map[string]string{"HOME": "/h", "SET": "/s"}
	get := func(k string) string { return env[k] }
	for in, want := range map[string]string{
		`${UNSET:-$HOME/tmp}`:        "/h/tmp",
		`"${SET:-$HOME/tmp}"`:        "/s",
		`${UNSET-${HOME}/.cache}/ws`: "/h/.cache/ws",
		`'$HOME/literal'`:            "$HOME/literal",
		`$HOME/x # a comment`:        "/h/x",
	} {
		if got := shellValue(in, get); got != want {
			t.Errorf("shellValue(%s) = %q, want %q", in, got, want)
		}
	}
}
