// Package tuitest provides snapshot (golden file) and layout assertions for
// TUI views. Snapshots are stored with ANSI styling stripped so they capture
// layout and text; regenerate them with:
//
//	UPDATE_SNAPSHOTS=1 go test ./internal/tui/...
//
// (An env var rather than a flag, so `go test ./...` works across packages.)
package tuitest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func updating() bool { return os.Getenv("UPDATE_SNAPSHOTS") == "1" }

// Golden compares view (ANSI stripped) with testdata/<name>.golden.
func Golden(t *testing.T, name, view string) {
	t.Helper()
	got := Plain(view)
	path := filepath.Join("testdata", name+".golden")
	if updating() {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing snapshot %s (run with UPDATE_SNAPSHOTS=1): %v", path, err)
	}
	if got != string(want) {
		t.Errorf("snapshot %s mismatch (run with UPDATE_SNAPSHOTS=1 to accept)\n--- want\n%s\n--- got\n%s", path, want, got)
	}
}

// Plain strips ANSI sequences and trailing spaces from each line.
func Plain(view string) string {
	lines := strings.Split(ansi.Strip(view), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n") + "\n"
}

// AssertFits fails if view has more than height lines or any line wider than
// width cells.
func AssertFits(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		t.Errorf("view is %d lines tall, terminal is %d", len(lines), height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > width {
			t.Errorf("line %d is %d cells wide, terminal is %d: %q", i+1, w, width, ansi.Strip(l))
		}
	}
}
