package backup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newBare makes a bare remote and a working repo that has it as origin.
func newRepo(t *testing.T) (work, remote string) {
	t.Helper()
	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "--initial-branch=main", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v %s", err, out)
	}
	work = filepath.Join(root, "work")
	if err := Init(work, "main"); err != nil {
		t.Fatal(err)
	}
	if err := SetRemote(work, remote); err != nil {
		t.Fatal(err)
	}
	return work, remote
}

func TestInitSetsIdentityAndBranch(t *testing.T) {
	work, _ := newRepo(t)
	if got := CurrentBranch(work); got != "main" {
		t.Fatalf("branch %q, want main", got)
	}
	name, _ := Git(work, "config", "--local", "user.name")
	email, _ := Git(work, "config", "--local", "user.email")
	if strings.TrimSpace(name) != "ws" || strings.TrimSpace(email) != "ws@localhost" {
		t.Fatalf("identity %q <%q>, want ws <ws@localhost>", name, email)
	}
}

func TestCommitPushAndInspect(t *testing.T) {
	work, _ := newRepo(t)
	if err := os.WriteFile(filepath.Join(work, "a"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := Inspect(work, "main", 10)
	if !st.Initialized || len(st.Uncommitted) != 1 {
		t.Fatalf("pending change not reported: %+v", st)
	}

	committed, err := CommitAll(work, "symlink add: x")
	if err != nil || !committed {
		t.Fatalf("CommitAll = %v, %v", committed, err)
	}
	if again, err := CommitAll(work, "nothing"); err != nil || again {
		t.Fatalf("a second CommitAll must find nothing to commit: %v %v", again, err)
	}
	if err := Push(work, "main"); err != nil {
		t.Fatal(err)
	}

	st = Inspect(work, "main", 10)
	if st.Ahead != 0 || st.Behind != 0 || len(st.Uncommitted) != 0 {
		t.Fatalf("after a push: %+v", st)
	}
	if len(st.Commits) != 1 || st.Commits[0].Subject != "symlink add: x" || st.Commits[0].Hash == "" {
		t.Fatalf("commits: %+v", st.Commits)
	}
	if !strings.HasSuffix(st.LastCommit, "symlink add: x") {
		t.Fatalf("last commit %q", st.LastCommit)
	}

	// A local commit makes the repo ahead of the remote.
	_ = os.WriteFile(filepath.Join(work, "b"), []byte("2"), 0o644)
	if _, err := CommitAll(work, "second"); err != nil {
		t.Fatal(err)
	}
	if st := Inspect(work, "main", 10); st.Ahead != 1 {
		t.Fatalf("ahead = %d, want 1", st.Ahead)
	}
}

func TestRemoveRemote(t *testing.T) {
	work, _ := newRepo(t)
	if RemoteURL(work) == "" {
		t.Fatal("remote not set")
	}
	if err := RemoveRemote(work); err != nil {
		t.Fatal(err)
	}
	if RemoteURL(work) != "" {
		t.Fatal("remote still set")
	}
	if err := RemoveRemote(work); err != nil {
		t.Fatalf("removing a missing remote must be a no-op: %v", err)
	}
}

func TestPushNeverForces(t *testing.T) {
	work, remote := newRepo(t)
	_ = os.WriteFile(filepath.Join(work, "a"), []byte("1"), 0o644)
	if _, err := CommitAll(work, "one"); err != nil {
		t.Fatal(err)
	}
	if err := Push(work, "main"); err != nil {
		t.Fatal(err)
	}

	// Another clone pushes a commit this repo lacks.
	other := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", remote, other).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	_ = os.WriteFile(filepath.Join(other, "c"), []byte("x"), 0o644)
	for _, args := range [][]string{{"add", "-A"}, {"-c", "user.name=o", "-c", "user.email=o@x", "commit", "-m", "theirs"}, {"push", "origin", "HEAD"}} {
		if out, err := exec.Command("git", append([]string{"-C", other}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}

	_ = os.WriteFile(filepath.Join(work, "b"), []byte("2"), 0o644)
	if _, err := CommitAll(work, "mine"); err != nil {
		t.Fatal(err)
	}
	if err := Push(work, "main"); err == nil {
		t.Fatal("a push the remote rejects must fail, never force")
	}
	if st := Inspect(work, "main", 5); st.Ahead != 1 {
		t.Fatalf("the commit must stay local: ahead = %d", st.Ahead)
	}
}
