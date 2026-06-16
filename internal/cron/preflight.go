package cron

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// PreflightError describes a crontab access failure detected before any write.
type PreflightError struct {
	Kind   string // "access_denied" | "binary_missing"
	User   string
	Detail string // raw stderr from crontab, for diagnostics
}

func (e *PreflightError) Error() string {
	switch e.Kind {
	case "access_denied":
		return fmt.Sprintf(
			"crontab access denied for user %s.\n"+
				"  You may be excluded by /etc/cron.allow or /etc/cron.deny.\n"+
				"  Ask your system administrator to add %s to /etc/cron.allow,\n"+
				"  or run with elevated privileges if that is the expected pathway.\n"+
				"  Detail: %s",
			e.User, e.User, e.Detail,
		)
	case "binary_missing":
		return "crontab binary not found in PATH. Install a cron daemon (e.g., `apt install cron`)."
	default:
		return fmt.Sprintf("crontab preflight failed: %s", e.Detail)
	}
}

// Remediation returns a short actionable fix suggestion.
func (e *PreflightError) Remediation() string {
	switch e.Kind {
	case "access_denied":
		return fmt.Sprintf("Add %s to /etc/cron.allow, or contact your system administrator.", e.User)
	case "binary_missing":
		return "Install a cron daemon: `apt install cron` or `yum install cronie`."
	default:
		return "Check your crontab configuration."
	}
}

// Preflight checks that the current user can read and write their crontab.
// Returns *PreflightError on access denial or missing binary; nil on success.
// "no crontab for user" (empty crontab) is treated as success.
func Preflight() error {
	if _, err := exec.LookPath("crontab"); err != nil {
		return &PreflightError{Kind: "binary_missing"}
	}

	cmd := exec.Command("crontab", "-l")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	lower := strings.ToLower(string(out))
	// "no crontab for user" is a benign error — empty crontab is fine.
	if strings.Contains(lower, "no crontab for") {
		return nil
	}

	if strings.Contains(lower, "not allowed") ||
		strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "cron.allow") ||
		strings.Contains(lower, "cron.deny") {
		return &PreflightError{
			Kind:   "access_denied",
			User:   currentUser(),
			Detail: strings.TrimSpace(string(out)),
		}
	}

	// Other errors: treat as benign (don't block add/rm on unexpected output).
	return nil
}

func currentUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u := os.Getenv("LOGNAME"); u != "" {
		return u
	}
	return "unknown"
}
