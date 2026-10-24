package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// dirs makes a workspace log directory and a machine-local state directory, and
// points the XDG trash at a temp directory: Remove sends sessions there, and a
// test must never touch the real one.
func dirs(t *testing.T) (logDir, stateDir string) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	root := t.TempDir()
	return filepath.Join(root, "Workspace", "data", "sessions"), filepath.Join(root, "state", "ws-tool")
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func start(t *testing.T, logDir, stateDir, tag string) StartResult {
	t.Helper()
	res, err := Start(StartOptions{LogDir: logDir, StateDir: stateDir, Tag: tag})
	if err != nil {
		t.Fatalf("start %q: %v", tag, err)
	}
	return res
}

func TestStartStopListAndRemove(t *testing.T) {
	logDir, stateDir := dirs(t)
	res := start(t, logDir, stateDir, "t1")
	if res.Tag != "t1" || res.SessionDir != filepath.Join(logDir, "t1") {
		t.Fatalf("result = %+v", res)
	}
	write(t, res.StdinPath, "echo one\n")
	write(t, res.StdoutPath, "one\n")

	sessions, err := List(logDir, stateDir)
	if err != nil || len(sessions) != 1 || !sessions[0].Active || sessions[0].Commands != 1 {
		t.Fatalf("sessions = %+v, %v", sessions, err)
	}
	if err := Remove(RemoveOptions{LogDir: logDir, StateDir: stateDir, Tag: "t1"}); err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("the recording in progress cannot be removed, got %v", err)
	}

	stop, err := Stop(StopOptions{LogDir: logDir, StateDir: stateDir})
	if err != nil || !stop.Stopped || stop.Tag != "t1" {
		t.Fatalf("stop = %+v, %v", stop, err)
	}
	if again, _ := Stop(StopOptions{LogDir: logDir, StateDir: stateDir}); again.Stopped {
		t.Fatal("a second stop has nothing to stop")
	}
	sessions, _ = List(logDir, stateDir)
	if sessions[0].Active || sessions[0].EndedAt.IsZero() {
		t.Fatalf("a stopped session is done: %+v", sessions[0])
	}

	if err := Remove(RemoveOptions{LogDir: logDir, StateDir: stateDir, Tag: "t1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(logDir, "t1")); !os.IsNotExist(err) {
		t.Fatal("the session directory must be gone from the workspace")
	}
	// Soft-deleted: it is in the trash, not removed permanently.
	trashed, _ := os.ReadDir(filepath.Join(os.Getenv("XDG_DATA_HOME"), "Trash", "files"))
	if len(trashed) != 1 || trashed[0].Name() != "t1" {
		t.Fatalf("the session must go to the trash, got %v", trashed)
	}
}

func TestOnlyOneRecordingAtATime(t *testing.T) {
	logDir, stateDir := dirs(t)
	start(t, logDir, stateDir, "a")
	if _, err := Start(StartOptions{LogDir: logDir, StateDir: stateDir, Tag: "b"}); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("got %v", err)
	}
}

func TestStaleMarkerIsFinalizedAndReplaced(t *testing.T) {
	logDir, stateDir := dirs(t)
	first := start(t, logDir, stateDir, "crashed")
	// The recorder is gone: a PID that is not running.
	if err := SetActivePID(stateDir, 2147483646); err != nil {
		t.Fatal(err)
	}
	if _, ok := ActiveMarker(stateDir); ok {
		t.Fatal("a marker whose recorder is gone is not active")
	}
	sessions, _ := List(logDir, stateDir)
	if sessions[0].Active {
		t.Fatal("a stale marker must not show as recording")
	}

	second := start(t, logDir, stateDir, "next")
	if second.Tag != "next" {
		t.Fatalf("second = %+v", second)
	}
	meta, err := loadMeta(first.SessionDir)
	if err != nil || meta.EndedAt.IsZero() {
		t.Fatalf("the stale session must be finalized: %+v %v", meta, err)
	}
}

func TestTagValidationAndUniqueness(t *testing.T) {
	logDir, stateDir := dirs(t)
	for _, bad := range []string{"../../x", "a/b", ".hidden", ".."} {
		if _, err := Start(StartOptions{LogDir: logDir, StateDir: stateDir, Tag: bad}); err == nil {
			t.Errorf("tag %q must be refused", bad)
		}
	}
	// A tag that exists gets -2, -3: script(1) would truncate the old transcript.
	a := start(t, logDir, stateDir, "deploy")
	write(t, a.StdoutPath, "first recording\n")
	if _, err := Stop(StopOptions{LogDir: logDir, StateDir: stateDir}); err != nil {
		t.Fatal(err)
	}
	b := start(t, logDir, stateDir, "deploy")
	if b.Tag != "deploy-2" {
		t.Fatalf("second tag = %q, want deploy-2", b.Tag)
	}
	if got, _ := os.ReadFile(a.StdoutPath); string(got) != "first recording\n" {
		t.Fatalf("the first recording was overwritten: %q", got)
	}
	if _, err := Stop(StopOptions{LogDir: logDir, StateDir: stateDir}); err != nil {
		t.Fatal(err)
	}
	if c := start(t, logDir, stateDir, "deploy"); c.Tag != "deploy-3" {
		t.Fatalf("third tag = %q", c.Tag)
	}
	// The default tag is the local time.
	if _, err := Stop(StopOptions{LogDir: logDir, StateDir: stateDir}); err != nil {
		t.Fatal(err)
	}
	if d := start(t, logDir, stateDir, ""); len(d.Tag) != len("2006-01-02-1504") {
		t.Fatalf("default tag = %q", d.Tag)
	}
}

// TestMarkerIsMachineLocal: the marker is in the state dir, not in the synced
// workspace (plan 0002 B2).
func TestMarkerIsMachineLocal(t *testing.T) {
	logDir, stateDir := dirs(t)
	start(t, logDir, stateDir, "t")
	if _, err := os.Stat(MarkerPath(stateDir)); err != nil {
		t.Fatalf("the marker must be in the state dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(logDir, "active.json")); !os.IsNotExist(err) {
		t.Fatal("nothing may be written to the synced workspace's active.json")
	}
	if filepath.Base(MarkerPath(stateDir)) != "session-active.json" {
		t.Fatalf("marker name %q", MarkerPath(stateDir))
	}
}

// TestLegacyMarkerIsFinalizedWithoutSignalling: a recording marked at the old
// synced path (written by another machine, or an old ws) is finalized, and no
// process is signalled, because its PID means nothing here.
func TestLegacyMarkerIsFinalizedWithoutSignalling(t *testing.T) {
	logDir, stateDir := dirs(t)
	dir := filepath.Join(logDir, "old")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := saveMeta(dir, sessionMeta{Tag: "old", StartedAt: time.Now().UTC().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	// This test's own PID: signalling it would end the test run.
	write(t, filepath.Join(logDir, "active.json"), `{"tag":"old","pid":`+itoa(os.Getpid())+`}`)

	res, err := StopActive(StopOptions{LogDir: logDir, StateDir: stateDir})
	if err != nil || !res.Stopped || res.Tag != "old" {
		t.Fatalf("stop = %+v, %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(logDir, "active.json")); !os.IsNotExist(err) {
		t.Fatal("the legacy marker must be removed")
	}
	if meta, _ := loadMeta(dir); meta.EndedAt.IsZero() {
		t.Fatal("the legacy session must be finalized")
	}
}

func itoa(n int) string {
	var b []byte
	if n == 0 {
		return "0"
	}
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func TestShowStripsEscapesAndChoosesTheFile(t *testing.T) {
	logDir, stateDir := dirs(t)
	res := start(t, logDir, stateDir, "s1")
	write(t, res.StdoutPath, "\x1b[31mred\x1b[0m text\r\n\x1b]0;title\x07$ ls\r\nfile\r\n")
	write(t, res.StdinPath, "ls\ndf -h\n")

	full, err := Show(logDir, "s1", false)
	if err != nil {
		t.Fatal(err)
	}
	if full.Text != "red text\n$ ls\nfile\n" || full.Path != res.StdoutPath {
		t.Fatalf("transcript = %q (%s)", full.Text, full.Path)
	}
	cmds, err := Show(logDir, "s1", true)
	if err != nil || cmds.Text != "ls\ndf -h\n" || cmds.Path != res.StdinPath {
		t.Fatalf("commands = %+v, %v", cmds, err)
	}
	if _, err := Show(logDir, "nope", false); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("an unknown tag, got %v", err)
	}
	if _, err := Show(logDir, "../x", false); err == nil {
		t.Fatal("a tag that escapes the directory must be refused")
	}
}

func TestStripEscapes(t *testing.T) {
	cases := map[string]string{
		"plain":                        "plain",
		"a\r\nb\r\n":                   "a\nb\n",
		"\x1b[1;32mgreen\x1b[0m":       "green",
		"\x1b[?2004hprompt\x1b[?2004l": "prompt",
		"\x1b]0;window title\x07after": "after",
		"\x1b]8;;http://x\x1b\\link":   "link",
		"\x1b(Bchars":                  "chars",
		"tail\x1b":                     "tail",
		"progress 10%\rprogress 100%":  "progress 10%progress 100%",
		"\x1b=\x1b>keypad":             "keypad",
		"unicode ● ws:rec":             "unicode ● ws:rec",
	}
	for in, want := range cases {
		if got := StripEscapes(in); got != want {
			t.Errorf("StripEscapes(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGrep(t *testing.T) {
	logDir, stateDir := dirs(t)
	a := start(t, logDir, stateDir, "rsync-backup")
	write(t, a.StdinPath, "rsync -avz --delete ~/Workspace/ backup:/data/\ndf -h /data\n")
	write(t, a.StdoutPath, "$ rsync -avz --delete ~/Workspace/ backup:/data/\r\n\x1b[32msent 1 file\x1b[0m\r\n")
	if _, err := Stop(StopOptions{LogDir: logDir, StateDir: stateDir}); err != nil {
		t.Fatal(err)
	}
	b := start(t, logDir, stateDir, "deploy")
	write(t, b.StdinPath, "kubectl rollout status deploy/api\n")
	write(t, b.StdoutPath, "RSYNC mentioned here too\n")

	matches, err := Grep(logDir, stateDir, "rsync", 0)
	if err != nil {
		t.Fatal(err)
	}
	// Newest session first, the commands file before the transcript.
	var got []string
	for _, m := range matches {
		got = append(got, m.Tag+":"+filepath.Base(m.Path)+":"+itoa(m.Line))
	}
	want := []string{"deploy:stdout.log:1", "rsync-backup:stdin.log:1", "rsync-backup:stdout.log:1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("matches = %v, want %v", got, want)
	}
	if matches[2].Text != "$ rsync -avz --delete ~/Workspace/ backup:/data/" {
		t.Fatalf("a match is stripped text: %q", matches[2].Text)
	}

	limited, _ := Grep(logDir, stateDir, "rsync", 2)
	if len(limited) != 2 {
		t.Fatalf("--max 2 gave %d matches", len(limited))
	}
	none, err := Grep(logDir, stateDir, "no-such-text", 0)
	if err != nil || len(none) != 0 || none == nil {
		t.Fatalf("no matches is an empty list: %v %v", none, err)
	}
	if _, err := Grep(logDir, stateDir, "  ", 0); err == nil {
		t.Fatal("empty text must be an error")
	}
	// Only the commands and the transcript are searched.
	write(t, filepath.Join(logDir, "deploy", "meta.json"), `{"tag":"deploy","note":"rsync"}`)
	if m, _ := Grep(logDir, stateDir, "note", 0); len(m) != 0 {
		t.Fatalf("meta.json must not be searched: %v", m)
	}
}

func session(tag string, ageDays int, mb int64, active bool) Session {
	return Session{Tag: tag, StartedAt: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -ageDays), SizeBytes: mb << 20, Active: active}
}

func tags(ss []Session) string {
	var out []string
	for _, s := range ss {
		out = append(out, s.Tag)
	}
	return strings.Join(out, ",")
}

func TestSelectOlderThan(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	all := []Session{
		session("new", 1, 100, false),
		session("mid", 10, 200, false),
		session("old", 40, 300, false),
		session("rec", 30, 50, true),
	}
	if got := tags(SelectOlderThan(all, 7*24*time.Hour, now)); got != "old,mid" {
		t.Errorf("older than 7d = %s (oldest first, never the recording)", got)
	}
	if got := SelectOlderThan(all, 90*24*time.Hour, now); len(got) != 0 {
		t.Errorf("nothing is that old: %v", got)
	}
}
