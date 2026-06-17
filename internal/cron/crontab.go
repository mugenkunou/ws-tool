package cron

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CronMarkerStart returns the opening marker line for a job's crontab block.
func CronMarkerStart(name string) string {
	return fmt.Sprintf("# >>> ws:%s >>>", name)
}

// CronMarkerEnd returns the closing marker line for a job's crontab block.
func CronMarkerEnd(name string) string {
	return fmt.Sprintf("# <<< ws:%s <<<", name)
}

// ReadCrontab reads the current user crontab. Returns empty string if no
// crontab exists (crontab -l exits non-zero in that case).
//
// If the WS_CRONTAB_FILE environment variable is set, the named file is read
// instead of invoking crontab(1). This is used by tests to isolate from the
// real system crontab.
func ReadCrontab() (string, error) {
	if f := os.Getenv("WS_CRONTAB_FILE"); f != "" {
		data, err := os.ReadFile(f)
		if err != nil {
			if os.IsNotExist(err) {
				return "", nil
			}
			return "", err
		}
		return string(data), nil
	}
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		// crontab -l exits non-zero when no crontab is set — not a real error.
		return "", nil
	}
	return string(out), nil
}

// WriteCrontab installs content as the user's crontab via crontab stdin.
//
// If WS_CRONTAB_FILE is set, the content is written to that file instead of
// invoking crontab(1).
func WriteCrontab(content string) error {
	if f := os.Getenv("WS_CRONTAB_FILE"); f != "" {
		return os.WriteFile(f, []byte(content), 0o600)
	}
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(content)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("crontab write failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// HasJob reports whether the named job's marker block is present in the
// current user crontab.
func HasJob(name string) (bool, error) {
	ct, err := ReadCrontab()
	if err != nil {
		return false, err
	}
	return strings.Contains(ct, CronMarkerStart(name)), nil
}

// AddJob adds (or replaces) the named job's block in the crontab.
//
// The block written is:
//
//	# >>> ws:<name> >>>
//	SHELL=/usr/bin/bash
//	<schedule> <scriptPath>
//	@reboot <scriptPath>
//	# <<< ws:<name> <<<
//
// The @reboot entry enables missed-run recovery on boot/resume: the wrapper
// script checks its state file and skips if the last run was recent enough.
func AddJob(job *BuiltinJob, scriptPath string) error {
	ct, err := ReadCrontab()
	if err != nil {
		return err
	}

	// Remove existing block for this job (idempotent replace).
	ct = removeBlock(ct, job.Name)

	block := fmt.Sprintf(
		"%s\nSHELL=/usr/bin/bash\n%s %s\n@reboot %s\n%s\n",
		CronMarkerStart(job.Name),
		job.Schedule, scriptPath,
		scriptPath,
		CronMarkerEnd(job.Name),
	)

	if ct != "" && !strings.HasSuffix(ct, "\n") {
		ct += "\n"
	}
	ct += block

	return WriteCrontab(ct)
}

// RemoveJob removes the named job's block from the crontab.
// Returns nil if the block is not present (idempotent).
func RemoveJob(name string) error {
	ct, err := ReadCrontab()
	if err != nil {
		return err
	}
	updated := removeBlock(ct, name)
	if updated == ct {
		return nil
	}
	return WriteCrontab(updated)
}

// removeBlock strips the marker-delimited block for name from ct and
// collapses any resulting double-blank lines.
func removeBlock(ct, name string) string {
	start := CronMarkerStart(name)
	end := CronMarkerEnd(name)

	lines := strings.Split(ct, "\n")
	var out []string
	inBlock := false
	for _, line := range lines {
		if strings.TrimSpace(line) == start {
			inBlock = true
			continue
		}
		if inBlock {
			if strings.TrimSpace(line) == end {
				inBlock = false
			}
			continue
		}
		out = append(out, line)
	}

	result := strings.Join(out, "\n")
	// Collapse triple (or more) newlines left by block removal.
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return result
}
