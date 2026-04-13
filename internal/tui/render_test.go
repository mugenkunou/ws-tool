package tui

import (
	"bytes"
	"testing"
	"time"

	"github.com/mugenkunou/ws-tool/internal/notify"
)

func TestRenderDashboardCompactFallback(t *testing.T) {
	var buf bytes.Buffer
	d := DashboardData{Workspace: "/tmp/test", LoadedAt: time.Now()}
	// Tiny terminal triggers compact mode.
	RenderDashboard(&buf, d, TermSize{Rows: 10, Cols: 40}, true)
	out := buf.String()
	if len(out) == 0 {
		t.Fatal("expected compact output, got empty")
	}
	if !bytes.Contains(buf.Bytes(), []byte("too small")) {
		t.Fatalf("expected 'too small' message in compact output, got: %s", out)
	}
}

func TestRenderDashboardFullNoViolations(t *testing.T) {
	var buf bytes.Buffer
	d := DashboardData{Workspace: "/home/user/Workspace", LoadedAt: time.Now()}
	RenderDashboard(&buf, d, TermSize{Rows: 40, Cols: 100}, true)
	out := buf.String()
	if len(out) == 0 {
		t.Fatal("expected dashboard output, got empty")
	}
	if !bytes.Contains(buf.Bytes(), []byte("No violations")) {
		t.Fatalf("expected 'No violations' in output, got: %s", out)
	}
}

func TestRenderDashboardWithViolations(t *testing.T) {
	var buf bytes.Buffer
	d := DashboardData{
		Workspace: "/home/user/Workspace",
		LoadedAt:  time.Now(),
		Violations: []notify.HealthViolation{
			{Group: "ignore", Type: "bloat", Severity: "WARNING", Path: "big-file.zip", SizeMB: 100},
		},
		IgnoreWarning: 1,
	}
	RenderDashboard(&buf, d, TermSize{Rows: 40, Cols: 100}, true)
	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("big-file.zip")) {
		t.Fatalf("expected violation path in output, got: %s", out)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		sec  int64
		want string
	}{
		{30, "30s"},
		{90, "1m"},
		{3661, "1h 1m"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.sec)
		if got != tt.want {
			t.Errorf("formatDuration(%d) = %q, want %q", tt.sec, got, tt.want)
		}
	}
}
