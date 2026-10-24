package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/resource"
)

func plainRepo(t *testing.T) string {
	t.Helper()
	gitAvail(t)
	dir := t.TempDir()
	gitOut(t, dir, "init", "-b", "main")
	return dir
}

func findingsFor(dir string, checks ...string) []Finding {
	return Doctor(dir, []Repository{{Path: dir}}, DoctorOptions{Checks: checks})
}

func TestCredentialsCheck(t *testing.T) {
	dir := plainRepo(t)

	if got := findingsFor(dir, "credentials"); len(got) != 0 {
		t.Fatalf("no remote: %+v", got)
	}

	gitOut(t, dir, "remote", "add", "origin", "git@github.com:me/x.git")
	if got := findingsFor(dir, "credentials"); len(got) != 0 {
		t.Fatalf("an SSH remote needs no helper: %+v", got)
	}

	gitOut(t, dir, "remote", "add", "web", "https://github.com/me/x.git")
	got := findingsFor(dir, "credentials")
	if len(got) != 1 || got[0].Severity != SeverityInfo || got[0].Check != "credentials" {
		t.Fatalf("HTTPS remote without a helper: %+v", got)
	}
	if !strings.Contains(got[0].Detail, "web") {
		t.Fatalf("the detail should name the remote: %q", got[0].Detail)
	}

	gitOut(t, dir, "config", "credential.helper", "store")
	if got := findingsFor(dir, "credentials"); len(got) != 0 {
		t.Fatalf("a helper is configured: %+v", got)
	}
}

func TestEveryFindingEndsWithItsFix(t *testing.T) {
	dir := plainRepo(t) // no identity, no upstream, never fetched
	got := Doctor(dir, []Repository{{Path: dir}}, DoctorOptions{})
	if len(got) < 3 {
		t.Fatalf("expected several findings, got %+v", got)
	}
	for _, f := range got {
		if !strings.HasSuffix(f.Detail, "ws repo configure "+dir+")") {
			t.Errorf("%s: detail does not end with its fix: %q", f.Check, f.Detail)
		}
	}
}

func TestHygieneChecksListsCredentials(t *testing.T) {
	found := false
	for _, c := range HygieneChecks {
		found = found || c == "credentials"
	}
	if !found {
		t.Fatalf("credentials is a check: %v", HygieneChecks)
	}
}

func TestIssuesCountWarnAndErrorOnly(t *testing.T) {
	e := Entry{Findings: []Finding{
		{Check: "identity", Severity: SeverityInfo},
		{Check: "default-branch", Severity: SeverityInfo},
		{Check: "upstream", Severity: SeverityWarn},
		{Check: "x", Severity: SeverityError},
	}}
	if e.Issues() != 2 {
		t.Fatalf("issues %d, want 2", e.Issues())
	}
	rec := Schema(resource.CLIView()).Records([]Entry{e})[0]
	field := func(key string) any {
		for _, kv := range rec {
			if kv.Key == key {
				return kv.Value
			}
		}
		return nil
	}
	if field("issues") != 2 {
		t.Fatalf("issues field: %v", field("issues"))
	}
	// findings still lists every check id.
	if ids, _ := field("findings").([]string); len(ids) != 4 {
		t.Fatalf("findings field: %v", field("findings"))
	}
}

func TestEntryState(t *testing.T) {
	cases := []struct {
		st   RepoStatus
		want string
	}{
		{RepoStatus{Error: "x", Dirty: true}, "error"},
		{RepoStatus{Dirty: true, HasUpstream: true}, "dirty"},
		{RepoStatus{Dirty: true}, "dirty"},
		{RepoStatus{}, "no upstream"},
		{RepoStatus{HasUpstream: true}, "clean"},
	}
	for _, c := range cases {
		if got := (Entry{Status: c.st}).State(); got != c.want {
			t.Errorf("%+v: %s, want %s", c.st, got, c.want)
		}
	}
}

func TestDetail(t *testing.T) {
	e := Entry{
		Status: RepoStatus{Path: "/w/blog", Branch: "main"},
		Findings: []Finding{
			{Check: "upstream", Severity: SeverityWarn, Detail: "current branch has no tracking upstream"},
			{Check: "default-branch", Severity: SeverityInfo, Detail: "init.defaultBranch not set and origin/HEAD not resolved"},
		},
	}
	want := "Path      /w/blog\n" +
		"Branch    main\n" +
		"State     no upstream\n" +
		"Upstream  none\n" +
		"Hygiene\n" +
		"  warn  upstream        current branch has no tracking upstream\n" +
		"  info  default-branch  init.defaultBranch not set and origin/HEAD not resolved\n"
	if got := Detail(e); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}

	clean := Entry{Status: RepoStatus{Path: "/w/x", Branch: "HEAD", Detached: true, HasUpstream: true, Ahead: 2, Behind: 1}}
	got := Detail(clean)
	for _, s := range []string{"Branch    detached HEAD\n", "Upstream  ↑2 ↓1\n", "Hygiene  none\n"} {
		if !strings.Contains(got, s) {
			t.Errorf("missing %q in:\n%s", s, got)
		}
	}

	broken := Entry{Status: RepoStatus{Path: "/w/y", Error: "not a git repository"}}
	if got := Detail(broken); !strings.Contains(got, "Error     not a git repository\n") || !strings.Contains(got, "State     error") {
		t.Fatalf("error repo:\n%s", got)
	}
}

func TestReadFactsAndConfigureHelpers(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte("[core]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	f := newFixture(t)
	facts := ReadFacts(f.a)
	if facts.Branch != "main" || !facts.HasRemote("origin") || !facts.OriginHasBranch {
		t.Fatalf("%+v", facts)
	}

	// Track origin/<branch> after the upstream was removed.
	gitOut(t, f.a, "branch", "--unset-upstream")
	if st := f.status(t); st.HasUpstream {
		t.Fatal("upstream should be gone")
	}
	if err := TrackOrigin(f.a, "main"); err != nil {
		t.Fatal(err)
	}
	if st := f.status(t); !st.HasUpstream {
		t.Fatal("TrackOrigin should set the upstream")
	}

	// Push a new branch and track it.
	gitOut(t, f.a, "checkout", "-b", "topic")
	if ReadFacts(f.a).OriginHasBranch {
		t.Fatal("origin has no topic yet")
	}
	if err := PushUpstream(f.a, "topic"); err != nil {
		t.Fatal(err)
	}
	if st := f.status(t); !st.HasUpstream {
		t.Fatal("PushUpstream should track the branch")
	}

	// Add a remote to a repo that has none.
	dir := plainRepo(t)
	if ReadFacts(dir).HasRemote("origin") {
		t.Fatal("no remote yet")
	}
	if err := AddOrigin(dir, f.remote); err != nil {
		t.Fatal(err)
	}
	if !ReadFacts(dir).HasRemote("origin") {
		t.Fatal("origin was added")
	}

	// Local and global settings.
	if err := SetConfig(dir, false, "user.name", "Local Me"); err != nil {
		t.Fatal(err)
	}
	if err := SetConfig(dir, true, "user.email", "global@example.com"); err != nil {
		t.Fatal(err)
	}
	facts = ReadFacts(dir)
	if facts.LocalName != "Local Me" || facts.GlobalEmail != "global@example.com" || facts.LocalEmail != "" {
		t.Fatalf("%+v", facts)
	}
	check := exec.Command("git", "config", "--global", "user.email")
	check.Dir = dir
	out, _ := check.Output()
	if strings.TrimSpace(string(out)) != "global@example.com" {
		t.Fatal("the global setting must land in GIT_CONFIG_GLOBAL")
	}
	if os.Getenv("GIT_CONFIG_GLOBAL") == "" {
		t.Fatal("tests must isolate the global git config")
	}
}
