package ignore

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEvaluateSafeHarborWithoutExclude is the two-view contract: a path
// inside a safe-harbor directory is suppressed even when no exclude rule
// ever matched it. The harbor dir keeps syncing everything in it by
// design; its bloat/depth warnings must land on the suppressed page, not
// the live one.
func TestEvaluateSafeHarborWithoutExclude(t *testing.T) {
	engine := BuildEngine(DefaultUserRules())

	res := engine.Evaluate("artifacts/plain.pdf", false)
	if !res.Included {
		t.Fatalf("expected artifacts/plain.pdf included, rule=%s", res.Rule)
	}
	if !res.SafeHarbor {
		t.Fatal("expected SafeHarbor=true for a harbor path with no matching exclude rule")
	}
	if res.OverriddenBy != "+g:artifacts/**" {
		t.Fatalf("expected OverriddenBy=+g:artifacts/**, got %s", res.OverriddenBy)
	}

	// A path outside every harbor is not suppressed just because it syncs.
	res = engine.Evaluate("notes/plain.md", false)
	if !res.Included {
		t.Fatal("expected notes/plain.md included")
	}
	if res.SafeHarbor {
		t.Fatal("expected SafeHarbor=false for a path outside all harbors")
	}

	// A path the excludes still own is not suppressed.
	res = engine.Evaluate("experiments/big.log", false)
	if res.Included {
		t.Fatal("expected experiments/big.log excluded")
	}
	if res.SafeHarbor {
		t.Fatal("expected SafeHarbor=false for an excluded path")
	}
}

// TestScanSplitsLiveAndSuppressed checks the two views end to end: scan
// puts harbor violations on the suppressed page (INFO) and leaves live
// ones actionable.
func TestScanSplitsLiveAndSuppressed(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "Workspace")
	if err := os.MkdirAll(filepath.Join(ws, "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}

	big := make([]byte, 2*1024*1024)
	if err := os.WriteFile(filepath.Join(ws, "artifacts", "big.pdf"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "notes", "big.pdf"), big, 0o644); err != nil {
		t.Fatal(err)
	}

	violations, err := Scan(ScanOptions{
		WorkspacePath: ws,
		WarnSizeMB:    1,
		CritSizeMB:    10,
		MaxDepth:      6,
		Engine:        BuildEngine(DefaultUserRules()),
	})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	var live, supp int
	for _, v := range violations {
		if v.Path != "artifacts/big.pdf" && v.Path != "notes/big.pdf" {
			continue
		}
		if v.Muted {
			supp++
			if v.Severity != "INFO" {
				t.Fatalf("harbor violation should be INFO, got %s", v.Severity)
			}
		} else {
			live++
		}
	}
	if supp != 1 {
		t.Fatalf("expected 1 suppressed violation (artifacts/big.pdf), got %d", supp)
	}
	if live != 1 {
		t.Fatalf("expected 1 live violation (notes/big.pdf), got %d", live)
	}
}
