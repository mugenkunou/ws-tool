package trash

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func seedItem(t *testing.T, root, name string, deleted *time.Time) {
	t.Helper()
	if err := os.MkdirAll(filesDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(infoDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filesDir(root), name), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if deleted != nil {
		if err := writeTrashInfo(root, name, "/orig/"+name, *deleted); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPurgeOlderThan(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	old := now.Add(-31 * 24 * time.Hour)
	edge := now.Add(-30 * 24 * time.Hour) // exactly 30d: not strictly older
	recent := now.Add(-2 * time.Hour)
	seedItem(t, root, "old.txt", &old)
	seedItem(t, root, "edge.txt", &edge)
	seedItem(t, root, "recent.txt", &recent)
	seedItem(t, root, "noinfo.txt", nil)

	opts := PurgeOptions{RootDir: root, OlderThan: 30 * 24 * time.Hour, Now: now}

	opts.DryRun = true
	res, err := Purge(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matched) != 1 || res.Matched[0].Name != "old.txt" || res.Kept != 2 || res.Skipped != 1 {
		t.Fatalf("unexpected dry-run result: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(filesDir(root), "old.txt")); err != nil {
		t.Fatalf("dry run removed file: %v", err)
	}

	opts.DryRun = false
	if _, err := Purge(opts); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"old.txt": false, "edge.txt": true, "recent.txt": true, "noinfo.txt": true} {
		_, err := os.Stat(filepath.Join(filesDir(root), name))
		if (err == nil) != want {
			t.Errorf("%s exists=%v, want %v", name, err == nil, want)
		}
	}
	if _, err := os.Stat(filepath.Join(infoDir(root), "old.txt.trashinfo")); !os.IsNotExist(err) {
		t.Errorf("old.txt.trashinfo should be removed, err=%v", err)
	}
}

func TestPurgeMissingRoot(t *testing.T) {
	res, err := Purge(PurgeOptions{RootDir: filepath.Join(t.TempDir(), "nope"), OlderThan: time.Hour})
	if err != nil || res.Exists || len(res.Matched) != 0 {
		t.Fatalf("got %+v, %v", res, err)
	}
	_ = fmt.Sprint()
}
