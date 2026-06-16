package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Severity classifies how serious a hygiene finding is.
type Severity int

const (
	SeverityInfo  Severity = iota // informational; no action required
	SeverityWarn                  // should be fixed
	SeverityError                 // infrastructure failure
)

func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarn:
		return "warn"
	case SeverityError:
		return "error"
	default:
		return "unknown"
	}
}

// MarshalJSON makes Severity serialize as its string name.
func (s Severity) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// Finding is one hygiene observation for a single repository.
type Finding struct {
	Repo     string   `json:"repo"`
	Check    string   `json:"check"`
	Severity Severity `json:"severity"`
	Detail   string   `json:"detail"`
}

// DoctorOptions controls which checks Doctor runs.
type DoctorOptions struct {
	// Checks lists which check IDs to run. nil or empty means all checks.
	Checks []string
	// FetchStalenessDays is the threshold for the fetch-staleness check (default 14).
	FetchStalenessDays int
}

// Doctor runs hygiene checks across repos and returns all findings.
// The checks run in a deterministic order.
func Doctor(workspacePath string, repos []Repository, opts DoctorOptions) []Finding {
	checks := opts.Checks
	if len(checks) == 0 {
		checks = []string{"identity", "upstream", "default-branch", "fetch-staleness", "dirty"}
	}
	staleDays := opts.FetchStalenessDays
	if staleDays == 0 {
		staleDays = 14
	}

	checkSet := make(map[string]bool, len(checks))
	for _, c := range checks {
		checkSet[c] = true
	}

	var findings []Finding
	for _, r := range repos {
		absPath := r.Path
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(workspacePath, r.Path)
		}
		if checkSet["identity"] {
			findings = append(findings, checkIdentity(r.Path, absPath)...)
		}
		if checkSet["upstream"] {
			findings = append(findings, checkUpstream(r.Path, absPath)...)
		}
		if checkSet["default-branch"] {
			findings = append(findings, checkDefaultBranch(r.Path, absPath)...)
		}
		if checkSet["fetch-staleness"] {
			findings = append(findings, checkFetchStaleness(r.Path, absPath, staleDays)...)
		}
		if checkSet["dirty"] {
			findings = append(findings, checkDirty(r.Path, absPath)...)
		}
	}
	return findings
}

// checkIdentity verifies user.name and user.email are configured.
func checkIdentity(repoRelPath, repoPath string) []Finding {
	var findings []Finding

	localName := gitConfigGet(repoPath, "--local", "user.name")
	globalName := gitConfigGet(repoPath, "--global", "user.name")
	if localName == "" && globalName == "" {
		findings = append(findings, Finding{
			Repo:     repoRelPath,
			Check:    "identity",
			Severity: SeverityWarn,
			Detail:   "user.name not set (local or global)",
		})
	} else if localName == "" && globalName != "" {
		findings = append(findings, Finding{
			Repo:     repoRelPath,
			Check:    "identity",
			Severity: SeverityInfo,
			Detail:   "user.name not set locally (using global: " + globalName + ")",
		})
	}

	localEmail := gitConfigGet(repoPath, "--local", "user.email")
	globalEmail := gitConfigGet(repoPath, "--global", "user.email")
	if localEmail == "" && globalEmail == "" {
		findings = append(findings, Finding{
			Repo:     repoRelPath,
			Check:    "identity",
			Severity: SeverityWarn,
			Detail:   "user.email not set (local or global)",
		})
	} else if localEmail == "" && globalEmail != "" {
		findings = append(findings, Finding{
			Repo:     repoRelPath,
			Check:    "identity",
			Severity: SeverityInfo,
			Detail:   "user.email not set locally (using global: " + globalEmail + ")",
		})
	}

	return findings
}

// checkUpstream verifies the current branch has a tracking upstream.
func checkUpstream(repoRelPath, repoPath string) []Finding {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return []Finding{{
			Repo:     repoRelPath,
			Check:    "upstream",
			Severity: SeverityWarn,
			Detail:   "current branch has no tracking upstream",
		}}
	}
	return nil
}

// checkDefaultBranch verifies init.defaultBranch is set or origin/HEAD resolves.
func checkDefaultBranch(repoRelPath, repoPath string) []Finding {
	if gitConfigGet(repoPath, "--local", "init.defaultBranch") != "" {
		return nil
	}
	if gitConfigGet(repoPath, "--global", "init.defaultBranch") != "" {
		return nil
	}
	cmd := exec.Command("git", "-C", repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return []Finding{{
			Repo:     repoRelPath,
			Check:    "default-branch",
			Severity: SeverityInfo,
			Detail:   "init.defaultBranch not set and origin/HEAD not resolved",
		}}
	}
	return nil
}

// checkFetchStaleness checks whether the repository has been fetched recently.
func checkFetchStaleness(repoRelPath, repoPath string, maxDays int) []Finding {
	fetchHead := filepath.Join(repoPath, ".git", "FETCH_HEAD")
	fi, err := os.Stat(fetchHead)
	if err != nil {
		return []Finding{{
			Repo:     repoRelPath,
			Check:    "fetch-staleness",
			Severity: SeverityInfo,
			Detail:   "repository has never been fetched",
		}}
	}
	age := time.Since(fi.ModTime())
	if age > time.Duration(maxDays)*24*time.Hour {
		return []Finding{{
			Repo:     repoRelPath,
			Check:    "fetch-staleness",
			Severity: SeverityInfo,
			Detail:   fmt.Sprintf("last fetch was %.0f days ago (threshold: %d)", age.Hours()/24, maxDays),
		}}
	}
	return nil
}

// checkDirty checks for uncommitted changes.
func checkDirty(repoRelPath, repoPath string) []Finding {
	cmd := exec.Command("git", "-C", repoPath, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		return []Finding{{
			Repo:     repoRelPath,
			Check:    "dirty",
			Severity: SeverityWarn,
			Detail:   "uncommitted changes",
		}}
	}
	return nil
}

// gitConfigGet reads a single git config value. scope is "--local" or "--global".
func gitConfigGet(repoPath, scope, key string) string {
	args := []string{"-C", repoPath, "config", scope, key}
	cmd := exec.Command("git", args...)
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
