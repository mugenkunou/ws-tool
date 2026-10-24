package ignore

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Rule sources: where a rule comes from.
const (
	SourceBuiltin = "builtin" // the ws binary's template
	SourceUser    = "user"    // ws.json ignore_rules
)

type Rule struct {
	Raw     string
	Include bool
	Glob    bool
	Pattern string
	Source  string // SourceBuiltin or SourceUser; empty for rules read from a file
}

type Engine struct {
	Rules []Rule
}

// EvalResult reports whether relPath syncs, and why. The final matching rule
// wins (last-match-wins): an exclude rule blocks sync, and a later include
// rule (an included directory) can hand the path back. ByInclude is true
// when such an include rule is the reason the path syncs; its warnings count
// as muted. OverriddenBy is the include rule that last matched. RuleSource is
// where the deciding rule comes from (SourceBuiltin, SourceUser, or empty when
// no rule matches).
type EvalResult struct {
	Included     bool   `json:"included"`
	Rule         string `json:"rule"`
	RuleSource   string `json:"rule_source,omitempty"`
	ByInclude    bool   `json:"by_include,omitempty"`
	OverriddenBy string `json:"overridden_by,omitempty"`
}

func LoadEngine(path string) (*Engine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	rules := make([]Rule, 0)
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		rule, ok := parseRule(line)
		if !ok {
			continue
		}
		rules = append(rules, rule)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}

	return &Engine{Rules: rules}, nil
}

func (e *Engine) Evaluate(relPath string, isDir bool) EvalResult {
	normalized := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(relPath)), "./")
	if normalized == "." {
		normalized = ""
	}

	result := EvalResult{Included: true, Rule: "<default>"}
	for _, rule := range e.Rules {
		if matchRule(rule, normalized, isDir) {
			if rule.Include {
				// An include override won: the path is inside an include rule
				// whether or not an earlier exclude matched it.
				result.ByInclude = true
				result.OverriddenBy = rule.Raw
			}
			result.Included = rule.Include
			result.Rule = rule.Raw
			result.RuleSource = rule.Source
		}
	}
	// Suppressed = included by a include override, even when no
	// exclude rule ever matched (an included dir keeps syncing everything in
	// it by design). A path the excludes still own is not suppressed.
	result.ByInclude = result.Included && result.ByInclude
	return result
}

func parseRule(line string) (Rule, bool) {
	include := false
	rest := ""
	switch {
	case strings.HasPrefix(line, "-:"):
		rest = strings.TrimPrefix(line, "-:")
	case strings.HasPrefix(line, "+:"):
		include = true
		rest = strings.TrimPrefix(line, "+:")
	case strings.HasPrefix(line, "-g:"):
		rest = strings.TrimPrefix(line, "-g:")
		return Rule{Raw: line, Include: false, Glob: true, Pattern: strings.TrimSpace(rest)}, true
	case strings.HasPrefix(line, "+g:"):
		include = true
		rest = strings.TrimPrefix(line, "+g:")
		return Rule{Raw: line, Include: true, Glob: true, Pattern: strings.TrimSpace(rest)}, true
	case strings.HasPrefix(line, "-s:"):
		// Sentinel in template. Keep default include behavior and do not override
		// earlier matches in our simplified evaluator.
		return Rule{}, false
	default:
		return Rule{}, false
	}

	return Rule{Raw: line, Include: include, Glob: false, Pattern: strings.TrimSpace(rest)}, true
}

func matchRule(rule Rule, relPath string, isDir bool) bool {
	if relPath == "" {
		return false
	}
	p := strings.TrimSpace(rule.Pattern)
	if p == "" {
		return false
	}

	if rule.Glob {
		return matchGlob(p, relPath)
	}

	if p == ".*" {
		parts := strings.Split(relPath, "/")
		for _, part := range parts {
			if strings.HasPrefix(part, ".") {
				return true
			}
		}
		return false
	}

	norm := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(p)), "./")
	if norm == relPath {
		return true
	}
	if strings.HasPrefix(relPath, norm+"/") {
		return true
	}

	parts := strings.Split(relPath, "/")
	for _, part := range parts {
		if part == norm {
			return true
		}
	}

	if strings.Contains(norm, "*") {
		return matchGlob(norm, relPath)
	}

	if isDir && strings.HasSuffix(norm, "/") {
		norm = strings.TrimSuffix(norm, "/")
		return relPath == norm || strings.HasPrefix(relPath, norm+"/")
	}

	return false
}

func matchGlob(pattern, relPath string) bool {
	if pattern == "*" {
		return true
	}

	path := filepath.ToSlash(relPath)
	base := filepath.Base(path)

	if ok, _ := filepath.Match(pattern, base); ok {
		return true
	}
	if ok, _ := filepath.Match(pattern, path); ok {
		return true
	}

	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		prefix = strings.TrimPrefix(filepath.ToSlash(filepath.Clean(prefix)), "./")
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}

	return false
}
