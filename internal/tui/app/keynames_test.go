package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestNoKeyNamesInTextAnywhere: spec §6.3 and plan phase D. A string in the TUI
// never names a key ("press a…", "G to view"): it asks the binding for its
// Help().Key, so renaming a key cannot leave a stale hint behind. The scan
// reads the AST of every non-test file under internal/tui except keys.go, where
// the bindings are declared.
func TestNoKeyNamesInTextAnywhere(t *testing.T) {
	pats := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bpress [a-z]\b`),
		regexp.MustCompile(`(?i)\bpress (ctrl|shift|alt)\+`),
		regexp.MustCompile(`(?i)\bpress (enter|esc|tab|space)\b`),
		regexp.MustCompile(`\b[A-Z] to (view|open|show|see|run)\b`),
	}
	root := filepath.Join("..", "..", "tui")
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "keys.go" ||
			filepath.ToSlash(path) == "../../tui/handover/handover.go" { // a shell's own "press enter" prompt
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			for _, p := range pats {
				if p.MatchString(s) {
					t.Errorf("%s: the string %q names a key; use the binding's Help().Key", fset.Position(lit.Pos()), s)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
