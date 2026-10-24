package repo

import (
	"fmt"
	"os"
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

// HygieneChecks lists every check ID Doctor knows, in run order.
// Dirty state is not a hygiene check: the list reports it in its state column.
var HygieneChecks = []string{"identity", "upstream", "default-branch", "fetch-staleness", "credentials"}

// FixHint is the end of every finding's detail: the command that fixes it.
func FixHint(repoPath string) string {
	return " (fix: ws repo configure " + repoPath + ")"
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
		checks = HygieneChecks
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
		if checkSet["credentials"] {
			findings = append(findings, checkCredentials(r.Path, absPath)...)
		}
	}
	// Every detail ends with its fix.
	for i := range findings {
		findings[i].Detail += FixHint(findings[i].Repo)
	}
	return findings
}

// checkCredentials reports an HTTPS remote when git has no credential helper
// (local, global or system), so every fetch or push would need a password.
func checkCredentials(repoRelPath, repoPath string) []Finding {
	remote := ""
	urls, err := gitCmd(repoPath, "config", "--get-regexp", `^remote\..*\.url$`).Output()
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(urls), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.HasPrefix(strings.ToLower(fields[1]), "https://") {
			remote = strings.TrimSuffix(strings.TrimPrefix(fields[0], "remote."), ".url")
			break
		}
	}
	if remote == "" {
		return nil
	}
	if out, err := gitCmd(repoPath, "config", "--get-all", "credential.helper").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		return nil
	}
	return []Finding{{
		Repo:     repoRelPath,
		Check:    "credentials",
		Severity: SeverityInfo,
		Detail:   "remote " + remote + " is HTTPS and git has no credential helper",
	}}
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
	cmd := gitCmd(repoPath, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
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
	cmd := gitCmd(repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
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

// gitConfigGet reads a single git config value. scope is "--local" or "--global".
func gitConfigGet(repoPath, scope, key string) string {
	cmd := gitCmd(repoPath, "config", scope, key)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
