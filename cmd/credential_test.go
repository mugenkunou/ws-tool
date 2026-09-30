package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialHelperHelp(t *testing.T) {
	// --help is an unknown git credential operation → silent exit 0.
	var out, errOut bytes.Buffer
	code := Execute([]string{"git-credential-helper", "--help"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, errOut.String())
	}
}

func TestCredentialGetEmptyStdin(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute([]string{"git-credential-helper", "get"}, strings.NewReader("\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
}

func TestCredentialStoreNoop(t *testing.T) {
	var out, errOut bytes.Buffer
	stdin := "protocol=https\nhost=example.com\nusername=bob\npassword=secret\n\n"
	code := Execute([]string{"git-credential-helper", "store"}, strings.NewReader(stdin), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0 for store (noop), got %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no output for store, got: %s", out.String())
	}
}

func TestCredentialEraseNoop(t *testing.T) {
	var out, errOut bytes.Buffer
	stdin := "protocol=https\nhost=example.com\n\n"
	code := Execute([]string{"git-credential-helper", "erase"}, strings.NewReader(stdin), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0 for erase (noop), got %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no output for erase, got: %s", out.String())
	}
}

func TestCredentialUnknownOpSilent(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute([]string{"git-credential-helper", "future-op"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0 for unknown op (silent ignore), got %d", code)
	}
}

func TestCredentialNoArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute([]string{"git-credential-helper"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0 for no args (shows help), got %d", code)
	}
	if !strings.Contains(out.String(), "ws git-credential-helper") {
		t.Fatalf("expected usage in stdout, got: %s", out.String())
	}
}

func TestCredentialHelperUnknownSubcommand(t *testing.T) {
	// Unknown subcommands are silently ignored per git credential helper spec.
	var out, errOut bytes.Buffer
	code := Execute([]string{"git-credential-helper", "future-op-v2"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0 for unknown op (silent ignore), got %d", code)
	}
}

func TestCredentialLegacyAlias(t *testing.T) {
	// "ws credential get" should still work (backward compat).
	var out, errOut bytes.Buffer
	code := Execute([]string{"credential", "get"}, strings.NewReader("\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0 for legacy alias, got %d", code)
	}
}

func TestCredentialNoArgsExitsNonZero(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute([]string{"git-credential-helper"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0 (shows help), got %d", code)
	}
	if !strings.Contains(out.String(), "ws git-credential-helper") {
		t.Fatalf("expected usage in stdout, got: %s", out.String())
	}
}

func TestCredentialNoArgsStdoutEmpty(t *testing.T) {
	var out, errOut bytes.Buffer
	Execute([]string{"git-credential-helper"}, strings.NewReader(""), &out, &errOut)
	if out.Len() == 0 {
		t.Fatalf("expected help in stdout, got empty output")
	}
}

func TestCredentialAliasNoArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute([]string{"credential"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0 for alias with no args (shows help), got %d", code)
	}
	if !strings.Contains(out.String(), "ws git-credential-helper") {
		t.Fatalf("expected usage in stdout, got: %s", out.String())
	}
}

func TestCredentialStatusHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute([]string{"git-credential-helper", "status", "--help"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "status") {
		t.Fatalf("expected status help text, got: %s", out.String())
	}
}

func TestCredentialDisconnectNotConnected(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute([]string{"git-credential-helper", "disconnect"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 1 {
		t.Fatalf("expected exit 1 for disconnect when not connected, got %d", code)
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

func TestCredentialDisconnectHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute([]string{"git-credential-helper", "disconnect", "--help"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "disconnect") {
		t.Fatalf("expected disconnect help text, got: %s", out.String())
	}
}

func TestCredentialHelperInTopLevelHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute([]string{"--help"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out.String(), "git-credential-helper") {
		t.Fatalf("expected git-credential-helper in help output, got: %s", out.String())
	}
}

func TestCredentialHelperHelpOutput(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute([]string{"git-credential-helper", "--help"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: stderr=%s", code, errOut.String())
	}
	output := out.String()
	// Should show both user commands and git plumbing.
	if !strings.Contains(output, "User commands") {
		t.Fatalf("expected 'User commands' in help, got: %s", output)
	}
	if !strings.Contains(output, "Git plumbing") {
		t.Fatalf("expected 'Git plumbing' in help, got: %s", output)
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
