package repo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// VisibilityResult describes the outcome of a repository visibility check.
type VisibilityResult struct {
	Provider string `json:"provider"`          // "github", "gitlab", "bitbucket", "unknown"
	Private  bool   `json:"private"`           // true if repo confirmed private
	Checked  bool   `json:"checked"`           // true if API was actually queried
	Warning  string `json:"warning,omitempty"` // set for unknown providers
	Error    string `json:"error,omitempty"`   // set on check failure
}

// CheckRepoVisibility checks whether a remote repository is private.
// For known providers (GitHub, GitLab, Bitbucket) it uses the public API.
// For unknown providers it returns a warning but allows the operation.
//
// token is optional; passing it allows checking private repos on GitHub
// (unauthenticated 404 is ambiguous — could be private or nonexistent).
func CheckRepoVisibility(remoteURL string, token string) VisibilityResult {
	owner, repoName, provider := parseRemoteURL(remoteURL)
	if provider == "unknown" || owner == "" || repoName == "" {
		return VisibilityResult{
			Provider: "unknown",
			Private:  false,
			Checked:  false,
			Warning:  "cannot verify repository visibility for this host — ensure the remote is private",
		}
	}

	switch provider {
	case "github":
		return checkGitHub(owner, repoName, token)
	case "gitlab":
		return checkGitLab(owner, repoName, token)
	case "bitbucket":
		return checkBitbucket(owner, repoName, token)
	default:
		return VisibilityResult{
			Provider: provider,
			Private:  false,
			Checked:  false,
			Warning:  "cannot verify repository visibility for this host — ensure the remote is private",
		}
	}
}

// parseRemoteURL extracts owner, repo name, and provider from HTTPS or SSH URLs.
func parseRemoteURL(rawURL string) (owner, repoName, provider string) {
	rawURL = strings.TrimSpace(rawURL)

	// Handle SSH format: git@github.com:owner/repo.git
	if strings.HasPrefix(rawURL, "git@") || strings.Contains(rawURL, "@") && strings.Contains(rawURL, ":") && !strings.Contains(rawURL, "://") {
		parts := strings.SplitN(rawURL, ":", 2)
		if len(parts) != 2 {
			return "", "", "unknown"
		}
		host := parts[0]
		host = host[strings.Index(host, "@")+1:]
		provider = identifyProvider(host)
		path := strings.TrimSuffix(parts[1], ".git")
		path = strings.TrimPrefix(path, "/")
		segments := strings.SplitN(path, "/", 2)
		if len(segments) != 2 {
			return "", "", provider
		}
		return segments[0], segments[1], provider
	}

	// Handle HTTPS format.
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "unknown"
	}
	provider = identifyProvider(u.Hostname())
	path := strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	segments := strings.SplitN(path, "/", 2)
	if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
		return "", "", provider
	}
	return segments[0], segments[1], provider
}

func identifyProvider(host string) string {
	host = strings.ToLower(host)
	switch {
	case host == "github.com" || host == "www.github.com":
		return "github"
	case host == "gitlab.com" || host == "www.gitlab.com":
		return "gitlab"
	case host == "bitbucket.org" || host == "www.bitbucket.org":
		return "bitbucket"
	default:
		return "unknown"
	}
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

func checkGitHub(owner, repoName, token string) VisibilityResult {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repoName)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return VisibilityResult{Provider: "github", Checked: false, Error: err.Error()}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return VisibilityResult{Provider: "github", Checked: false, Error: fmt.Sprintf("API request failed: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		if token == "" {
			return VisibilityResult{
				Provider: "github",
				Checked:  false,
				Warning:  "repository not found (may be private — provide a token for authenticated check)",
			}
		}
		return VisibilityResult{Provider: "github", Checked: true, Error: "repository not found"}
	}
	if resp.StatusCode != http.StatusOK {
		return VisibilityResult{Provider: "github", Checked: false, Error: fmt.Sprintf("API returned status %d", resp.StatusCode)}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return VisibilityResult{Provider: "github", Checked: false, Error: err.Error()}
	}
	var result struct {
		Private bool `json:"private"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return VisibilityResult{Provider: "github", Checked: false, Error: err.Error()}
	}
	return VisibilityResult{Provider: "github", Private: result.Private, Checked: true}
}

func checkGitLab(owner, repoName, token string) VisibilityResult {
	// GitLab uses URL-encoded project path: owner%2Frepo
	projectPath := url.PathEscape(owner + "/" + repoName)
	apiURL := fmt.Sprintf("https://gitlab.com/api/v4/projects/%s", projectPath)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return VisibilityResult{Provider: "gitlab", Checked: false, Error: err.Error()}
	}
	if token != "" {
		req.Header.Set("PRIVATE-TOKEN", token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return VisibilityResult{Provider: "gitlab", Checked: false, Error: fmt.Sprintf("API request failed: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		if token == "" {
			return VisibilityResult{
				Provider: "gitlab",
				Checked:  false,
				Warning:  "project not found (may be private — provide a token for authenticated check)",
			}
		}
		return VisibilityResult{Provider: "gitlab", Checked: true, Error: "project not found"}
	}
	if resp.StatusCode != http.StatusOK {
		return VisibilityResult{Provider: "gitlab", Checked: false, Error: fmt.Sprintf("API returned status %d", resp.StatusCode)}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return VisibilityResult{Provider: "gitlab", Checked: false, Error: err.Error()}
	}
	var result struct {
		Visibility string `json:"visibility"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return VisibilityResult{Provider: "gitlab", Checked: false, Error: err.Error()}
	}
	return VisibilityResult{Provider: "gitlab", Private: result.Visibility == "private", Checked: true}
}

func checkBitbucket(owner, repoName, token string) VisibilityResult {
	apiURL := fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s/%s", owner, repoName)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return VisibilityResult{Provider: "bitbucket", Checked: false, Error: err.Error()}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return VisibilityResult{Provider: "bitbucket", Checked: false, Error: fmt.Sprintf("API request failed: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return VisibilityResult{Provider: "bitbucket", Checked: true, Error: "repository not found"}
	}
	if resp.StatusCode != http.StatusOK {
		return VisibilityResult{Provider: "bitbucket", Checked: false, Error: fmt.Sprintf("API returned status %d", resp.StatusCode)}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return VisibilityResult{Provider: "bitbucket", Checked: false, Error: err.Error()}
	}
	var result struct {
		IsPrivate bool `json:"is_private"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return VisibilityResult{Provider: "bitbucket", Checked: false, Error: err.Error()}
	}
	return VisibilityResult{Provider: "bitbucket", Private: result.IsPrivate, Checked: true}
}

// ErrPublicRepository is returned when a dotfile remote is public.
var ErrPublicRepository = errors.New("ws requires dotfile Git remotes to be private repositories")
