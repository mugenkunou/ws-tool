package cron

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// machine is one isolated user: a state dir (cron.state, cron.log), a stand-in
// live crontab (WS_CRONTAB_FILE) and a workspace with its declared crontab.
type machine struct {
	state, live, workspace, declared string
}

func newMachine(t *testing.T) machine {
	t.Helper()
	root := t.TempDir()
	m := machine{
		state:     filepath.Join(root, "state"),
		live:      filepath.Join(root, "crontab"),
		workspace: filepath.Join(root, "Workspace"),
	}
	m.declared = DeclaredPath(m.workspace)
	t.Setenv("XDG_STATE_HOME", m.state)
	t.Setenv("WS_CRONTAB_FILE", m.live)
	t.Setenv("DISPLAY", ":7")
	if err := os.MkdirAll(m.workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	return m
}

func (m machine) setLive(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(m.live, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (m machine) setDeclared(t *testing.T, content string) {
	t.Helper()
	if err := WriteDeclared(m.declared, content); err != nil {
		t.Fatal(err)
	}
}

func (m machine) liveText(t *testing.T) string {
	t.Helper()
	b, _ := os.ReadFile(m.live)
	return string(b)
}

func (m machine) declaredText(t *testing.T) string {
	t.Helper()
	b, _ := os.ReadFile(m.declared)
	return string(b)
}

func (m machine) opts() PlanOptions {
	return PlanOptions{Bin: "/usr/local/bin/ws", Workspace: m.workspace, Display: ":7"}
}

// ── registry ───────────────────────────────────────────────────────

func TestRegistry(t *testing.T) {
	want := []string{"dotfile-backup", "maintenance", "mega-sync", "pass-backup", "session-prune", "sync"}
	if got := AllNames(); !slices.Equal(got, want) {
		t.Fatalf("AllNames = %v, want %v (no aliases, no retired names)", got, want)
	}
	for name, j := range Builtins {
		if j.Name != name || j.Schedule == "" || j.IntervalSecs <= 0 || j.Description == "" {
			t.Errorf("job %s is incomplete: %+v", name, j)
		}
	}
	// Schedules per spec: every 30 minutes, session-prune at 02:00.
	for _, n := range []string{"mega-sync", "dotfile-backup", "pass-backup"} {
		if Builtins[n].Schedule != "*/30 * * * *" || Builtins[n].IntervalSecs != 1800 {
			t.Errorf("%s schedule = %q interval %d", n, Builtins[n].Schedule, Builtins[n].IntervalSecs)
		}
	}
	if Builtins["session-prune"].Schedule != "0 2 * * *" || Builtins["session-prune"].IntervalSecs != 86400 {
		t.Errorf("session-prune = %+v", Builtins["session-prune"])
	}
	if !slices.Equal(Presets["sync"], []string{"mega-sync", "dotfile-backup", "pass-backup"}) || !slices.Equal(Presets["maintenance"], []string{"session-prune"}) {
		t.Errorf("presets = %v", Presets)
	}
	for preset, members := range Presets {
		for _, member := range members {
			if _, ok := Builtins[member]; !ok {
				t.Errorf("preset %s names unknown job %s", preset, member)
			}
		}
	}
}

func TestJobBodies(t *testing.T) {
	bodies := map[string]string{
		"dotfile-backup": "/ws --workspace /W dotfile backup push --quiet",
		"pass-backup":    "/ws --workspace /W pass backup push --quiet",
		"session-prune":  "/ws --workspace /W session prune --quiet",
	}
	for name, want := range bodies {
		if got := Builtins[name].scriptBody("/ws", ":1", "/W"); got != want {
			t.Errorf("%s body = %q, want %q", name, got, want)
		}
	}
	if body := Builtins["mega-sync"].scriptBody("/ws", ":9", "/W"); !strings.Contains(body, "export DISPLAY=:9") || !strings.Contains(body, "megasync") {
		t.Errorf("mega-sync body = %q", body)
	}
}

func TestAliasesAndRetired(t *testing.T) {
	if c, old := Canonical("dotfile-sync"); c != "dotfile-backup" || !old {
		t.Errorf("dotfile-sync -> %s %v", c, old)
	}
	if c, old := Canonical("log-prune"); c != "session-prune" || !old {
		t.Errorf("log-prune -> %s %v", c, old)
	}
	if c, old := Canonical("mega-sync"); c != "mega-sync" || old {
		t.Errorf("mega-sync -> %s %v", c, old)
	}
	if got := Names("dotfile-backup"); !slices.Equal(got, []string{"dotfile-backup", "dotfile-sync"}) {
		t.Errorf("Names = %v", got)
	}
	for _, r := range []string{"secret-scan", "ignore-scan", "repo-sync"} {
		if !IsRetired(r) {
			t.Errorf("%s must be retired", r)
		}
		if _, _, err := Resolve(r); err == nil || !strings.Contains(err.Error(), "was removed") {
			t.Errorf("Resolve(%s) = %v, want a was-removed error", r, err)
		}
	}
}

func TestResolve(t *testing.T) {
	jobs, aliased, err := Resolve("sync")
	if err != nil || aliased || len(jobs) != 3 || jobs[0].Name != "mega-sync" || jobs[2].Name != "pass-backup" {
		t.Fatalf("preset: %v %v %v", jobs, aliased, err)
	}
	jobs, aliased, err = Resolve("dotfile-sync")
	if err != nil || !aliased || len(jobs) != 1 || jobs[0].Name != "dotfile-backup" {
		t.Fatalf("old name: %v %v %v", jobs, aliased, err)
	}
	if _, _, err = Resolve("nope"); err == nil || !strings.Contains(err.Error(), "unknown cron job") {
		t.Fatalf("unknown: %v", err)
	}
}

// ── blocks ─────────────────────────────────────────────────────────

func TestBlockForIsTheSpecText(t *testing.T) {
	got := BlockFor(Builtins["mega-sync"], "/home/user/.local/bin/ws", "/home/user/Workspace", ":1")
	want := `# >>> ws:mega-sync >>>
SHELL=/usr/bin/bash
*/30 * * * * /home/user/.local/bin/ws --workspace /home/user/Workspace cron run --if-due --display :1 mega-sync
@reboot /home/user/.local/bin/ws --workspace /home/user/Workspace cron run --if-due --display :1 mega-sync
# <<< ws:mega-sync <<<
`
	if got != want {
		t.Fatalf("block:\n%s\nwant:\n%s", got, want)
	}
	// Only mega-sync carries --display.
	if blk := BlockFor(Builtins["dotfile-backup"], "/ws", "/W", ":1"); strings.Contains(blk, "--display") || !strings.Contains(blk, "/ws --workspace /W cron run --if-due dotfile-backup") {
		t.Fatalf("dotfile-backup block:\n%s", blk)
	}
}

func TestParseBlocksIsGeneric(t *testing.T) {
	ct := "MAILTO=me\n\n# >>> ws:dotfile-sync >>>\n*/30 * * * * x\n# <<< ws:dotfile-sync <<<\n0 5 * * * backup\n# >>> ws:secret-scan >>>\n0 * * * * y\n# <<< ws:secret-scan <<<\n"
	blocks, other := ParseBlocks(ct)
	if len(blocks) != 2 || blocks[0].Name != "dotfile-sync" || blocks[1].Name != "secret-scan" {
		t.Fatalf("blocks = %+v (every ws name counts, retired and old included)", blocks)
	}
	if !slices.Equal(other, []string{"MAILTO=me", "0 5 * * * backup"}) {
		t.Fatalf("other = %v", other)
	}
}

func TestRemoveBlocksKeepsOtherLines(t *testing.T) {
	ct := "MAILTO=me\n\n" + BlockFor(Builtins["mega-sync"], "/ws", "/W", ":1") + "\n0 5 * * * mine\n" + BlockFor(Builtins["session-prune"], "/ws", "/W", ":1")
	got := removeBlocks(ct, "mega-sync")
	if strings.Contains(got, "mega-sync") || !strings.Contains(got, "MAILTO=me") || !strings.Contains(got, "0 5 * * * mine") || !strings.Contains(got, "ws:session-prune") {
		t.Fatalf("removed:\n%s", got)
	}
	if again := removeBlocks(got, "mega-sync"); again != got {
		t.Fatal("removing an absent block must be a no-op")
	}
	if strings.Contains(removeBlocks(ct, "mega-sync", "session-prune"), "ws:") {
		t.Fatal("both blocks go")
	}
}

func TestOutdated(t *testing.T) {
	cur := Block{Name: "dotfile-backup", Text: "# >>> ws:dotfile-backup >>>\n*/30 * * * * /a/ws --workspace /W cron run --if-due dotfile-backup\n@reboot /a/ws --workspace /W cron run --if-due dotfile-backup\n# <<< ws:dotfile-backup <<<\n"}
	if Outdated(cur) {
		t.Error("a current block is not outdated")
	}
	// A different binary or workspace does not make it outdated.
	moved := Block{Name: "dotfile-backup", Text: strings.ReplaceAll(cur.Text, "/a/ws", "/home/u/tmp/wsux/ws")}
	if Outdated(moved) {
		t.Error("a build in another directory must not mark everything outdated")
	}
	old := Block{Name: "dotfile-sync", Text: "# >>> ws:dotfile-sync >>>\n*/30 * * * * /a/ws cron run dotfile-sync\n# <<< ws:dotfile-sync <<<\n"}
	if !Outdated(old) {
		t.Error("a block under an old name is outdated")
	}
	noDue := Block{Name: "mega-sync", Text: "# >>> ws:mega-sync >>>\n*/30 * * * * /a/ws cron run mega-sync\n# <<< ws:mega-sync <<<\n"}
	if !Outdated(noDue) {
		t.Error("a block that lacks --if-due is outdated")
	}
}

func TestDeclaredFileIsWrittenAtomically(t *testing.T) {
	m := newMachine(t)
	if text, err := ReadDeclared(m.declared); err != nil || text != "" {
		t.Fatalf("a missing declared crontab is empty: %q %v", text, err)
	}
	m.setDeclared(t, "x\n")
	fi, err := os.Stat(m.declared)
	if err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("declared crontab: %v %v", fi, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(m.declared)); len(entries) != 1 {
		t.Fatalf("no temp file may be left behind: %v", entries)
	}
}

// ── state and log ──────────────────────────────────────────────────

func writeState(t *testing.T, m machine, lines ...string) {
	t.Helper()
	dir := filepath.Join(m.state, "ws-tool")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cron.state"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLastRunCountsOldNames(t *testing.T) {
	m := newMachine(t)
	writeState(t, m,
		`{"job":"dotfile-sync","ts":"2026-10-20T10:00:00Z","exit":1}`,
		`{"job":"dotfile-backup","ts":"2026-10-21T10:00:00Z","exit":0}`,
		`{"job":"dotfile-sync","ts":"2026-10-22T10:00:00Z","exit":3}`,
		`{"job":"mega-sync","ts":"2026-10-22T11:00:00Z","exit":0}`)
	statePath, _ := StateFilePath()
	r, err := LastRun(statePath, "dotfile-backup")
	if err != nil || r.ExitCode != 3 || !r.Time.Equal(time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("a record under the old name counts as the new job's, newest wins: %+v %v", r, err)
	}
	if r, _ := LastRun(statePath, "dotfile-sync"); r.ExitCode != 3 {
		t.Fatalf("asking by the old name finds the same history: %+v", r)
	}
	if r, _ := LastRun(statePath, "pass-backup"); !r.Time.IsZero() {
		t.Fatalf("no history: %+v", r)
	}
}

func TestReadLogFiltersAndCountsOldNames(t *testing.T) {
	m := newMachine(t)
	dir := filepath.Join(m.state, "ws-tool")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	logText := "t1 [dotfile-sync] Starting\nt2 [mega-sync] Starting\nt3 [dotfile-backup] Done (exit 0)\nt4 [session-prune] Starting\n"
	if err := os.WriteFile(filepath.Join(dir, "cron.log"), []byte(logText), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath, _ := LogFilePath()

	got, _ := ReadLog(logPath, []string{"dotfile-backup"}, 0)
	if !slices.Equal(got, []string{"t1 [dotfile-sync] Starting", "t3 [dotfile-backup] Done (exit 0)"}) {
		t.Fatalf("one job, old name included: %v", got)
	}
	if all, _ := ReadLog(logPath, nil, 0); len(all) != 4 {
		t.Fatalf("no job means all: %v", all)
	}
	if two, _ := ReadLog(logPath, []string{"mega-sync", "session-prune"}, 0); len(two) != 2 {
		t.Fatalf("several jobs: %v", two)
	}
	if last, _ := ReadLog(logPath, nil, 2); !slices.Equal(last, []string{"t3 [dotfile-backup] Done (exit 0)", "t4 [session-prune] Starting"}) {
		t.Fatalf("the last n lines: %v", last)
	}
	if none, err := ReadLog(filepath.Join(dir, "absent.log"), nil, 0); err != nil || none != nil {
		t.Fatalf("a missing log is empty: %v %v", none, err)
	}
}

// ── ls ─────────────────────────────────────────────────────────────

func stateOf(t *testing.T, m machine, name string) string {
	t.Helper()
	jobs, err := Jobs(m.workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range jobs {
		if j.Name == name {
			return j.State
		}
	}
	t.Fatalf("no row for %s in %v", name, jobs)
	return ""
}

func TestJobStates(t *testing.T) {
	m := newMachine(t)
	cur := BlockFor(Builtins["mega-sync"], "/ws", "/W", ":1")
	if got := stateOf(t, m, "mega-sync"); got != StateNotInstalled {
		t.Fatalf("neither file: %s", got)
	}
	m.setLive(t, cur)
	if got := stateOf(t, m, "mega-sync"); got != StateUndeclared {
		t.Fatalf("live only: %s", got)
	}
	m.setLive(t, "")
	m.setDeclared(t, cur)
	if got := stateOf(t, m, "mega-sync"); got != StateNotApplied {
		t.Fatalf("declared only: %s", got)
	}
	m.setLive(t, cur)
	if got := stateOf(t, m, "mega-sync"); got != StateInstalled {
		t.Fatalf("both, current: %s", got)
	}
	// Outdated beats undeclared: the live block lacks --if-due.
	m.setLive(t, "# >>> ws:mega-sync >>>\n*/30 * * * * /ws cron run mega-sync\n# <<< ws:mega-sync <<<\n")
	if got := stateOf(t, m, "mega-sync"); got != StateOutdated {
		t.Fatalf("no --if-due: %s", got)
	}
	// A block under an old name counts as the new job's, and is outdated.
	m.setLive(t, "# >>> ws:dotfile-sync >>>\n*/30 * * * * /ws cron run dotfile-sync\n# <<< ws:dotfile-sync <<<\n")
	if got := stateOf(t, m, "dotfile-backup"); got != StateOutdated {
		t.Fatalf("old name: %s", got)
	}
}

func TestPresetStatesAndRetiredRows(t *testing.T) {
	m := newMachine(t)
	m.setLive(t, BlockFor(Builtins["mega-sync"], "/ws", "/W", ":1")+BlockFor(Builtins["pass-backup"], "/ws", "/W", ":1"))
	if got := stateOf(t, m, "sync"); got != "2 of 3 installed" {
		t.Fatalf("sync preset: %s", got)
	}
	if got := stateOf(t, m, "maintenance"); got != StateNotInstalled {
		t.Fatalf("maintenance preset: %s", got)
	}

	// A retired job has a row only while a block remains, last, in either file.
	jobs, _ := Jobs(m.workspace)
	for _, j := range jobs {
		if j.Retired {
			t.Fatalf("no retired row without a block: %+v", j)
		}
	}
	m.setLive(t, m.liveText(t)+"# >>> ws:secret-scan >>>\n0 * * * * x\n# <<< ws:secret-scan <<<\n")
	m.setDeclared(t, "# >>> ws:repo-sync >>>\n*/30 * * * * y\n# <<< ws:repo-sync <<<\n")
	jobs, _ = Jobs(m.workspace)
	var retired []string
	for _, j := range jobs {
		if j.Retired {
			retired = append(retired, j.Name+":"+j.State)
		}
	}
	if !slices.Equal(retired, []string{"secret-scan:retired", "repo-sync:retired"}) {
		t.Fatalf("retired rows = %v (in the registry's order)", retired)
	}
	if last := jobs[len(jobs)-1]; !last.Retired {
		t.Fatalf("retired rows come last: %+v", last)
	}
	// Rows: jobs, then presets, then retired.
	names := []string{}
	for _, j := range jobs[:6] {
		names = append(names, j.Name)
	}
	if !slices.Equal(names, []string{"dotfile-backup", "mega-sync", "pass-backup", "session-prune", "maintenance", "sync"}) {
		t.Fatalf("row order = %v", names)
	}
}

func TestNextRun(t *testing.T) {
	last := time.Now().Add(-10 * time.Minute)
	j := Job{Installed: true, LastRun: RunRecord{Time: last}, Interval: 30 * time.Minute}
	if got := j.NextRun(); !got.Equal(last.Add(30 * time.Minute)) {
		t.Fatalf("next = %v", got)
	}
	j.Installed = false
	if !j.NextRun().IsZero() {
		t.Fatal("next_run is set only for installed jobs")
	}
}

// ── add, rm ────────────────────────────────────────────────────────

func TestAddActionsWriteBothFilesTogether(t *testing.T) {
	m := newMachine(t)
	m.setLive(t, "MAILTO=me\n")
	m.setDeclared(t, "CRON_TZ=UTC\n")
	jobs, _, _ := Resolve("sync")
	acts, already, err := AddActions(jobs, m.opts())
	if err != nil || len(already) != 0 || len(acts) != 3 {
		t.Fatalf("actions=%d already=%v err=%v", len(acts), already, err)
	}
	if !strings.Contains(acts[0].Description, "Install mega-sync (*/30 * * * * + @reboot) in the crontab and "+m.declared) {
		t.Fatalf("description names both files: %q", acts[0].Description)
	}
	for _, a := range acts {
		if err := a.Execute(); err != nil {
			t.Fatal(err)
		}
	}
	for _, text := range []string{m.liveText(t), m.declaredText(t)} {
		for _, job := range []string{"mega-sync", "dotfile-backup", "pass-backup"} {
			if !strings.Contains(text, "ws:"+job) {
				t.Fatalf("%s missing from:\n%s", job, text)
			}
		}
		if !strings.Contains(text, "--workspace "+m.workspace+" cron run --if-due") {
			t.Fatalf("lines carry --workspace and --if-due:\n%s", text)
		}
	}
	if !strings.Contains(m.liveText(t), "--display :7") || strings.Count(m.liveText(t), "--display") != 2 {
		t.Fatalf("only mega-sync's two lines carry --display:\n%s", m.liveText(t))
	}
	if !strings.HasPrefix(m.liveText(t), "MAILTO=me\n") || !strings.HasPrefix(m.declaredText(t), "CRON_TZ=UTC\n") {
		t.Fatal("lines outside ws blocks are untouched")
	}

	// Adding again: every job is already installed, no action.
	acts, already, _ = AddActions(jobs, m.opts())
	if len(acts) != 0 || !slices.Equal(already, []string{"mega-sync", "dotfile-backup", "pass-backup"}) {
		t.Fatalf("second add: actions=%d already=%v", len(acts), already)
	}
}

func TestAddReplacesAnOldNameInBothFiles(t *testing.T) {
	m := newMachine(t)
	old := "# >>> ws:dotfile-sync >>>\n*/30 * * * * /bin/ws cron run dotfile-sync\n@reboot /bin/ws cron run dotfile-sync\n# <<< ws:dotfile-sync <<<\n"
	m.setLive(t, "MAILTO=me\n"+old)
	m.setDeclared(t, old)
	jobs, _, _ := Resolve("dotfile-backup")
	acts, _, err := AddActions(jobs, m.opts())
	if err != nil || len(acts) != 1 || !strings.Contains(acts[0].Description, "replacing dotfile-sync") {
		t.Fatalf("acts=%v err=%v", acts, err)
	}
	if err := acts[0].Execute(); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{m.liveText(t), m.declaredText(t)} {
		if strings.Contains(text, "dotfile-sync") || !strings.Contains(text, "ws:dotfile-backup") {
			t.Fatalf("the old block must be replaced in both files:\n%s", text)
		}
	}
	if !strings.Contains(m.liveText(t), "MAILTO=me") {
		t.Fatal("other lines stay")
	}
}

// TestAddFixesAnUndeclaredJob: the live block is current but configs/crontab was
// never written (the bug: add wrote only the live crontab).
func TestAddFixesAnUndeclaredOrUnappliedJob(t *testing.T) {
	m := newMachine(t)
	jobs, _, _ := Resolve("session-prune")
	block := BlockFor(Builtins["session-prune"], m.opts().Bin, m.workspace, ":7")
	m.setLive(t, block)
	acts, already, _ := AddActions(jobs, m.opts())
	if len(acts) != 1 || len(already) != 0 {
		t.Fatalf("undeclared: acts=%d already=%v", len(acts), already)
	}
	if err := acts[0].Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.declaredText(t), "ws:session-prune") {
		t.Fatal("configs/crontab must now declare it")
	}
	// not applied: declared only.
	m.setLive(t, "")
	acts, _, _ = AddActions(jobs, m.opts())
	if len(acts) != 1 {
		t.Fatalf("not applied: acts=%d", len(acts))
	}
}

func TestRemoveActions(t *testing.T) {
	m := newMachine(t)
	old := "# >>> ws:dotfile-sync >>>\n*/30 * * * * /bin/ws cron run dotfile-sync\n# <<< ws:dotfile-sync <<<\n"
	retired := "# >>> ws:secret-scan >>>\n0 * * * * /bin/ws secret scan\n# <<< ws:secret-scan <<<\n"
	mega := BlockFor(Builtins["mega-sync"], "/ws", m.workspace, ":7")
	m.setLive(t, "MAILTO=me\n"+old+retired+mega)
	m.setDeclared(t, old+mega)
	script, _ := ScriptPath("dotfile-backup")
	oldScript, _ := ScriptPath("dotfile-sync")
	for _, p := range []string{script, oldScript} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	acts, notInstalled, err := RemoveActions([]string{"dotfile-backup", "secret-scan", "pass-backup", "dotfile-sync"}, m.opts())
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 2 || !slices.Equal(notInstalled, []string{"pass-backup"}) {
		t.Fatalf("actions=%d notInstalled=%v (a repeat under its old name counts once)", len(acts), notInstalled)
	}
	if !strings.Contains(acts[0].Description, "Remove dotfile-backup from the crontab and "+m.declared) {
		t.Fatalf("description: %q", acts[0].Description)
	}
	for _, a := range acts {
		if err := a.Execute(); err != nil {
			t.Fatal(err)
		}
	}
	live := m.liveText(t)
	if strings.Contains(live, "dotfile-sync") || strings.Contains(live, "secret-scan") || !strings.Contains(live, "ws:mega-sync") || !strings.Contains(live, "MAILTO=me") {
		t.Fatalf("live:\n%s", live)
	}
	if strings.Contains(m.declaredText(t), "dotfile-sync") || !strings.Contains(m.declaredText(t), "ws:mega-sync") {
		t.Fatalf("declared:\n%s", m.declaredText(t))
	}
	for _, p := range []string{script, oldScript} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("the legacy wrapper script %s must go", p)
		}
	}
}

// ── apply ──────────────────────────────────────────────────────────

func applyAll(t *testing.T, m machine) ApplyResult {
	t.Helper()
	acts, res, err := ApplyActions(m.opts())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range acts {
		if err := a.Execute(); err != nil {
			t.Fatalf("%s: %v", a.Description, err)
		}
	}
	return res
}

func TestApplyRegeneratesBlocksForThisMachine(t *testing.T) {
	m := newMachine(t)
	// Declared on another machine: other paths, an old name, and a plain line.
	m.setDeclared(t, "CRON_TZ=UTC\n"+
		"# >>> ws:dotfile-sync >>>\n*/30 * * * * /home/other/ws --workspace /home/other/Workspace cron run dotfile-sync\n# <<< ws:dotfile-sync <<<\n"+
		"# >>> ws:secret-scan >>>\n0 * * * * old\n# <<< ws:secret-scan <<<\n")
	m.setLive(t, "MAILTO=me\n# >>> ws:mega-sync >>>\n*/30 * * * * x\n# <<< ws:mega-sync <<<\n")

	res := applyAll(t, m)
	if !slices.Equal(res.Installed, []string{"dotfile-backup"}) || !slices.Equal(res.Removed, []string{"mega-sync"}) ||
		!slices.Equal(res.Skipped, []string{"secret-scan"}) || res.LinesAdded != 1 {
		t.Fatalf("result = %+v", res)
	}
	live := m.liveText(t)
	if !strings.Contains(live, m.opts().Bin+" --workspace "+m.workspace+" cron run --if-due dotfile-backup") {
		t.Fatalf("the block is regenerated for this machine, not copied:\n%s", live)
	}
	if strings.Contains(live, "/home/other") || strings.Contains(live, "dotfile-sync") || strings.Contains(live, "secret-scan") || strings.Contains(live, "ws:mega-sync") {
		t.Fatalf("other machines' paths, old and retired names and undeclared blocks must go:\n%s", live)
	}
	if !strings.Contains(live, "MAILTO=me") || !strings.Contains(live, "CRON_TZ=UTC") {
		t.Fatalf("live lines stay, and declared plain lines are added:\n%s", live)
	}
	if strings.Contains(m.declaredText(t), "ws:dotfile-backup") {
		t.Fatal("apply never changes configs/crontab")
	}
}

// TestApplyIsIdempotent: apply used to append the declared plain lines again on
// every run.
func TestApplyIsIdempotent(t *testing.T) {
	m := newMachine(t)
	m.setDeclared(t, "CRON_TZ=UTC\nMAILTO=me\n"+BlockFor(Builtins["mega-sync"], "/elsewhere/ws", "/elsewhere", ":1"))
	m.setLive(t, "MAILTO=me\n")
	applyAll(t, m)
	first := m.liveText(t)
	if strings.Count(first, "MAILTO=me") != 1 || strings.Count(first, "CRON_TZ=UTC") != 1 {
		t.Fatalf("each declared line is added once, and not when already live:\n%s", first)
	}
	res := applyAll(t, m)
	if m.liveText(t) != first {
		t.Fatalf("a second apply must change nothing:\n%s\n---\n%s", first, m.liveText(t))
	}
	if len(res.Installed) != 0 || len(res.Removed) != 0 || res.LinesAdded != 0 || !slices.Equal(res.Unchanged, []string{"mega-sync"}) {
		t.Fatalf("second apply result = %+v", res)
	}
	acts, _, _ := ApplyActions(m.opts())
	if len(acts) != 0 {
		t.Fatalf("nothing left to do: %d actions", len(acts))
	}
}

func TestApplyNeedsTheDeclaredFile(t *testing.T) {
	m := newMachine(t)
	_, _, err := ApplyActions(m.opts())
	if err == nil || !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "ws cron add <job> creates it") {
		t.Fatalf("got %v", err)
	}
}

// ── run ────────────────────────────────────────────────────────────

func logText(t *testing.T) string {
	t.Helper()
	p, _ := LogFilePath()
	b, _ := os.ReadFile(p)
	return string(b)
}

func TestRunDryRunRunsNothing(t *testing.T) {
	newMachine(t)
	res, err := RunJob("session-prune", RunOptions{WorkspacePath: "/W", DryRun: true})
	if err != nil || !strings.Contains(res.Command, "--workspace /W session prune --quiet") {
		t.Fatalf("%+v %v", res, err)
	}
	if logText(t) != "" {
		t.Fatal("--dry-run logs and records nothing")
	}
}

func TestRunRetiredAndPreset(t *testing.T) {
	newMachine(t)
	res, err := RunJob("secret-scan", RunOptions{WorkspacePath: "/W"})
	if err != nil || !res.Skipped || res.ExitCode != 0 {
		t.Fatalf("a retired job is skipped with success, so old crontab lines do not fail: %+v %v", res, err)
	}
	if !strings.Contains(logText(t), "[secret-scan] Skipping: secret-scan is no longer a ws job; run ws cron rm secret-scan") {
		t.Fatalf("log:\n%s", logText(t))
	}
	if _, err := RunJob("sync", RunOptions{}); err == nil || !strings.Contains(err.Error(), "preset") {
		t.Fatalf("a preset cannot be run: %v", err)
	}
	if _, err := RunJob("nope", RunOptions{}); err == nil {
		t.Fatal("an unknown job is an error")
	}
}

func TestIfDueSkipsAndPlainRunAlwaysRuns(t *testing.T) {
	m := newMachine(t)
	// Fake a job body that prints and exits 3, by replacing the job for the test.
	saved := Builtins["session-prune"].scriptBody
	t.Cleanup(func() { Builtins["session-prune"].scriptBody = saved })
	Builtins["session-prune"].scriptBody = func(_, _, _ string) string { return "echo out-line; echo err-line >&2; exit 3" }

	var out, errOut strings.Builder
	res, err := RunJob("session-prune", RunOptions{Stdout: &out, Stderr: &errOut})
	if err != nil || res.ExitCode != 3 || res.Skipped {
		t.Fatalf("first run: %+v %v", res, err)
	}
	// Output is captured into cron.log, line by line, with the job's name, and
	// also reaches the writers.
	log := logText(t)
	for _, want := range []string{"[session-prune] Starting", "[session-prune] out-line", "[session-prune] err-line", "[session-prune] Failed (exit 3)"} {
		if !strings.Contains(log, want) {
			t.Fatalf("log lacks %q:\n%s", want, log)
		}
	}
	if out.String() != "out-line\n" || errOut.String() != "err-line\n" {
		t.Fatalf("writers got out=%q err=%q", out.String(), errOut.String())
	}
	statePath, _ := StateFilePath()
	if r, _ := LastRun(statePath, "session-prune"); r.ExitCode != 3 || time.Since(r.Time) > time.Minute {
		t.Fatalf("state = %+v (the job's own code is recorded)", r)
	}

	// Just ran: --if-due skips, a plain run runs again.
	res, _ = RunJob("session-prune", RunOptions{IfDue: true})
	if !res.Skipped || res.ExitCode != 0 || !strings.Contains(logText(t), "Skipping: last run") {
		t.Fatalf("--if-due must skip a job that just ran: %+v", res)
	}
	res, _ = RunJob("session-prune", RunOptions{})
	if res.Skipped || res.ExitCode != 3 {
		t.Fatalf("a plain run always runs: %+v", res)
	}
	_ = m
}

func TestRunUnderAnOldNameIsSilentAndRecordsUnderTheNewOne(t *testing.T) {
	newMachine(t)
	saved := Builtins["dotfile-backup"].scriptBody
	t.Cleanup(func() { Builtins["dotfile-backup"].scriptBody = saved })
	Builtins["dotfile-backup"].scriptBody = func(_, _, _ string) string { return "echo pushed" }

	res, err := RunJob("dotfile-sync", RunOptions{})
	if err != nil || res.Job != "dotfile-backup" || res.ExitCode != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	statePath, _ := StateFilePath()
	records, _ := ReadState(statePath)
	if len(records) != 1 || records[0].Job != "dotfile-backup" {
		t.Fatalf("new records use the new name: %+v", records)
	}
}

func TestLogIsTrimmedWhenOverTheLimit(t *testing.T) {
	m := newMachine(t)
	dir := filepath.Join(m.state, "ws-tool")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := "2026-10-22T00:00:00Z [mega-sync] " + strings.Repeat("x", 100) + "\n"
	big := strings.Repeat(line, (6<<20)/len(line))
	logPath := filepath.Join(dir, "cron.log")
	if err := os.WriteFile(logPath, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	trimLog(logPath)
	fi, _ := os.Stat(logPath)
	if fi.Size() > 5<<19 || fi.Size() < (5<<19)-int64(len(line)) {
		t.Fatalf("size after trim = %d, want about 2.5 MB", fi.Size())
	}
	b, _ := os.ReadFile(logPath)
	if !strings.HasPrefix(string(b), "2026-10-22T00:00:00Z [mega-sync] ") {
		t.Fatal("the trimmed log starts at a line boundary")
	}
	// A log under the limit is left alone.
	small := "a [x] y\n"
	if err := os.WriteFile(logPath, []byte(small), 0o644); err != nil {
		t.Fatal(err)
	}
	trimLog(logPath)
	if b, _ := os.ReadFile(logPath); string(b) != small {
		t.Fatal("a small log must not be touched")
	}
}
