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

func TestRefusal(t *testing.T) {
	cases := []struct {
		name string
		v    VisibilityResult
		want error
	}{
		{"private", VisibilityResult{Checked: true, Private: true}, nil},
		{"public", VisibilityResult{Checked: true, Private: false}, ErrPublicRepository},
		// An authenticated 404 leaves Private false. Reading Private alone calls
		// a missing repository "public"; the spec says "repository not found".
		{"authenticated 404", VisibilityResult{Checked: true, Private: false, Error: "repository not found"}, ErrRepositoryNotFound},
		{"unauthenticated 404 is undetermined", VisibilityResult{Warning: "repository not found (may be private)"}, nil},
		{"unknown host", VisibilityResult{Provider: "unknown", Warning: "cannot verify"}, nil},
		{"network error", VisibilityResult{Error: "API request failed: timeout"}, nil},
	}
	for _, c := range cases {
		if got := c.v.Refusal(); got != c.want {
			t.Errorf("%s: Refusal() = %v, want %v", c.name, got, c.want)
		}
	}
	if n := (VisibilityResult{Warning: "w"}).Notice(); n != "w" {
		t.Errorf("Notice: %q", n)
	}
	if n := (VisibilityResult{Error: "boom"}).Notice(); n != "visibility check failed: boom" {
		t.Errorf("Notice: %q", n)
	}
	if n := (VisibilityResult{Checked: true, Private: true}).Notice(); n != "" {
		t.Errorf("Notice: %q", n)
	}
}
