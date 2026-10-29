package secret

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetFileContext(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "test.txt")
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := GetFileContext(file, 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx) != 5 {
		t.Fatalf("expected 5 context lines, got %d", len(ctx))
	}
	if ctx[0].Number != 2 || ctx[4].Number != 6 {
		t.Fatalf("unexpected line range: %d-%d", ctx[0].Number, ctx[len(ctx)-1].Number)
	}
	if !ctx[2].IsMatch {
		t.Fatal("expected line 4 to be marked as match")
	}
	if ctx[0].IsMatch {
		t.Fatal("expected line 2 to not be marked as match")
	}
}

func TestGetFileContextAtStart(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "test.txt")
	content := "first\nsecond\nthird\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := GetFileContext(file, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if ctx[0].Number != 1 {
		t.Fatalf("expected start at line 1, got %d", ctx[0].Number)
	}
	if !ctx[0].IsMatch {
		t.Fatal("expected line 1 to be the match")
	}
}

func TestPruneAllowlist(t *testing.T) {
	violations := []Violation{
		{Path: "app.env", Line: 2},
		{Path: "config.yml", Line: 5},
	}
	allowlist := []string{
		"app.env:2",     // still valid
		"config.yml:5",  // still valid
		"old.txt:10",    // stale — file/line no longer matches
		"deleted.env:1", // stale
	}

	kept, pruned := PruneAllowlist(allowlist, violations)
	if len(kept) != 2 {
		t.Fatalf("expected 2 kept, got %d: %v", len(kept), kept)
	}
	if len(pruned) != 2 {
		t.Fatalf("expected 2 pruned, got %d: %v", len(pruned), pruned)
	}
	if kept[0] != "app.env:2" || kept[1] != "config.yml:5" {
		t.Fatalf("unexpected kept entries: %v", kept)
	}
	if pruned[0] != "old.txt:10" || pruned[1] != "deleted.env:1" {
		t.Fatalf("unexpected pruned entries: %v", pruned)
	}
}

func TestPruneAllowlistAllStale(t *testing.T) {
	violations := []Violation{}
	allowlist := []string{"gone.txt:1", "removed.env:5"}

	kept, pruned := PruneAllowlist(allowlist, violations)
	if len(kept) != 0 {
		t.Fatalf("expected 0 kept, got %d", len(kept))
	}
	if len(pruned) != 2 {
		t.Fatalf("expected 2 pruned, got %d", len(pruned))
	}
}

func TestPruneAllowlistEmpty(t *testing.T) {
	kept, pruned := PruneAllowlist(nil, nil)
	if len(kept) != 0 || len(pruned) != 0 {
		t.Fatalf("expected empty results, got kept=%v pruned=%v", kept, pruned)
	}
}
