package secret

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// ParseAnchor reads a `<file:line>` argument (spec §5 secret, "Findings and
// anchors"). The file part resolves like any path: absolute, `~`, or relative
// to the current directory. rel is the workspace-relative, forward-slashed
// file, the form the allowlist stores.
func ParseAnchor(workspacePath, arg string) (rel string, line int, err error) {
	i := strings.LastIndex(arg, ":")
	if i <= 0 || i == len(arg)-1 {
		return "", 0, fmt.Errorf("%q is not <file:line>", arg)
	}
	line, convErr := strconv.Atoi(arg[i+1:])
	if convErr != nil || line < 1 {
		return "", 0, fmt.Errorf("%q is not <file:line>: the line must be a positive number", arg)
	}
	abs, err := wsconfig.ExpandUserPath(arg[:i])
	if err != nil {
		return "", 0, fmt.Errorf("invalid path in %q: %w", arg, err)
	}
	ws := filepath.Clean(workspacePath)
	if !repo.IsWithin(abs, ws) || abs == ws {
		return "", 0, fmt.Errorf("%s is outside the workspace %s", abs, ws)
	}
	r, err := filepath.Rel(ws, abs)
	if err != nil {
		return "", 0, err
	}
	return filepath.ToSlash(r), line, nil
}

// FindingAt returns the finding at rel:line.
func FindingAt(vs []Violation, rel string, line int) (Violation, bool) {
	for _, v := range vs {
		if v.Path == rel && v.Line == line {
			return v, true
		}
	}
	return Violation{}, false
}

// AbsAnchor is a finding's location as shown to the user: /abs/file:line.
func AbsAnchor(workspacePath string, v Violation) string {
	return fmt.Sprintf("%s:%d", filepath.Join(workspacePath, filepath.FromSlash(v.Path)), v.Line)
}
