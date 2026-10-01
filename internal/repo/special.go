package repo

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/config"
)

// SpecialRepos returns repos managed by ws itself (dotfiles and pass store)
// that should always be included in repo operations regardless of configured roots.
func SpecialRepos(workspacePath string) []Repository {
	var special []Repository

	// Dotfiles repo: <workspace>/ws/dotfiles/
	dotfilesPath := filepath.Join(workspacePath, "ws", "dotfiles")
	if isGitRepo(dotfilesPath) {
		special = append(special, Repository{Path: dotfilesPath})
	}

	// Pass store: $PASSWORD_STORE_DIR or ~/.password-store
	passStorePath := ""
	if env := os.Getenv("PASSWORD_STORE_DIR"); env != "" {
		if abs, err := config.ExpandUserPath(env); err == nil {
			passStorePath = abs
		}
	} else if home, err := os.UserHomeDir(); err == nil {
		passStorePath = filepath.Join(home, ".password-store")
	}
	if passStorePath != "" && isGitRepo(passStorePath) {
		special = append(special, Repository{Path: passStorePath})
	}

	return special
}

func isGitRepo(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && info.IsDir()
}

// AppendMissing appends repos from extra that are not already in repos.
func AppendMissing(repos []Repository, extra []Repository) []Repository {
	seen := make(map[string]struct{}, len(repos))
	for _, r := range repos {
		seen[filepath.Clean(r.Path)] = struct{}{}
	}
	for _, r := range extra {
		if _, ok := seen[filepath.Clean(r.Path)]; !ok {
			repos = append(repos, r)
		}
	}
	return repos
}

// DescribeSync is the human description of a sync action; short is the repo
// path as the caller wants it displayed.
func DescribeSync(sp SyncPlan, short string, rebase bool) string {
	mode := "merge"
	if rebase {
		mode = "rebase"
	}
	switch sp.Strategy {
	case SyncPull:
		if sp.Status.Dirty {
			return fmt.Sprintf("Commit, pull (%s), push %s  (%s)", mode, short, sp.Detail)
		}
		return fmt.Sprintf("Pull %s  (%s, ff)", short, sp.Detail)
	case SyncPush:
		return fmt.Sprintf("Push %s  (%s)", short, sp.Detail)
	case SyncCommitPush:
		return fmt.Sprintf("Commit and push %s  (%s)", short, sp.Detail)
	case SyncPullPush:
		if sp.Status.Dirty {
			return fmt.Sprintf("Commit, pull (%s), push %s  (%s)", mode, short, sp.Detail)
		}
		return fmt.Sprintf("Pull (%s) + push %s  (%s)", mode, short, sp.Detail)
	default:
		return fmt.Sprintf("Sync %s", short)
	}
}
