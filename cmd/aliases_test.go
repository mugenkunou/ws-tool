package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// withAllAliasesLive switches every alias on for a test of the rewriting
// itself, whatever the state of the noun migration.
func withAllAliasesLive(t *testing.T) {
	t.Helper()
	saved := aliasLive
	aliasLive = map[string]bool{}
	for _, a := range aliases {
		aliasLive[a.Old[0]] = true
	}
	t.Cleanup(func() { aliasLive = saved })
}

// TestAliasTableIsHidden: an alias is never a command in the table, and the
// old word is never offered by completion (spec §3.1 rule 6).
func TestAliasTableIsHidden(t *testing.T) {
	for _, a := range aliases {
		if _, ok := findCommand(a.Old); ok && !slices.Equal(a.Old, a.New) {
			// An alias that keeps its command name only rewrites old arguments
			// (`repo scan`).
			t.Errorf("alias %q is also a command in the table", strings.Join(a.Old, " "))
		}
		if len(a.Old) == 1 {
			if slices.Contains(topLevelCommands, a.Old[0]) && a.Old[0] != "config" {
				t.Errorf("completion offers the hidden alias %q", a.Old[0])
			}
			continue
		}
		prefix := a.Old[:len(a.Old)-1]
		offered, _ := resolveCompletions(append(slices.Clone(prefix), ""), globalFlags{})
		if slices.Contains(offered, a.Old[len(a.Old)-1]) && !slices.Equal(a.Old, a.New) {
			t.Errorf("completion offers the hidden alias %q", strings.Join(a.Old, " "))
		}
	}
}

// TestAliasTargetsExist: every alias without a rewrite reaches a command in the
// table, or one of the plumbing commands.
func TestAliasTargetsExist(t *testing.T) {
	for _, a := range aliases {
		if a.Removed != "" {
			if !strings.HasPrefix(a.Removed, "ws: ") {
				t.Errorf("%q: removed message %q should start with \"ws: \"", strings.Join(a.Old, " "), a.Removed)
			}
			continue
		}
		target := slices.DeleteFunc(slices.Clone(a.New), func(w string) bool { return strings.HasPrefix(w, "-") })
		if _, ok := findCommand(target); ok {
			continue
		}
		if len(target) == 1 && isNoun(target[0]) {
			continue // a whole noun renamed
		}
		if slices.Contains(plumbingCommands, strings.Join(target, " ")) {
			continue
		}
		t.Errorf("alias %q has no target %q", strings.Join(a.Old, " "), strings.Join(target, " "))
	}
}

// TestAliasRewrites pins the argument rewriting of the aliases that change
// their arguments, not just the command words.
func TestAliasRewrites(t *testing.T) {
	withAllAliasesLive(t)
	ws := filepath.Join(t.TempDir(), "Workspace")
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"repo scan fetches", []string{"repo", "scan"}, []string{"repo", "ls", "--fetch"}},
		{"repo scan --no-fetch", []string{"repo", "scan", "--no-fetch", "--dirty"}, []string{"repo", "ls", "--dirty"}},
		{"repo doctor", []string{"repo", "doctor"}, []string{"repo", "ls"}},
		{"repo add-root", []string{"repo", "add-root", "/x"}, []string{"repo", "root", "add", "/x"}},
		{"trash purge", []string{"trash", "purge", "--older-than", "30"}, []string{"trash", "prune", "--older-than", "30"}},
		{"scratch search", []string{"scratch", "search", "dns", "proxy"},
			[]string{"scratch", "ls", "--filter", "dns", "--filter", "proxy"}},
		{"ignore scan", []string{"ignore", "scan"}, []string{"ignore", "lint"}},
		{"ignore scan expand", []string{"ignore", "scan", "--expand-suppressed"}, []string{"ignore", "ls", "--muted"}},
		{"secret scan", []string{"secret", "scan", "--skip-dir", "d"}, []string{"secret", "ls", "--skip-dir", "d"}},
		{"secret scan --pass", []string{"secret", "scan", "--pass"}, []string{"pass", "audit"}},
		{"secret git remote", []string{"secret", "git", "remote", "url"}, []string{"pass", "backup", "enable", "--remote", "url"}},
		{"cron status job", []string{"cron", "status", "mega-sync"}, []string{"cron", "ls", "--filter", "name=mega-sync"}},
		{"config --defaults", []string{"config", "--defaults"}, []string{"config", "show", "--defaults"}},
		{"config show is not an alias", []string{"config", "show"}, nil},
		{"completions bash", []string{"completions", "bash"}, []string{"completion", "bash"}},
		{"credential get", []string{"credential", "get"}, []string{"git-credential-helper", "get"}},
		{"credential enable is current", []string{"credential", "enable"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := globalFlags{workspace: ws}
			var errOut bytes.Buffer
			got, _, done := resolveAlias(c.in, &g, &errOut)
			if done {
				t.Fatalf("unexpectedly a removed command: %s", errOut.String())
			}
			want := c.want
			if want == nil {
				want = c.in
			}
			if !slices.Equal(got, want) {
				t.Fatalf("%v rewrote to %v, want %v", c.in, got, want)
			}
		})
	}
}

// TestAliasesPrintOneLine: a typed alias prints the rename once on stderr, and
// --quiet, --json and plumbing aliases print nothing (spec §3.1 rule 6).
func TestAliasesPrintOneLine(t *testing.T) {
	withAllAliasesLive(t)
	run := func(g globalFlags, in ...string) string {
		var errOut bytes.Buffer
		resolveAlias(in, &g, &errOut)
		return errOut.String()
	}
	if got := run(globalFlags{}, "trash", "purge"); got != "ws: \"trash purge\" is now \"trash prune\"\n" {
		t.Fatalf("typed alias printed %q", got)
	}
	if got := run(globalFlags{quiet: true}, "ignore", "scan"); got != "" {
		t.Fatalf("--quiet must silence the line, got %q", got)
	}
	if got := run(globalFlags{json: true}, "ignore", "scan"); got != "" {
		t.Fatalf("--json must silence the line, got %q", got)
	}
}

// TestPlumbingAliasesAreSilent: what rc files, crontab lines and git call
// prints nothing of its own (spec §3.5).
func TestPlumbingAliasesAreSilent(t *testing.T) {
	withAllAliasesLive(t)
	for _, a := range aliases {
		if !a.Plumbing {
			continue
		}
		var errOut bytes.Buffer
		g := globalFlags{}
		resolveAlias(a.Old, &g, &errOut)
		if errOut.Len() != 0 {
			t.Errorf("plumbing alias %q printed %q", strings.Join(a.Old, " "), errOut.String())
		}
	}
	// The shell scripts and crontab lines the ws binary itself writes call
	// these; none of them may be a typed alias.
	for _, line := range [][]string{
		{"completions", "bash"}, {"completions", "zsh"}, {"completions", "fish"},
		{"credential", "get"}, {"credential", "store"}, {"credential", "erase"},
	} {
		a, _, ok := findAlias(line, &globalFlags{})
		if !ok || !a.Plumbing {
			t.Errorf("%v must be a plumbing alias", line)
		}
	}
}

// TestRemovedCommandsPointToSuccessors: reset and restore, ignore fix and
// secret fix print what replaces them and exit 1 (spec §5, §3.5).
func TestRemovedCommandsPointToSuccessors(t *testing.T) {
	withAllAliasesLive(t)
	cases := map[string]string{
		"reset":      "ws symlink unlink --all, ws trash disable, ws cron rm <job>",
		"restore":    "ws init, ws symlink link, ws trash enable, ws cron apply",
		"ignore fix": "ws ignore add",
		"secret fix": "ws secret allow",
	}
	for line, want := range cases {
		var out, errOut bytes.Buffer
		code := Execute(strings.Fields(line), strings.NewReader(""), &out, &errOut)
		if code != 1 || !strings.Contains(errOut.String(), want) || out.Len() != 0 {
			t.Errorf("ws %s: exit %d stdout %q stderr %q, want exit 1 and %q on stderr", line, code, out.String(), errOut.String(), want)
		}
	}
}

// TestAliasesReachTheirTarget: for every alias that is switched on, the old
// command prints what the new one does, with the new command name in --json.
func TestAliasesReachTheirTarget(t *testing.T) {
	workspace := initWorkspace(t)
	pairs := [][2][]string{
		{{"cron", "list"}, {"cron", "ls"}},
		{{"config", "view"}, {"config", "show"}},
		{{"scratch", "search"}, {"scratch", "ls"}},
		{{"trash", "setup", "--dry-run"}, {"trash", "enable", "--dry-run"}},
		{{"secret", "scan"}, {"secret", "ls"}},
		{{"secret", "scan", "--pass"}, {"pass", "audit"}},
		{{"secret", "status"}, {"pass", "status"}},
		{{"secret", "git", "status"}, {"pass", "backup", "status"}},
		{{"git-credential-helper", "status"}, {"credential", "status"}},
	}
	for _, p := range pairs {
		if !aliasLive[p[0][0]] {
			continue // the noun's new commands have not landed yet
		}
		oldJSON := runJSON(t, workspace, p[0])
		newJSON := runJSON(t, workspace, p[1])
		if oldJSON.Command != newJSON.Command {
			t.Errorf("ws %s reports command %q, ws %s reports %q", strings.Join(p[0], " "), oldJSON.Command, strings.Join(p[1], " "), newJSON.Command)
		}
	}
}

type jsonEnvelope struct {
	Schema  int             `json:"schema"`
	Command string          `json:"command"`
	Data    json.RawMessage `json:"data"`
}

// runJSON runs a command with --json on a workspace and decodes the envelope.
func runJSON(t *testing.T, workspace string, args []string) jsonEnvelope {
	t.Helper()
	var out, errOut bytes.Buffer
	full := append([]string{"--workspace", workspace, "--json"}, args...)
	Execute(full, strings.NewReader(""), &out, &errOut)
	var env jsonEnvelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("ws %s --json: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, out.String(), errOut.String())
	}
	return env
}
