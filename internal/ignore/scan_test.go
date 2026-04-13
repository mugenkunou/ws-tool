package ignore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEngineEvaluateLastMatchWins(t *testing.T) {
	tmp := t.TempDir()
	megaignorePath := filepath.Join(tmp, ".megaignore")
	content := "-:.*\n+:ws\n+g:ws/**\n-s:*\n"
	if err := os.WriteFile(megaignorePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write megaignore failed: %v", err)
	}

	engine, err := LoadEngine(megaignorePath)
	if err != nil {
		t.Fatalf("load engine failed: %v", err)
	}

	res := engine.Evaluate("ws/ws-log/session/stdout.log", false)
	if !res.Included {
		t.Fatalf("expected ws safe harbor to be included, got excluded by %s", res.Rule)
	}

	res = engine.Evaluate(".git/config", false)
	if res.Included {
		t.Fatalf("expected hidden file to be excluded, rule=%s", res.Rule)
	}
}

func TestEvaluateSafeHarborFlag(t *testing.T) {
	tmp := t.TempDir()
	megaignorePath := filepath.Join(tmp, ".megaignore")
	content := "-:.*\n-g:*.log\n+:ws\n+g:ws/**\n-s:*\n"
	if err := os.WriteFile(megaignorePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write megaignore failed: %v", err)
	}

	engine, err := LoadEngine(megaignorePath)
	if err != nil {
		t.Fatalf("load engine failed: %v", err)
	}

	// ws/ path excluded by -g:*.log but re-included by +g:ws/** — safe harbor
	res := engine.Evaluate("ws/ws-log/session.log", false)
	if !res.Included {
		t.Fatal("expected ws safe harbor path to be included")
	}
	if !res.SafeHarbor {
		t.Fatal("expected SafeHarbor=true for ws/ws-log/session.log")
	}

	// Normal file not excluded then re-included — not a safe harbor
	res = engine.Evaluate("README.md", false)
	if !res.Included {
		t.Fatal("expected README.md to be included")
	}
	if res.SafeHarbor {
		t.Fatal("expected SafeHarbor=false for README.md (never excluded)")
	}

	// Excluded file — not a safe harbor
	res = engine.Evaluate(".git/config", false)
	if res.Included {
		t.Fatal("expected .git/config to be excluded")
	}
	if res.SafeHarbor {
		t.Fatal("expected SafeHarbor=false for excluded path")
	}
}

func TestScanDetectsBloatDepthAndProjectMeta(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "Workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(ws, ".megaignore"), []byte("-s:*\n"), 0o644); err != nil {
		t.Fatalf("write megaignore failed: %v", err)
	}
	engine, err := LoadEngine(filepath.Join(ws, ".megaignore"))
	if err != nil {
		t.Fatalf("load engine failed: %v", err)
	}

	deepPath := filepath.Join(ws, "a", "b", "c", "d", "e", "f", "file.txt")
	if err := os.MkdirAll(filepath.Dir(deepPath), 0o755); err != nil {
		t.Fatalf("mkdir deep failed: %v", err)
	}
	if err := os.WriteFile(deepPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write deep failed: %v", err)
	}

	bigPath := filepath.Join(ws, "big.bin")
	big := make([]byte, 2*1024*1024)
	if err := os.WriteFile(bigPath, big, 0o644); err != nil {
		t.Fatalf("write big failed: %v", err)
	}

	goProj := filepath.Join(ws, "proj")
	if err := os.MkdirAll(filepath.Join(goProj, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir project failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goProj, "go.mod"), []byte("module example.com/proj\n"), 0o644); err != nil {
		t.Fatalf("write go.mod failed: %v", err)
	}

	violations, err := Scan(ScanOptions{
		WorkspacePath: ws,
		WarnSizeMB:    1,
		CritSizeMB:    10,
		MaxDepth:      3,
		Engine:        engine,
	})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	hasBloat, hasDepth, hasProjectMeta := false, false, false
	for _, v := range violations {
		switch v.Type {
		case "bloat":
			hasBloat = true
		case "depth":
			hasDepth = true
		case "project-meta":
			hasProjectMeta = true
		}
	}

	if !hasBloat || !hasDepth || !hasProjectMeta {
		t.Fatalf("expected all violation types, got %+v", violations)
	}
}

func TestScanSkipsBrokenSymlinksAndMissingDirs(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "Workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(ws, ".megaignore"), []byte("-s:*\n"), 0o644); err != nil {
		t.Fatalf("write megaignore failed: %v", err)
	}
	engine, err := LoadEngine(filepath.Join(ws, ".megaignore"))
	if err != nil {
		t.Fatalf("load engine failed: %v", err)
	}

	// Create a broken symlink (target does not exist).
	if err := os.Symlink(filepath.Join(ws, "nonexistent"), filepath.Join(ws, "broken-link")); err != nil {
		t.Fatalf("symlink failed: %v", err)
	}

	// Create a symlink to a missing directory.
	if err := os.Symlink(filepath.Join(ws, "no-such-dir"), filepath.Join(ws, "broken-dir-link")); err != nil {
		t.Fatalf("symlink failed: %v", err)
	}

	// Create a normal file to prove the scan still works.
	if err := os.WriteFile(filepath.Join(ws, "ok.txt"), []byte("fine"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	violations, err := Scan(ScanOptions{
		WorkspacePath: ws,
		WarnSizeMB:    100,
		CritSizeMB:    200,
		MaxDepth:      10,
		Engine:        engine,
	})
	if err != nil {
		t.Fatalf("scan must not fail on broken symlinks, got: %v", err)
	}
	_ = violations // no assertion on content — just that it didn't crash
}

func TestScanSafeHarborDowngradesToInfo(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "Workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// Template: exclude hidden files and large files, but safe-harbor ws/
	megaignore := "-:.*\n-g:*.bin\n+:ws\n+g:ws/**\n-s:*\n"
	if err := os.WriteFile(filepath.Join(ws, ".megaignore"), []byte(megaignore), 0o644); err != nil {
		t.Fatalf("write megaignore failed: %v", err)
	}
	engine, err := LoadEngine(filepath.Join(ws, ".megaignore"))
	if err != nil {
		t.Fatalf("load engine failed: %v", err)
	}

	// Big file inside safe harbor — should be INFO
	harborDir := filepath.Join(ws, "ws", "ws-log")
	if err := os.MkdirAll(harborDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	big := make([]byte, 2*1024*1024)
	if err := os.WriteFile(filepath.Join(harborDir, "session.bin"), big, 0o644); err != nil {
		t.Fatalf("write big failed: %v", err)
	}

	// Big file outside safe harbor — should be WARNING/CRITICAL
	if err := os.WriteFile(filepath.Join(ws, "data.bin"), big, 0o644); err != nil {
		t.Fatalf("write big failed: %v", err)
	}

	violations, err := Scan(ScanOptions{
		WorkspacePath: ws,
		WarnSizeMB:    1,
		CritSizeMB:    10,
		MaxDepth:      3,
		Engine:        engine,
	})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	var harborViolation, normalViolation *Violation
	for i, v := range violations {
		if v.Type == "bloat" && v.InSafeHarbor {
			harborViolation = &violations[i]
		}
		if v.Type == "bloat" && !v.InSafeHarbor {
			normalViolation = &violations[i]
		}
	}

	if harborViolation == nil {
		t.Fatal("expected a safe harbor bloat violation for ws/ws-log/session.bin")
	}
	if harborViolation.Severity != "INFO" {
		t.Fatalf("safe harbor violation should be INFO, got %s", harborViolation.Severity)
	}

	if normalViolation != nil {
		t.Fatal("data.bin should be excluded by -g:*.bin and not appear as a violation")
	}

	// Verify depth violation inside safe harbor is also INFO.
	// The path must match an exclude rule that the safe harbor overrides.
	// Use a hidden directory name so -:.* fires, then +g:ws/** re-includes.
	deepPath := filepath.Join(ws, "ws", ".deep", "a", "b", "c", "file.txt")
	if err := os.MkdirAll(filepath.Dir(deepPath), 0o755); err != nil {
		t.Fatalf("mkdir deep failed: %v", err)
	}
	if err := os.WriteFile(deepPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write deep failed: %v", err)
	}

	violations, err = Scan(ScanOptions{
		WorkspacePath: ws,
		WarnSizeMB:    1,
		CritSizeMB:    10,
		MaxDepth:      3,
		Engine:        engine,
	})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	for _, v := range violations {
		if v.Type == "depth" && v.InSafeHarbor {
			if v.Severity != "INFO" {
				t.Fatalf("safe harbor depth violation should be INFO, got %s", v.Severity)
			}
			return
		}
	}
	t.Fatal("expected a safe harbor depth violation inside ws/")
}
