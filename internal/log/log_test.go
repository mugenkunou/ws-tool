package log

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartStopListAndRemove(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "Workspace")
	logDir := filepath.Join(t.TempDir(), "Scratch", ".ws-log")
	if err := os.MkdirAll(filepath.Join(ws, "ws"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	start, err := Start(StartOptions{LogDir: logDir, Tag: "t1"})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if !start.Active || start.Tag != "t1" {
		t.Fatalf("unexpected start result: %+v", start)
	}

	if err := os.WriteFile(start.StdinPath, []byte("echo one\n"), 0o644); err != nil {
		t.Fatalf("write stdin failed: %v", err)
	}
	if err := os.WriteFile(start.StdoutPath, []byte("one\n"), 0o644); err != nil {
		t.Fatalf("write stdout failed: %v", err)
	}

	sessions, err := List(logDir)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(sessions) != 1 || !sessions[0].Active {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}

	stop, err := Stop(StopOptions{LogDir: logDir})
	if err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	if !stop.Stopped {
		t.Fatal("expected stopped=true")
	}
}

func TestShowPruneAndRemove(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "Scratch", ".ws-log")

	_, err := Start(StartOptions{LogDir: logDir, Tag: "s1"})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	dir := filepath.Join(logDir, "s1")
	if err := os.WriteFile(filepath.Join(dir, "stdin.log"), []byte("git status\n"), 0o644); err != nil {
		t.Fatalf("write stdin failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stdout.log"), []byte("clean\n"), 0o644); err != nil {
		t.Fatalf("write stdout failed: %v", err)
	}
	if _, err := Stop(StopOptions{LogDir: logDir}); err != nil {
		t.Fatalf("stop failed: %v", err)
	}

	show, err := Show(logDir, "s1", "commands-only")
	if err != nil {
		t.Fatalf("show failed: %v", err)
	}
	if !strings.Contains(show, "git status") {
		t.Fatalf("unexpected show output: %s", show)
	}

	pruned, err := Prune(PruneOptions{LogDir: logDir, All: true})
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}
	if len(pruned.Removed) != 1 {
		t.Fatalf("expected one removed session, got %+v", pruned)
	}
}
