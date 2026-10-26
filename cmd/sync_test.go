package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// syncWorkspace initialises a workspace whose sync.mirror.target points at a
// fresh folder, and isolates the state dir.
func syncWorkspace(t *testing.T) (ws, target string) {
	t.Helper()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not installed")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ws = initWorkspace(t)
	cloud := filepath.Join(t.TempDir(), "Cloud")
	if err := os.MkdirAll(cloud, 0o755); err != nil {
		t.Fatal(err)
	}
	target = filepath.Join(cloud, "Workspace")
	cfgPath := wsconfig.ConfigPath(ws)
	cfg, err := wsconfig.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Sync.Mirror.Target = target
	if err := wsconfig.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	return ws, target
}

func runWS(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Execute(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestSyncPushNeedsTarget(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not installed")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ws := initWorkspace(t)
	code, _, errOut := runWS(t, "", "--workspace", ws, "sync", "push")
	if code == 0 || !strings.Contains(errOut, "sync.mirror.target is not set") {
		t.Fatalf("code=%d stderr=%s", code, errOut)
	}
}

func TestSyncPushStatusAndGuard(t *testing.T) {
	ws, target := syncWorkspace(t)
	for i := 0; i < 20; i++ {
		p := filepath.Join(ws, "notes", "bulk", strings.Repeat("n", i+1)+".md")
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	if code, _, e := runWS(t, "", "--workspace", ws, "sync", "push", "--quiet"); code != 0 {
		t.Fatalf("push: %d %s", code, e)
	}
	if _, err := os.Stat(filepath.Join(target, "notes", "bulk", "n.md")); err != nil {
		t.Fatalf("mirror lacks a pushed file: %v", err)
	}
	code, out, _ := runWS(t, "", "--workspace", ws, "sync", "status")
	if code != 0 || !strings.Contains(out, "ok (written by this machine") || !strings.Contains(out, "0 to copy · 0 to delete") {
		t.Fatalf("status: %d %s", code, out)
	}

	// A mass deletion: cron (--quiet) refuses, a human can confirm.
	os.RemoveAll(filepath.Join(ws, "notes", "bulk"))
	if code, _, e := runWS(t, "", "--workspace", ws, "sync", "push", "--quiet"); code == 0 || !strings.Contains(e, "too many deletions") {
		t.Fatalf("guard under --quiet: code=%d stderr=%s", code, e)
	}
	if _, err := os.Stat(filepath.Join(target, "notes", "bulk", "n.md")); err != nil {
		t.Fatal("a refused push changed the mirror")
	}
	if code, _, e := runWS(t, "n\n", "--workspace", ws, "sync", "push"); code == 0 {
		t.Fatalf("answering n should refuse: %s", e)
	}
	if code, _, e := runWS(t, "y\n", "--workspace", ws, "sync", "push"); code != 0 {
		t.Fatalf("answering y should push: %d %s", code, e)
	}
	if _, err := os.Stat(filepath.Join(target, "notes", "bulk")); !os.IsNotExist(err) {
		t.Fatal("confirmed push did not delete")
	}
}

func TestSyncPushWarnsWhenMirrorWiped(t *testing.T) {
	ws, target := syncWorkspace(t)
	runWS(t, "", "--workspace", ws, "sync", "push", "--quiet")
	os.RemoveAll(target)
	os.MkdirAll(target, 0o755)
	code, _, e := runWS(t, "", "--workspace", ws, "sync", "push", "--quiet")
	if code != 0 || !strings.Contains(e, "mirror was emptied") {
		t.Fatalf("code=%d stderr=%s", code, e)
	}
	if _, err := os.Stat(filepath.Join(target, "ws.json")); err != nil {
		t.Fatal("the mirror was not refilled")
	}
}

func TestSyncRestore(t *testing.T) {
	ws, target := syncWorkspace(t)
	runWS(t, "", "--workspace", ws, "sync", "push", "--quiet")

	// Over an existing workspace: refused unattended.
	if code, _, e := runWS(t, "", "--workspace", ws, "sync", "restore", "--quiet"); code == 0 || !strings.Contains(e, "not empty") {
		t.Fatalf("restore over a workspace: code=%d stderr=%s", code, e)
	}

	// A fresh machine: no ws.json, the mirror dir is passed.
	fresh := filepath.Join(t.TempDir(), "Workspace")
	code, out, e := runWS(t, "", "--workspace", fresh, "sync", "restore", target)
	if code != 0 || !strings.Contains(out, "ws symlink link") {
		t.Fatalf("restore: %d %s %s", code, out, e)
	}
	if _, err := os.Stat(filepath.Join(fresh, "ws.json")); err != nil {
		t.Fatal("ws.json not restored")
	}
	if _, err := os.Stat(filepath.Join(fresh, ".ws-sync-stamp")); !os.IsNotExist(err) {
		t.Fatal("the stamp was copied into the workspace")
	}
}
