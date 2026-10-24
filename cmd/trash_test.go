package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// trEnv is one isolated machine for the trash commands. None of them needs a
// workspace, so none is made. HOME, the XDG directories and $SHELL are all
// temporary, and the shell integration edits only files under HOME.
type trEnv struct {
	home, root string
}

func newTrEnv(t *testing.T) trEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("SHELL", "/bin/bash")
	return trEnv{home: home, root: filepath.Join(home, ".local", "share", "Trash")}
}

func (e trEnv) run(stdin string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := Execute(append([]string{"--no-color"}, append(args, "")[:len(args)]...), strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func (e trEnv) mustRun(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	code, out, errOut := e.run(stdin, args...)
	if code != 0 {
		t.Fatalf("ws %s: exit %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), code, out, errOut)
	}
	return out
}

// trashed puts an item in the trash with a .trashinfo.
func (e trEnv) trashed(t *testing.T, name, original, content string, deleted time.Time) {
	t.Helper()
	for _, d := range []string{"files", "info"} {
		if err := os.MkdirAll(filepath.Join(e.root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(e.root, "files", name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	info := "[Trash Info]\nPath=" + original + "\n"
	if !deleted.IsZero() {
		info += "DeletionDate=" + deleted.Format("2006-01-02T15:04:05") + "\n"
	}
	if err := os.WriteFile(filepath.Join(e.root, "info", name+".trashinfo"), []byte(info), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e trEnv) inTrash(name string) bool { return exists2(filepath.Join(e.root, "files", name)) }

// ── status, enable, disable ────────────────────────────────────────

func TestTrashStatusAndEnable(t *testing.T) {
	e := newTrEnv(t)
	out := e.mustRun(t, "", "trash", "status") // exit 0: off integrations are reported, not an error
	for _, want := range []string{"Trash root", e.root, "[err] shell-rm", "[err] file-explorer", "ws trash enable", "Items 0 · Files 0 · Size 0 B · Warns at 1.0 GB"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "[ok] vscode") {
		t.Fatalf("VS Code deletes to the trash unless turned off:\n%s", out)
	}

	out = e.mustRun(t, "y\ny\n", "trash", "enable")
	if strings.Count(out, "[y/n/a/q]") != 2 || !strings.Contains(out, "Make shell rm move files to the trash") || !strings.Contains(out, "Create "+filepath.Join(e.root, "files")) {
		t.Fatalf("one action per integration that is off:\n%s", out)
	}
	if got := read(t, filepath.Join(e.home, ".bashrc")); !strings.Contains(got, "command -v ws-trash-rm >/dev/null 2>&1 && alias rm='ws-trash-rm'") {
		t.Fatalf("the guarded alias:\n%s", got)
	}

	out = e.mustRun(t, "", "trash", "status")
	if !strings.Contains(out, "[ok] shell-rm") || !strings.Contains(out, "[ok] file-explorer") || strings.Contains(out, "[err]") {
		t.Fatalf("status after enable:\n%s", out)
	}
	if out := e.mustRun(t, "", "trash", "enable"); strings.TrimSpace(out) != "Soft-delete is already on." {
		t.Fatalf("enable twice:\n%s", out)
	}
}

func TestTrashEnableFlagsAndRemovedOptions(t *testing.T) {
	e := newTrEnv(t)
	out := e.mustRun(t, "y\n", "trash", "enable", "--no-shell-rm", "--no-vscode")
	if strings.Count(out, "[y/n/a/q]") != 1 || !strings.Contains(out, "Create ") {
		t.Fatalf("only file-explorer is left:\n%s", out)
	}
	if code, _, errOut := e.run("", "trash", "enable", "--root-dir", "/x"); code != 1 || !strings.Contains(errOut, "root-dir") {
		t.Fatalf("--root-dir is removed: exit %d stderr=%s", code, errOut)
	}
	if code, out, _ := e.run("", "--dry-run", "trash", "enable", "--no-vscode"); code != 0 || !strings.Contains(out, "[dry-run]") {
		t.Fatalf("--dry-run prints the plan: %d\n%s", code, out)
	}
	if exists2(filepath.Join(e.home, ".bashrc")) {
		t.Fatal("--dry-run must change nothing")
	}
}

func TestTrashStatusJSON(t *testing.T) {
	e := newTrEnv(t)
	e.trashed(t, "a", "/x/a", "hello", time.Now())
	_, js, _ := e.run("", "--json", "trash", "status")
	var env struct {
		Command string `json:"command"`
		Data    struct {
			Root         string `json:"root"`
			Integrations []struct {
				Name    string `json:"name"`
				Enabled bool   `json:"enabled"`
				Detail  string `json:"detail"`
			} `json:"integrations"`
			Items, Files  int
			SizeBytes     int64 `json:"size_bytes"`
			WarnSizeBytes int64 `json:"warn_size_bytes"`
			OverLimit     bool  `json:"over_limit"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(js), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, js)
	}
	d := env.Data
	if env.Command != "trash.status" || d.Root != e.root || len(d.Integrations) != 3 || d.Items != 1 || d.Files != 1 || d.SizeBytes != 5 || d.WarnSizeBytes != 10<<30 || d.OverLimit {
		t.Fatalf("envelope = %+v", env)
	}
}

func TestTrashDisable(t *testing.T) {
	e := newTrEnv(t)
	if out := e.mustRun(t, "", "trash", "disable"); !strings.Contains(out, "not enabled") {
		t.Fatalf("nothing to disable:\n%s", out)
	}
	e.mustRun(t, "y\ny\n", "trash", "enable")
	out := e.mustRun(t, "y\n", "trash", "disable")
	if !strings.Contains(out, "Stop shell rm from using the trash") {
		t.Fatalf("plan line:\n%s", out)
	}
	if got := read(t, filepath.Join(e.home, ".bashrc")); strings.Contains(got, "ws-trash-rm") {
		t.Fatalf("the alias must be gone:\n%s", got)
	}
	if exists2(filepath.Join(e.home, ".local", "bin", "ws-trash-rm")) {
		t.Fatal("the script must be gone")
	}
	if !exists2(filepath.Join(e.root, "files")) {
		t.Fatal("the trash directories stay")
	}
	_, js, _ := e.run("", "--json", "trash", "disable")
	if !strings.Contains(js, `"command": "trash.disable"`) {
		t.Fatalf("json:\n%s", js)
	}
}

func TestTrashSetupAliasPrintsTheHint(t *testing.T) {
	e := newTrEnv(t)
	code, _, errOut := e.run("y\ny\n", "trash", "setup")
	if code != 0 || !strings.Contains(errOut, `"trash setup" is now "trash enable"`) {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
}

// ── ls ─────────────────────────────────────────────────────────────

func TestTrashLsOutput(t *testing.T) {
	e := newTrEnv(t)
	if out := e.mustRun(t, "", "trash", "ls"); strings.TrimSpace(out) != "Trash is empty." {
		t.Fatalf("empty:\n%s", out)
	}
	e.trashed(t, "old-notes.md", "/home/u/notes/old-notes.md", "x", time.Now().Add(-3*time.Hour))
	if err := os.MkdirAll(filepath.Join(e.root, "files", "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := e.mustRun(t, "", "trash", "ls")
	head := strings.Fields(strings.SplitN(out, "\n", 2)[0])
	if strings.Join(head, " ") != "NAME DELETED FROM SIZE DELETED" || !strings.Contains(out, "build/") || !strings.Contains(out, "3h ago") {
		t.Fatalf("ls:\n%s", out)
	}
	recs := listRecords(t, "trash", "ls")
	if len(recs) != 2 {
		t.Fatalf("records = %v", recs)
	}
	for _, k := range []string{"name", "original_path", "size_bytes", "files", "deleted_at", "dir", "path"} {
		if _, ok := recs[0][k]; !ok {
			t.Errorf("record lacks %q", k)
		}
	}
}

// ── restore ────────────────────────────────────────────────────────

func TestTrashRestore(t *testing.T) {
	e := newTrEnv(t)
	dest := filepath.Join(e.home, "work", "notes", "old-notes.md")
	e.trashed(t, "old-notes.md", dest, "notes", time.Now())

	out := e.mustRun(t, "y\n", "trash", "restore", "old-notes.md")
	if !strings.Contains(out, "Restore old-notes.md to "+dest+" (creates "+filepath.Join(e.home, "work")+")") {
		t.Fatalf("plan line names the parent it creates:\n%s", out)
	}
	if read(t, dest) != "notes" || e.inTrash("old-notes.md") {
		t.Fatal("the item must be back at its original path")
	}
}

func TestTrashRestoreByOriginalPathAndNeverOverwrites(t *testing.T) {
	e := newTrEnv(t)
	here := filepath.Join(e.home, "here.txt")
	gone := filepath.Join(e.home, "gone.txt")
	if err := os.WriteFile(here, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.trashed(t, "here.txt", here, "trashed", time.Now())
	e.trashed(t, "gone.txt", gone, "trashed", time.Now())

	// One restored and one skipped: exit 3.
	code, out, _ := e.run("y\n", "trash", "restore", here, gone)
	if code != 3 || !strings.Contains(out, "! here.txt: "+here+" already exists; skipped") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if read(t, here) != "current" || !e.inTrash("here.txt") {
		t.Fatal("an existing file is never overwritten, and the item stays in the trash")
	}
	if read(t, gone) != "trashed" {
		t.Fatal("the other item is restored (named by its original path)")
	}

	// Only skipped: exit 1.
	if code, _, _ = e.run("y\n", "trash", "restore", "here.txt"); code != 1 {
		t.Fatalf("none restored: exit %d, want 1", code)
	}
	// Declining every action is not a failure: exit 0.
	e.trashed(t, "other", filepath.Join(e.home, "other"), "o", time.Now())
	if code, _, _ = e.run("n\n", "trash", "restore", "other"); code != 0 {
		t.Fatalf("every action skipped by choice: exit %d, want 0", code)
	}
}

func TestTrashRestoreArgumentErrors(t *testing.T) {
	e := newTrEnv(t)
	e.trashed(t, "a", filepath.Join(e.home, "a"), "a", time.Now())
	if code, _, errOut := e.run("", "trash", "restore"); code != 1 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("no target: exit %d stderr=%s", code, errOut)
	}
	// A target that matches nothing aborts the whole command: nothing runs.
	code, out, errOut := e.run("y\n", "trash", "restore", "a", "ghost")
	if code != 1 || !strings.Contains(errOut, "no such trash item: ghost") || strings.Contains(out, "[y/n/a/q]") {
		t.Fatalf("exit %d out=%s err=%s", code, out, errOut)
	}
	if !e.inTrash("a") || exists2(filepath.Join(e.home, "a")) {
		t.Fatal("nothing may run when a target is unknown")
	}
}

func TestTrashRestoreJSON(t *testing.T) {
	e := newTrEnv(t)
	here := filepath.Join(e.home, "here.txt")
	if err := os.WriteFile(here, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.trashed(t, "here.txt", here, "t", time.Now())
	e.trashed(t, "gone.txt", filepath.Join(e.home, "gone.txt"), "t", time.Now())
	code, js, _ := e.run("", "--json", "trash", "restore", "here.txt", "gone.txt")
	if code != 3 {
		t.Fatalf("exit %d\n%s", code, js)
	}
	var env struct {
		Command string `json:"command"`
		Data    struct {
			Restored []struct{ Name, Path string }         `json:"restored"`
			Skipped  []struct{ Name, Path, Reason string } `json:"skipped"`
			Actions  []struct{ Status string }             `json:"actions"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(js), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, js)
	}
	if env.Command != "trash.restore" || len(env.Data.Restored) != 1 || env.Data.Restored[0].Name != "gone.txt" ||
		len(env.Data.Skipped) != 1 || env.Data.Skipped[0].Name != "here.txt" || len(env.Data.Actions) != 1 {
		t.Fatalf("envelope = %+v", env)
	}
}

// ── rm, empty, prune ───────────────────────────────────────────────

func TestTrashRm(t *testing.T) {
	e := newTrEnv(t)
	e.trashed(t, "a", "/x/a", "aaa", time.Now())
	e.trashed(t, "b", "/x/b", "bbb", time.Now())
	e.trashed(t, "c", "/x/c", "ccc", time.Now())
	if err := os.MkdirAll(filepath.Join(e.root, "files", "build"), 0o755); err != nil {
		t.Fatal(err)
	}

	out := e.mustRun(t, "y\ny\n", "trash", "rm", "a", "build")
	if strings.Count(out, "[y/n/a/q]") != 2 || !strings.Contains(out, "Permanently delete a (3 B), deleted from /x/a") || !strings.Contains(out, "Permanently delete build/") {
		t.Fatalf("plan lines:\n%s", out)
	}
	if e.inTrash("a") || e.inTrash("build") || !e.inTrash("b") || !e.inTrash("c") {
		t.Fatal("only the named items go")
	}
	if exists2(filepath.Join(e.root, "info", "a.trashinfo")) {
		t.Fatal("the .trashinfo goes with it")
	}

	// An unknown item aborts the command with nothing deleted.
	code, _, errOut := e.run("y\n", "trash", "rm", "b", "ghost")
	if code != 1 || !strings.Contains(errOut, "ghost") || !e.inTrash("b") {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
	if code, _, errOut = e.run("", "trash", "rm"); code != 1 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("no target: exit %d stderr=%s", code, errOut)
	}
	_, js, _ := e.run("", "--json", "trash", "rm", "b")
	if !strings.Contains(js, `"removed": [`) || !strings.Contains(js, `"b"`) {
		t.Fatalf("json:\n%s", js)
	}
}

func TestTrashEmpty(t *testing.T) {
	e := newTrEnv(t)
	if out := e.mustRun(t, "", "trash", "empty"); strings.TrimSpace(out) != "Trash is empty." {
		t.Fatalf("empty:\n%s", out)
	}
	e.trashed(t, "a", "/x/a", "a", time.Now())
	e.trashed(t, "b", "/x/b", "bb", time.Now())

	code, out, _ := e.run("y\n", "--dry-run", "trash", "empty")
	if code != 0 || !strings.Contains(out, "Permanently delete 2 items (3 B) in "+e.root) || !e.inTrash("a") {
		t.Fatalf("--dry-run changes nothing: exit %d\n%s", code, out)
	}
	if code, _, errOut := e.run("", "trash", "empty", "a"); code != 1 || !strings.Contains(errOut, "no targets") {
		t.Fatalf("empty takes no targets: exit %d stderr=%s", code, errOut)
	}
	out = e.mustRun(t, "y\n", "trash", "empty")
	if strings.Count(out, "[y/n/a/q]") != 1 || e.inTrash("a") || e.inTrash("b") {
		t.Fatalf("one action for everything:\n%s", out)
	}
	for _, d := range []string{"files", "info"} {
		if !exists2(filepath.Join(e.root, d)) {
			t.Fatalf("%s must be kept", d)
		}
	}
	e.trashed(t, "c", "/x/c", "c", time.Now())
	_, js, _ := e.run("", "--json", "trash", "empty")
	for _, want := range []string{`"command": "trash.empty"`, `"items": 1`, `"files": 1`, `"size_bytes": 1`} {
		if !strings.Contains(js, want) {
			t.Fatalf("json lacks %s:\n%s", want, js)
		}
	}
}

func TestTrashPrune(t *testing.T) {
	e := newTrEnv(t)
	now := time.Now()
	e.trashed(t, "old.txt", "/o/old.txt", "x", now.Add(-40*24*time.Hour))
	e.trashed(t, "week.txt", "/o/week.txt", "x", now.Add(-10*24*time.Hour))
	e.trashed(t, "new.txt", "/o/new.txt", "x", now.Add(-time.Hour))
	e.trashed(t, "undated", "/o/undated", "x", time.Time{})

	if code, _, errOut := e.run("", "trash", "prune"); code != 1 || !strings.Contains(errOut, "--older-than") {
		t.Fatalf("--older-than is required: exit %d stderr=%s", code, errOut)
	}
	for _, bad := range []string{"soon", "0", "-3d"} {
		if code, _, errOut := e.run("", "trash", "prune", "--older-than", bad); code != 1 || errOut == "" {
			t.Fatalf("--older-than %q: exit %d stderr=%s", bad, code, errOut)
		}
	}

	// 30d, a bare 30 and 4w all mean the same; one action per matched item.
	for _, age := range []string{"30d", "30", "4w"} {
		out := e.mustRun(t, "y\n", "--dry-run", "trash", "prune", "--older-than", age)
		if strings.Count(out, "[dry-run]") != 1 || !strings.Contains(out, "Permanently delete old.txt (1 B, deleted 40d ago)") {
			t.Fatalf("--older-than %s:\n%s", age, out)
		}
		if !strings.Contains(out, "1 item(s) have no deletion date and are never pruned") {
			t.Fatalf("undated items are reported:\n%s", out)
		}
	}
	if !e.inTrash("old.txt") {
		t.Fatal("--dry-run deletes nothing")
	}

	e.mustRun(t, "y\n", "trash", "prune", "--older-than", "30d")
	if e.inTrash("old.txt") || !e.inTrash("week.txt") || !e.inTrash("new.txt") || !e.inTrash("undated") {
		t.Fatal("only items trashed before the age go; undated ones are never pruned")
	}
	e.mustRun(t, "y\n", "trash", "prune", "--older-than", "12h") // hours
	if e.inTrash("week.txt") || !e.inTrash("new.txt") {
		t.Fatal("12h prunes the older ones, not the one from an hour ago")
	}
	if out := e.mustRun(t, "", "trash", "prune", "--older-than", "90d"); !strings.Contains(out, "Nothing to prune") {
		t.Fatalf("nothing matched:\n%s", out)
	}
}

func TestTrashPurgeAliasAcceptsDays(t *testing.T) {
	e := newTrEnv(t)
	e.trashed(t, "old.txt", "/o/old.txt", "x", time.Now().Add(-40*24*time.Hour))
	code, _, errOut := e.run("y\n", "trash", "purge", "--older-than", "30")
	if code != 0 || !strings.Contains(errOut, `"trash purge" is now "trash prune"`) || e.inTrash("old.txt") {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
}

func TestTrashPruneJSON(t *testing.T) {
	e := newTrEnv(t)
	e.trashed(t, "old.txt", "/o/old.txt", "xy", time.Now().Add(-40*24*time.Hour))
	e.trashed(t, "new.txt", "/o/new.txt", "x", time.Now())
	_, js, _ := e.run("", "--json", "--dry-run", "trash", "prune", "--older-than", "30d")
	var env struct {
		Command string `json:"command"`
		Data    struct {
			Matched []struct {
				Name      string `json:"name"`
				SizeBytes int64  `json:"size_bytes"`
			} `json:"matched"`
			Kept, Skipped int
			SizeBytes     int64                     `json:"size_bytes"`
			Actions       []struct{ Status string } `json:"actions"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(js), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, js)
	}
	d := env.Data
	if env.Command != "trash.prune" || len(d.Matched) != 1 || d.Matched[0].Name != "old.txt" || d.Kept != 1 || d.SizeBytes != 2 || len(d.Actions) != 1 {
		t.Fatalf("envelope = %+v", env)
	}
}

func TestTrashNeedsNoWorkspace(t *testing.T) {
	e := newTrEnv(t)
	// No init, and a workspace that does not exist: trash still works.
	code, _, errOut := e.run("", "--workspace", filepath.Join(e.home, "nope"), "trash", "ls")
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut)
	}
}

func TestTrashNoArgsShowsHelp(t *testing.T) {
	testSetXDG(t)
	var out, errOut bytes.Buffer
	code := Execute([]string{"trash"}, strings.NewReader(""), &out, &errOut)
	if code != 0 || !strings.Contains(out.String(), "restore") || errOut.Len() != 0 {
		t.Fatalf("ws trash (no args): exit %d out=%s err=%s", code, out.String(), errOut.String())
	}
}
