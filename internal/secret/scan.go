package secret

import (
	"bufio"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mugenkunou/ws-tool/internal/ignore"
)

type Violation struct {
	Group    string `json:"group"`
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
	Snippet  string `json:"snippet"`
}

type ScanOptions struct {
	WorkspacePath string
	Engine        *ignore.Engine
	Allowlist     map[string]struct{}
	SkipDirs      []string
}

var patterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)password\s*[:=]\s*[^\s]+`),
	regexp.MustCompile(`(?i)api[_-]?key\s*[:=]\s*[^\s]+`),
	regexp.MustCompile(`(?i)secret\s*[:=]\s*[^\s]+`),
	regexp.MustCompile(`(?i)token\s*[:=]\s*[^\s]+`),
	regexp.MustCompile(`(?i)auth\s*[:=]\s*[^\s]+`),
	regexp.MustCompile(`(?i)access[_-]?key\s*[:=]\s*[^\s]+`),
	regexp.MustCompile(`(?i)client[_-]?secret\s*[:=]\s*[^\s]+`),
	regexp.MustCompile(`(?i)connection[_-]?string\s*[:=]\s*[^\s]+`),
	regexp.MustCompile(`-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----`),
	// High-confidence token prefixes (provider-specific, no key= required).
	regexp.MustCompile(`(?:ghp|gho|ghs|ghr)_[A-Za-z0-9_]{36,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`xox[bpras]-[0-9a-zA-Z-]{10,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),
}

// isCommentLine returns true if the trimmed line is a comment in common formats.
func isCommentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "#") ||
		strings.HasPrefix(trimmed, "//") ||
		strings.HasPrefix(trimmed, "--") ||
		strings.HasPrefix(trimmed, "/*") ||
		strings.HasPrefix(trimmed, "*") ||
		strings.HasPrefix(trimmed, ";") ||
		strings.HasPrefix(trimmed, "rem ") ||
		strings.HasPrefix(trimmed, "REM ")
}

// placeholderPattern matches values that are obviously not real secrets.
var placeholderPattern = regexp.MustCompile(`(?i)^["'` + "`" + `]*(?:<[^>]+>|\$\{[^}]+\}|\{\{[^}]+\}\}|changeme|change_me|replace_?me|example|xxx+|your[_-]?(?:password|token|key|secret)|TODO|FIXME|PLACEHOLDER|INSERT|FILL)["'` + "`" + `]*$`)

func Scan(opts ScanOptions) ([]Violation, error) {
	violations := make([]Violation, 0)

	err := filepath.WalkDir(opts.WorkspacePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Skip entries that vanish, have broken symlinks, or are
			// unreadable. A scan must never abort on a single bad entry.
			return nil
		}
		if path == opts.WorkspacePath {
			return nil
		}

		rel, err := filepath.Rel(opts.WorkspacePath, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		// Check skip_dirs before engine evaluation.
		if d.IsDir() && matchesSkipDir(rel, opts.SkipDirs) {
			return filepath.SkipDir
		}

		if opts.Engine != nil {
			eval := opts.Engine.Evaluate(rel, d.IsDir())
			if !eval.Included {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		if d.IsDir() {
			return nil
		}

		matches, err := scanFile(path, rel, opts.Allowlist)
		if err != nil {
			return nil // skip unreadable files
		}
		violations = append(violations, matches...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return violations, nil
}

func scanFile(absPath, rel string, allowlist map[string]struct{}) ([]Violation, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	violations := make([]Violation, 0)
	s := bufio.NewScanner(f)
	buf := make([]byte, 64*1024)
	s.Buffer(buf, 16*1024*1024)
	lineNum := 0
	for s.Scan() {
		lineNum++
		line := s.Text()
		if !utf8.ValidString(line) {
			continue
		}
		if strings.ContainsRune(line, '\x00') {
			continue
		}

		if isCommentLine(line) {
			continue
		}

		for _, p := range patterns {
			m := p.FindString(line)
			if m == "" {
				continue
			}
			// Extract the value portion and check for placeholders.
			if isPlaceholderMatch(m) {
				continue
			}
			anchor := rel + ":" + itoa(lineNum)
			if _, ok := allowlist[anchor]; ok {
				continue
			}
			violations = append(violations, Violation{
				Group:    "Secret",
				Type:     "secret",
				Severity: "CRITICAL",
				Path:     rel,
				Line:     lineNum,
				Message:  "potential secret detected",
				Snippet:  strings.TrimSpace(line),
			})
			break
		}
	}

	if err := s.Err(); err != nil {
		return nil, err
	}

	return violations, nil
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + (v % 10))
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// valueExtractor captures the value after a key=value or key: value operator.
var valueExtractor = regexp.MustCompile(`(?i)(?:password|api[_-]?key|secret|token|auth|access[_-]?key|client[_-]?secret|connection[_-]?string)\s*[:=]\s*(.+)`)

// isPlaceholderMatch returns true if the matched fragment contains only a
// placeholder value — not a real secret.
func isPlaceholderMatch(match string) bool {
	m := valueExtractor.FindStringSubmatch(match)
	if len(m) < 2 {
		// Pattern without key=value structure (e.g. private key header, token prefix).
		// These are never placeholders.
		return false
	}
	value := strings.TrimSpace(m[1])
	if placeholderPattern.MatchString(value) {
		return true
	}
	return isLowEntropy(value)
}

// isLowEntropy returns true if value has trivially low Shannon entropy,
// indicating a fake or test secret (e.g. "FAKEKEYFAKEKEY", "aaaaaa").
// The threshold is calibrated to catch obvious fakes while preserving
// real secrets like "hunter2" or "s3cret!!".
func isLowEntropy(value string) bool {
	// Strip surrounding quotes for analysis.
	cleaned := strings.Trim(value, `"'`+"`")
	if len(cleaned) < 8 {
		return false // short values can be real passwords with low entropy
	}

	// Check for repeating substring patterns (e.g. "FAKEKEYFAKEKEY", "abcabc").
	if hasRepeatingPattern(cleaned) {
		return true
	}

	// Count character frequencies.
	freq := make(map[rune]int)
	total := 0
	for _, r := range cleaned {
		freq[r]++
		total++
	}
	if total == 0 {
		return false
	}

	// Shannon entropy in bits per character.
	var entropy float64
	ft := float64(total)
	for _, count := range freq {
		p := float64(count) / ft
		entropy -= p * math.Log2(p)
	}

	// Threshold: real passwords typically have entropy > 2.0 bits/char.
	// "aaaaaa" = 0, "ababab" = 1.0, "hunter2" ≈ 2.8.
	return entropy < 2.0
}

// hasRepeatingPattern returns true if s is composed of a short substring
// repeated 2+ times (e.g. "FAKEKEY" x2 = "FAKEKEYFAKEKEY").
func hasRepeatingPattern(s string) bool {
	n := len(s)
	// Check pattern lengths from 1 to n/2.
	for plen := 1; plen <= n/2; plen++ {
		if n%plen != 0 {
			continue
		}
		pat := s[:plen]
		match := true
		for i := plen; i < n; i += plen {
			if s[i:i+plen] != pat {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// matchesSkipDir returns true if rel equals or is under any of the skip directories.
func matchesSkipDir(rel string, skipDirs []string) bool {
	for _, d := range skipDirs {
		if rel == d || strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}
