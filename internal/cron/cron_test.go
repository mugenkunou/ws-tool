package cron

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── removeBlock ───────────────────────────────────────────────────────────────

func TestRemoveBlock_removesBlock(t *testing.T) {
	ct := "CRON_TZ=UTC\n" +
		CronMarkerStart("mega-sync") + "\n" +
		"SHELL=/usr/bin/bash\n" +
		"*/30 * * * * /path/mega-sync.sh\n" +
		"@reboot /path/mega-sync.sh\n" +
		CronMarkerEnd("mega-sync") + "\n" +
		"# some other job\n"

	result := removeBlock(ct, "mega-sync")

	if strings.Contains(result, "mega-sync.sh") {
		t.Errorf("result still contains mega-sync.sh:\n%s", result)
	}
	if strings.Contains(result, CronMarkerStart("mega-sync")) {
		t.Errorf("result still contains start marker")
	}
	if !strings.Contains(result, "CRON_TZ=UTC") {
		t.Errorf("result lost unrelated lines:\n%s", result)
	}
	if !strings.Contains(result, "# some other job") {
		t.Errorf("result lost trailing lines:\n%s", result)
	}
}

func TestRemoveBlock_noOp_whenAbsent(t *testing.T) {
	ct := "*/5 * * * * /other/job.sh\n"
	result := removeBlock(ct, "mega-sync")
	if result != ct {
		t.Errorf("expected no change, got:\n%s", result)
	}
}

func TestRemoveBlock_idempotent(t *testing.T) {
	ct := CronMarkerStart("log-prune") + "\n" +
		"0 2 * * * /path/log-prune.sh\n" +
		CronMarkerEnd("log-prune") + "\n"

	once := removeBlock(ct, "log-prune")
	twice := removeBlock(once, "log-prune")
	if once != twice {
		t.Errorf("second removeBlock changed output:\nbefore=%q\nafter=%q", once, twice)
	}
}

// ── CronMarkers ──────────────────────────────────────────────────────────────

func TestCronMarkers(t *testing.T) {
	name := "dotfile-sync"
	start := CronMarkerStart(name)
	end := CronMarkerEnd(name)

	if !strings.Contains(start, name) {
		t.Errorf("start marker does not contain job name: %s", start)
	}
	if !strings.Contains(end, name) {
		t.Errorf("end marker does not contain job name: %s", end)
	}
	if start == end {
		t.Error("start and end markers are identical")
	}
}

// ── GenerateScript ────────────────────────────────────────────────────────────

func TestGenerateScript_containsJobName(t *testing.T) {
	job := megaSyncJob
	script := GenerateScript(job, "/usr/local/bin/ws", ":1", "/home/user/Workspace",
		"/home/user/.local/share/ws/cron.state",
		"/home/user/.local/share/ws/cron.log",
	)

	checks := []string{
		"#!/usr/bin/env bash",
		"mega-sync",
		"WS_INTERVAL_SECS=1800",
		"WS_STATE=",
		"WS_LOG=",
		"_ws_log",
		"Log rotated",
		"missed-run recovery",
		"@reboot",
		"NOW_TS=",
		"exit $EXIT_CODE",
		"DISPLAY=:1",
	}
	for _, want := range checks {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q", want)
		}
	}
}

func TestGenerateScript_wsBinEmbedded(t *testing.T) {
	for _, job := range []*BuiltinJob{dotfileSyncJob, repoSyncJob, secretScanJob, ignoreScanJob, logPruneJob, scratchPruneJob} {
		script := GenerateScript(job, "/usr/local/bin/ws", "", "/home/user/Workspace",
			"/tmp/cron.state", "/tmp/cron.log")
		if !strings.Contains(script, "/usr/local/bin/ws") {
			t.Errorf("job %s: script does not embed ws bin path", job.Name)
		}
		if !strings.Contains(script, "/home/user/Workspace") {
			t.Errorf("job %s: script does not embed workspace path", job.Name)
		}
	}
}

func TestGenerateScript_megaSyncDefaultDisplay(t *testing.T) {
	// When display is empty, the script should fall back to :1.
	script := GenerateScript(megaSyncJob, "/usr/bin/ws", "", "/tmp/ws",
		"/tmp/cron.state", "/tmp/cron.log")
	if !strings.Contains(script, "DISPLAY=:1") {
		t.Error("mega-sync script should default DISPLAY to :1 when none provided")
	}
}

// ── Resolve ───────────────────────────────────────────────────────────────────

func TestResolve_singleJob(t *testing.T) {
	jobs, err := Resolve("mega-sync")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Name != "mega-sync" {
		t.Errorf("unexpected job name: %s", jobs[0].Name)
	}
}

func TestResolve_preset(t *testing.T) {
	jobs, err := Resolve("maintenance")
	if err != nil {
		t.Fatal(err)
	}
	expected := Presets["maintenance"]
	if len(jobs) != len(expected) {
		t.Fatalf("expected %d jobs, got %d", len(expected), len(jobs))
	}
	for i, j := range jobs {
		if j.Name != expected[i] {
			t.Errorf("job[%d]: want %s, got %s", i, expected[i], j.Name)
		}
	}
}

func TestResolve_unknown(t *testing.T) {
	_, err := Resolve("nonexistent-job")
	if err == nil {
		t.Error("expected error for unknown job name")
	}
}

func TestAllNames_containsAllJobsAndPresets(t *testing.T) {
	names := AllNames()
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}
	for k := range Builtins {
		if !nameSet[k] {
			t.Errorf("AllNames missing builtin job %q", k)
		}
	}
	for k := range Presets {
		if !nameSet[k] {
			t.Errorf("AllNames missing preset %q", k)
		}
	}
}

// ── ReadState / LastRun ────────────────────────────────────────────────────────

func TestReadState_empty(t *testing.T) {
	dir := t.TempDir()
	records, err := ReadState(filepath.Join(dir, "cron.state"))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

func TestReadState_parsesRecords(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "cron.state")

	content := "mega-sync 2026-05-08T10:00:00Z 0\n" +
		"log-prune 2026-05-08T02:00:00Z 1\n" +
		"mega-sync 2026-05-08T10:30:00Z 0\n"
	if err := os.WriteFile(stateFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	records, err := ReadState(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	if records[0].Job != "mega-sync" {
		t.Errorf("record[0].Job = %s", records[0].Job)
	}
	if records[1].ExitCode != 1 {
		t.Errorf("record[1].ExitCode = %d", records[1].ExitCode)
	}
}

func TestLastRun_returnsLatest(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "cron.state")

	content := "mega-sync 2026-05-08T10:00:00Z 0\n" +
		"mega-sync 2026-05-08T10:30:00Z 0\n"
	if err := os.WriteFile(stateFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := LastRun(stateFile, "mega-sync")
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := time.Parse(time.RFC3339, "2026-05-08T10:30:00Z")
	if !r.Time.Equal(expected) {
		t.Errorf("LastRun time = %v, want %v", r.Time, expected)
	}
}

func TestLastRun_zerValueWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	r, err := LastRun(filepath.Join(dir, "cron.state"), "mega-sync")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Time.IsZero() {
		t.Errorf("expected zero time when no records, got %v", r.Time)
	}
}

// ── ReadLog ───────────────────────────────────────────────────────────────────

func TestReadLog_filtersByJob(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "cron.log")

	content := "2026-05-08T10:00:00Z [mega-sync] Starting\n" +
		"2026-05-08T10:00:01Z [log-prune] Starting\n" +
		"2026-05-08T10:05:01Z [mega-sync] Done (exit 0)\n"
	if err := os.WriteFile(logFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	lines, err := ReadLog(logFile, "mega-sync", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	for _, l := range lines {
		if !strings.Contains(l, "[mega-sync]") {
			t.Errorf("unexpected line without mega-sync prefix: %q", l)
		}
	}
}

func TestReadLog_limitsLines(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "cron.log")

	var sb strings.Builder
	for i := 0; i < 10; i++ {
		sb.WriteString("2026-05-08T10:00:00Z [mega-sync] entry\n")
	}
	if err := os.WriteFile(logFile, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	lines, err := ReadLog(logFile, "mega-sync", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Errorf("expected 3 lines with n=3, got %d", len(lines))
	}
}

func TestReadLog_emptyWhenFileAbsent(t *testing.T) {
	dir := t.TempDir()
	lines, err := ReadLog(filepath.Join(dir, "cron.log"), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(lines))
	}
}

// ── BuiltinJob fields ─────────────────────────────────────────────────────────

func TestAllBuiltins_haveNonZeroIntervals(t *testing.T) {
	for name, job := range Builtins {
		if job.IntervalSecs <= 0 {
			t.Errorf("job %q has zero IntervalSecs", name)
		}
		if job.Schedule == "" {
			t.Errorf("job %q has empty Schedule", name)
		}
		if job.Description == "" {
			t.Errorf("job %q has empty Description", name)
		}
	}
}

func TestAllPresets_referenceKnownJobs(t *testing.T) {
	for preset, jobs := range Presets {
		for _, name := range jobs {
			if _, ok := Builtins[name]; !ok {
				t.Errorf("preset %q references unknown job %q", preset, name)
			}
		}
	}
}
