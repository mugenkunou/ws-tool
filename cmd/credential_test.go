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
	// No args → silent exit 0 per git credential helper spec.
	var out, errOut bytes.Buffer
	code := Execute([]string{"git-credential-helper"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0 for no args (silent), got %d", code)
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
