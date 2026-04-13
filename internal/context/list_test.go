package context

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTrackAndListWithoutUpdate(t *testing.T) {
	workspace := t.TempDir()
	project := filepath.Join(workspace, "project-a")
	if err := os.MkdirAll(filepath.Join(project, ".ws-context", "task-1"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	if err := Track(workspace, project, "task-1"); err != nil {
		t.Fatalf("track failed: %v", err)
	}

	res, err := List(ListOptions{WorkspacePath: workspace})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if res.Updated {
		t.Fatalf("expected updated=false")
	}
	if len(res.Contexts) != 1 {
		t.Fatalf("expected 1 context, got %d", len(res.Contexts))
	}
	if res.Contexts[0].Task != "task-1" {
		t.Fatalf("unexpected task: %s", res.Contexts[0].Task)
	}
}

func TestListUpdateScansWorkspace(t *testing.T) {
	workspace := t.TempDir()
	project := filepath.Join(workspace, "project-b")
	if err := os.MkdirAll(filepath.Join(project, ".ws-context", "manual-task"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	res, err := List(ListOptions{WorkspacePath: workspace, Update: true})
	if err != nil {
		t.Fatalf("list update failed: %v", err)
	}
	if !res.Updated {
		t.Fatalf("expected updated=true")
	}
	if len(res.Contexts) != 1 {
		t.Fatalf("expected 1 context, got %d", len(res.Contexts))
	}
	if res.Contexts[0].Task != "manual-task" {
		t.Fatalf("unexpected task: %s", res.Contexts[0].Task)
	}

	res2, err := List(ListOptions{WorkspacePath: workspace})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(res2.Contexts) != 1 {
		t.Fatalf("expected persisted index, got %d contexts", len(res2.Contexts))
	}
}
