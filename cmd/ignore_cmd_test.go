package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// igEnv is one isolated machine with an initialized workspace for the ignore
// commands. HOME is temporary too, so nothing reads the real one.
type igEnv struct {
	ws string
}

func newIgEnv(t *testing.T) igEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return igEnv{ws: initWorkspace(t)}
}

func (e igEnv) run(stdin string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := Execute(append([]string{"--workspace", e.ws, "--no-color"}, args...), strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

// do runs a command that accepts every action and must succeed.
func (e igEnv) do(t *testing.T, args ...string) string {
	t.Helper()
	code, out, errOut := e.run("a\n", args...)
	if code != 0 {
		t.Fatalf("ws %s: exit %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), code, out, errOut)
	}
	return out
}

func (e igEnv) write(t *testing.T, rel, content string) string {
	t.Helper()
	p := filepath.Join(e.ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func (e igEnv) abs(rel string) string { return filepath.Join(e.ws, filepath.FromSlash(rel)) }

func (e igEnv) cfg(t *testing.T) wsconfig.WsConfig {
	t.Helper()
	c, err := wsconfig.Load(filepath.Join(e.ws, "ws.json"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (e igEnv) excludes(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, x := range e.cfg(t).IgnoreRules.Exclude {
		out = append(out, x.Pattern)
	}
	return out
}

func (e igEnv) muted(t *testing.T) []string { return e.cfg(t).Ignore.Muted }

func (e igEnv) readFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.ws, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (e igEnv) setCfg(t *testing.T, f func(*wsconfig.WsConfig)) {
	t.Helper()
	c := e.cfg(t)
	f(&c)
	if err := wsconfig.Save(filepath.Join(e.ws, "ws.json"), c); err != nil {
		t.Fatal(err)
	}
}

func (e igEnv) jsonOf(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	code, out, errOut := e.run("", append([]string{"--json"}, args...)...)
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("ws %s --json: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, out, errOut)
	}
	return code, env
}

// bigFile writes a file over the 1 MB warning threshold.
func (e igEnv) bigFile(t *testing.T, rel string) string {
	t.Helper()
	return e.write(t, rel, strings.Repeat("x", 2*1024*1024))
}

func igHas(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s lacks %q:\n%s", what, w, got)
		}
	}
}

// ── add ────────────────────────────────────────────────────────────

func TestIgnoreAddWritesRuleAndMegaignore(t *testing.T) {
	e := newIgEnv(t)
	e.write(t, "experiments/run/dump.bin", "x")
	e.write(t, "experiments/run/keep.txt", "y")

	out := e.do(t, "ignore", "add", e.abs("experiments/run/dump.bin"))
	igHas(t, "plan", out, "Ignore "+e.abs("experiments/run/dump.bin")+" (ws.json ignore_rules.exclude)")

	if got := e.excludes(t); len(got) != 1 || got[0] != "experiments/run/dump.bin" {
		t.Fatalf("ignore_rules.exclude = %v", got)
	}
	igHas(t, ".megaignore", e.readFile(t, ".megaignore"), "-:experiments/run/dump.bin")

	_, check, _ := e.run("", "ignore", "check", e.abs("experiments/run/dump.bin"))
	igHas(t, "check", check, "IGNORED", "rule `-:experiments/run/dump.bin` (ws.json)")
	_, check, _ = e.run("", "ignore", "check", e.abs("experiments/run/keep.txt"))
	igHas(t, "check", check, "SYNCED", "no rule matches")
}

func TestIgnoreAddDirectoryIgnoresEverythingInIt(t *testing.T) {
	e := newIgEnv(t)
	e.write(t, "experiments/run/a.txt", "1")
	e.write(t, "experiments/run/sub/b.txt", "2")
	e.do(t, "ignore", "add", e.abs("experiments/run"))

	_, ls, _ := e.run("", "ignore", "ls")
	igHas(t, "ls", ls, e.abs("experiments/run/a.txt"), e.abs("experiments/run/sub/b.txt"), "-:experiments/run")
}

func TestIgnoreAddNeedsAPath(t *testing.T) {
	e := newIgEnv(t)
	code, _, errOut := e.run("a\n", "ignore", "add")
	if code != 1 || !strings.Contains(errOut, "usage: ws ignore add <path…>") {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	if len(e.excludes(t)) != 0 {
		t.Fatal("nothing may be added without a target")
	}
}

func TestIgnoreAddDryRunChangesNothing(t *testing.T) {
	e := newIgEnv(t)
	f := e.write(t, "a/big.bin", "x")
	before := e.readFile(t, "ws.json")
	megaBefore := e.readFile(t, ".megaignore")

	code, out, _ := e.run("", "--dry-run", "ignore", "add", f)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	igHas(t, "dry run", out, "[dry-run]", "Ignore "+f)
	if e.readFile(t, "ws.json") != before || e.readFile(t, ".megaignore") != megaBefore {
		t.Fatal("--dry-run must change nothing")
	}
}

func TestIgnoreAddInvalidTargetChangesNothing(t *testing.T) {
	e := newIgEnv(t)
	good := e.write(t, "a/good.bin", "x")
	outside := filepath.Join(t.TempDir(), "elsewhere.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := e.readFile(t, "ws.json")

	for name, bad := range map[string]string{
		"missing":   e.abs("a/missing.bin"),
		"outside":   outside,
		"workspace": e.ws,
	} {
		code, _, errOut := e.run("a\n", "ignore", "add", good, bad)
		if code != 1 {
			t.Errorf("%s: exit %d, want 1", name, code)
		}
		if errOut == "" {
			t.Errorf("%s: no reason printed", name)
		}
		if e.readFile(t, "ws.json") != before {
			t.Errorf("%s: ws.json changed although a target was invalid", name)
		}
	}
}

func TestIgnoreAddRefusesSymlinkOriginal(t *testing.T) {
	e := newIgEnv(t)
	orig := e.write(t, "symlinks/home/.bashrc", "export A=1\n")
	e.write(t, "symlinks/home/dir/x", "x")
	e.setCfg(t, func(c *wsconfig.WsConfig) {
		c.Symlinks = append(c.Symlinks,
			wsconfig.Link{System: "/home/u/.bashrc", Original: "symlinks/home/.bashrc"},
			wsconfig.Link{System: "/home/u/.d", Original: "symlinks/home/dir", Dir: true})
	})

	for _, p := range []string{orig, e.abs("symlinks/home/dir"), e.abs("symlinks/home/dir/x"), e.abs("symlinks/home"), e.abs("symlinks")} {
		code, _, errOut := e.run("a\n", "ignore", "add", p)
		if code != 1 || !strings.Contains(errOut, "symlink") {
			t.Errorf("%s: exit %d stderr %q, want a refusal naming the symlink", p, code, errOut)
		}
	}
	if len(e.excludes(t)) != 0 {
		t.Fatalf("a symlink original must never be ignored: %v", e.excludes(t))
	}
}

func TestIgnoreAddRefusesGlobNames(t *testing.T) {
	e := newIgEnv(t)
	f := e.write(t, "a/we*ird.txt", "x")
	code, _, errOut := e.run("a\n", "ignore", "add", f)
	if code != 1 || !strings.Contains(errOut, "*") {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
}

func TestIgnoreAddAlreadyIgnoredIsNoAction(t *testing.T) {
	e := newIgEnv(t)
	csv := e.write(t, "a/data.csv", "1,2")
	before := e.readFile(t, "ws.json")

	code, out, _ := e.run("a\n", "ignore", "add", csv)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	igHas(t, "message", out, "Already ignored", "-g:*.csv", "(built-in)")
	if e.readFile(t, "ws.json") != before {
		t.Fatal("an already ignored path gets no action")
	}

	// Adding twice adds one entry.
	f := e.write(t, "a/x.bin", "x")
	e.do(t, "ignore", "add", f)
	_, out, _ = e.run("a\n", "ignore", "add", f)
	igHas(t, "second add", out, "Already ignored", "(ws.json)")
	if len(e.excludes(t)) != 1 {
		t.Fatalf("excludes = %v", e.excludes(t))
	}
}

func TestIgnoreAddJSONReportsActions(t *testing.T) {
	e := newIgEnv(t)
	f := e.write(t, "a/x.bin", "x")
	code, env := e.jsonOf(t, "ignore", "add", f)
	if code != 0 || env["command"] != "ignore.add" || env["schema"] != float64(3) {
		t.Fatalf("exit %d envelope %v", code, env)
	}
	actions := env["data"].(map[string]any)["actions"].([]any)
	if len(actions) != 1 || actions[0].(map[string]any)["status"] != "executed" {
		t.Fatalf("actions = %v", actions)
	}
	if got := e.excludes(t); len(got) != 1 {
		t.Fatalf("excludes = %v", got)
	}
}

// TestIgnoreAddWorksInsideIncludedDirectories: user excludes come last, so
// adding a file under an included directory (configs/, archive/ or labs/)
// really stops it syncing.
func TestIgnoreAddWorksInsideIncludedDirectories(t *testing.T) {
	e := newIgEnv(t)
	e.includeDir(t, "configs")
	e.includeDir(t, "archive")
	e.write(t, "configs/app/old.conf", "x")
	e.write(t, "archive/2026/dump.sql", "x")
	e.write(t, "labs/run.bin", "x")
	e.setCfg(t, func(c *wsconfig.WsConfig) {
		c.IgnoreRules.Include = append(c.IgnoreRules.Include, ruleEntry("labs/**"))
	})
	for _, p := range []string{"configs/app/old.conf", "archive/2026/dump.sql", "labs/run.bin"} {
		e.do(t, "ignore", "add", e.abs(p))
		_, out, _ := e.run("", "ignore", "check", e.abs(p))
		igHas(t, p, out, "IGNORED")
	}
	mega := e.readFile(t, ".megaignore")
	if strings.Index(mega, "-:configs/app/old.conf") < strings.Index(mega, "+g:configs/**") ||
		strings.Index(mega, "-:labs/run.bin") < strings.Index(mega, "+g:labs/**") {
		t.Fatalf("user excludes must follow every include rule:\n%s", mega)
	}
}

// TestActivatedExcludeRulesTakeEffect: an ignore_rules.exclude entry written
// when user excludes still came before the include rules (a silent no-op,
// for instance from the old `secret fix --secret-mode exclude`) now ignores its
// file, and the next rewrite puts it into .megaignore.
func TestActivatedExcludeRulesTakeEffect(t *testing.T) {
	e := newIgEnv(t)
	e.includeDir(t, "configs")
	secret := e.write(t, "configs/old.env", "TOKEN=x")
	dot := e.write(t, "symlinks/home/.bashrc", "export A=1\n")
	e.setCfg(t, func(c *wsconfig.WsConfig) {
		c.IgnoreRules.Exclude = append(c.IgnoreRules.Exclude, ruleEntry("configs/old.env"), ruleEntry("symlinks/home/.bashrc"))
	})

	_, ls, _ := e.run("", "ignore", "ls")
	igHas(t, "ignore ls", ls, secret, dot)
	_, check, _ := e.run("", "ignore", "check", secret)
	igHas(t, "check", check, "IGNORED", "(ws.json)")

	// The hand-written entries are not in .megaignore yet: ws warns, then the
	// next rewrite writes them after the include rules.
	_, _, errOut := e.run("", "ignore", "ls")
	igHas(t, "stale warning", errOut, "differs from what ws.json generates")
	f := e.write(t, "notes/x.bin", "x")
	e.do(t, "ignore", "add", f)
	mega := e.readFile(t, ".megaignore")
	igHas(t, ".megaignore", mega, "-:configs/old.env", "-:symlinks/home/.bashrc", "-:notes/x.bin")
	if strings.Index(mega, "-:configs/old.env") < strings.Index(mega, "+g:configs/**") {
		t.Fatalf("activated excludes must follow +g:configs/**:\n%s", mega)
	}
	_, _, errOut = e.run("", "ignore", "ls")
	if strings.Contains(errOut, "differs") {
		t.Fatalf("a rewritten .megaignore is not stale: %s", errOut)
	}
}

// ── rm ─────────────────────────────────────────────────────────────

func TestIgnoreRmRemovesExactEntry(t *testing.T) {
	e := newIgEnv(t)
	f := e.write(t, "a/x.bin", "x")
	keep := e.write(t, "a/y.bin", "y")
	e.do(t, "ignore", "add", f, keep)

	out := e.do(t, "ignore", "rm", f)
	igHas(t, "plan", out, "Stop ignoring "+f+` (remove "a/x.bin" from ignore_rules.exclude)`)
	if got := e.excludes(t); len(got) != 1 || got[0] != "a/y.bin" {
		t.Fatalf("excludes = %v", got)
	}
	mega := e.readFile(t, ".megaignore")
	if strings.Contains(mega, "-:a/x.bin") || !strings.Contains(mega, "-:a/y.bin") {
		t.Fatalf(".megaignore not regenerated:\n%s", mega)
	}
	_, check, _ := e.run("", "ignore", "check", f)
	igHas(t, "check", check, "SYNCED")
}

func TestIgnoreRmPathNeedNotExist(t *testing.T) {
	e := newIgEnv(t)
	f := e.write(t, "a/gone.bin", "x")
	e.do(t, "ignore", "add", f)
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	e.do(t, "ignore", "rm", f)
	if len(e.excludes(t)) != 0 {
		t.Fatalf("excludes = %v", e.excludes(t))
	}
}

func TestIgnoreRmWiderRuleRefuses(t *testing.T) {
	e := newIgEnv(t)
	e.write(t, "exp/run/a.txt", "1")
	e.do(t, "ignore", "add", e.abs("exp/run"))
	before := e.readFile(t, "ws.json")

	code, _, errOut := e.run("a\n", "ignore", "rm", e.abs("exp/run/a.txt"))
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	igHas(t, "refusal", errOut, "`-:exp/run`", "ws ignore edit")
	if e.readFile(t, "ws.json") != before {
		t.Fatal("a refused rm changes nothing")
	}
}

func TestIgnoreRmBuiltinRuleRefusesWithHint(t *testing.T) {
	e := newIgEnv(t)
	csv := e.write(t, "a/data.csv", "1")
	before := e.readFile(t, "ws.json")
	code, _, errOut := e.run("a\n", "ignore", "rm", csv)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	igHas(t, "refusal", errOut, "built-in rule `-g:*.csv`", "suppress_defaults", `"*.csv"`, "ws ignore edit")
	if strings.Contains(errOut, "ignore mute") {
		t.Fatalf("muting does not change syncing and must not be suggested:\n%s", errOut)
	}
	if e.readFile(t, "ws.json") != before {
		t.Fatal("a refused rm changes nothing")
	}
}

func TestIgnoreRmNotIgnoredIsNothingToDo(t *testing.T) {
	e := newIgEnv(t)
	f := e.write(t, "a/x.txt", "x")
	code, out, _ := e.run("a\n", "ignore", "rm", f)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	igHas(t, "message", out, "Not ignored")
}

func TestIgnoreRmMixedTargetsChangeNothingWhenOneIsRefused(t *testing.T) {
	e := newIgEnv(t)
	f := e.write(t, "a/x.bin", "x")
	csv := e.write(t, "a/d.csv", "1")
	e.do(t, "ignore", "add", f)
	code, _, _ := e.run("a\n", "ignore", "rm", f, csv)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if len(e.excludes(t)) != 1 {
		t.Fatalf("nothing may change: %v", e.excludes(t))
	}
}

func TestIgnoreRmPlanSaysWhenStillIgnored(t *testing.T) {
	e := newIgEnv(t)
	data := e.write(t, "exp/x.csv", "1")
	e.setCfg(t, func(c *wsconfig.WsConfig) {
		c.IgnoreRules.Exclude = append(c.IgnoreRules.Exclude, ruleEntry("exp/x.csv"))
	})
	code, out, _ := e.run("", "--dry-run", "ignore", "rm", data)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	igHas(t, "plan", out, "still ignored by `-g:*.csv`")
}

func TestIgnoreRmNeedsAPath(t *testing.T) {
	e := newIgEnv(t)
	if code, _, errOut := e.run("", "ignore", "rm"); code != 1 || !strings.Contains(errOut, "usage: ws ignore rm") {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
}

// ── mute / unmute ─────────────────────────────────────────────────

func TestIgnoreMuteHidesWarningsAndLeavesSyncAlone(t *testing.T) {
	e := newIgEnv(t)
	e.bigFile(t, "ml/model.bin")
	e.bigFile(t, "other/blob.bin")

	_, lint, _ := e.run("", "ignore", "lint")
	igHas(t, "lint before", lint, e.abs("ml/model.bin"), e.abs("other/blob.bin"))

	megaBefore := e.readFile(t, ".megaignore")
	rulesBefore := e.cfg(t).IgnoreRules
	out := e.do(t, "ignore", "mute", e.abs("ml"))
	igHas(t, "plan", out, "Mute "+e.abs("ml")+" (it keeps syncing)")

	if got := e.muted(t); len(got) != 1 || got[0] != "ml" {
		t.Fatalf("ignore.muted = %v", got)
	}
	if e.readFile(t, ".megaignore") != megaBefore {
		t.Fatal("mute must not touch .megaignore")
	}
	after := e.cfg(t).IgnoreRules
	if len(after.Exclude) != len(rulesBefore.Exclude) || len(after.Include) != len(rulesBefore.Include) {
		t.Fatal("mute must add no rule")
	}
	_, check, _ := e.run("", "ignore", "check", e.abs("ml/model.bin"))
	igHas(t, "check", check, "SYNCED")

	code, lint, errOut := e.run("", "ignore", "lint")
	if code != 0 {
		t.Fatalf("lint exit %d", code)
	}
	if strings.Contains(lint, "ml/model.bin") || !strings.Contains(lint, "other/blob.bin") {
		t.Fatalf("lint after mute:\n%s", lint)
	}
	igHas(t, "muted count", errOut, "1 muted warning", "ws ignore ls --muted")

	_, muted, _ := e.run("", "ignore", "ls", "--muted")
	igHas(t, "ls --muted", muted, e.abs("ml/model.bin"), "INFO")
	if strings.Contains(muted, "other/blob.bin") {
		t.Fatalf("ls --muted lists a live warning:\n%s", muted)
	}
}

func TestIgnoreMuteSingleFile(t *testing.T) {
	e := newIgEnv(t)
	f := e.bigFile(t, "a/one.bin")
	e.bigFile(t, "a/two.bin")
	e.do(t, "ignore", "mute", f)
	_, lint, _ := e.run("", "ignore", "lint")
	if strings.Contains(lint, "one.bin") || !strings.Contains(lint, "two.bin") {
		t.Fatalf("lint:\n%s", lint)
	}
}

func TestIgnoreMuteRefusals(t *testing.T) {
	e := newIgEnv(t)
	before := e.readFile(t, "ws.json")
	for name, args := range map[string][]string{
		"no path":   {"ignore", "mute"},
		"workspace": {"ignore", "mute", e.ws},
		"missing":   {"ignore", "mute", e.abs("nope")},
		"outside":   {"ignore", "mute", t.TempDir()},
	} {
		if code, _, _ := e.run("a\n", args...); code != 1 {
			t.Errorf("%s: exit %d, want 1", name, code)
		}
	}
	if e.readFile(t, "ws.json") != before {
		t.Fatal("refused mutes change nothing")
	}
}

func TestIgnoreMuteAlreadyMutedSaysWhy(t *testing.T) {
	e := newIgEnv(t)
	e.includeDir(t, "archive")
	e.bigFile(t, "ml/sub/m.bin")
	e.bigFile(t, "archive/a.bin")
	e.do(t, "ignore", "mute", e.abs("ml"))

	_, out, _ := e.run("a\n", "ignore", "mute", e.abs("ml"))
	igHas(t, "itself", out, "Already muted")
	_, out, _ = e.run("a\n", "ignore", "mute", e.abs("ml/sub"))
	igHas(t, "parent", out, "Already muted", "parent "+e.abs("ml"))
	_, out, _ = e.run("a\n", "ignore", "mute", e.abs("archive/a.bin"))
	igHas(t, "include", out, "Already muted", "included directory", "+g:archive/**")
	if got := e.muted(t); len(got) != 1 {
		t.Fatalf("ignore.muted = %v", got)
	}
}

func TestIgnoreMuteSiblingWithSharedPrefixIsNotMuted(t *testing.T) {
	e := newIgEnv(t)
	e.bigFile(t, "ml/a.bin")
	e.bigFile(t, "ml-old/b.bin")
	e.do(t, "ignore", "mute", e.abs("ml"))
	_, lint, _ := e.run("", "ignore", "lint")
	if !strings.Contains(lint, "ml-old/b.bin") || strings.Contains(lint, "ml/a.bin") {
		t.Fatalf("lint:\n%s", lint)
	}
}

func TestIgnoreMuteJSONAndDryRun(t *testing.T) {
	e := newIgEnv(t)
	e.bigFile(t, "ml/a.bin")
	code, env := e.jsonOf(t, "--dry-run", "ignore", "mute", e.abs("ml"))
	if code != 0 || env["command"] != "ignore.mute" || env["dry_run"] != true {
		t.Fatalf("exit %d envelope %v", code, env)
	}
	if len(e.muted(t)) != 0 {
		t.Fatal("--dry-run must not mute")
	}
	code, env = e.jsonOf(t, "ignore", "mute", e.abs("ml"))
	actions := env["data"].(map[string]any)["actions"].([]any)
	if code != 0 || len(actions) != 1 || len(e.muted(t)) != 1 {
		t.Fatalf("exit %d actions %v muted %v", code, actions, e.muted(t))
	}
}

func TestIgnoreUnmute(t *testing.T) {
	e := newIgEnv(t)
	e.bigFile(t, "ml/a.bin")
	e.do(t, "ignore", "mute", e.abs("ml"))
	out := e.do(t, "ignore", "unmute", e.abs("ml"))
	igHas(t, "plan", out, "Unmute "+e.abs("ml"))
	if len(e.muted(t)) != 0 {
		t.Fatalf("ignore.muted = %v", e.muted(t))
	}
	_, lint, _ := e.run("", "ignore", "lint")
	igHas(t, "lint", lint, "ml/a.bin")
}

func TestIgnoreUnmuteHandWrittenAbsoluteEntry(t *testing.T) {
	e := newIgEnv(t)
	e.bigFile(t, "ml/a.bin")
	e.setCfg(t, func(c *wsconfig.WsConfig) { c.Ignore.Muted = []string{e.abs("ml")} })
	_, lint, _ := e.run("", "ignore", "lint")
	if strings.Contains(lint, "ml/a.bin") {
		t.Fatalf("an absolute entry mutes too:\n%s", lint)
	}
	e.do(t, "ignore", "unmute", e.abs("ml"))
	if len(e.muted(t)) != 0 {
		t.Fatalf("ignore.muted = %v", e.muted(t))
	}
}

func TestIgnoreUnmuteRefusals(t *testing.T) {
	e := newIgEnv(t)
	e.includeDir(t, "archive")
	e.bigFile(t, "ml/sub/a.bin")
	e.bigFile(t, "archive/a.bin")
	e.do(t, "ignore", "mute", e.abs("ml"))
	before := e.readFile(t, "ws.json")

	code, _, errOut := e.run("a\n", "ignore", "unmute", e.abs("ml/sub"))
	if code != 1 {
		t.Fatalf("parent: exit %d", code)
	}
	igHas(t, "parent", errOut, "muted by its parent "+e.abs("ml"), "unmute that")

	code, _, errOut = e.run("a\n", "ignore", "unmute", e.abs("archive"))
	if code != 1 {
		t.Fatalf("include: exit %d", code)
	}
	igHas(t, "include", errOut, "included directory", "stay muted", "ws ignore edit")

	if code, _, _ := e.run("a\n", "ignore", "unmute"); code != 1 {
		t.Fatalf("no path: exit %d", code)
	}
	if code, _, _ := e.run("a\n", "ignore", "unmute", e.ws); code != 1 {
		t.Fatalf("workspace: exit %d", code)
	}
	if e.readFile(t, "ws.json") != before {
		t.Fatal("refused unmutes change nothing")
	}
}

func TestIgnoreUnmuteNotMutedIsNothingToDo(t *testing.T) {
	e := newIgEnv(t)
	e.write(t, "a/x.txt", "x")
	code, out, _ := e.run("a\n", "ignore", "unmute", e.abs("a"))
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	igHas(t, "message", out, "Not muted")
}

// ── lint ───────────────────────────────────────────────────────────

func TestIgnoreLintExitsZeroWithWarnings(t *testing.T) {
	e := newIgEnv(t)
	e.bigFile(t, "a/big.bin")
	code, out, _ := e.run("", "ignore", "lint")
	if code != 0 {
		t.Fatalf("exit %d, want 0 with warnings", code)
	}
	igHas(t, "lint", out, "SEVERITY", "WARNING", "bloat", e.abs("a/big.bin"))
}

func TestIgnoreLintCleanWorkspace(t *testing.T) {
	e := newIgEnv(t)
	e.write(t, "a/x.txt", "x")
	code, out, _ := e.run("", "ignore", "lint")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	igHas(t, "lint", out, "No warnings.")
}

func TestIgnoreLintPathsRestrictAndValidate(t *testing.T) {
	e := newIgEnv(t)
	e.bigFile(t, "a/big.bin")
	e.bigFile(t, "b/big.bin")
	code, out, _ := e.run("", "ignore", "lint", e.abs("a"))
	if code != 0 || !strings.Contains(out, "a/big.bin") || strings.Contains(out, "b/big.bin") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if code, _, _ := e.run("", "ignore", "lint", t.TempDir()); code != 1 {
		t.Fatalf("outside the workspace: exit %d, want 1", code)
	}
	if code, _, _ := e.run("", "ignore", "lint", e.abs("missing")); code != 1 {
		t.Fatalf("missing: exit %d, want 1", code)
	}
}

func TestIgnoreLintJSONRecords(t *testing.T) {
	e := newIgEnv(t)
	e.includeDir(t, "archive")
	e.bigFile(t, "a/big.bin")
	e.bigFile(t, "archive/muted.bin")
	code, env := e.jsonOf(t, "ignore", "lint")
	if code != 0 || env["command"] != "ignore.lint" {
		t.Fatalf("exit %d envelope %v", code, env)
	}
	rows := env["data"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows = %v", rows)
	}
	row := rows[0].(map[string]any)
	if row["path"] != e.abs("a/big.bin") || row["type"] != "bloat" || row["severity"] != "WARNING" {
		t.Fatalf("row = %v", row)
	}
	if _, ok := row["muted"]; !ok {
		t.Fatalf("a lint record has a muted field: %v", row)
	}
}

func TestIgnoreLintFilterAndUnknownField(t *testing.T) {
	e := newIgEnv(t)
	e.bigFile(t, "a/one.bin")
	e.bigFile(t, "a/two.bin")
	_, out, _ := e.run("", "ignore", "lint", "--filter", "path=two")
	if strings.Contains(out, "one.bin") || !strings.Contains(out, "two.bin") {
		t.Fatalf("filter:\n%s", out)
	}
	if code, _, errOut := e.run("", "ignore", "lint", "--filter", "nosuch=1"); code != 1 || errOut == "" {
		t.Fatalf("unknown field: exit %d stderr %q", code, errOut)
	}
}

func TestIgnoreLintProjectMetaUnderMutedDirIsMuted(t *testing.T) {
	e := newIgEnv(t)
	e.write(t, "app/go.mod", "module x\n")
	e.write(t, "app/bin/tool", "x")
	_, out, _ := e.run("", "ignore", "lint")
	igHas(t, "lint", out, "project-meta", e.abs("app/bin"))
	e.do(t, "ignore", "mute", e.abs("app"))
	_, out, _ = e.run("", "ignore", "lint")
	if strings.Contains(out, "project-meta") {
		t.Fatalf("muted project-meta must leave lint:\n%s", out)
	}
	_, out, _ = e.run("", "ignore", "ls", "--muted")
	igHas(t, "ls --muted", out, "project-meta")
}

// ── ls ─────────────────────────────────────────────────────────────

func TestIgnoreLsListsIgnoredPathsWithRule(t *testing.T) {
	e := newIgEnv(t)
	csv := e.write(t, "exp/metrics.csv", "1,2,3")
	e.write(t, "exp/notes.md", "n")
	run := e.write(t, "exp/run/a.txt", "a")
	e.do(t, "ignore", "add", e.abs("exp/run"))

	code, out, _ := e.run("", "ignore", "ls")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	igHas(t, "ls", out, "SIZE", "PATH", "RULE", csv, "-g:*.csv", run, "-:exp/run")
	if strings.Contains(out, "notes.md") {
		t.Fatalf("a synced path is listed:\n%s", out)
	}

	_, out, _ = e.run("", "ignore", "ls", "--filter", "rule=csv")
	if !strings.Contains(out, "metrics.csv") || strings.Contains(out, "a.txt") {
		t.Fatalf("--filter rule=csv:\n%s", out)
	}
}

func TestIgnoreLsJSONFields(t *testing.T) {
	e := newIgEnv(t)
	csv := e.write(t, "exp/metrics.csv", "1,2,3")
	code, env := e.jsonOf(t, "ignore", "ls")
	if code != 0 || env["command"] != "ignore.ls" {
		t.Fatalf("exit %d envelope %v", code, env)
	}
	// The workspace's own .megaignore is a dot-file, so it is listed too.
	var row map[string]any
	for _, r := range env["data"].([]any) {
		if m := r.(map[string]any); m["path"] == csv {
			row = m
		}
	}
	if row == nil {
		t.Fatalf("no row for %s in %v", csv, env["data"])
	}
	if row["path"] != csv || row["rule"] != "-g:*.csv" || row["rule_source"] != "builtin" || row["size_bytes"] != float64(5) {
		t.Fatalf("row = %v", row)
	}
}

func TestIgnoreLsMutedAndUnknownField(t *testing.T) {
	e := newIgEnv(t)
	e.includeDir(t, "archive")
	e.bigFile(t, "archive/a.bin")
	_, out, _ := e.run("", "ignore", "ls", "--muted")
	igHas(t, "muted view", out, e.abs("archive/a.bin"))
	if code, _, errOut := e.run("", "ignore", "ls", "--sort", "nosuch"); code != 1 || errOut == "" {
		t.Fatalf("unknown field: exit %d stderr %q", code, errOut)
	}
	if code, _, _ := e.run("", "ignore", "ls", "--bogus"); code != 1 {
		t.Fatalf("bad flag: exit %d", code)
	}
}

func TestIgnoreLsOldFlagsStillWork(t *testing.T) {
	e := newIgEnv(t)
	e.write(t, "a/x.csv", "1")
	e.write(t, "b/y.log", "1")
	_, out, _ := e.run("", "ignore", "ls", "--path", e.abs("a"))
	if !strings.Contains(out, "x.csv") || strings.Contains(out, "y.log") {
		t.Fatalf("--path:\n%s", out)
	}
	_, out, _ = e.run("", "ignore", "ls", "--rule", "-g:*.log")
	if !strings.Contains(out, "y.log") || strings.Contains(out, "x.csv") {
		t.Fatalf("--rule:\n%s", out)
	}
	_, out, _ = e.run("", "ignore", "ls", e.abs("a/x.csv"))
	igHas(t, "positional path is check", out, "IGNORED")
}

func TestIgnoreLsWarnsOnStaleMegaignore(t *testing.T) {
	e := newIgEnv(t)
	if err := os.WriteFile(e.abs(".megaignore"), []byte("-:hand-edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"ignore", "ls"}, {"ignore", "lint"}, {"ignore", "check", e.ws}, {"ignore", "tree"}} {
		code, _, errOut := e.run("", args...)
		if code != 0 {
			t.Errorf("%v: exit %d", args, code)
		}
		if strings.Count(errOut, "differs from what ws.json generates") != 1 {
			t.Errorf("%v: want exactly one stale warning, stderr %q", args, errOut)
		}
	}
	// An RW command rewrites it.
	e.write(t, "a/x.bin", "x")
	e.do(t, "ignore", "add", e.abs("a/x.bin"))
	if strings.Contains(e.readFile(t, ".megaignore"), "hand-edit") {
		t.Fatal("ignore add must rewrite a stale .megaignore")
	}
}

// ── aliases ────────────────────────────────────────────────────────

func TestIgnoreScanAliasIsLint(t *testing.T) {
	e := newIgEnv(t)
	e.includeDir(t, "archive")
	e.bigFile(t, "a/big.bin")
	code, out, errOut := e.run("", "ignore", "scan")
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	igHas(t, "scan", out, "big.bin")
	igHas(t, "alias line", errOut, `"ignore scan" is now "ignore lint"`)

	e.bigFile(t, "archive/m.bin")
	_, out, errOut = e.run("", "ignore", "scan", "--expand-suppressed")
	igHas(t, "expand", out, "archive/m.bin")
	igHas(t, "alias line", errOut, `"ignore scan" is now`)
}

func TestIgnoreFixIsGone(t *testing.T) {
	e := newIgEnv(t)
	e.bigFile(t, "a/big.bin")
	before := e.readFile(t, "ws.json")
	code, out, errOut := e.run("d\n", "ignore", "fix")
	if code != 1 || out != "" {
		t.Fatalf("exit %d stdout %q", code, out)
	}
	igHas(t, "stderr", errOut, `"ignore fix" is gone`, "ws ignore add", "ws ignore mute", "ws scratch add")
	if e.readFile(t, "ws.json") != before || !exists2(e.abs("a/big.bin")) {
		t.Fatal("ignore fix must change nothing")
	}
}

func TestIgnoreUnknownVerb(t *testing.T) {
	e := newIgEnv(t)
	if code, _, errOut := e.run("", "ignore", "frobnicate"); code != 1 || !strings.Contains(errOut, "unknown command: ignore frobnicate") {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
}

// ── check ──────────────────────────────────────────────────────────

func TestIgnoreCheckReasons(t *testing.T) {
	e := newIgEnv(t)
	e.includeDir(t, "archive")
	csv := e.write(t, "exp/m.csv", "123")
	arch := e.write(t, "archive/inc.bin", "x")
	e.write(t, "plain/n.txt", "x")

	code, out, _ := e.run("", "ignore", "check", csv)
	if code != 0 {
		t.Fatalf("an ignored path exits 0, got %d", code)
	}
	igHas(t, "ignored", out, "IGNORED", csv, "Reason: rule `-g:*.csv` (built-in)", "Size:")

	_, out, _ = e.run("", "ignore", "check", arch)
	igHas(t, "include", out, "SYNCED", "Reason: included directory `+g:archive/**` (ws.json)")

	_, out, _ = e.run("", "ignore", "check", e.abs("plain/n.txt"))
	igHas(t, "no rule", out, "SYNCED", "Reason: no rule matches")

	_, out, _ = e.run("", "ignore", "check", e.abs("plain"))
	if strings.Contains(out, "Size:") {
		t.Fatalf("a directory has no size line:\n%s", out)
	}
}

func TestIgnoreCheckUserRuleAndUserAlwaysSyncSource(t *testing.T) {
	e := newIgEnv(t)
	e.write(t, "labs/x.csv", "1")
	e.write(t, "exp/z.bin", "1")
	e.setCfg(t, func(c *wsconfig.WsConfig) {
		c.IgnoreRules.Include = append(c.IgnoreRules.Include, ruleEntry("labs/**"))
		c.IgnoreRules.Exclude = append(c.IgnoreRules.Exclude, ruleEntry("exp/z.bin"))
	})
	_, out, _ := e.run("", "ignore", "check", e.abs("labs/x.csv"))
	igHas(t, "user include", out, "SYNCED", "included directory `+g:labs/**` (ws.json)")
	_, out, _ = e.run("", "ignore", "check", e.abs("exp/z.bin"))
	igHas(t, "user exclude", out, "IGNORED", "rule `-:exp/z.bin` (ws.json)")
}

func TestIgnoreCheckJSONKeys(t *testing.T) {
	e := newIgEnv(t)
	csv := e.write(t, "exp/m.csv", "123")
	code, env := e.jsonOf(t, "ignore", "check", csv)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	d := env["data"].(map[string]any)
	if d["path"] != csv || d["included"] != false || d["rule"] != "-g:*.csv" || d["rule_source"] != "builtin" || d["muted"] != false || d["size_bytes"] != float64(3) {
		t.Fatalf("data = %v", d)
	}
	if _, old := d["by_include"]; old {
		t.Fatalf("by_include was renamed: %v", d)
	}

	e.write(t, "plain/n.txt", "x")
	_, env = e.jsonOf(t, "ignore", "check", e.abs("plain/n.txt"))
	d = env["data"].(map[string]any)
	if d["included"] != true || d["rule"] != "<default>" {
		t.Fatalf("no rule: %v", d)
	}
	if src, _ := d["rule_source"].(string); src != "" {
		t.Fatalf("rule_source must be empty when no rule matches: %v", d)
	}
	_, env = e.jsonOf(t, "ignore", "check", e.abs("plain"))
	if _, has := env["data"].(map[string]any)["size_bytes"]; has {
		t.Fatal("size_bytes is for files only")
	}
}

func TestIgnoreCheckMutedFlagNeverChangesAnswer(t *testing.T) {
	e := newIgEnv(t)
	f := e.write(t, "ml/a.bin", "x")
	e.do(t, "ignore", "mute", e.abs("ml"))
	_, env := e.jsonOf(t, "ignore", "check", f)
	d := env["data"].(map[string]any)
	if d["included"] != true || d["muted"] != true {
		t.Fatalf("data = %v", d)
	}
}

func TestIgnoreCheckErrors(t *testing.T) {
	e := newIgEnv(t)
	outside := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := e.run("", "ignore", "check", outside)
	if code != 1 || out != "" || !strings.Contains(errOut, "outside the workspace") {
		t.Fatalf("outside: exit %d stdout %q stderr %q", code, out, errOut)
	}
	if code, _, _ := e.run("", "ignore", "check", e.abs("missing")); code != 1 {
		t.Fatalf("missing: exit %d", code)
	}
	if code, _, _ := e.run("", "ignore", "check"); code != 1 {
		t.Fatalf("no path: exit %d", code)
	}
}

// ── tree ───────────────────────────────────────────────────────────

func TestIgnoreTreeLevelsAndErrors(t *testing.T) {
	e := newIgEnv(t)
	e.write(t, "a/b/c/deep.txt", "x")
	e.write(t, "a/top.csv", "1")

	_, out, _ := e.run("", "ignore", "tree", "-L", "1")
	igHas(t, "tree -L 1", out, e.ws, "a/")
	if strings.Contains(out, "deep.txt") || strings.Contains(out, "top.csv") {
		t.Fatalf("-L 1 shows only one level:\n%s", out)
	}
	_, out, _ = e.run("", "ignore", "tree", e.abs("a"))
	igHas(t, "tree dir", out, "top.csv", "deep.txt", "1 files")

	_, out, _ = e.run("", "ignore", "tree", "--depth", "1", e.abs("a"))
	if strings.Contains(out, "deep.txt") {
		t.Fatalf("hidden --depth still works:\n%s", out)
	}

	if code, _, _ := e.run("", "ignore", "tree", t.TempDir()); code != 1 {
		t.Fatalf("outside: exit %d, want 1", code)
	}
	if code, _, _ := e.run("", "ignore", "tree", e.abs("missing")); code != 1 {
		t.Fatalf("missing: exit %d, want 1", code)
	}
}

func TestIgnoreTreeJSONRecords(t *testing.T) {
	e := newIgEnv(t)
	e.write(t, "a/top.csv", "1")
	e.write(t, "a/ok.txt", "1")
	code, env := e.jsonOf(t, "ignore", "tree")
	if code != 0 || env["command"] != "ignore.tree" {
		t.Fatalf("exit %d envelope %v", code, env)
	}
	status := map[string]string{}
	for _, r := range env["data"].([]any) {
		m := r.(map[string]any)
		status[m["path"].(string)] = m["status"].(string)
	}
	if status[e.abs("a")] != "partial" || status[e.abs("a/top.csv")] != "ignored" || status[e.abs("a/ok.txt")] != "synced" {
		t.Fatalf("status = %v", status)
	}
}

// ── edit ───────────────────────────────────────────────────────────

// editorShim returns a vim stand-in (a terminal editor, so ws waits for it)
// that copies $WS_TEST_EDIT_SRC over the file it is given. With no source it
// leaves the file alone.
func editorShim(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	shim := filepath.Join(dir, "vim")
	script := "#!/bin/sh\nif [ -n \"$WS_TEST_EDIT_SRC\" ]; then cp \"$WS_TEST_EDIT_SRC\" \"$1\"; fi\n"
	if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if content != "" {
		src := filepath.Join(dir, "edited.json")
		if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("WS_TEST_EDIT_SRC", src)
	} else {
		t.Setenv("WS_TEST_EDIT_SRC", "")
	}
	return shim
}

func TestIgnoreEditUnchangedIsNothingToDo(t *testing.T) {
	e := newIgEnv(t)
	shim := editorShim(t, "")
	before := e.readFile(t, "ws.json")
	code, out, _ := e.run("a\n", "ignore", "edit", "--editor", shim)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	igHas(t, "message", out, "Nothing to do")
	if e.readFile(t, "ws.json") != before {
		t.Fatal("unchanged rules must not rewrite ws.json")
	}
	if exists2(e.abs(".ignore-edit.json")) {
		t.Fatal("the temporary file must be removed")
	}
}

func TestIgnoreEditSavesChangedRulesAndRewritesMegaignore(t *testing.T) {
	e := newIgEnv(t)
	shim := editorShim(t, `{"schema":1,"exclude":[{"pattern":"scratchpad/out.bin","note":"big"}],"include":[],"suppress_defaults":["*.csv"]}`)
	code, out, errOut := e.run("a\n", "ignore", "edit", "--editor", shim)
	if code != 0 {
		t.Fatalf("exit %d stderr %s", code, errOut)
	}
	igHas(t, "plan", out, "Save ignore_rules to "+e.abs("ws.json")+" and rewrite "+e.abs(".megaignore"))
	if got := e.excludes(t); len(got) != 1 || got[0] != "scratchpad/out.bin" {
		t.Fatalf("excludes = %v", got)
	}
	mega := e.readFile(t, ".megaignore")
	igHas(t, ".megaignore", mega, "-:scratchpad/out.bin")
	if strings.Contains(mega, "-g:*.csv") {
		t.Fatalf("suppressed default still written:\n%s", mega)
	}
	if exists2(e.abs(".ignore-edit.json")) {
		t.Fatal("the temporary file must be removed")
	}
}

func TestIgnoreEditInvalidJSONLeavesConfigAlone(t *testing.T) {
	e := newIgEnv(t)
	shim := editorShim(t, `{ not json`)
	before := e.readFile(t, "ws.json")
	code, _, errOut := e.run("a\n", "ignore", "edit", "--editor", shim)
	if code != 1 || errOut == "" {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	if e.readFile(t, "ws.json") != before {
		t.Fatal("ws.json must stay as it was")
	}
	if exists2(e.abs(".ignore-edit.json")) {
		t.Fatal("the temporary file must be removed")
	}
}

func TestIgnoreEditDryRunAndStaleMegaignore(t *testing.T) {
	e := newIgEnv(t)
	shim := editorShim(t, "")
	if err := os.WriteFile(e.abs(".megaignore"), []byte("-:old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Unchanged rules, stale file: one action.
	code, out, _ := e.run("", "--dry-run", "ignore", "edit", "--editor", shim)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	igHas(t, "dry run", out, "[dry-run]", "rewrite "+e.abs(".megaignore"))
	if e.readFile(t, ".megaignore") != "-:old\n" {
		t.Fatal("--dry-run must not rewrite")
	}
	e.do(t, "ignore", "edit", "--editor", shim)
	if strings.Contains(e.readFile(t, ".megaignore"), "-:old") {
		t.Fatal("a stale .megaignore is rewritten")
	}
}

func TestIgnoreEditJSONHasStats(t *testing.T) {
	e := newIgEnv(t)
	shim := editorShim(t, `{"schema":1,"exclude":[{"pattern":"a/b"}],"include":[],"suppress_defaults":[]}`)
	code, env := e.jsonOf(t, "ignore", "edit", "--editor", shim)
	if code != 0 || env["command"] != "ignore.edit" {
		t.Fatalf("exit %d envelope %v", code, env)
	}
	d := env["data"].(map[string]any)
	if len(d["actions"].([]any)) != 1 {
		t.Fatalf("data = %v", d)
	}
	if d["stats"].(map[string]any)["UserExclude"] != float64(1) {
		t.Fatalf("stats = %v", d["stats"])
	}
}

// includeDir adds an include rule for dir/** to ws.json: no include rule is
// built in, so a test that needs an included directory writes one.
func (e igEnv) includeDir(t *testing.T, dir string) {
	t.Helper()
	e.setCfg(t, func(c *wsconfig.WsConfig) {
		c.IgnoreRules.Include = append(c.IgnoreRules.Include, ruleEntry(dir+"/**"))
	})
}

func ruleEntry(p string) ignore.RuleEntry { return ignore.RuleEntry{Pattern: p} }
