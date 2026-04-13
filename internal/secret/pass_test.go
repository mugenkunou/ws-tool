package secret

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckPassReturnsHealth(t *testing.T) {
	h := CheckPass()

	// We can't predict the test environment state, but the struct must be valid.
	if h.StorePath == "" {
		t.Fatal("expected non-empty StorePath")
	}
}

func TestCheckPassRespectsEnvOverride(t *testing.T) {
	tmp := t.TempDir()
	storePath := filepath.Join(tmp, ".password-store")
	if err := os.MkdirAll(storePath, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a .gpg-id to mark as initialized
	if err := os.WriteFile(filepath.Join(storePath, ".gpg-id"), []byte("ABCD1234\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PASSWORD_STORE_DIR", storePath)

	h := CheckPass()
	if h.StorePath != storePath {
		t.Fatalf("expected StorePath=%s, got %s", storePath, h.StorePath)
	}
	if !h.Initialized {
		t.Fatal("expected Initialized=true when .gpg-id exists")
	}
	if h.GitBacked {
		t.Fatal("expected GitBacked=false when no .git dir")
	}
}

func TestCheckPassDetectsGitBacked(t *testing.T) {
	tmp := t.TempDir()
	storePath := filepath.Join(tmp, ".password-store")
	if err := os.MkdirAll(filepath.Join(storePath, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storePath, ".gpg-id"), []byte("ABCD1234\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PASSWORD_STORE_DIR", storePath)

	h := CheckPass()
	if !h.GitBacked {
		t.Fatal("expected GitBacked=true when .git dir exists")
	}
	if h.GitRemote {
		t.Fatal("expected GitRemote=false when no git config with remote")
	}
}

func TestCheckPassDetectsGitRemote(t *testing.T) {
	tmp := t.TempDir()
	storePath := filepath.Join(tmp, ".password-store")
	gitDir := filepath.Join(storePath, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storePath, ".gpg-id"), []byte("ABCD1234\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitConfig := `[core]
	repositoryformatversion = 0
[remote "origin"]
	url = git@github.com:user/pass-store.git
	fetch = +refs/heads/*:refs/remotes/origin/*
`
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(gitConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PASSWORD_STORE_DIR", storePath)

	h := CheckPass()
	if !h.GitBacked {
		t.Fatal("expected GitBacked=true")
	}
	if !h.GitRemote {
		t.Fatal("expected GitRemote=true when remote origin is configured")
	}
}

func TestHasGitRemote(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"with_remote", "[core]\n\tbare = false\n[remote \"origin\"]\n\turl = git@github.com:user/repo.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n", true},
		{"no_remote", "[core]\n\tbare = false\n[branch \"main\"]\n\tmerge = refs/heads/main\n", false},
		{"empty", "", false},
		{"remote_no_url", "[remote \"origin\"]\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), "config")
			os.WriteFile(f, []byte(tc.content), 0o644)
			got := hasGitRemote(f)
			if got != tc.want {
				t.Fatalf("hasGitRemote() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCheckPassCountsEntries(t *testing.T) {
	tmp := t.TempDir()
	storePath := filepath.Join(tmp, ".password-store")
	if err := os.MkdirAll(filepath.Join(storePath, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storePath, ".gpg-id"), []byte("ABCD1234\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create some .gpg entries
	for _, name := range []string{"github.gpg", "work/db.gpg", "work/api.gpg"} {
		if err := os.WriteFile(filepath.Join(storePath, name), []byte("encrypted"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("PASSWORD_STORE_DIR", storePath)

	h := CheckPass()
	if h.EntryCount != 3 {
		t.Fatalf("expected 3 entries, got %d", h.EntryCount)
	}
}

func TestAuditPassStoreNotInitialized(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PASSWORD_STORE_DIR", filepath.Join(tmp, "nonexistent"))

	h := CheckPass()
	result := AuditPassStore(h)
	if len(result.Findings) == 0 {
		t.Fatal("expected findings for uninitialized store")
	}
	found := false
	for _, f := range result.Findings {
		if f.Message == "pass store is not initialized — run `ws secret setup` or `pass init <gpg-id>`" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'not initialized' finding, got %+v", result.Findings)
	}
}

func TestAuditPassStoreNoGit(t *testing.T) {
	tmp := t.TempDir()
	storePath := filepath.Join(tmp, ".password-store")
	if err := os.MkdirAll(storePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storePath, ".gpg-id"), []byte("ABCD1234\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Add an entry so it's not empty
	if err := os.WriteFile(filepath.Join(storePath, "test.gpg"), []byte("encrypted-data-here-padding"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PASSWORD_STORE_DIR", storePath)

	h := CheckPass()
	result := AuditPassStore(h)
	foundGit := false
	for _, f := range result.Findings {
		if f.Message == "pass store is not git-backed — run `pass git init` for version history" {
			foundGit = true
		}
	}
	if !foundGit {
		t.Fatalf("expected 'not git-backed' finding, got %+v", result.Findings)
	}
}

func TestAuditPassStoreSmallEntries(t *testing.T) {
	tmp := t.TempDir()
	storePath := filepath.Join(tmp, ".password-store")
	if err := os.MkdirAll(filepath.Join(storePath, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(storePath, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storePath, ".gpg-id"), []byte("ABCD1234\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Small entry under git/ (likely single-line password) — should trigger.
	if err := os.WriteFile(filepath.Join(storePath, "git", "github.gpg"), []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Small entry NOT under git/ — should NOT trigger (only git/ entries are audited).
	if err := os.WriteFile(filepath.Join(storePath, "tiny.gpg"), []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PASSWORD_STORE_DIR", storePath)

	h := CheckPass()
	result := AuditPassStore(h)
	foundGit := false
	foundNonGit := false
	for _, f := range result.Findings {
		if f.Entry == "git/github" {
			foundGit = true
		}
		if f.Entry == "tiny" {
			foundNonGit = true
		}
	}
	if !foundGit {
		t.Fatalf("expected metadata finding for git/ entry, got %+v", result.Findings)
	}
	if foundNonGit {
		t.Fatalf("did not expect metadata finding for non-git entry, got %+v", result.Findings)
	}
}
