package secret

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
