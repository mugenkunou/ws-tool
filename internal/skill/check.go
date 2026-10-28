package skill

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Finding is one drift `ws skill check` reports.
type Finding struct {
	// Level is "error" or "warning". An error makes the check exit 1.
	Level   string `json:"level"`
	Shelf   Shelf  `json:"shelf,omitempty"`
	Skill   string `json:"skill,omitempty"`
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

const (
	LevelError   = "error"
	LevelWarning = "warning"
)

// CheckInput is what Check holds the skills and scripts against.
type CheckInput struct {
	Home, Workspace string
	// Command reports whether the words after "ws" name a real command. known
	// is false when the words are not a ws command line at all (words[0] is
	// not a noun), so they are not checked.
	Command func(words []string) (real, known bool)
	// Anchors are the workspace's top-level folders. A backticked path is
	// workspace-relative when its first part is one of them.
	Anchors []string
	// Schema is ws.json's schema, held against a skill's ws-layout metadata.
	Schema int
	// Env is what `ws env` prints: WS_ROOT, WS_TMP, … It is held against the
	// fallbacks in scripts/lib/common.sh. Getenv expands those (default os.Getenv).
	Env    map[string]string
	Getenv func(string) string
}

// Check reads both shelves and <workspace>/scripts and reports drift. It
// changes nothing.
func Check(in CheckInput) ([]Finding, error) {
	skills, err := All(in.Home, in.Workspace)
	if err != nil {
		return nil, err
	}
	out := []Finding{}
	for _, s := range skills {
		if s.Broken {
			out = append(out, Finding{Level: LevelError, Shelf: s.Shelf, Skill: s.Name, File: s.Path, Message: "the skill's folder is gone"})
			continue
		}
		if s.Shadowed {
			out = append(out, Finding{Level: LevelWarning, Shelf: s.Shelf, Skill: s.Name, File: s.Path, Message: "same name as a built-in; the built-in wins"})
		}
		out = append(out, checkSkill(in, s)...)
	}
	out = append(out, checkScripts(in)...)
	out = append(out, checkCommonSh(in)...)
	return out, nil
}

// checkSkill reads every .md file of one skill.
func checkSkill(in CheckInput, s Skill) []Finding {
	var out []Finding
	add := func(level, file string, line int, format string, a ...any) {
		out = append(out, Finding{Level: level, Shelf: s.Shelf, Skill: s.Name, File: file, Line: line, Message: fmt.Sprintf(format, a...)})
	}
	sfs := os.DirFS(s.Path) // follows a skill that is a symlink
	var files []string
	_ = fs.WalkDir(sfs, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && path.Ext(p) == ".md" {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	for _, rel := range files {
		data, err := fs.ReadFile(sfs, rel)
		if err != nil {
			continue
		}
		file := filepath.Join(s.Path, filepath.FromSlash(rel))
		for _, c := range codeSpans(data) {
			for _, words := range wsCommandLines(c.text) {
				if real, known := in.Command(words); known && !real {
					add(LevelError, file, c.line, "`ws %s` is not a ws command", strings.Join(words, " "))
				}
			}
			if s.Shelf == Yours && c.inline {
				if p, ok := workspacePath(c.text, in.Anchors); ok {
					if _, err := os.Stat(filepath.Join(in.Workspace, p)); err != nil {
						add(LevelError, file, c.line, "`%s` does not exist in the workspace", p)
					}
				}
			}
		}
		if s.Shelf == Yours && rel == "SKILL.md" {
			if v, line, ok := layoutVersion(data); ok && v != strconv.Itoa(in.Schema) {
				add(LevelWarning, file, line, "ws-layout is %q; ws.json's schema is %d", v, in.Schema)
			}
		}
	}
	return out
}

// span is one piece of code in a markdown file: an inline `span`, or a line of
// a fenced block.
type span struct {
	text   string
	line   int
	inline bool
}

var inlineCode = regexp.MustCompile("`([^`]+)`")

func codeSpans(data []byte) []span {
	var out []span
	fenced := false
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			out = append(out, span{text: line, line: n})
			continue
		}
		for _, m := range inlineCode.FindAllStringSubmatch(line, -1) {
			out = append(out, span{text: m[1], line: n, inline: true})
		}
	}
	return out
}

var wsLine = regexp.MustCompile(`(?:^|[\s(|;&])ws((?:[ \t]+[a-z][a-z0-9-]*){1,3})`)

// wsCommandLines finds `ws <noun> <verb>…` in a piece of code: up to three
// lowercase words after ws.
func wsCommandLines(text string) [][]string {
	var out [][]string
	for _, m := range wsLine.FindAllStringSubmatch(text, -1) {
		out = append(out, strings.Fields(m[1]))
	}
	return out
}

var concretePath = regexp.MustCompile(`^[A-Za-z0-9_.+@-]+(?:/[A-Za-z0-9_.+@-]+)*/?$`)

// workspacePath is a backticked span that names one concrete workspace file or
// folder: no placeholder (<, *, $), first part a top-level workspace folder.
func workspacePath(text string, anchors []string) (string, bool) {
	text = strings.TrimSpace(text)
	if !concretePath.MatchString(text) || !strings.Contains(text, "/") {
		return "", false
	}
	first, _, _ := strings.Cut(text, "/")
	for _, a := range anchors {
		if first == a {
			return strings.TrimSuffix(text, "/"), true
		}
	}
	return "", false
}

var layoutLine = regexp.MustCompile(`^\s+ws-layout:\s*["']?([^"'\s]+)["']?\s*$`)

// layoutVersion reads `metadata: ws-layout: "<n>"` from SKILL.md's frontmatter.
func layoutVersion(data []byte) (string, int, bool) {
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", 0, false
	}
	inMeta := false
	for i, l := range lines[1:] {
		if strings.TrimSpace(l) == "---" {
			break
		}
		if !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") {
			inMeta = strings.HasPrefix(l, "metadata:")
			continue
		}
		if m := layoutLine.FindStringSubmatch(l); inMeta && m != nil {
			return m[1], i + 2, true
		}
	}
	return "", 0, false
}

var tmpMention = regexp.MustCompile(`(?:^|[^\w~.$}/-])/tmp\b`)

// checkScripts holds each top-level script in <workspace>/scripts to a header
// comment and keeps /tmp out of it (the workspace's temp folder is WS_TMP).
func checkScripts(in CheckInput) []Finding {
	dir := filepath.Join(in.Workspace, "scripts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Finding
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if strings.HasPrefix(e.Name(), ".") || filepath.Ext(e.Name()) == ".md" {
			continue
		}
		if info, err := os.Stat(p); err != nil || !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if !hasHeader(data) {
			out = append(out, Finding{Level: LevelError, File: p, Line: 1, Message: "no header comment saying what the script does"})
		}
		for i, l := range strings.Split(string(data), "\n") {
			if tmpMention.MatchString(l) {
				out = append(out, Finding{Level: LevelError, File: p, Line: i + 1, Message: "mentions /tmp; use $WS_TMP"})
			}
		}
	}
	return out
}

// hasHeader: after a shebang and blank lines, the first line is a comment.
func hasHeader(data []byte) bool {
	lines := strings.Split(string(data), "\n")
	for i, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || (i == 0 && strings.HasPrefix(l, "#!")) {
			continue
		}
		for _, p := range []string{"#", "//", `"""`, "'''"} {
			if strings.HasPrefix(l, p) {
				return true
			}
		}
		return false
	}
	return false
}

var assign = regexp.MustCompile(`^\s*(?:export\s+)?(WS_[A-Z_]+)=(.*)$`)

// checkCommonSh holds the fallbacks scripts/lib/common.sh assigns against
// what `ws env` prints. A value computed by a command, $(…), is not compared.
func checkCommonSh(in CheckInput) []Finding {
	p := filepath.Join(in.Workspace, "scripts", "lib", "common.sh")
	data, err := os.ReadFile(p)
	if err != nil || len(in.Env) == 0 {
		return nil
	}
	getenv := in.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	var out []Finding
	for i, l := range strings.Split(string(data), "\n") {
		m := assign.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		want, ok := in.Env[m[1]]
		if !ok || strings.Contains(m[2], "$(") || strings.Contains(m[2], "`") {
			continue
		}
		if got := shellValue(m[2], getenv); filepath.Clean(got) != filepath.Clean(want) {
			out = append(out, Finding{Level: LevelWarning, File: p, Line: i + 1,
				Message: fmt.Sprintf("%s falls back to %s; ws env says %s", m[1], got, want)})
		}
	}
	return out
}

// shellValue evaluates the right side of a shell assignment, as far as
// common.sh needs: quotes, $NAME, ${NAME}, ${NAME:-word} and ${NAME-word}.
func shellValue(s string, getenv func(string) string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, " #"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return b.String() + s[i+1:]
			}
			b.WriteString(s[i+1 : i+1+j])
			i += j + 1
		case c == '"':
			// Double quotes only group; expansion runs the same inside them.
		case c == '$' && i+1 < len(s) && s[i+1] == '{':
			end, depth := -1, 0
			for j := i + 1; j < len(s); j++ {
				if s[j] == '{' {
					depth++
				} else if s[j] == '}' {
					if depth--; depth == 0 {
						end = j
						break
					}
				}
			}
			if end < 0 {
				return b.String()
			}
			b.WriteString(paramValue(s[i+2:end], getenv))
			i = end
		case c == '$':
			j := i + 1
			for j < len(s) && (s[j] == '_' || s[j] >= 'A' && s[j] <= 'Z' || s[j] >= 'a' && s[j] <= 'z' || s[j] >= '0' && s[j] <= '9') {
				j++
			}
			b.WriteString(getenv(s[i+1 : j]))
			i = j - 1
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func paramValue(expr string, getenv func(string) string) string {
	// ${NAME-word} differs from ${NAME:-word} only for a variable set to "",
	// which getenv cannot tell from unset; both take the word then.
	name, word, ok := strings.Cut(expr, "-")
	if !ok {
		return getenv(expr)
	}
	name = strings.TrimSuffix(name, ":")
	if v := getenv(name); v != "" {
		return v
	}
	return shellValue(word, getenv)
}
