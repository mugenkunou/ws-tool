package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWhere(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "Workspace")
	scratch := filepath.Join(t.TempDir(), "Scratch")
	cache := filepath.Join(t.TempDir(), "cache")
	homes := []Home{
		{Name: "cache", Var: "WS_CACHE", Path: cache, Purpose: "regenerable caches"},
		{Name: "scratch", Var: "WS_SCRATCH", Path: scratch, Purpose: "throwaway work and captures"},
	}
	outside := t.TempDir()
	now := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)

	write := func(dir, name string, lines int) string {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(strings.Repeat("echo hi\n", lines)), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	repo := filepath.Join(outside, "proj")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(outside, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		path   string
		folder string
		inside bool
		match  bool
	}{
		{"layout folder", filepath.Join(ws, "scripts", "lib", "x.sh"), "scripts/lib/", true, true},
		{"layout folder itself", filepath.Join(ws, "artifacts"), "artifacts/", true, true},
		{"notes", filepath.Join(ws, "notes", "a.md"), "notes/", true, true},
		{"home", filepath.Join(cache, "zdcm-prod", "x"), "$WS_CACHE/", true, true},
		{"git repo", repo, "experiments/proj/", false, true},
		{"plain dir", plain, "", false, false},
		{"pdf", filepath.Join(outside, "Report.PDF"), "artifacts/", false, true},
		{"tarball", filepath.Join(outside, "dump.tar"), "artifacts/", false, true},
		{"har", filepath.Join(outside, "login.har"), "$WS_SCRATCH/login.2026-03/", false, true},
		{"wisdom", filepath.Join(outside, "WISDOM.md"), "references/", false, true},
		{"openapi", filepath.Join(outside, "dns.openapi.yaml"), "references/", false, true},
		{"openapi bare", filepath.Join(outside, "openapi.json"), "references/", false, true},
		{"swagger", filepath.Join(outside, "swagger.yml"), "references/", false, true},
		{"yaml", filepath.Join(outside, "app.yaml"), "configs/<system>-<instance>/", false, true},
		{"env file", filepath.Join(outside, ".env.prod"), "configs/<system>-<instance>/", false, true},
		{"dotfile", filepath.Join(outside, ".gitconfig"), "symlinks/", false, true},
		{"etc", "/etc/hosts", "symlinks/", false, true},
		{"small script", write(outside, "small.sh", 10), "scripts/", false, true},
		{"limit script", write(outside, "limit.py", 60), "scripts/", false, true},
		{"long script", write(outside, "long.py", 61), "experiments/", false, true},
		{"missing script", filepath.Join(outside, "nope.sh"), "experiments/", false, true},
		{"unknown", filepath.Join(outside, "notes.txt"), "", false, false},
		{"markdown", filepath.Join(outside, "design.md"), "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := whereAt(ws, homes, tc.path, now)
			if got.Folder != tc.folder || got.Inside != tc.inside || got.Matched != tc.match {
				t.Fatalf("%s: got %+v, want folder %q inside %v matched %v", tc.path, got, tc.folder, tc.inside, tc.match)
			}
			if got.Reason == "" {
				t.Fatalf("%s: no reason", tc.path)
			}
		})
	}
}

func TestWhereNotesReasonAndSymlinkReason(t *testing.T) {
	ws := t.TempDir()
	got := Where(ws, nil, filepath.Join(ws, "notes", "a.md"))
	if !strings.Contains(got.Reason, "the user's own; read it, write elsewhere") {
		t.Fatalf("%+v", got)
	}
	got = Where(ws, nil, "/etc/hosts")
	if !strings.Contains(got.Reason, "ws symlink add /etc/hosts") {
		t.Fatalf("%+v", got)
	}
}

func TestWhereSecretNoteOnConfigs(t *testing.T) {
	got := Where(t.TempDir(), nil, filepath.Join(t.TempDir(), "app.xml"))
	if !strings.Contains(got.Reason, "pass") {
		t.Fatalf("%+v", got)
	}
}
