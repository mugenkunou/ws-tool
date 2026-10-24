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

	wssession "github.com/mugenkunou/ws-tool/internal/session"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// sessEnv is one isolated machine for the session commands: its own state
// directory (the marker), its own XDG trash, and an initialized workspace.
type sessEnv struct {
	workspace, logDir, stateDir, trash string
}

func newSessEnv(t *testing.T) sessEnv {
	t.Helper()
	data := filepath.Join(t.TempDir(), "data")
	t.Setenv("XDG_DATA_HOME", data)
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	ws := initWorkspace(t)
	e := sessEnv{
		workspace: ws,
		logDir:    filepath.Join(t.TempDir(), "Sessions"),
		stateDir:  filepath.Join(state, "ws-tool"),
		trash:     filepath.Join(data, "Trash", "files"),
	}
	e.setSession(t, 100)
	return e
}

func (e sessEnv) run(stdin string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := Execute(append([]string{"--workspace", e.workspace}, args...), strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func (e sessEnv) mustRun(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	code, out, errOut := e.run(stdin, args...)
	if code != 0 {
		t.Fatalf("ws %s: exit %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), code, out, errOut)
	}
	return out
}

// seed creates a finished session with the given commands and transcript.
func (e sessEnv) seed(t *testing.T, tag, commands, transcript string) wssession.StartResult {
	t.Helper()
	res, err := wssession.Start(wssession.StartOptions{LogDir: e.logDir, StateDir: e.stateDir, Tag: tag})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(res.StdinPath, []byte(commands), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(res.StdoutPath, []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wssession.Stop(wssession.StopOptions{LogDir: e.logDir, StateDir: e.stateDir}); err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSessionLsAndShow(t *testing.T) {
	e := newSessEnv(t)
	e.seed(t, "rsync-backup", "rsync -avz ~/Workspace/ backup:/data/\ndf -h /data\n", "\x1b[32m$ rsync\x1b[0m\r\nsent 1 file\r\n")

	out := e.mustRun(t, "", "session", "ls")
	for _, want := range []string{"TAG", "STARTED", "DURATION", "CMDS", "SIZE", "STATE", "rsync-backup", "done"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ls lacks %q:\n%s", want, out)
		}
	}

	// show prints the transcript without escape sequences or carriage returns.
	if got := e.mustRun(t, "", "session", "show", "rsync-backup"); got != "$ rsync\nsent 1 file\n" {
		t.Fatalf("transcript = %q", got)
	}
	if got := e.mustRun(t, "", "session", "show", "rsync-backup", "--commands"); got != "rsync -avz ~/Workspace/ backup:/data/\ndf -h /data\n" {
		t.Fatalf("commands = %q", got)
	}
	if code, _, errOut := e.run("", "session", "show", "nope"); code != 1 || !strings.Contains(errOut, "not found") {
		t.Fatalf("an unknown tag: exit %d stderr=%s", code, errOut)
	}
	if code, _, errOut := e.run("", "session", "show"); code != 1 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("no tag: exit %d stderr=%s", code, errOut)
	}

	_, js, _ := e.run("", "--json", "session", "show", "rsync-backup", "--commands")
	var env struct {
		Command string `json:"command"`
		Data    struct {
			Tag, Path, Text string
			Commands        bool
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(js), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, js)
	}
	if env.Command != "session.show" || !env.Data.Commands || env.Data.Tag != "rsync-backup" ||
		env.Data.Path != filepath.Join(e.logDir, "rsync-backup", "stdin.log") || !strings.HasPrefix(env.Data.Text, "rsync") {
		t.Fatalf("envelope = %+v", env)
	}
}

func TestSessionLsShowsTheRecording(t *testing.T) {
	e := newSessEnv(t)
	if _, err := wssession.Start(wssession.StartOptions{LogDir: e.logDir, StateDir: e.stateDir, Tag: "deploy-fix"}); err != nil {
		t.Fatal(err)
	}
	out := e.mustRun(t, "", "session", "ls")
	if !strings.Contains(out, "recording") || !strings.Contains(out, "—") {
		t.Fatalf("the recording row reads recording, with no duration:\n%s", out)
	}
	recs := listRecords(t, "--workspace", e.workspace, "session", "ls")
	if len(recs) != 1 || recs[0]["active"] != true {
		t.Fatalf("records = %v", recs)
	}
	for _, k := range []string{"tag", "started_at", "ended_at", "duration_sec", "commands", "size_bytes", "active"} {
		if _, ok := recs[0][k]; !ok {
			t.Errorf("record lacks %q", k)
		}
	}
}

func TestSessionGrep(t *testing.T) {
	e := newSessEnv(t)
	e.seed(t, "rsync-backup", "rsync -avz --delete ~/Workspace/ backup:/data/\ndf -h /data\n", "$ rsync -avz --delete ~/Workspace/ backup:/data/\r\nok\r\n")
	e.seed(t, "other", "ls\n", "nothing\n")

	out := e.mustRun(t, "", "session", "grep", "RSYNC")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("one match in the commands, one in the transcript:\n%s", out)
	}
	want0 := filepath.Join(e.logDir, "rsync-backup", "stdin.log") + ":1: rsync -avz --delete ~/Workspace/ backup:/data/"
	want1 := filepath.Join(e.logDir, "rsync-backup", "stdout.log") + ":1: $ rsync -avz --delete ~/Workspace/ backup:/data/"
	if lines[0] != want0 || lines[1] != want1 {
		t.Fatalf("lines:\n%s\nwant:\n%s\n%s", out, want0, want1)
	}

	// Several arguments are one text.
	if out := e.mustRun(t, "", "session", "grep", "delete", "~/Workspace/"); !strings.Contains(out, "stdin.log:1:") {
		t.Fatalf("joined text:\n%s", out)
	}
	if out := e.mustRun(t, "", "session", "grep", "rsync", "--max", "1"); strings.Count(out, "\n") != 1 {
		t.Fatalf("--max 1:\n%s", out)
	}
	// Whether or not anything matched, exit 0.
	if out := e.mustRun(t, "", "session", "grep", "zzz-nothing"); strings.TrimSpace(out) != "No matches." {
		t.Fatalf("no match:\n%s", out)
	}
	if code, _, errOut := e.run("", "session", "grep"); code != 1 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("no text: exit %d stderr=%s", code, errOut)
	}
	_, js, _ := e.run("", "--json", "session", "grep", "rsync")
	var env struct {
		Command string `json:"command"`
		Data    []struct {
			Tag, Path, Text string
			Line            int
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(js), &env); err != nil || env.Command != "session.grep" || len(env.Data) != 2 || env.Data[0].Tag != "rsync-backup" {
		t.Fatalf("envelope = %+v err=%v\n%s", env, err, js)
	}
}

func TestSessionStop(t *testing.T) {
	e := newSessEnv(t)
	if out := e.mustRun(t, "", "session", "stop"); strings.TrimSpace(out) != "No active recording session." {
		t.Fatalf("nothing recording:\n%s", out)
	}

	// A recording whose recorder is this very process: stop finalizes it
	// without signalling (it would end the test run).
	if _, err := wssession.Start(wssession.StartOptions{LogDir: e.logDir, StateDir: e.stateDir, Tag: "demo"}); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := e.run("", "session", "stop", "--dry-run"); code != 0 || !strings.Contains(out, "Would stop the recording demo") {
		t.Fatalf("dry-run: %d %s", code, out)
	}
	if _, ok := wssession.ActiveMarker(e.stateDir); !ok {
		t.Fatal("--dry-run must stop nothing")
	}

	out := e.mustRun(t, "", "session", "stop")
	for _, want := range []string{"Session ended", "Tag", "demo", "Duration", "Transcript", filepath.Join(e.logDir, "demo", "stdout.log"), "Commands", "0 commands"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stop output lacks %q:\n%s", want, out)
		}
	}
	if _, ok := wssession.ActiveMarker(e.stateDir); ok {
		t.Fatal("the marker must be removed")
	}

	if _, err := wssession.Start(wssession.StartOptions{LogDir: e.logDir, StateDir: e.stateDir, Tag: "again"}); err != nil {
		t.Fatal(err)
	}
	_, js, _ := e.run("", "--json", "session", "stop")
	var env struct {
		Command string         `json:"command"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(js), &env); err != nil || env.Command != "session.stop" || env.Data["tag"] != "again" || env.Data["stopped"] != true {
		t.Fatalf("envelope = %+v err=%v", env, err)
	}
}

func TestSessionRm(t *testing.T) {
	e := newSessEnv(t)
	e.seed(t, "a", "x\n", "x\n")
	e.seed(t, "b", "x\n", "x\n")
	e.seed(t, "c", "x\n", "x\n")

	out := e.mustRun(t, "y\ny\n", "session", "rm", "a", "b")
	if strings.Count(out, "[y/n/a/q]") != 2 || !strings.Contains(out, "Delete "+filepath.Join(e.logDir, "a")+" (") {
		t.Fatalf("one action per session:\n%s", out)
	}
	for _, tag := range []string{"a", "b"} {
		if _, err := os.Stat(filepath.Join(e.logDir, tag)); !os.IsNotExist(err) {
			t.Fatalf("%s must be gone from the workspace", tag)
		}
	}
	// Soft-deleted, not removed permanently (spec §7.3).
	trashed, _ := os.ReadDir(e.trash)
	if len(trashed) != 2 {
		t.Fatalf("both sessions must be in the trash, got %v", trashed)
	}

	// Tags must match exactly; if one is wrong, nothing is deleted.
	if code, _, errOut := e.run("y\n", "session", "rm", "c", "nope"); code != 1 || !strings.Contains(errOut, "session not found: nope") {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(e.logDir, "c")); err != nil {
		t.Fatal("nothing is deleted when a tag is wrong")
	}
	if code, _, _ := e.run("", "session", "rm", "../c"); code != 1 {
		t.Fatalf("a tag that escapes the directory must be refused, got %d", code)
	}
	if code, _, errOut := e.run("", "session", "rm"); code != 1 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("no tag: %d %s", code, errOut)
	}

	// The recording in progress cannot be deleted.
	if _, err := wssession.Start(wssession.StartOptions{LogDir: e.logDir, StateDir: e.stateDir, Tag: "live"}); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := e.run("", "session", "rm", "live", "c"); code != 1 || !strings.Contains(errOut, "active session") {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(e.logDir, "c")); err != nil {
		t.Fatal("nothing is deleted when one tag is the recording")
	}
}

// setSessionConfig rewrites session settings in ws.json.
func (e sessEnv) setSession(t *testing.T, maxMB int) {
	t.Helper()
	cfgPath := filepath.Join(e.workspace, "ws.json")
	cfg, err := wsconfig.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Session.RootDir, cfg.Session.MaxSessionMB = e.logDir, maxMB
	if err := wsconfig.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
}

// aged makes a session look as if it started n days ago and holds size bytes.
func (e sessEnv) aged(t *testing.T, tag string, days int, size int) {
	t.Helper()
	res := e.seed(t, tag, "", strings.Repeat("x", size))
	meta := filepath.Join(res.SessionDir, "meta.json")
	started := time.Now().UTC().AddDate(0, 0, -days)
	body, _ := json.Marshal(map[string]any{"tag": tag, "started_at": started, "ended_at": started.Add(time.Minute), "duration_sec": 60})
	if err := os.WriteFile(meta, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSessionRmOlderThan(t *testing.T) {
	e := newSessEnv(t)
	e.aged(t, "old", 40, 10)
	e.aged(t, "recent", 2, 10)

	// A bare number is days, the shared age format.
	out := e.mustRun(t, "y\n", "session", "rm", "--older-than", "30")
	if strings.Count(out, "[y/n/a/q]") != 1 || !strings.Contains(out, filepath.Join(e.logDir, "old")) || !strings.Contains(out, "40d old") {
		t.Fatalf("--older-than 30:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(e.logDir, "recent")); err != nil {
		t.Fatal("a session younger than the age stays")
	}
	if out := e.mustRun(t, "", "session", "rm", "--older-than", "30"); !strings.Contains(out, "Nothing to delete") {
		t.Fatalf("nothing is that old now:\n%s", out)
	}
	if code, _, errOut := e.run("", "session", "rm", "--older-than", "soon"); code != 1 || !strings.Contains(errOut, "invalid age") {
		t.Fatalf("a bad age: %d %s", code, errOut)
	}
	if code, _, errOut := e.run("", "session", "rm", "recent", "--older-than", "1d"); code != 1 || !strings.Contains(errOut, "usage") {
		t.Fatalf("tags with --older-than: %d %s", code, errOut)
	}

	// The recording in progress is never deleted, however old.
	if _, err := wssession.Start(wssession.StartOptions{LogDir: e.logDir, StateDir: e.stateDir, Tag: "live"}); err != nil {
		t.Fatal(err)
	}
	e.mustRun(t, "", "session", "rm", "--older-than", "1h")
	if _, err := os.Stat(filepath.Join(e.logDir, "live")); err != nil {
		t.Fatal("the recording in progress is never deleted")
	}
}

func TestSessionRmOlderThanDryRunJSON(t *testing.T) {
	e := newSessEnv(t)
	e.aged(t, "big", 5, 10)
	_, js, _ := e.run("", "--json", "--dry-run", "session", "rm", "--older-than", "1d")
	var env struct {
		Command string `json:"command"`
		DryRun  bool   `json:"dry_run"`
		Data    struct {
			Actions []struct{ Status string } `json:"actions"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(js), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, js)
	}
	if env.Command != "session.rm" || !env.DryRun || len(env.Data.Actions) != 1 {
		t.Fatalf("envelope = %+v", env)
	}
	if _, err := os.Stat(filepath.Join(e.logDir, "big")); err != nil {
		t.Fatal("--dry-run must delete nothing")
	}
}

// TestSessionRecordNeedsATerminal: with no terminal ws refuses and creates
// nothing. It used to fall back to "metadata-only mode" and leave a dangling
// active session.
func TestSessionRecordNeedsATerminal(t *testing.T) {
	e := newSessEnv(t)
	code, _, errOut := e.run("", "session", "record", "demo")
	if code != 1 || !strings.Contains(errOut, "interactive terminal") {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(e.logDir, "demo")); !os.IsNotExist(err) {
		t.Fatal("nothing may be created when ws refuses to record")
	}
	if _, ok := wssession.ActiveMarker(e.stateDir); ok {
		t.Fatal("no marker may be left behind")
	}
	for _, bad := range []string{"../../x", "a/b", ".hidden"} {
		if code, _, errOut := e.run("", "session", "record", bad); code != 1 || !strings.Contains(errOut, "invalid session tag") {
			t.Errorf("tag %q: exit %d stderr=%s", bad, code, errOut)
		}
	}
	if code, out, _ := e.run("", "session", "record", "demo", "--dry-run"); code != 0 || !strings.Contains(out, "Would record into "+filepath.Join(e.logDir, "demo")) {
		t.Fatalf("--dry-run prints the directory: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(e.logDir, "demo")); !os.IsNotExist(err) {
		t.Fatal("--dry-run creates nothing")
	}
}

// TestRecordedShellInit: the generated init files carry the prompt indicator
// and the command-capture hook, and script(1) is not asked to log keystrokes.
func TestRecordedShellInit(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "stdin.log")

	for _, shell := range []string{"bash", "zsh"} {
		colour := sessionShellInitContent(shell, log, true, false)
		if !strings.Contains(colour, "●") || !strings.Contains(colour, "ws:rec") || strings.Contains(colour, "ws:log") {
			t.Errorf("%s: the indicator is ● ws:rec:\n%s", shell, colour)
		}
		plain := sessionShellInitContent(shell, log, true, true)
		if !strings.Contains(plain, "(*) ws:rec") || strings.Contains(plain, "●") {
			t.Errorf("%s: without colour the indicator is (*) ws:rec:\n%s", shell, plain)
		}
		bare := sessionShellInitContent(shell, log, false, false)
		if strings.Contains(bare, "ws:rec") {
			t.Errorf("%s: --no-prompt leaves the prompt alone:\n%s", shell, bare)
		}
		for _, c := range []string{colour, plain, bare} {
			if !strings.Contains(c, log) {
				t.Errorf("%s: commands are captured into stdin.log", shell)
			}
		}
	}

	// zsh reads $ZDOTDIR/.zshrc in both modes (the --no-prompt branch used to
	// write a file zsh never read) and the user's .zshenv.
	for _, noPrompt := range []bool{false, true} {
		d := t.TempDir()
		t.Setenv("SHELL", "/usr/bin/zsh")
		cmd, env := sessionShellInit(d, false, noPrompt)
		if cmd != "/usr/bin/zsh" && cmd != "zsh" && !strings.HasSuffix(cmd, "zsh") {
			t.Errorf("shell command = %q", cmd)
		}
		if !slices.Contains(env, "ZDOTDIR="+d) || !slices.Contains(env, "WS_LOG_RECORDING=1") {
			t.Errorf("env = %v", env)
		}
		for _, f := range []string{".zshrc", ".zshenv"} {
			if _, err := os.Stat(filepath.Join(d, f)); err != nil {
				t.Errorf("noPrompt=%v: %s must exist: %v", noPrompt, f, err)
			}
		}
	}

	args := scriptArgs("/x/stdout.log", "bash --rcfile i")
	if slices.Contains(args, "-I") {
		t.Fatalf("script -I logs raw keystrokes, passwords included: %v", args)
	}
	if !slices.Contains(args, "-O") || !slices.Contains(args, "--flush") {
		t.Fatalf("script args = %v", args)
	}
}

func TestSessionUnknownVerbAndHelp(t *testing.T) {
	e := newSessEnv(t)
	if code, _, errOut := e.run("", "session", "frobnicate"); code != 1 || !strings.Contains(errOut, "unknown command: session frobnicate") {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
}
