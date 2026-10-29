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

	res := engine.Evaluate("ws/logs/build/stdout.log", false)
	if !res.Included {
		t.Fatalf("expected ws include rule to be included, got excluded by %s", res.Rule)
	}

	res = engine.Evaluate(".git/config", false)
	if res.Included {
		t.Fatalf("expected hidden file to be excluded, rule=%s", res.Rule)
	}
}

func TestEvaluateByIncludeFlag(t *testing.T) {
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

	// ws/ path excluded by -g:*.log but re-included by +g:ws/** — include rule
	res := engine.Evaluate("ws/logs/build.log", false)
	if !res.Included {
		t.Fatal("expected ws include rule path to be included")
	}
	if !res.ByInclude {
		t.Fatal("expected ByInclude=true for ws/logs/build.log")
	}

	// Normal file not excluded then re-included — not an include rule
	res = engine.Evaluate("README.md", false)
	if !res.Included {
		t.Fatal("expected README.md to be included")
	}
	if res.ByInclude {
		t.Fatal("expected ByInclude=false for README.md (never excluded)")
	}

	// Excluded file — not an include rule
	res = engine.Evaluate(".git/config", false)
	if res.Included {
		t.Fatal("expected .git/config to be excluded")
	}
	if res.ByInclude {
		t.Fatal("expected ByInclude=false for excluded path")
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

func TestScanByIncludeDowngradesToInfo(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "Workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// Template: exclude hidden files and large files, but include ws/
	megaignore := "-:.*\n-g:*.bin\n+:ws\n+g:ws/**\n-s:*\n"
	if err := os.WriteFile(filepath.Join(ws, ".megaignore"), []byte(megaignore), 0o644); err != nil {
		t.Fatalf("write megaignore failed: %v", err)
	}
	engine, err := LoadEngine(filepath.Join(ws, ".megaignore"))
	if err != nil {
		t.Fatalf("load engine failed: %v", err)
	}

	// Big file inside include rule — should be INFO
	includeDir := filepath.Join(ws, "ws", "logs")
	if err := os.MkdirAll(includeDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	big := make([]byte, 2*1024*1024)
	if err := os.WriteFile(filepath.Join(includeDir, "build.bin"), big, 0o644); err != nil {
		t.Fatalf("write big failed: %v", err)
	}

	// Big file outside include rule — should be WARNING/CRITICAL
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

	var includeViolation, normalViolation *Violation
	for i, v := range violations {
		if v.Type == "bloat" && v.Muted {
			includeViolation = &violations[i]
		}
		if v.Type == "bloat" && !v.Muted {
			normalViolation = &violations[i]
		}
	}

	if includeViolation == nil {
		t.Fatal("expected an include rule bloat violation for ws/logs/build.bin")
	}
	if includeViolation.Severity != "INFO" {
		t.Fatalf("include rule violation should be INFO, got %s", includeViolation.Severity)
	}

	if normalViolation != nil {
		t.Fatal("data.bin should be excluded by -g:*.bin and not appear as a violation")
	}

	// Verify depth violation inside include rule is also INFO.
	// The path must match an exclude rule that the include rule overrides.
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
		if v.Type == "depth" && v.Muted {
			if v.Severity != "INFO" {
				t.Fatalf("include rule depth violation should be INFO, got %s", v.Severity)
			}
			return
		}
	}
	t.Fatal("expected an include rule depth violation inside ws/")
}

func TestScanWarnsOnSyncedSymlinks(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "Workspace")
	for _, d := range []string{"notes/2026", "proj/node_modules/.bin"} {
		if err := os.MkdirAll(filepath.Join(ws, d), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
	}
	engine := BuildEngine(DefaultUserRules())

	links := map[string]string{
		"notes/current":              "2026",          // synced dir link: warn
		"notes/gone":                 "no-such-file",  // broken link: warn
		"proj/node_modules/.bin/tsc": "../typescript", // ignored path: silent
	}
	for link, target := range links {
		if err := os.Symlink(target, filepath.Join(ws, link)); err != nil {
			t.Fatalf("symlink failed: %v", err)
		}
	}

	violations, err := Scan(ScanOptions{WorkspacePath: ws, MaxDepth: 10, Engine: engine})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	got := map[string]Violation{}
	for _, v := range violations {
		if v.Type == "symlink" {
			got[v.Path] = v
		}
	}
	if len(got) != 2 {
		t.Fatalf("want 2 symlink warnings, got %d: %+v", len(got), got)
	}
	for _, p := range []string{"notes/current", "notes/gone"} {
		v, ok := got[p]
		if !ok {
			t.Fatalf("missing symlink warning for %s", p)
		}
		if v.Severity != "WARNING" {
			t.Fatalf("%s: want WARNING, got %s", p, v.Severity)
		}
	}
	if _, ok := got["proj/node_modules/.bin/tsc"]; ok {
		t.Fatal("a link under an ignored path must not warn")
	}
}
