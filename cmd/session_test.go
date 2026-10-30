package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mugenkunou/ws-tool/internal/agenttmux"
	"github.com/mugenkunou/ws-tool/internal/agenttmux/agenttmuxtest"
)

// fakeExec replaces the exec of agent-tmux for one test and records it.
func fakeExec(t *testing.T) *[]string {
	t.Helper()
	var got []string
	old := execProgram
	execProgram = func(bin string, argv, env []string) error {
		got = append([]string{bin}, argv...)
		return nil
	}
	t.Cleanup(func() { execProgram = old })
	return &got
}

func TestSessionArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		rest []string
		ok   bool
	}{
		{[]string{"session"}, []string{}, true},
		{[]string{"session", "ls", "--json"}, []string{"ls", "--json"}, true},
		{[]string{"--no-color", "-w", "/w", "session", "--help"}, []string{"--help"}, true},
		{[]string{"--workspace", "session", "session", "ls"}, []string{"ls"}, true}, // "session" as -w's value
		{[]string{"help", "session"}, nil, false},
		{[]string{"scratch", "session"}, nil, false},
		{[]string{"sess"}, nil, false},
		{nil, nil, false},
	} {
		rest, ok := sessionArgs(c.args)
		if ok != c.ok || !slices.Equal(rest, c.rest) {
			t.Errorf("sessionArgs(%q) = %q %v, want %q %v", c.args, rest, ok, c.rest, c.ok)
		}
	}
}

// TestSessionPassesEverythingThrough: `ws session …` execs agent-tmux with the
// same words, its flags and --help included; ws adds and prints nothing.
func TestSessionPassesEverythingThrough(t *testing.T) {
	agenttmuxtest.Fake(t, "exit 0")
	bin, err := agenttmux.Path()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ args, want []string }{
		{[]string{"session", "ls", "--json"}, []string{"agent-tmux", "ls", "--json"}},
		{[]string{"session", "--help"}, []string{"agent-tmux", "--help"}},
		{[]string{"session"}, []string{"agent-tmux"}},
		{[]string{"--workspace", "/w", "session", "grep", "--limit", "5", "kube", "ctl"}, []string{"agent-tmux", "grep", "--limit", "5", "kube", "ctl"}},
		{[]string{"session", "rm", "--dry-run", "-q"}, []string{"agent-tmux", "rm", "--dry-run", "-q"}},
	} {
		got := fakeExec(t)
		var out, errOut bytes.Buffer
		if code := Execute(c.args, strings.NewReader(""), &out, &errOut); code != 0 || out.Len() != 0 || errOut.Len() != 0 {
			t.Errorf("ws %q: exit %d, stdout %q, stderr %q", c.args, code, out.String(), errOut.String())
		}
		if len(*got) == 0 || (*got)[0] != bin || !slices.Equal((*got)[1:], c.want) {
			t.Errorf("ws %q execs %q, want %s %q", c.args, *got, bin, c.want)
		}
	}
}

// TestSessionWithoutAgentTmux: the install lines and exit 127.
func TestSessionWithoutAgentTmux(t *testing.T) {
	agenttmuxtest.Missing(t)
	got := fakeExec(t)
	var out, errOut bytes.Buffer
	if code := Execute([]string{"session", "ls"}, strings.NewReader(""), &out, &errOut); code != 127 {
		t.Fatalf("exit %d, want 127", code)
	}
	for _, l := range []string{
		"curl -fLO https://github.com/mugenkunou/agent-tmux/releases/latest/download/agent-tmux-linux-amd64",
		"chmod +x agent-tmux-linux-amd64",
		"mv agent-tmux-linux-amd64 ",
	} {
		if !strings.Contains(errOut.String(), l) {
			t.Errorf("stderr lacks %q:\n%s", l, errOut.String())
		}
	}
	if len(*got) != 0 || out.Len() != 0 {
		t.Errorf("nothing runs and nothing goes to stdout: exec %q, stdout %q", *got, out.String())
	}
}

// TestHelpSessionNamesAgentTmux: `ws help session` is ws's one table row.
func TestHelpSessionNamesAgentTmux(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Execute([]string{"help", "session"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, want := range []string{"ws session <agent-tmux args…>", "agent-tmux's own help."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help lacks %q:\n%s", want, out.String())
		}
	}
}

// TestCompleteSessionDelegates: `ws session <words>` completes as
// `agent-tmux __complete <words>` does, values without their descriptions.
func TestCompleteSessionDelegates(t *testing.T) {
	log := agenttmuxtest.Fake(t, `printf 'web\tlive, 1 client\nweb-2026-10-29-1402\tended\n'`)
	comps, dir := resolveCompletions([]string{"session", "show", "w"}, globalFlags{})
	if !slices.Equal(comps, []string{"web", "web-2026-10-29-1402"}) || dir != compDirectiveNoFileComp {
		t.Errorf("candidates %q directive %d", comps, dir)
	}

	// Through the hidden command, as the shell scripts call it; flags after
	// `session` are agent-tmux's and reach it untouched.
	var out, errOut bytes.Buffer
	if code := Execute([]string{"__complete", "-w", "/w", "session", "grep", "--json", "--session", ""}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if out.String() != "web\nweb-2026-10-29-1402\n:4\n" {
		t.Errorf("__complete output %q", out.String())
	}

	calls := agenttmuxtest.Calls(t, log)
	want := [][]string{{"__complete", "show", "w"}, {"__complete", "grep", "--json", "--session", ""}}
	if len(calls) != 2 || !slices.Equal(calls[0], want[0]) || !slices.Equal(calls[1], want[1]) {
		t.Errorf("agent-tmux calls %q, want %q", calls, want)
	}

	// Still typing the word "session" itself: ws completes it, agent-tmux is not asked.
	if comps, _ := resolveCompletions([]string{"sess"}, globalFlags{}); !slices.Equal(comps, []string{"session"}) {
		t.Errorf("ws sess<tab> = %q", comps)
	}
	if n := len(agenttmuxtest.Calls(t, log)); n != 2 {
		t.Errorf("agent-tmux asked %d times, want 2", n)
	}
}

// TestCompleteSessionWithoutAgentTmux: no candidates, no error.
func TestCompleteSessionWithoutAgentTmux(t *testing.T) {
	agenttmuxtest.Missing(t)
	var out, errOut bytes.Buffer
	if code := Execute([]string{"__complete", "session", ""}, strings.NewReader(""), &out, &errOut); code != 0 || out.String() != ":4\n" || errOut.Len() != 0 {
		t.Errorf("exit %d stdout %q stderr %q", code, out.String(), errOut.String())
	}
}

// TestVersionShowsAgentTmux: `ws version` names the installed agent-tmux and,
// when the version cache knows a newer one, says so with the install lines.
// The cache is fresh, so nothing is spawned or asked.
func TestVersionShowsAgentTmux(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv(agenttmux.NoCheckEnv, "")
	spawns := noSpawn(t)
	if err := os.MkdirAll(filepath.Join(cache, "ws"), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	fresh := `{"ws":{"latest":"v0.1.0"},"agent_tmux":{"latest":"v0.2.0"},"checked_at":"` + now + `","attempted_at":"` + now + `"}`
	if err := os.WriteFile(filepath.Join(cache, "ws", "versions.json"), []byte(fresh), 0o644); err != nil {
		t.Fatal(err)
	}
	agenttmuxtest.Fake(t, `echo '{"version":"v0.1.0","commit":"abc1234","date":""}'`)

	var out, errOut bytes.Buffer
	if code := Execute([]string{"version", "--no-color"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, want := range append([]string{"v0.1.0", "(latest v0.2.0 \u2014 update available)"}, agenttmux.InstallLines()...) {
		if !strings.Contains(out.String(), want) {
			t.Errorf("version lacks %q:\n%s", want, out.String())
		}
	}
	if spawns.Load() != 0 {
		t.Error("a fresh cache spawned a refresh")
	}

	out.Reset()
	if code := Execute([]string{"version", "--json"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var env struct {
		Data versionData `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil || env.Data.AgentTmux != "v0.1.0" || env.Data.AgentTmuxLatest != "v0.2.0" {
		t.Errorf("json %+v %v", env.Data, err)
	}

	// No agent-tmux: how to install it.
	agenttmuxtest.Missing(t)
	out.Reset()
	Execute([]string{"version", "--no-color"}, strings.NewReader(""), &out, &errOut)
	if !strings.Contains(out.String(), "not installed") || !strings.Contains(out.String(), "mugenkunou/agent-tmux/releases/latest") {
		t.Errorf("without agent-tmux:\n%s", out.String())
	}
}
