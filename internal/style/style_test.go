package style

import (
	"bytes"
	"strings"
	"testing"
)

func TestPaintWithColor(t *testing.T) {
	result := Paint(FgGreen, "hello", false)
	if !strings.Contains(result, "hello") {
		t.Fatal("should contain text")
	}
	if !strings.HasPrefix(result, esc) {
		t.Fatal("should start with ANSI escape")
	}
	if !strings.HasSuffix(result, Reset) {
		t.Fatal("should end with reset")
	}
}

func TestPaintNoColor(t *testing.T) {
	result := Paint(FgGreen, "hello", true)
	if result != "hello" {
		t.Fatalf("expected plain text, got %q", result)
	}
}

func TestBadge(t *testing.T) {
	tests := []struct {
		label    string
		noColor  bool
		contains string
	}{
		{"ok", false, "OK"},
		{"ok", true, "OK"},
		{"critical", false, "CRITICAL"},
		{"warning", true, "WARNING"},
		{"broken", false, "BROKEN"},
		{"synced", false, "SYNCED"},
		{"custom", false, "CUSTOM"},
	}
	for _, tt := range tests {
		result := Badge(tt.label, tt.noColor)
		if !strings.Contains(result, tt.contains) {
			t.Errorf("Badge(%q, %v) = %q, want containing %q", tt.label, tt.noColor, result, tt.contains)
		}
	}
}

func TestCounts(t *testing.T) {
	result := Counts(2, 1, true)
	if !strings.Contains(result, "2 critical") {
		t.Fatalf("expected '2 critical', got %q", result)
	}
	if !strings.Contains(result, "1 warning") {
		t.Fatalf("expected '1 warning', got %q", result)
	}
	if !strings.Contains(result, "·") {
		t.Fatalf("expected separator, got %q", result)
	}
}

func TestCountsColored(t *testing.T) {
	result := Counts(0, 3, false)
	if !strings.Contains(result, "0 critical") {
		t.Fatalf("expected '0 critical', got %q", result)
	}
	if !strings.Contains(result, "3 warning") {
		t.Fatalf("expected '3 warning', got %q", result)
	}
}

func TestDivider(t *testing.T) {
	plain := Divider(true)
	if len(plain) == 0 {
		t.Fatal("divider should not be empty")
	}
	if !strings.Contains(plain, "─") {
		t.Fatal("divider should contain box-drawing char")
	}
}

func TestHeader(t *testing.T) {
	var buf bytes.Buffer
	Header(&buf, "Test Section", true)
	out := buf.String()
	if !strings.Contains(out, "Test Section") {
		t.Fatalf("header should contain title, got %q", out)
	}
	if !strings.Contains(out, "─") {
		t.Fatalf("header should contain divider, got %q", out)
	}
}

func TestKV(t *testing.T) {
	var buf bytes.Buffer
	KV(&buf, "Key", "Value", true)
	out := buf.String()
	if !strings.Contains(out, "Key") || !strings.Contains(out, "Value") {
		t.Fatalf("KV output should contain key and value, got %q", out)
	}
}

func TestIconCheck(t *testing.T) {
	if IconCheck(false) != "✔" {
		t.Fatalf("expected ✔, got %q", IconCheck(false))
	}
	if IconCheck(true) != "[ok]" {
		t.Fatalf("expected [ok], got %q", IconCheck(true))
	}
}

func TestResultSuccess(t *testing.T) {
	result := ResultSuccess(true, "Moved %s", "file.txt")
	if !strings.Contains(result, "[ok]") {
		t.Fatalf("expected [ok] icon, got %q", result)
	}
	if !strings.Contains(result, "Moved file.txt") {
		t.Fatalf("expected message, got %q", result)
	}
}

func TestResultError(t *testing.T) {
	result := ResultError(true, "Failed %s", "file.txt")
	if !strings.Contains(result, "[err]") {
		t.Fatalf("expected [err] icon, got %q", result)
	}
}

func TestRenderTable(t *testing.T) {
	var buf bytes.Buffer
	rows := []TableRow{
		{Columns: []string{"A", "BB", "CCC"}},
		{Columns: []string{"DD", "E", "F"}},
	}
	RenderTable(&buf, rows, []int{6, 6, 6})
	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
}

func TestSuccessf(t *testing.T) {
	result := Successf(true, "done %d", 42)
	if result != "done 42" {
		t.Fatalf("expected plain 'done 42', got %q", result)
	}
	colored := Successf(false, "done %d", 42)
	if !strings.Contains(colored, "done 42") {
		t.Fatalf("colored should contain text, got %q", colored)
	}
	if !strings.Contains(colored, esc) {
		t.Fatal("colored should contain escape sequence")
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		name string
		in   int64
		out  string
	}{
		{name: "bytes", in: 999, out: "999 B"},
		{name: "kb", in: 1024, out: "1.0 KB"},
		{name: "mb", in: 2 * 1024 * 1024, out: "2.0 MB"},
		{name: "gb", in: 3 * 1024 * 1024 * 1024, out: "3.0 GB"},
		{name: "negative", in: -1024, out: "-1.0 KB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HumanBytes(tt.in); got != tt.out {
				t.Fatalf("HumanBytes(%d) = %q, want %q", tt.in, got, tt.out)
			}
		})
	}
}
