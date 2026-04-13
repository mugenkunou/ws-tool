package repo

import (
	"testing"
)

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		owner    string
		repo     string
		provider string
	}{
		{"github https", "https://github.com/user/repo.git", "user", "repo", "github"},
		{"github https no .git", "https://github.com/user/repo", "user", "repo", "github"},
		{"github ssh", "git@github.com:user/repo.git", "user", "repo", "github"},
		{"github ssh no .git", "git@github.com:user/repo", "user", "repo", "github"},
		{"gitlab https", "https://gitlab.com/user/repo.git", "user", "repo", "gitlab"},
		{"gitlab ssh", "git@gitlab.com:user/repo.git", "user", "repo", "gitlab"},
		{"bitbucket https", "https://bitbucket.org/user/repo.git", "user", "repo", "bitbucket"},
		{"bitbucket ssh", "git@bitbucket.org:user/repo.git", "user", "repo", "bitbucket"},
		{"unknown host", "https://git.example.com/user/repo.git", "user", "repo", "unknown"},
		{"self-hosted ssh", "git@git.company.com:team/project.git", "team", "project", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repoName, provider := parseRemoteURL(tt.url)
			if owner != tt.owner {
				t.Errorf("owner: got %q, want %q", owner, tt.owner)
			}
			if repoName != tt.repo {
				t.Errorf("repo: got %q, want %q", repoName, tt.repo)
			}
			if provider != tt.provider {
				t.Errorf("provider: got %q, want %q", provider, tt.provider)
			}
		})
	}
}

func TestCheckRepoVisibilityUnknownHost(t *testing.T) {
	result := CheckRepoVisibility("https://git.selfhosted.com/user/repo.git", "")
	if result.Provider != "unknown" {
		t.Errorf("expected provider=unknown, got %s", result.Provider)
	}
	if result.Checked {
		t.Error("expected checked=false for unknown host")
	}
	if result.Warning == "" {
		t.Error("expected a warning for unknown host")
	}
}
