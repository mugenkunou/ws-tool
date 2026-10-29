package secret

import (
	"bufio"
	"os"
)

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
