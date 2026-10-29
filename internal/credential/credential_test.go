package credential

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCredentialInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  CredentialRequest
	}{
		{
			name:  "typical https",
			input: "protocol=https\nhost=github.com\n\n",
			want:  CredentialRequest{Protocol: "https", Host: "github.com"},
		},
		{
			name:  "with path",
			input: "protocol=https\nhost=github.com\npath=myorg/repo.git\n\n",
			want:  CredentialRequest{Protocol: "https", Host: "github.com", Path: "myorg/repo.git"},
		},
		{
			name:  "with username",
			input: "protocol=https\nhost=github.com\nusername=bob\n\n",
			want:  CredentialRequest{Protocol: "https", Host: "github.com", Username: "bob"},
		},
		{
			name:  "empty input",
			input: "\n",
			want:  CredentialRequest{},
		},
		{
			name:  "eof without blank line",
			input: "protocol=https\nhost=gitlab.com",
			want:  CredentialRequest{Protocol: "https", Host: "gitlab.com"},
		},
		{
			name:  "host with port",
			input: "protocol=https\nhost=gitlab.work.com:8443\n\n",
			want:  CredentialRequest{Protocol: "https", Host: "gitlab.work.com:8443"},
		},
		{
			name:  "ignores unknown keys",
			input: "protocol=https\nhost=github.com\nfoo=bar\n\n",
			want:  CredentialRequest{Protocol: "https", Host: "github.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseCredentialInput(strings.NewReader(tt.input))
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParsePassEntry(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    CredentialResponse
	}{
		{
			name:    "password only",
			content: "s3cret\n",
			want:    CredentialResponse{Password: "s3cret"},
		},
		{
			name:    "password and username",
			content: "ghp_abc123\nusername: bob\nurl: https://github.com\n",
			want:    CredentialResponse{Password: "ghp_abc123", Username: "bob"},
		},
		{
			name:    "user prefix",
			content: "mytoken\nuser: alice\n",
			want:    CredentialResponse{Password: "mytoken", Username: "alice"},
		},
		{
			name:    "login prefix",
			content: "pw123\nlogin: deploy-bot\n",
			want:    CredentialResponse{Password: "pw123", Username: "deploy-bot"},
		},
		{
			name:    "case insensitive username",
			content: "token\nUsername: MixedCase\n",
			want:    CredentialResponse{Password: "token", Username: "MixedCase"},
		},
		{
			name:    "equals separator username",
			content: "token\nusername=bob\n",
			want:    CredentialResponse{Password: "token", Username: "bob"},
		},
		{
			name:    "empty content",
			content: "",
			want:    CredentialResponse{},
		},
		{
			name:    "password with trailing whitespace",
			content: "secret  \n",
			want:    CredentialResponse{Password: "secret"},
		},
		{
			name:    "no username line",
			content: "onlypassword\nurl: https://example.com\nnotes: some note\n",
			want:    CredentialResponse{Password: "onlypassword"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParsePassEntry(tt.content)
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFormatCredentialOutput(t *testing.T) {
	tests := []struct {
		name string
		resp CredentialResponse
		want string
	}{
		{
			name: "both fields",
			resp: CredentialResponse{Password: "secret", Username: "bob"},
			want: "password=secret\nusername=bob\n",
		},
		{
			name: "password only",
			resp: CredentialResponse{Password: "token"},
			want: "password=token\n",
		},
		{
			name: "empty response",
			resp: CredentialResponse{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf strings.Builder
			FormatCredentialOutput(&buf, tt.resp)
			if buf.String() != tt.want {
				t.Errorf("got %q, want %q", buf.String(), tt.want)
			}
		})
	}
}

func TestLookupCandidateOrder(t *testing.T) {
	// We can't test actual pass lookup without a store, but we can verify
	// that LookupCredential doesn't panic on empty host.
	resp := LookupCredential(CredentialRequest{})
	if resp.Password != "" || resp.Username != "" {
		t.Errorf("expected empty response for empty host, got %+v", resp)
	}
}

func TestCredentialCandidates(t *testing.T) {
	tests := []struct {
		host, path string
		want       []string
	}{
		{"", "owner/repo", nil},
		{"github.com", "", []string{"git/github.com"}},
		{"github.com", "owner/repo", []string{"git/github.com/owner/repo", "git/github.com"}},
		{"github.com", "owner/repo.git", []string{"git/github.com/owner/repo", "git/github.com"}},
		{"github.com", "owner/repo/", []string{"git/github.com/owner/repo", "git/github.com"}},
	}
	for _, tt := range tests {
		got := CredentialCandidates(tt.host, tt.path)
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("CredentialCandidates(%q, %q) = %v, want %v", tt.host, tt.path, got, tt.want)
		}
	}
}

// fakePassStore creates an initialized pass store containing the given
// entries (empty .gpg files — enough for existence checks).
func fakePassStore(t *testing.T, entries ...string) {
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

func TestResolveCredentialEntry(t *testing.T) {
	fakePassStore(t, "git/github.com", "git/github.com/me/scoped")

	tests := []struct {
		host, path, want string
	}{
		{"github.com", "me/scoped", "git/github.com/me/scoped"},
		{"github.com", "me/scoped.git", "git/github.com/me/scoped"},
		{"github.com", "me/other", "git/github.com"}, // falls back to host token
		{"github.com", "", "git/github.com"},
		{"gitlab.com", "me/scoped", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		if got := ResolveCredentialEntry(tt.host, tt.path); got != tt.want {
			t.Errorf("ResolveCredentialEntry(%q, %q) = %q, want %q", tt.host, tt.path, got, tt.want)
		}
	}
}

// fakePassCommand puts a `pass` on PATH that shows only the given entries.
func fakePassCommand(t *testing.T, entries map[string]string) {
	t.Helper()
	dir := t.TempDir()
	var cases strings.Builder
	for entry, content := range entries {
		cases.WriteString("  " + entry + ") printf '%s\\n' '" + content + "' ;;\n")
	}
	script := "#!/bin/sh\n[ \"$1\" = show ] || exit 2\ncase \"$2\" in\n" + cases.String() + "  *) exit 1 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "pass"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// fakeCredentialHelper points git at a credential helper that answers for the
// per-repo path me/configs.git with repo-token and for any other path on
// github.com with host-token, like the ws helper's candidate order.
func fakeCredentialHelper(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n[ \"$1\" = get ] || exit 0\ninput=$(cat)\ncase \"$input\" in\n" +
		"  *path=me/configs.git*) echo password=repo-token ;;\n" +
		"  *host=github.com*) echo password=host-token ;;\n" +
		"esac\n"
	helper := filepath.Join(dir, "helper")
	if err := os.WriteFile(helper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(cfg, []byte("[credential]\n\thelper = "+helper+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

// TestResolveGitTokenAsksGit: the visibility check used its own shorter lookup
// (git/<host> only) and missed per-repo entries that a push would use.
func TestResolveGitTokenAsksGit(t *testing.T) {
	fakePassCommand(t, map[string]string{"work/gh-api": "explicit-token"})
	fakeCredentialHelper(t)
	if got := ResolveGitToken("https://github.com/me/configs.git", "work/gh-api"); got != "explicit-token" {
		t.Errorf("explicit pass_entry: got %q, want explicit-token", got)
	}
	if got := ResolveGitToken("https://github.com/me/configs.git", ""); got != "repo-token" {
		t.Errorf("per-repo entry: got %q, want repo-token", got)
	}
	if got := ResolveGitToken("https://github.com/me/other.git", "missing/entry"); got != "host-token" {
		t.Errorf("a missing pass_entry falls back to git: got %q, want host-token", got)
	}
	if got := ResolveGitToken("https://gitlab.com/me/x.git", ""); got != "" {
		t.Errorf("no credential: got %q, want empty", got)
	}
	if got := ResolveGitToken("git@github.com:me/configs.git", ""); got != "" {
		t.Errorf("ssh remote: got %q, want empty", got)
	}
}
