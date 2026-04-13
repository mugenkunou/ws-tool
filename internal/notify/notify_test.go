package notify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStartStatusStopAndTest(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "Workspace")
	if err := os.MkdirAll(filepath.Join(ws, "ws"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	st, err := Start(ws, 12345)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if !st.Active {
		t.Fatal("expected active state after start")
	}
	if st.PID != 12345 {
		t.Fatalf("expected PID 12345, got %d", st.PID)
	}

	status, err := Status(ws)
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if !status.Active {
		t.Fatal("expected running status")
	}

	tested, err := Test(ws)
	if err != nil {
		t.Fatalf("test failed: %v", err)
	}
	if tested.LastAlert == "" {
		t.Fatal("expected last alert to be set")
	}

	stopped, err := Stop(ws)
	if err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	if stopped.Active {
		t.Fatal("expected inactive state after stop")
	}
	if stopped.PID != 0 {
		t.Fatalf("expected PID 0 after stop, got %d", stopped.PID)
	}
}

func TestHealthRoundTrip(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "Workspace")
	if err := os.MkdirAll(filepath.Join(ws, "ws"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	h := HealthSummary{
		Trigger:         "test",
		ViolationsCount: 1,
		Summary: HealthSubsystems{
			Ignore: HealthCounts{Critical: 0, Warning: 1},
		},
		Violations: []HealthViolation{
			{Group: "ignore", Type: "bloat", Severity: "WARNING", Path: "big.csv", SizeMB: 50},
		},
	}
	if err := WriteHealth(ws, h); err != nil {
		t.Fatalf("write health failed: %v", err)
	}

	got, err := ReadHealth(ws)
	if err != nil {
		t.Fatalf("read health failed: %v", err)
	}
	if got.ViolationsCount != 1 {
		t.Fatalf("expected 1 violation, got %d", got.ViolationsCount)
	}
	if got.Violations[0].Path != "big.csv" {
		t.Fatalf("unexpected violation path: %s", got.Violations[0].Path)
	}
}

func TestHealthMissingFile(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "Workspace")
	h, err := ReadHealth(ws)
	if err != nil {
		t.Fatalf("expected nil error for missing health file, got: %v", err)
	}
	if h.ViolationsCount != 0 {
		t.Fatalf("expected 0 violations for missing file, got %d", h.ViolationsCount)
	}
}

func TestDiffViolations(t *testing.T) {
	known := []ViolationKey{
		{Group: "ignore", Type: "bloat", Path: "old.csv"},
	}
	current := []HealthViolation{
		{Group: "ignore", Type: "bloat", Path: "old.csv"},
		{Group: "secret", Type: "secret", Path: "new-leak.sh"},
	}
	newOnes := DiffViolations(known, current)
	if len(newOnes) != 1 {
		t.Fatalf("expected 1 new violation, got %d", len(newOnes))
	}
	if newOnes[0].Path != "new-leak.sh" {
		t.Fatalf("unexpected new violation path: %s", newOnes[0].Path)
	}
}

func TestDiffViolationsAllKnown(t *testing.T) {
	known := []ViolationKey{
		{Group: "ignore", Type: "bloat", Path: "old.csv"},
	}
	current := []HealthViolation{
		{Group: "ignore", Type: "bloat", Path: "old.csv"},
	}
	newOnes := DiffViolations(known, current)
	if len(newOnes) != 0 {
		t.Fatalf("expected 0 new violations, got %d", len(newOnes))
	}
}

func TestFilterByEvents(t *testing.T) {
	violations := []HealthViolation{
		{Group: "ignore", Type: "bloat", Path: "a"},
		{Group: "secret", Type: "secret", Path: "b"},
		{Group: "dotfile", Type: "BROKEN", Path: "c"},
		{Group: "trash", Type: "machine-setup", Path: "d"},
	}

	// Only bloat and dotfile events.
	filtered := FilterByEvents(violations, []string{"bloat", "dotfile"})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered, got %d", len(filtered))
	}
	if filtered[0].Path != "a" || filtered[1].Path != "c" {
		t.Fatalf("unexpected filtered paths: %s, %s", filtered[0].Path, filtered[1].Path)
	}
}

func TestFilterByEventsEmpty(t *testing.T) {
	violations := []HealthViolation{
		{Group: "ignore", Type: "bloat", Path: "a"},
	}
	// Empty events = pass all.
	filtered := FilterByEvents(violations, nil)
	if len(filtered) != 1 {
		t.Fatalf("expected 1, got %d", len(filtered))
	}
}

func TestViolationKeys(t *testing.T) {
	violations := []HealthViolation{
		{Group: "ignore", Type: "bloat", Path: "a"},
		{Group: "secret", Type: "secret", Path: "b"},
	}
	keys := ViolationKeys(violations)
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	if keys[0].Group != "ignore" || keys[1].Group != "secret" {
		t.Fatal("unexpected key groups")
	}
}

func TestGenerateUnit(t *testing.T) {
	unit := GenerateUnit("/usr/local/bin/ws", "/home/test/Workspace", "/home/test/Workspace/ws/config.json")
	if unit == "" {
		t.Fatal("expected non-empty unit content")
	}
	if !contains(unit, "ExecStart=/usr/local/bin/ws notify daemon") {
		t.Fatal("expected ExecStart in unit")
	}
	if !contains(unit, "--workspace /home/test/Workspace") {
		t.Fatal("expected workspace flag in unit")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestFormatNotification(t *testing.T) {
	tests := []struct {
		v    HealthViolation
		want string
	}{
		{
			HealthViolation{Group: "dotfile", Type: "BROKEN", Path: "~/.bashrc"},
			"⚠ ~/.bashrc symlink is broken",
		},
		{
			HealthViolation{Group: "secret", Type: "secret", Path: "leak.sh"},
			"🔒 new secret pattern found in leak.sh",
		},
		{
			HealthViolation{Group: "ignore", Type: "bloat", Path: "big.csv", SizeMB: 100},
			"📁 new 100 MB file detected — big.csv",
		},
	}
	for _, tt := range tests {
		got := formatNotification(tt.v)
		if got != tt.want {
			t.Errorf("formatNotification(%+v) = %q, want %q", tt.v, got, tt.want)
		}
	}
}
