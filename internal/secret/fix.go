package secret

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type FixMode string

const (
	FixModeAllowlist FixMode = "allowlist"
	FixModeExclude   FixMode = "exclude"
	FixModePass      FixMode = "pass"
)

type FixOptions struct {
	WorkspacePath  string
	ManifestPath   string
	MegaignorePath string
	Violations     []Violation
	Mode           FixMode
	DryRun         bool
}

type FixResult struct {
	Mode        string   `json:"mode"`
	Added       []string `json:"added"`
	Messages    []string `json:"messages"`
	Processed   int      `json:"processed"`
	Allowlisted int      `json:"allowlisted"`
	Excluded    int      `json:"excluded"`
	PassStored  int      `json:"pass_stored"`
	DirSkipped  int      `json:"dir_skipped"`
	Skipped     int      `json:"skipped"`
	DryRun      bool     `json:"dry_run"`
}

// ContextLine represents a single line of file context around a violation.
type ContextLine struct {
	Number  int    `json:"number"`
	Content string `json:"content"`
	IsMatch bool   `json:"is_match"`
}

// GetFileContext reads lines around targetLine from absPath.
// contextLines controls how many lines before and after to include.
func GetFileContext(absPath string, targetLine, contextLines int) ([]ContextLine, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	start := targetLine - contextLines
	if start < 1 {
		start = 1
	}
	end := targetLine + contextLines

	var result []ContextLine
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum < start {
			continue
		}
		if lineNum > end {
			break
		}
		result = append(result, ContextLine{
			Number:  lineNum,
			Content: scanner.Text(),
			IsMatch: lineNum == targetLine,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

var secretValuePattern = regexp.MustCompile(`(?i)(?:password|api[_-]?key|secret|token|auth|access[_-]?key|client[_-]?secret|connection[_-]?string)\s*[:=]\s*(.+)`)

// ExtractSecretValue extracts the value portion from a secret pattern match.
// It strips surrounding quotes and inline trailing comments.
// Returns empty string if no value can be extracted.
func ExtractSecretValue(snippet string) string {
	snippet = strings.TrimSpace(snippet)
	m := secretValuePattern.FindStringSubmatch(snippet)
	if len(m) < 2 {
		return ""
	}
	v := strings.TrimSpace(m[1])
	v = stripInlineComment(v)
	v = stripQuotes(v)
	return strings.TrimSpace(v)
}

// stripQuotes removes matching surrounding quotes (' " `).
func stripQuotes(s string) string {
	if len(s) >= 2 {
		for _, q := range []byte{'\'', '"', '`'} {
			if s[0] == q && s[len(s)-1] == q {
				return s[1 : len(s)-1]
			}
		}
	}
	return s
}

// stripInlineComment removes trailing inline comments (# or //).
// It handles quoted values by not stripping comment chars inside quotes.
func stripInlineComment(s string) string {
	var inQuote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote != 0 {
			if c == inQuote {
				inQuote = 0
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			inQuote = c
			continue
		}
		if c == '#' {
			return strings.TrimSpace(s[:i])
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '/' {
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

var keyNamePattern = regexp.MustCompile(`(?i)(password|api[_-]?key|secret|token|auth|access[_-]?key|client[_-]?secret|connection[_-]?string)\s*[:=]`)

// SuggestPassEntry derives a pass entry name from the file path and snippet.
// Example: "configs/myapp/app.env" + "password=" → "myapp/app/password"
func SuggestPassEntry(relPath, snippet string) string {
	keyName := "secret"
	m := keyNamePattern.FindStringSubmatch(strings.TrimSpace(snippet))
	if len(m) >= 2 {
		keyName = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(m[1], "-", "_"), " ", "_"))
	}

	dir := filepath.Dir(relPath)
	base := filepath.Base(relPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	var entry string
	if dir == "." {
		entry = name + "/" + keyName
	} else {
		parts := strings.Split(filepath.ToSlash(dir), "/")
		last := parts[len(parts)-1]
		entry = last + "/" + name + "/" + keyName
	}
	return SanitizeEntryName(entry)
}

// SanitizeEntryName cleans a pass entry name by replacing characters that
// cause trouble in the password store (spaces, colons, backslashes, control
// chars) and normalizing path separators. Leading dots in path segments are
// stripped to avoid hidden directories in the store.
func SanitizeEntryName(name string) string {
	// Normalize separators.
	name = filepath.ToSlash(name)

	// Replace problematic characters.
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r == '/' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ' || r == ':' || r == '\\':
			b.WriteRune('_')
		default:
			if r > 31 && r != 127 {
				b.WriteRune(r)
			}
			// Drop control characters.
		}
	}
	name = b.String()

	// Strip leading dots from each path segment.
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = strings.TrimLeft(p, ".")
	}

	// Remove empty segments from stripping.
	var clean []string
	for _, p := range parts {
		if p != "" {
			clean = append(clean, p)
		}
	}

	if len(clean) == 0 {
		return "entry"
	}
	return strings.Join(clean, "/")
}

// PruneAllowlist returns the subset of allowlist entries that still correspond
// to an active violation. Entries whose file:line no longer matches a current
// violation are considered stale and excluded from the result.
func PruneAllowlist(allowlist []string, violations []Violation) (kept, pruned []string) {
	active := make(map[string]struct{}, len(violations))
	for _, v := range violations {
		// Build the anchor the same way scanFile does, but we need
		// this to work even for allowlisted violations (which won't
		// appear in the violations list). So instead we rebuild anchors
		// from violations that *would* have matched if not allowlisted.
		// The caller should pass violations scanned with an empty
		// allowlist to get the full picture.
		active[v.Path+":"+itoa(v.Line)] = struct{}{}
	}
	for _, a := range allowlist {
		if _, ok := active[a]; ok {
			kept = append(kept, a)
		} else {
			pruned = append(pruned, a)
		}
	}
	return kept, pruned
}
