// Package complete provides prompt completers. They do filesystem IO and
// are run by the prompt inside a tea.Cmd, never in Update.
package complete

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/config"
)

const maxEntries = 50

// Paths completes the last path component of value. Directories get a
// trailing slash. A leading ~ is kept as typed.
func Paths(value string) []string { return paths(value, false) }

// Dirs is Paths restricted to directories.
func Dirs(value string) []string { return paths(value, true) }

func paths(value string, dirsOnly bool) []string {
	if value == "" {
		value = "~/"
	}
	dirPart, base := value, ""
	if !strings.HasSuffix(value, "/") {
		dirPart, base = filepath.Dir(value), filepath.Base(value)
		if dirPart == "." && !strings.HasPrefix(value, ".") {
			dirPart = ""
		}
	}
	abs, err := config.ExpandUserPath(orDot(dirPart))
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil
	}
	prefix := dirPart
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	showHidden := strings.HasPrefix(base, ".")
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasPrefix(name, base) {
			continue
		}
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			if fi, err := os.Stat(filepath.Join(abs, name)); err == nil {
				isDir = fi.IsDir()
			}
		}
		if dirsOnly && !isDir {
			continue
		}
		if isDir {
			name += "/"
		}
		out = append(out, prefix+name)
	}
	sort.Strings(out)
	if len(out) > maxEntries {
		out = out[:maxEntries]
	}
	return out
}

func orDot(s string) string {
	if s == "" {
		return "."
	}
	return s
}
