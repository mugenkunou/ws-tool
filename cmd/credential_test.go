package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── plumbing: git-credential-helper get|store|erase ──

// runPlumbing runs the plumbing with git's request on stdin and returns the
// exact bytes of stdout and stderr.
func runPlumbing(t *testing.T, stdin string, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = Execute(args, strings.NewReader(stdin), &o, &e)
	return code, o.String(), e.String()
}

func TestPlumbingGetIsByteForByte(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.entry(t, "git/example.com", "tok-host\nusername: bob\nnotes: ignored\n")

	req := "protocol=https\nhost=example.com\n\n"
	code, out, errOut := runPlumbing(t, req, "git-credential-helper", "get")
	if code != 0 || errOut != "" {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	// password first, then username, one "key=value\n" each, nothing else.
	if out != "password=tok-host\nusername=bob\n" {
		t.Fatalf("stdout %q", out)
	}
}

func TestPlumbingGetLookupOrder(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.entry(t, "git/example.com", "host-token\nuser: hostuser\n")
	shims.entry(t, "git/example.com/team/api", "repo-token\nlogin: repouser\n")

	// git sends the path only with useHttpPath; the repo token wins then.
	_, out, _ := runPlumbing(t, "protocol=https\nhost=example.com\npath=team/api.git\n\n", "git-credential-helper", "get")
	if out != "password=repo-token\nusername=repouser\n" {
		t.Fatalf("repo token: %q", out)
	}
	// Another repo falls back to the host token.
	_, out, _ = runPlumbing(t, "protocol=https\nhost=example.com\npath=team/other\n\n", "git-credential-helper", "get")
	if out != "password=host-token\nusername=hostuser\n" {
		t.Fatalf("host token: %q", out)
	}
	// Without a path, the host token.
	_, out, _ = runPlumbing(t, "protocol=https\nhost=example.com\n\n", "git-credential-helper", "get")
	if out != "password=host-token\nusername=hostuser\n" {
		t.Fatalf("no path: %q", out)
	}
}

func TestPlumbingGetNothingFoundPrintsNothing(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	for _, req := range []string{"protocol=https\nhost=nowhere.example\n\n", "\n", ""} {
		code, out, errOut := runPlumbing(t, req, "git-credential-helper", "get")
		if code != 0 || out != "" || errOut != "" {
			t.Errorf("request %q: code=%d stdout=%q stderr=%q, want silence", req, code, out, errOut)
		}
	}
}

func TestPlumbingNeedsNoWorkspace(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.entry(t, "git/example.com", "tok\n")
	t.Setenv("WS_WORKSPACE", filepath.Join(t.TempDir(), "nothing-here"))
	if _, out, _ := runPlumbing(t, "host=example.com\n\n", "git-credential-helper", "get"); out != "password=tok\n" {
		t.Fatalf("stdout %q", out)
	}
}

func TestPlumbingStoreAndEraseDoNothing(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	req := "protocol=https\nhost=example.com\nusername=bob\npassword=secret\n\n"
	for _, verb := range []string{"store", "erase"} {
		code, out, errOut := runPlumbing(t, req, "git-credential-helper", verb)
		if code != 0 || out != "" || errOut != "" {
			t.Errorf("%s: code=%d stdout=%q stderr=%q", verb, code, out, errOut)
		}
	}
	if strings.Contains(shims.calls(t), "insert") {
		t.Fatalf("store must not write to pass: %q", shims.calls(t))
	}
	entries, _ := os.ReadDir(shims.store)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".gpg") || e.Name() == "git" {
			t.Fatalf("store created %s", e.Name())
		}
	}
}

func TestPlumbingUnknownOperationIsSilent(t *testing.T) {
	for _, args := range [][]string{
		{"git-credential-helper", "future-op"},
		{"git-credential-helper"},
	} {
		code, out, errOut := runPlumbing(t, "", args...)
		if code != 0 || out != "" || errOut != "" {
			t.Errorf("%v: code=%d stdout=%q stderr=%q, want exit 0 and silence", args, code, out, errOut)
		}
	}
}

func TestPlumbingIsHiddenFromHelpAndTable(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Execute([]string{"--help"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(out.String(), "git-credential-helper") {
		t.Fatalf("help must not list the plumbing:\n%s", out.String())
	}
	if _, ok := findCommand([]string{"git-credential-helper", "get"}); ok {
		t.Fatal("the plumbing is not a table command")
	}
}

func TestPlumbingExplicitHelp(t *testing.T) {
	code, out, _ := runPlumbing(t, "", "git-credential-helper", "--help")
	if code != 0 || !strings.Contains(out, "ws credential enable") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

// TestLegacyCredentialVerbsAreSilentPlumbing: git configs written by older
// versions say `!ws credential`, and those must keep working without a word.
func TestLegacyCredentialVerbsAreSilentPlumbing(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.entry(t, "git/example.com", "tok\nusername: bob\n")
	code, out, errOut := runPlumbing(t, "host=example.com\n\n", "credential", "get")
	if code != 0 || out != "password=tok\nusername=bob\n" || errOut != "" {
		t.Fatalf("credential get: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	for _, verb := range []string{"store", "erase"} {
		if code, out, errOut := runPlumbing(t, "host=example.com\n\n", "credential", verb); code != 0 || out != "" || errOut != "" {
			t.Errorf("credential %s: code=%d stdout=%q stderr=%q", verb, code, out, errOut)
		}
	}
}

func TestCredentialNounWithoutVerbShowsHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute([]string{"credential"}, strings.NewReader(""), &out, &errOut)
	for _, want := range []string{"ws credential <verb>", "enable", "disable", "status"} {
		if code != 0 || !strings.Contains(out.String(), want) {
			t.Fatalf("want %q: code=%d out=%q err=%q", want, code, out.String(), errOut.String())
		}
	}
	if code := Execute([]string{"credential", "frobnicate"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("unknown verb: exit %d", code)
	}
}

// ── enable / disable / status ──

// credWorkspace is a workspace with one repo whose remote is HTTPS.
func credWorkspace(t *testing.T, shims *passShims, remote string) (workspace, repoDir string) {
	t.Helper()
	workspace = initWorkspace(t)
	t.Setenv("PASSWORD_STORE_DIR", shims.store) // initWorkspace gave the test a fresh one
	repoDir = filepath.Join(workspace, "experiments", "api")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitLocal(t, repoDir, "init", "-q")
	runGitLocal(t, repoDir, "remote", "add", "origin", remote)
	return workspace, repoDir
}

// gitGlobal reads a key of the temp global git config ("" when unset).
func gitGlobal(t *testing.T, shims *passShims, key string) string {
	t.Helper()
	out, _ := execGit(shims.gitCf, key)
	return out
}

func execGit(cfg, key string) (string, error) {
	var o bytes.Buffer
	cmd := gitCmd("config", "--file", cfg, "--get", key)
	cmd.Stdout = &o
	err := cmd.Run()
	return strings.TrimSpace(o.String()), err
}

func TestCredentialEnableNeedsAnInitializedStore(t *testing.T) {
	shims := newPassShims(t)
	code, _, errOut := runPlumbing(t, "", "credential", "enable", "--quiet")
	if code != 1 || !strings.Contains(errOut, "ws pass init") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if gitGlobal(t, shims, "credential.helper") != "" {
		t.Fatal("nothing should be configured")
	}
}

func TestCredentialEnableWritesWrapperAndConfig(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	wrapper := filepath.Join(shims.home, ".local", "bin", "ws-credential-helper")

	code, out, errOut := runPlumbing(t, "", "credential", "enable", "--dry-run", "--no-color")
	if code != 0 {
		t.Fatalf("dry-run: %d %s", code, errOut)
	}
	for _, want := range []string{
		"Install the helper wrapper " + wrapper,
		"Set global credential.helper to '!" + wrapper + "'",
		"Set global credential.useHttpPath = true",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(wrapper); err == nil {
		t.Fatal("--dry-run installed the wrapper")
	}

	if code, out, errOut = runPlumbing(t, "", "credential", "enable", "--quiet"); code != 0 {
		t.Fatalf("enable: %d %s%s", code, out, errOut)
	}
	script, err := os.ReadFile(wrapper)
	if err != nil || string(script) != credentialWrapperScript {
		t.Fatalf("wrapper: %q (%v)", script, err)
	}
	if info, _ := os.Stat(wrapper); info.Mode()&0o111 == 0 {
		t.Fatal("the wrapper must be executable")
	}
	if got := gitGlobal(t, shims, "credential.helper"); got != "!"+wrapper {
		t.Fatalf("credential.helper = %q", got)
	}
	if got := gitGlobal(t, shims, "credential.useHttpPath"); got != "true" {
		t.Fatalf("credential.useHttpPath = %q", got)
	}

	// Already enabled: nothing to do.
	code, out, _ = runPlumbing(t, "", "credential", "enable")
	if code != 0 || !strings.Contains(out, "already enabled") {
		t.Fatalf("second enable: code=%d out=%q", code, out)
	}
}

func TestCredentialEnableReplacesAStaleValue(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	// The old broken form: the wrapper called with a duplicate subcommand.
	stale := "!" + filepath.Join(shims.home, ".local", "bin", "ws-credential-helper") + " git-credential-helper"
	if err := shims.setGlobal("credential.helper", stale); err != nil {
		t.Fatal(err)
	}
	_, out, _ := runPlumbing(t, "", "credential", "enable", "--dry-run", "--no-color")
	if !strings.Contains(out, "Set global credential.helper to '!") || !strings.Contains(out, "stale") {
		t.Fatalf("a stale ws value is replaced:\n%s", out)
	}
}

func TestCredentialEnableLeavesAForeignHelperAlone(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	if err := shims.setGlobal("credential.helper", "store"); err != nil {
		t.Fatal(err)
	}
	// Not a ws helper: enable sets ws's (the plan names it); disable leaves the foreign one.
	_, out, _ := runPlumbing(t, "", "credential", "disable", "--no-color")
	if !strings.Contains(out, "not enabled") {
		t.Fatalf("disable should find nothing to do:\n%s", out)
	}
	if got := gitGlobal(t, shims, "credential.helper"); got != "store" {
		t.Fatalf("a foreign helper was touched: %q", got)
	}
}

func TestCredentialEnableCreatesHostEntriesNeedingATerminal(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	workspace, _ := credWorkspace(t, shims, "https://gitlab.work.com/team/api.git")

	// One action per host whose HTTPS remotes resolve to no entry.
	_, out, _ := wsRun(t, workspace, "", "credential", "enable", "--dry-run", "--no-color")
	if !strings.Contains(out, "Create pass entry git/gitlab.work.com (host token for 1 remote)") {
		t.Fatalf("plan:\n%s", out)
	}

	// Without a terminal, the action fails (it would read garbage as the token).
	called := false
	oldInsert, oldTerm := insertPassInteractive, stdinIsTerminal
	t.Cleanup(func() { insertPassInteractive, stdinIsTerminal = oldInsert, oldTerm })
	insertPassInteractive = func(string) error { called = true; return nil }
	stdinIsTerminal = func() bool { return false }

	code, _, errOut := wsRun(t, workspace, "", "credential", "enable", "--quiet")
	if code != 3 {
		t.Fatalf("the helper actions ran and the entry failed: exit %d, want 3\n%s", code, errOut)
	}
	if called {
		t.Fatal("pass insert must not run without a terminal")
	}
	env := runJSON(t, workspace, []string{"credential", "enable"})
	var data struct {
		Actions []ActionStatus `json:"actions"`
	}
	_ = json.Unmarshal(env.Data, &data)
	failed := false
	for _, a := range data.Actions {
		if a.ID == "create-pass-entry-git/gitlab.work.com" && a.Status == "failed" && strings.Contains(a.Error, "needs a terminal") {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("expected the entry action to fail for want of a terminal: %s", env.Data)
	}

	// With a terminal, pass insert asks for the token.
	stdinIsTerminal = func() bool { return true }
	insertPassInteractive = func(entry string) error { called = true; shims.entry(t, entry, "tok\n"); return nil }
	if code, _, errOut = wsRun(t, workspace, "", "credential", "enable", "--quiet"); code != 0 || !called {
		t.Fatalf("with a terminal: exit %d called=%v\n%s", code, called, errOut)
	}
	if _, err := os.Stat(filepath.Join(shims.store, "git", "gitlab.work.com.gpg")); err != nil {
		t.Fatalf("the host entry should exist: %v", err)
	}
}

func TestCredentialEnableSkipsCoveredAndSSHRemotes(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.entry(t, "git/gitlab.work.com", "tok\n")
	workspace, repoDir := credWorkspace(t, shims, "https://gitlab.work.com/team/api.git")
	runGitLocal(t, repoDir, "remote", "add", "ssh", "git@github.com:me/x.git")
	_, out, _ := wsRun(t, workspace, "", "credential", "enable", "--dry-run", "--no-color")
	if strings.Contains(out, "Create pass entry") {
		t.Fatalf("a covered host and an SSH remote need no entry:\n%s", out)
	}
}

func TestCredentialEnableUpdatesStaleLocalHelper(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.entry(t, "git/gitlab.work.com", "tok\n")
	workspace, repoDir := credWorkspace(t, shims, "https://gitlab.work.com/team/api.git")
	runGitLocal(t, repoDir, "config", "credential.helper", "!/nowhere/ws-credential-helper")
	_, out, _ := wsRun(t, workspace, "", "credential", "enable", "--dry-run", "--no-color")
	if !strings.Contains(out, "Update credential.helper in "+repoDir) {
		t.Fatalf("plan:\n%s", out)
	}
	// A foreign local helper is the user's: left alone.
	runGitLocal(t, repoDir, "config", "credential.helper", "cache")
	_, out, _ = wsRun(t, workspace, "", "credential", "enable", "--dry-run", "--no-color")
	if strings.Contains(out, "Update credential.helper in") {
		t.Fatalf("a non-ws local helper must not be updated:\n%s", out)
	}
}

func TestCredentialDisable(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	workspace, repoDir := credWorkspace(t, shims, "https://gitlab.work.com/team/api.git")
	shims.entry(t, "git/gitlab.work.com", "tok\n")
	if code, o, e := wsRun(t, workspace, "", "credential", "enable", "--quiet"); code != 0 {
		t.Fatalf("enable: %d %s%s", code, o, e)
	}
	wrapper := filepath.Join(shims.home, ".local", "bin", "ws-credential-helper")
	runGitLocal(t, repoDir, "config", "credential.helper", "!"+wrapper)

	_, out, _ := wsRun(t, workspace, "", "credential", "disable", "--dry-run", "--no-color")
	for _, want := range []string{
		"Remove credential.helper from global git config",
		"Remove credential.useHttpPath from global git config",
		"Remove credential.helper from " + repoDir + " (local config)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
	if code, _, e := wsRun(t, workspace, "", "credential", "disable", "--quiet"); code != 0 {
		t.Fatalf("disable: %d %s", code, e)
	}
	if gitGlobal(t, shims, "credential.helper") != "" || gitGlobal(t, shims, "credential.useHttpPath") != "" {
		t.Fatal("the global settings should be gone")
	}
	if gitConfigGetLocal(repoDir, "credential.helper") != "" {
		t.Fatal("the local override should be gone")
	}
	if _, err := os.Stat(wrapper); err != nil {
		t.Fatal("the wrapper script stays")
	}

	// Not enabled: nothing to do, exit 0 (it used to exit 1).
	code, out, _ := wsRun(t, workspace, "", "credential", "disable")
	if code != 0 || !strings.Contains(out, "not enabled") {
		t.Fatalf("second disable: code=%d out=%q", code, out)
	}
	env := runJSON(t, workspace, []string{"credential", "disable"})
	if env.Command != "credential.disable" {
		t.Fatalf("command %q", env.Command)
	}
}

func TestCredentialStatusJSON(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	workspace, repoDir := credWorkspace(t, shims, "https://gitlab.work.com/team/api.git")
	runGitLocal(t, repoDir, "remote", "add", "ssh", "git@github.com:me/x.git")
	shims.entry(t, "git/gitlab.work.com/team/api", "tok\n")

	env := runJSON(t, workspace, []string{"credential", "status"})
	if env.Command != "credential.status" {
		t.Fatalf("command %q", env.Command)
	}
	var data struct {
		Connected      bool           `json:"connected"`
		Helper         string         `json:"helper"`
		Pass           map[string]any `json:"pass"`
		Remotes        []remoteEntry  `json:"remotes"`
		LocalOverrides []any          `json:"local_overrides"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Connected || data.Helper != "" {
		t.Fatalf("not enabled yet: %+v", data)
	}
	if data.Pass["initialized"] != true || data.Pass["pass_installed"] != true {
		t.Fatalf("pass health: %v", data.Pass)
	}
	if len(data.Remotes) != 2 || data.LocalOverrides == nil {
		t.Fatalf("remotes/overrides: %s", env.Data)
	}
	var https, ssh remoteEntry
	for _, r := range data.Remotes {
		if r.Transport == "https" {
			https = r
		} else {
			ssh = r
		}
	}
	// useHttpPath is off, so the repo token is never asked for: it is ignored.
	if !https.HelperUsed || https.Exists || https.IgnoredEntry != "git/gitlab.work.com/team/api" {
		t.Fatalf("https remote: %+v", https)
	}
	if ssh.HelperUsed {
		t.Fatalf("ssh remote: %+v", ssh)
	}
}

func TestCredentialStatusText(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	workspace, _ := credWorkspace(t, shims, "https://gitlab.work.com/team/api.git")
	_, out, _ := wsRun(t, workspace, "", "credential", "status", "--no-color")
	for _, want := range []string{"DISCONNECTED", "no credential", "Next: ws credential enable"} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "git-credential-helper setup") || strings.Contains(out, "ws secret setup") {
		t.Fatalf("status names an old command:\n%s", out)
	}

	shims.entry(t, "git/gitlab.work.com", "tok\n")
	if code, o, e := wsRun(t, workspace, "", "credential", "enable", "--quiet"); code != 0 {
		t.Fatalf("enable: %d %s%s", code, o, e)
	}
	_, out, _ = wsRun(t, workspace, "", "credential", "status", "--no-color")
	if strings.Contains(out, "Next:") || !strings.Contains(out, "CONNECTED") {
		t.Fatalf("a fully enabled setup has nothing next:\n%s", out)
	}
}

func TestOldCredentialCommandsAreAliases(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	for old, want := range map[string]string{
		"setup":      `"credential setup" is now "credential enable"`,
		"disconnect": `"credential disconnect" is now "credential disable"`,
	} {
		_, _, errOut := runPlumbing(t, "", "credential", old, "--dry-run")
		if !strings.Contains(errOut, want) {
			t.Errorf("credential %s: stderr %q lacks %q", old, errOut, want)
		}
	}
	_, _, errOut := runPlumbing(t, "", "git-credential-helper", "status", "--no-color")
	if !strings.Contains(errOut, `"git-credential-helper status" is now "credential status"`) {
		t.Errorf("stderr %q", errOut)
	}
}

func TestExtractHost(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/user/repo.git", "github.com"},
		{"git@github.com:user/repo.git", "github.com"},
		{"ssh://git@gitlab.com/user/repo.git", "gitlab.com"},
		{"https://gitlab.work.com:8443/project.git", "gitlab.work.com"},
		{"git@bitbucket.org:team/repo.git", "bitbucket.org"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := extractHost(tt.url)
			if got != tt.want {
				t.Errorf("extractHost(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestExtractRepoPath(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/user/repo.git", "user/repo"},
		{"git@github.com:user/repo.git", "user/repo"},
		{"ssh://git@gitlab.com/user/repo.git", "user/repo"},
		{"https://github.com/org/project", "org/project"},
		{"git@bitbucket.org:team/app.git", "team/app"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := extractRepoPath(tt.url)
			if got != tt.want {
				t.Errorf("extractRepoPath(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestHelperPathStatusEmpty(t *testing.T) {
	label, detail, connected := helperPathStatus("", false)
	if connected {
		t.Fatal("expected disconnected for empty helper")
	}
	if !strings.Contains(strings.ToLower(label), "disconnected") {
		t.Fatalf("expected DISCONNECTED label, got: %s", label)
	}
	if detail != "" {
		t.Fatalf("expected empty detail, got: %s", detail)
	}
}

func TestHelperPathStatusNonWs(t *testing.T) {
	label, detail, connected := helperPathStatus("store", false)
	if connected {
		t.Fatal("expected disconnected for non-ws helper")
	}
	if !strings.Contains(strings.ToLower(label), "disconnected") {
		t.Fatalf("expected DISCONNECTED label, got: %s", label)
	}
	if !strings.Contains(detail, "not a ws helper") {
		t.Fatalf("expected 'not a ws helper' detail, got: %s", detail)
	}
}

func TestHelperPathStatusStaleBinary(t *testing.T) {
	helper := "!/nonexistent/path/to/ws git-credential-helper"
	label, detail, connected := helperPathStatus(helper, false)
	if connected {
		t.Fatal("expected disconnected for stale binary path")
	}
	if !strings.Contains(strings.ToLower(label), "stale") {
		t.Fatalf("expected STALE label, got: %s", label)
	}
	if !strings.Contains(detail, "binary not found") {
		t.Fatalf("expected 'binary not found' in detail, got: %s", detail)
	}
}

func TestHelperPathStatusValidBinary(t *testing.T) {
	// Create a temp file that acts as a "binary".
	tmpDir := t.TempDir()
	fakeBin := filepath.Join(tmpDir, "ws")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	helper := "!" + fakeBin + " git-credential-helper"
	label, _, connected := helperPathStatus(helper, false)
	if !connected {
		t.Fatalf("expected connected for valid binary, got label: %s", label)
	}
	if !strings.Contains(strings.ToLower(label), "connected") {
		t.Fatalf("expected CONNECTED label, got: %s", label)
	}
}

func TestGitConfigGetLocalEmpty(t *testing.T) {
	// Running against a non-repo directory should return empty.
	tmpDir := t.TempDir()
	got := gitConfigGetLocal(tmpDir, "credential.helper")
	if got != "" {
		t.Fatalf("expected empty for non-repo, got: %q", got)
	}
}

// credentialPassStore creates an initialized pass store containing the given
// entries (empty .gpg files — enough for existence checks).
func credentialPassStore(t *testing.T, entries ...string) {
	t.Helper()
	store := t.TempDir()
	t.Setenv("PASSWORD_STORE_DIR", store)
	if err := os.WriteFile(filepath.Join(store, ".gpg-id"), []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		p := filepath.Join(store, e+".gpg")
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func mustRemoteEntry(t *testing.T, rawURL string) remoteEntry {
	t.Helper()
	e, ok := newRemoteEntry(rawURL)
	if !ok {
		t.Fatalf("newRemoteEntry(%q): not ok", rawURL)
	}
	return e
}

func TestNewRemoteEntry(t *testing.T) {
	tests := []struct {
		url, label, transport string
		helperUsed            bool
	}{
		{"https://github.com/me/repo.git", "github.com/me/repo", "https", true},
		{"http://git.local/team/app", "git.local/team/app", "http", true},
		{"git@github.com:me/repo.git", "github.com/me/repo", "ssh", false},
		{"ssh://git@github.com/me/repo.git", "github.com/me/repo", "ssh", false},
	}
	for _, tt := range tests {
		e := mustRemoteEntry(t, tt.url)
		if e.Label() != tt.label || e.Transport != tt.transport || e.HelperUsed != tt.helperUsed {
			t.Errorf("newRemoteEntry(%q) = {label %q, transport %q, helperUsed %v}, want {%q, %q, %v}",
				tt.url, e.Label(), e.Transport, e.HelperUsed, tt.label, tt.transport, tt.helperUsed)
		}
	}
	if _, ok := newRemoteEntry("/srv/git/local.git"); ok {
		t.Error("expected local-path remote to be skipped")
	}
}

func TestResolveRemoteEntry(t *testing.T) {
	credentialPassStore(t, "git/github.com", "git/github.com/me/scoped")

	tests := []struct {
		name        string
		url         string
		useHTTPPath bool
		wantEntry   string
		wantScope   string
		wantIgnored string
	}{
		{"repo token", "https://github.com/me/scoped.git", true, "git/github.com/me/scoped", "repo", ""},
		{"host token fallback", "https://github.com/me/shared.git", true, "git/github.com", "host", ""},
		{"no credential", "https://gitlab.com/me/app.git", true, "", "", ""},
		{"repo token ignored without useHttpPath", "https://github.com/me/scoped.git", false, "git/github.com", "host", "git/github.com/me/scoped"},
		{"ssh untouched", "git@github.com:me/scoped.git", true, "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := resolveRemoteEntry(mustRemoteEntry(t, tt.url), tt.useHTTPPath)
			if e.PassEntry != tt.wantEntry || e.Scope != tt.wantScope || e.IgnoredEntry != tt.wantIgnored {
				t.Errorf("got {entry %q, scope %q, ignored %q}, want {%q, %q, %q}",
					e.PassEntry, e.Scope, e.IgnoredEntry, tt.wantEntry, tt.wantScope, tt.wantIgnored)
			}
			if e.Exists != (tt.wantEntry != "") {
				t.Errorf("Exists = %v, want %v", e.Exists, tt.wantEntry != "")
			}
		})
	}
}

func TestPrintRemoteEntries(t *testing.T) {
	credentialPassStore(t, "git/github.com", "git/github.com/me/scoped")
	remotes := []remoteEntry{
		resolveRemoteEntry(mustRemoteEntry(t, "https://github.com/me/shared.git"), true),
		resolveRemoteEntry(mustRemoteEntry(t, "https://github.com/me/scoped.git"), true),
		resolveRemoteEntry(mustRemoteEntry(t, "https://gitlab.com/me/app.git"), true),
		resolveRemoteEntry(mustRemoteEntry(t, "git@github.com:me/ssh.git"), true),
	}
	var out bytes.Buffer
	printRemoteEntries(&out, remotes, true)
	got := out.String()

	for _, want := range []string{
		"github.com/me/shared  git/github.com  (host token)",
		"github.com/me/scoped  git/github.com/me/scoped  (repo token)",
		"gitlab.com/me/app     no credential (needs git/gitlab.com or git/gitlab.com/me/app)",
		"github.com/me/ssh     ssh — credential helper not used",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "missing") {
		t.Errorf("host-token remotes must not be reported missing:\n%s", got)
	}
}

func TestPrintRemoteEntriesIgnoredRepoToken(t *testing.T) {
	credentialPassStore(t, "git/github.com", "git/github.com/me/scoped")
	remotes := []remoteEntry{resolveRemoteEntry(mustRemoteEntry(t, "https://github.com/me/scoped.git"), false)}
	var out bytes.Buffer
	printRemoteEntries(&out, remotes, true)
	if want := "git/github.com/me/scoped ignored — credential.useHttpPath is off"; !strings.Contains(out.String(), want) {
		t.Errorf("output missing %q:\n%s", want, out.String())
	}
}

func TestUseHTTPPathFor(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	// Not in a git checkout: in a worktree, .git points outside the test container.
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))

	if useHTTPPathFor("github.com") {
		t.Fatal("expected false with no config")
	}
	if err := os.WriteFile(cfg, []byte("[credential]\n\tuseHttpPath = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !useHTTPPathFor("github.com") {
		t.Fatal("expected true with global credential.useHttpPath")
	}
	if err := os.WriteFile(cfg, []byte("[credential \"https://gitlab.com\"]\n\tuseHttpPath = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !useHTTPPathFor("gitlab.com") || useHTTPPathFor("github.com") {
		t.Fatal("expected URL-scoped useHttpPath to apply only to gitlab.com")
	}
}

func TestHelperPathStatusWrapper(t *testing.T) {
	tmpDir := t.TempDir()
	wrapper := filepath.Join(tmpDir, "ws-credential-helper")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := helperPathStatus("!"+wrapper, false); !ok {
		t.Fatal("expected bare wrapper to be connected")
	}
	label, detail, ok := helperPathStatus("!"+wrapper+" git-credential-helper", false)
	if ok || !strings.Contains(strings.ToLower(label), "stale") || !strings.Contains(detail, "duplicate") {
		t.Fatalf("expected legacy wrapper form to be stale, got %s / %s", label, detail)
	}
}
