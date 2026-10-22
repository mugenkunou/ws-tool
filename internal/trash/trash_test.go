package trash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain points the XDG trash at a temp directory for the whole package: the
// tests here move, restore and empty things, and a host test run once deleted real
// trash state. Tests that want the HOME-derived default clear XDG_DATA_HOME.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ws-tool-trash-test-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("XDG_DATA_HOME", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// machine is one isolated user: a home, the XDG trash under it, a config dir and
// a state dir.
type machine struct {
	home, root string
}

func newMachine(t *testing.T) machine {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("SHELL", "/bin/bash")
	return machine{home: home, root: filepath.Join(home, ".local", "share", "Trash")}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// trashed puts an item straight into the trash with a .trashinfo, as the shell
// integration or the file manager would.
func (m machine) trashed(t *testing.T, name, original, content string, deleted time.Time) {
	t.Helper()
	write(t, filepath.Join(m.root, "files", name), content)
	info := "[Trash Info]\nPath=" + original + "\n"
	if !deleted.IsZero() {
		info += "DeletionDate=" + deleted.Format("2006-01-02T15:04:05") + "\n"
	}
	write(t, filepath.Join(m.root, "info", name+".trashinfo"), info)
}

// ── Root and SoftDelete ────────────────────────────────────────────

func TestRootFollowsXDGDataHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	if got, _ := Root(); got != filepath.Join(home, ".local", "share", "Trash") {
		t.Fatalf("default root = %s", got)
	}
	data := filepath.Join(t.TempDir(), "data")
	t.Setenv("XDG_DATA_HOME", data)
	if got, _ := Root(); got != filepath.Join(data, "Trash") {
		t.Fatalf("root with XDG_DATA_HOME = %s", got)
	}
}

func TestSoftDeleteMovesIntoFilesWithTrashInfo(t *testing.T) {
	m := newMachine(t)
	src := filepath.Join(m.home, "doomed.txt")
	write(t, src, "bye")
	if err := SoftDelete(src); err != nil {
		t.Fatalf("SoftDelete failed: %v", err)
	}
	if exists(src) {
		t.Fatal("expected original path to be gone")
	}
	if got := read(t, filepath.Join(m.root, "files", "doomed.txt")); got != "bye" {
		t.Fatalf("trashed content = %q", got)
	}
	info := read(t, filepath.Join(m.root, "info", "doomed.txt.trashinfo"))
	for _, want := range []string{"[Trash Info]", "Path=", "DeletionDate=", src} {
		if !strings.Contains(info, want) {
			t.Fatalf("trashinfo lacks %q: %s", want, info)
		}
	}
}

func TestSoftDeleteCollisionGetsUniqueName(t *testing.T) {
	m := newMachine(t)
	write(t, filepath.Join(m.root, "files", "dup.txt"), "first")
	src := filepath.Join(m.home, "dup.txt")
	write(t, src, "second")
	if err := SoftDelete(src); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(m.root, "files", "dup.txt.2")); got != "second" {
		t.Fatalf("collision-renamed entry = %q", got)
	}
}

// TestSoftDeleteNeverDeletesForGood: SoftDelete used to fall back to os.RemoveAll
// when it could not find the trash, a silent permanent delete.
func TestSoftDeleteNeverDeletesForGood(t *testing.T) {
	newMachine(t)
	src := filepath.Join(t.TempDir(), "precious.txt")
	write(t, src, "keep me")
	// With no home and no XDG_DATA_HOME the trash cannot be located.
	t.Setenv("HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	if err := SoftDelete(src); err == nil {
		t.Fatal("SoftDelete must fail when the trash cannot be found")
	}
	if read(t, src) != "keep me" {
		t.Fatal("the file must still be there")
	}
}

// ── Items and Resolve ──────────────────────────────────────────────

func TestItemsReadsTrashInfo(t *testing.T) {
	m := newMachine(t)
	now := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	m.trashed(t, "old-notes.md", "/home/u/notes/old%20notes.md", "hello", now)
	m.trashed(t, "relative", "projects/x", "r", now.Add(-time.Hour))
	write(t, filepath.Join(m.root, "files", "orphan.txt"), "no info")

	items, err := Items()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Item{}
	for _, it := range items {
		byName[it.Name] = it
	}
	if len(items) != 3 {
		t.Fatalf("items = %+v", items)
	}
	if got := byName["old-notes.md"]; got.OriginalPath != "/home/u/notes/old notes.md" || !got.HasDate || !got.DeletedAt.Equal(now) || got.SizeBytes != 5 || got.Files != 1 {
		t.Fatalf("old-notes.md = %+v", got)
	}
	// A relative Path= is resolved against the directory that holds the trash directory.
	if got := byName["relative"].OriginalPath; got != filepath.Join(filepath.Dir(m.root), "projects", "x") {
		t.Fatalf("relative Path= resolved to %s", got)
	}
	// No .trashinfo: no original path, and the date falls back to the mtime.
	if got := byName["orphan.txt"]; got.OriginalPath != "" || got.HasDate || got.DeletedAt.IsZero() {
		t.Fatalf("orphan = %+v", got)
	}
	// Newest first.
	if items[0].Name == "relative" {
		t.Fatalf("the newest item comes first, got %s", items[0].Name)
	}
}

func TestItemsMissingTrashIsEmpty(t *testing.T) {
	newMachine(t)
	items, err := Items()
	if err != nil || len(items) != 0 {
		t.Fatalf("items = %v, %v", items, err)
	}
}

func TestResolveNamesAndPaths(t *testing.T) {
	m := newMachine(t)
	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now().Add(-time.Hour)
	m.trashed(t, "a.txt", filepath.Join(m.home, "work", "a.txt"), "old", old)
	m.trashed(t, "a.txt.2", filepath.Join(m.home, "work", "a.txt"), "new", recent) // the same file deleted twice
	m.trashed(t, "b", filepath.Join(m.home, "b"), "b", recent)

	got, err := Resolve([]string{"b", "a.txt"})
	if err != nil || len(got) != 2 || got[0].Name != "b" || got[1].Name != "a.txt" {
		t.Fatalf("by name: %+v %v", got, err)
	}
	// A path matches the most recently deleted item with that original path.
	got, err = Resolve([]string{filepath.Join(m.home, "work", "a.txt")})
	if err != nil || len(got) != 1 || got[0].Name != "a.txt.2" {
		t.Fatalf("by path: %+v %v", got, err)
	}
	// ~ expands, and the same item named twice counts once.
	got, err = Resolve([]string{"~/b", "b"})
	if err != nil || len(got) != 1 || got[0].Name != "b" {
		t.Fatalf("by ~ path and name: %+v %v", got, err)
	}
	// A relative path (it has a /) is relative to the current directory.
	if err := os.MkdirAll(filepath.Join(m.home, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(m.home, "work"))
	if got, err = Resolve([]string{"./a.txt"}); err != nil || len(got) != 1 || got[0].Name != "a.txt.2" {
		t.Fatalf("by relative path: %+v %v", got, err)
	}

	// An argument that matches nothing aborts, naming every one.
	if _, err = Resolve([]string{"b", "ghost", "~/nothing"}); err == nil || !strings.Contains(err.Error(), "ghost") || !strings.Contains(err.Error(), "~/nothing") {
		t.Fatalf("missing items: %v", err)
	}
}

// ── Restore ────────────────────────────────────────────────────────

func TestRestoreRoundTripAndCleanup(t *testing.T) {
	m := newMachine(t)
	orig := filepath.Join(m.home, "work", "old-notes.md")
	m.trashed(t, "old-notes.md", orig, "notes", time.Now())
	// directorysizes: another item's line stays, this one's goes.
	write(t, filepath.Join(m.root, "directorysizes"), "12 1700000000 old-notes.md\n34 1700000001 other\n")

	items, _ := Items()
	actions, skipped := RestorePlan(items)
	if len(skipped) != 0 || len(actions) != 1 {
		t.Fatalf("actions=%d skipped=%v", len(actions), skipped)
	}
	if !strings.Contains(actions[0].Description, "Restore old-notes.md to "+orig) || !strings.Contains(actions[0].Description, "creates "+filepath.Join(m.home, "work")) {
		t.Fatalf("description %q must name the parent it creates", actions[0].Description)
	}
	if err := actions[0].Execute(); err != nil {
		t.Fatal(err)
	}
	if read(t, orig) != "notes" {
		t.Fatal("the file must be back")
	}
	if exists(filepath.Join(m.root, "files", "old-notes.md")) || exists(filepath.Join(m.root, "info", "old-notes.md.trashinfo")) {
		t.Fatal("the item and its .trashinfo must be gone from the trash")
	}
	if got := read(t, filepath.Join(m.root, "directorysizes")); got != "34 1700000001 other\n" {
		t.Fatalf("directorysizes = %q", got)
	}
}

func TestRestoreNeverOverwrites(t *testing.T) {
	m := newMachine(t)
	here := filepath.Join(m.home, "here.txt")
	write(t, here, "current")
	dangling := filepath.Join(m.home, "dangling")
	if err := os.Symlink("/nonexistent", dangling); err != nil {
		t.Fatal(err)
	}
	m.trashed(t, "here.txt", here, "trashed", time.Now())
	m.trashed(t, "dangling", dangling, "trashed", time.Now())
	m.trashed(t, "x1", filepath.Join(m.home, "same"), "1", time.Now())
	m.trashed(t, "x2", filepath.Join(m.home, "same"), "2", time.Now())
	write(t, filepath.Join(m.root, "files", "noinfo"), "orphan")

	// Two targets that share an original path: the first one named wins.
	var targets []Item
	for _, n := range []string{"here.txt", "dangling", "x1", "x2", "noinfo"} {
		it, err := Resolve([]string{n})
		if err != nil {
			t.Fatal(err)
		}
		targets = append(targets, it...)
	}
	actions, skipped := RestorePlan(targets)
	if len(actions) != 1 || actions[0].ID != "trash-restore-x1" {
		t.Fatalf("only x1 can be restored: %+v", actions)
	}
	reasons := map[string]string{}
	for _, s := range skipped {
		reasons[s.Name] = s.Reason
	}
	if !strings.Contains(reasons["here.txt"], "already exists") || !strings.Contains(reasons["dangling"], "already exists") ||
		!strings.Contains(reasons["x2"], "restored by x1") || !strings.Contains(reasons["noinfo"], "no original path") {
		t.Fatalf("skips = %v", reasons)
	}
	if read(t, here) != "current" {
		t.Fatal("an existing file is never overwritten")
	}

	// Something appears between the plan and the action: still no overwrite.
	write(t, filepath.Join(m.home, "same"), "appeared")
	if err := actions[0].Execute(); err == nil {
		t.Fatal("the action must re-check and refuse to overwrite")
	}
	if !exists(filepath.Join(m.root, "files", "x1")) {
		t.Fatal("a failed restore leaves the item in the trash")
	}
}

// ── Delete, Empty, Prune ───────────────────────────────────────────

func TestDeleteForgetsEverythingAboutTheItem(t *testing.T) {
	m := newMachine(t)
	m.trashed(t, "gone", "/x/gone", "bye", time.Now())
	m.trashed(t, "stays", "/x/stays", "ok", time.Now())
	write(t, filepath.Join(m.root, "directorysizes"), "1 1 gone\n2 2 stays\n")
	items, _ := Resolve([]string{"gone"})
	if err := Delete(items[0]); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(m.root, "files", "gone")) || exists(filepath.Join(m.root, "info", "gone.trashinfo")) {
		t.Fatal("files/<name> and its .trashinfo must go")
	}
	if got := read(t, filepath.Join(m.root, "directorysizes")); got != "2 2 stays\n" {
		t.Fatalf("directorysizes = %q", got)
	}
	if !exists(filepath.Join(m.root, "files", "stays")) {
		t.Fatal("other items stay")
	}
}

func TestEmptyKeepsDirectoriesAndLeavesOthersAlone(t *testing.T) {
	m := newMachine(t)
	m.trashed(t, "a", "/x/a", "a", time.Now())
	write(t, filepath.Join(m.root, "files", "dir", "nested", "b"), "b")
	write(t, filepath.Join(m.root, "info", "orphan.trashinfo"), "[Trash Info]\n")
	write(t, filepath.Join(m.root, "directorysizes"), "1 1 dir\n")
	write(t, filepath.Join(m.root, "expunged", "keep"), "k")

	sum, err := Summarize()
	if err != nil || sum.Items != 2 || sum.Files != 2 || sum.SizeBytes != 2 {
		t.Fatalf("summary = %+v, %v", sum, err)
	}
	if err := Empty(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"files", "info"} {
		entries, err := os.ReadDir(filepath.Join(m.root, d))
		if err != nil || len(entries) != 0 {
			t.Fatalf("%s must be kept and empty: %v %v", d, entries, err)
		}
	}
	if exists(filepath.Join(m.root, "directorysizes")) {
		t.Fatal("directorysizes must go")
	}
	if !exists(filepath.Join(m.root, "expunged", "keep")) {
		t.Fatal("expunged/ is not ours to touch")
	}
	if err := Empty(); err != nil {
		t.Fatalf("emptying an empty trash is a no-op: %v", err)
	}
}

func TestEmptyMissingTrashIsANoOp(t *testing.T) {
	newMachine(t)
	if err := Empty(); err != nil {
		t.Fatal(err)
	}
	if sum, err := Summarize(); err != nil || sum.Items != 0 {
		t.Fatalf("summary = %+v, %v", sum, err)
	}
}

func TestSelectOlderThan(t *testing.T) {
	m := newMachine(t)
	now := time.Now()
	m.trashed(t, "old", "/x/old", "aaaa", now.Add(-40*24*time.Hour))
	m.trashed(t, "older", "/x/older", "bb", now.Add(-60*24*time.Hour))
	m.trashed(t, "fresh", "/x/fresh", "c", now.Add(-24*time.Hour))
	// No DeletionDate: never pruned, whatever its mtime says.
	m.trashed(t, "undated", "/x/undated", "d", time.Time{})
	if err := os.Chtimes(filepath.Join(m.root, "files", "undated"), now.Add(-365*24*time.Hour), now.Add(-365*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	res, matched, err := SelectOlderThan(30*24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 2 || matched[0].Name != "older" || matched[1].Name != "old" {
		t.Fatalf("matched = %+v (oldest first)", res.Matched)
	}
	if res.Kept != 1 || res.Skipped != 1 || res.SizeBytes != 6 || len(res.Matched) != 2 {
		t.Fatalf("result = %+v", res)
	}
}

// ── Integrations ───────────────────────────────────────────────────

func enabledNames(t *testing.T, opts EnableOptions) []string {
	t.Helper()
	actions, err := EnableActions(opts)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, a := range actions {
		out = append(out, strings.TrimPrefix(a.ID, "trash-enable-"))
	}
	return out
}

func runAll(t *testing.T, opts EnableOptions) {
	t.Helper()
	actions, err := EnableActions(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range actions {
		if err := a.Execute(); err != nil {
			t.Fatalf("%s: %v", a.Description, err)
		}
	}
}

func TestStatusReadsTheLiveSystem(t *testing.T) {
	m := newMachine(t)
	st, err := GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	if st.Root != m.root || st.WarnSizeBytes != 1<<30 || st.OverLimit {
		t.Fatalf("status = %+v", st)
	}
	// Nothing configured on a fresh machine: shell-rm and file-explorer are off,
	// VS Code is on (it deletes to the trash unless explicitly turned off).
	if st.Enabled(ShellRM) || !st.Enabled(VSCode) || st.Enabled(FileExplorer) || st.WarningCount() != 2 {
		t.Fatalf("integrations = %+v", st.Integrations)
	}
	if !strings.Contains(st.Integrations[2].Detail, filepath.Join(m.root, "files")) || !strings.Contains(st.Integrations[2].Detail, "ws trash enable") {
		t.Fatalf("file-explorer detail = %q", st.Integrations[2].Detail)
	}

	// An explicit false turns VS Code off.
	write(t, filepath.Join(m.home, ".config", "Code", "User", "settings.json"), "{\n  // keep\n  \"files.enableTrash\": false\n}\n")
	if st, _ = GetStatus(); st.Enabled(VSCode) {
		t.Fatal("files.enableTrash false means VS Code is off")
	}
}

func TestEnableShellRM(t *testing.T) {
	m := newMachine(t)
	write(t, filepath.Join(m.home, ".bashrc"), "export A=1\n")
	write(t, filepath.Join(m.home, ".zshrc"), "export B=2")

	runAll(t, EnableOptions{NoVSCode: true, NoFileExplorer: true})
	const guarded = "command -v ws-trash-rm >/dev/null 2>&1 && alias rm='ws-trash-rm'"
	for _, rc := range []string{".bashrc", ".zshrc"} {
		got := read(t, filepath.Join(m.home, rc))
		if strings.Count(got, guarded) != 1 {
			t.Fatalf("%s must hold the guarded alias once:\n%s", rc, got)
		}
	}
	if !strings.HasPrefix(read(t, filepath.Join(m.home, ".zshrc")), "export B=2\n") {
		t.Fatal("existing content stays, and a missing final newline is added")
	}
	script := filepath.Join(m.home, ".local", "bin", "ws-trash-rm")
	fi, err := os.Stat(script)
	if err != nil || fi.Mode()&0o111 == 0 || !strings.Contains(read(t, script), m.root) {
		t.Fatalf("the script is executable and names the trash: %v", err)
	}
	if st, _ := GetStatus(); !st.Enabled(ShellRM) || !strings.Contains(st.Integrations[0].Detail, ".bashrc") {
		t.Fatalf("status after enable: %+v", st.Integrations)
	}

	// Enabling twice adds nothing.
	if names := enabledNames(t, EnableOptions{NoVSCode: true, NoFileExplorer: true}); len(names) != 0 {
		t.Fatalf("already on, got actions for %v", names)
	}
	runAll(t, EnableOptions{NoVSCode: true, NoFileExplorer: true})
	if strings.Count(read(t, filepath.Join(m.home, ".bashrc")), guarded) != 1 {
		t.Fatal("the alias must not be added twice")
	}
}

func TestEnableCreatesTheRCFileOfTheShell(t *testing.T) {
	m := newMachine(t)
	t.Setenv("SHELL", "/usr/bin/zsh")
	runAll(t, EnableOptions{NoVSCode: true, NoFileExplorer: true})
	if !exists(filepath.Join(m.home, ".zshrc")) || exists(filepath.Join(m.home, ".bashrc")) {
		t.Fatal("with no rc file, the rc file of $SHELL is created, and only that one")
	}
}

func TestEnableReplacesTheLegacyAliasAndFollowsSymlinkedRC(t *testing.T) {
	m := newMachine(t)
	// ~/.bashrc is a dotfile: a symlink into the workspace. The alias goes into the
	// file it points at, through the link.
	real := filepath.Join(m.home, "Workspace", "configs", "dotfiles", "bashrc")
	write(t, real, "export A=1\nalias rm='ws-trash-rm'\n")
	if err := os.Symlink(real, filepath.Join(m.home, ".bashrc")); err != nil {
		t.Fatal(err)
	}
	// The legacy unguarded alias counts as on.
	write(t, filepath.Join(m.home, ".local", "bin", "ws-trash-rm"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(m.home, ".local", "bin", "ws-trash-rm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if st, _ := GetStatus(); !st.Enabled(ShellRM) {
		t.Fatal("the legacy alias line still counts as enabled")
	}

	// A fresh script (and the upgrade to the guarded line) is written when enabling anyway.
	if err := enableShellRM(m.root); err != nil {
		t.Fatal(err)
	}
	got := read(t, real)
	if strings.Contains(got, "\nalias rm='ws-trash-rm'\n") || !strings.Contains(got, "command -v ws-trash-rm") {
		t.Fatalf("the legacy line is upgraded to the guarded one:\n%s", got)
	}
	if !isSymlink(filepath.Join(m.home, ".bashrc")) {
		t.Fatal("the symlink must survive")
	}
}

func isSymlink(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

func TestEnableVSCodeEditsOnlyTheValue(t *testing.T) {
	m := newMachine(t)
	settings := filepath.Join(m.home, ".config", "Code", "User", "settings.json")
	orig := "{\n  // my settings\n  \"editor.fontSize\": 14,\n  \"files.enableTrash\":  false,  // off\n  \"x\": [1, 2]\n}\n"
	write(t, settings, orig)
	runAll(t, EnableOptions{NoShellRM: true, NoFileExplorer: true})
	want := strings.Replace(orig, "false", "true", 1)
	if got := read(t, settings); got != want {
		t.Fatalf("only the value changes; comments and layout stay:\n%s\nwant\n%s", got, want)
	}
	if st, _ := GetStatus(); !st.Enabled(VSCode) {
		t.Fatal("VS Code is on after enable")
	}
	// A missing file or key means it is on already: nothing to do.
	if names := enabledNames(t, EnableOptions{NoShellRM: true, NoFileExplorer: true}); len(names) != 0 {
		t.Fatalf("got actions for %v", names)
	}
}

func TestEnableFileExplorerCreatesDirectories(t *testing.T) {
	m := newMachine(t)
	runAll(t, EnableOptions{NoShellRM: true, NoVSCode: true})
	for _, d := range []string{"files", "info"} {
		fi, err := os.Stat(filepath.Join(m.root, d))
		if err != nil || fi.Mode().Perm() != 0o700 {
			t.Fatalf("%s: %v %v", d, fi, err)
		}
	}
	if st, _ := GetStatus(); !st.Enabled(FileExplorer) {
		t.Fatal("file-explorer is on after enable")
	}
}

func TestEnableLeavesOutWhatTheFlagsSay(t *testing.T) {
	newMachine(t)
	got := enabledNames(t, EnableOptions{NoShellRM: true})
	if strings.Join(got, ",") != FileExplorer {
		t.Fatalf("--no-shell-rm leaves it out; vscode is already on: %v", got)
	}
	if got := enabledNames(t, EnableOptions{NoShellRM: true, NoFileExplorer: true}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

// TestDisableRemovesTheShellIntegration: disable used to delete only a state file,
// so the script and the rc alias stayed (plan 0002 trash bugs).
func TestDisableRemovesTheShellIntegration(t *testing.T) {
	m := newMachine(t)
	write(t, filepath.Join(m.home, ".bashrc"), "export A=1\nalias rm='ws-trash-rm'\n")
	write(t, filepath.Join(m.home, ".zshrc"), "export B=1\n")
	runAll(t, EnableOptions{NoVSCode: true, NoFileExplorer: true})
	write(t, filepath.Join(m.home, ".local", "state", "ws-tool", "trash-setup.json"), "{}")
	write(t, filepath.Join(m.root, "files", "keep"), "k")
	settings := filepath.Join(m.home, ".config", "Code", "User", "settings.json")
	write(t, settings, "{\"files.enableTrash\": false}")

	actions := DisableActions()
	if len(actions) != 2 {
		t.Fatalf("one action for the shell integration and one for the legacy state file, got %d", len(actions))
	}
	if _, err := Disable(); err != nil {
		t.Fatal(err)
	}
	for _, rc := range []string{".bashrc", ".zshrc"} {
		if got := read(t, filepath.Join(m.home, rc)); strings.Contains(got, "ws-trash-rm") {
			t.Fatalf("%s still has the alias:\n%s", rc, got)
		}
	}
	if !strings.Contains(read(t, filepath.Join(m.home, ".bashrc")), "export A=1") {
		t.Fatal("other lines stay")
	}
	if exists(filepath.Join(m.home, ".local", "bin", "ws-trash-rm")) || exists(filepath.Join(m.home, ".local", "state", "ws-tool", "trash-setup.json")) {
		t.Fatal("the script and the legacy state file go")
	}
	if !exists(filepath.Join(m.root, "files", "keep")) || !strings.Contains(read(t, settings), "false") {
		t.Fatal("the trash and VS Code's setting are left alone")
	}
	if st, _ := GetStatus(); st.Enabled(ShellRM) {
		t.Fatal("shell-rm is off after disable")
	}
	if got := DisableActions(); len(got) != 0 {
		t.Fatalf("nothing left to disable: %d actions", len(got))
	}
}
