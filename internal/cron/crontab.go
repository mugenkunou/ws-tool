package cron

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

var markerStart = regexp.MustCompile(`^# >>> ws:([a-z0-9-]+) >>>$`)

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

// ReadDeclared reads the declared crontab. A missing file is empty, not an error.
func ReadDeclared(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// WriteDeclared writes the declared crontab atomically (mode 0644), creating the
// file and its directory if missing.
func WriteDeclared(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".crontab-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Block is one ws-owned region of a crontab: the lines from the start marker to
// the end marker, markers included, with a trailing newline.
type Block struct {
	Name string
	Text string
}

// ParseBlocks splits a crontab into its ws blocks (any name, in order) and the
// lines outside them, which ws never changes. Blank lines outside blocks are
// dropped from other.
func ParseBlocks(content string) (blocks []Block, other []string) {
	var cur *Block
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if cur != nil {
			lines = append(lines, line)
			if trimmed == CronMarkerEnd(cur.Name) {
				cur.Text = strings.Join(lines, "\n") + "\n"
				blocks = append(blocks, *cur)
				cur, lines = nil, nil
			}
			continue
		}
		if m := markerStart.FindStringSubmatch(trimmed); m != nil {
			cur, lines = &Block{Name: m[1]}, []string{line}
			continue
		}
		if trimmed != "" {
			other = append(other, line)
		}
	}
	return blocks, other
}

// FindBlock returns the first block named one of names.
func FindBlock(content string, names ...string) (Block, bool) {
	blocks, _ := ParseBlocks(content)
	for _, b := range blocks {
		for _, n := range names {
			if b.Name == n {
				return b, true
			}
		}
	}
	return Block{}, false
}

// HasJob reports whether the named job's marker block is present in the
// current user crontab, under that name.
func HasJob(name string) (bool, error) {
	ct, err := ReadCrontab()
	if err != nil {
		return false, err
	}
	_, ok := FindBlock(ct, name)
	return ok, nil
}

// BlockFor builds a job's crontab block for this machine: this binary, this
// workspace, the built-in schedule, and for jobs that need one, the X11 display.
// Both lines call `cron run --if-due`, so a run that happened moments ago (for
// example just before a reboot) is skipped. The lines carry --workspace because
// cron does not set $WS_WORKSPACE.
//
//	# >>> ws:mega-sync >>>
//	SHELL=/usr/bin/bash
//	*/30 * * * * <ws> --workspace <W> cron run --if-due --display :1 mega-sync
//	@reboot <ws> --workspace <W> cron run --if-due --display :1 mega-sync
//	# <<< ws:mega-sync <<<
func BlockFor(job *BuiltinJob, wsBin, workspace, display string) string {
	run := fmt.Sprintf("%s --workspace %s cron run --if-due", wsBin, workspace)
	if job.NeedsDisplay {
		if display == "" {
			display = DefaultDisplay()
		}
		run += " --display " + display
	}
	run += " " + job.Name
	return fmt.Sprintf("%s\nSHELL=/usr/bin/bash\n%s %s\n@reboot %s\n%s\n",
		CronMarkerStart(job.Name), job.Schedule, run, run, CronMarkerEnd(job.Name))
}

// Outdated reports whether a block should be rewritten by `cron add`: it is
// under an old job name, or one of its lines lacks `cron run --if-due`.
// Differences in the binary or workspace path do not count, so a build in another
// directory does not mark everything outdated.
func Outdated(b Block) bool {
	if _, old := Aliases[b.Name]; old {
		return true
	}
	for _, line := range strings.Split(b.Text, "\n") {
		if strings.Contains(line, " cron run ") && !strings.Contains(line, "cron run --if-due") {
			return true
		}
	}
	return false
}

// upsertBlock removes the blocks named one of names from content and appends
// block, which is how `cron add` replaces a block under an old name. Lines
// outside ws blocks are untouched.
func upsertBlock(content, block string, names ...string) string {
	content = removeBlocks(content, names...)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + block
}

// removeBlocks strips the blocks named one of names from content and collapses
// any resulting double-blank lines.
func removeBlocks(content string, names ...string) string {
	blocks, _ := ParseBlocks(content)
	out := content
	for _, b := range blocks {
		for _, n := range names {
			if b.Name == n {
				out = strings.Replace(out, b.Text, "", 1)
				// A block whose end marker was not newline-terminated.
				out = strings.Replace(out, strings.TrimSuffix(b.Text, "\n"), "", 1)
			}
		}
	}
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return out
}

// RemoveJob removes the named job's block from the live crontab, under its
// current and old names. Returns nil if no block is present (idempotent).
func RemoveJob(name string) error {
	ct, err := ReadCrontab()
	if err != nil {
		return err
	}
	canon, _ := Canonical(name)
	updated := removeBlocks(ct, Names(canon)...)
	if updated == ct {
		return nil
	}
	return WriteCrontab(updated)
}
