package ignore

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type Rule struct {
	Raw     string
	Include bool
	Glob    bool
	Pattern string
}

type Engine struct {
	Rules []Rule
}

type EvalResult struct {
	Included    bool   `json:"included"`
	Rule        string `json:"rule"`
	SafeHarbor  bool   `json:"safe_harbor,omitempty"`
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
	wasExcluded := false
	for _, rule := range e.Rules {
		if matchRule(rule, normalized, isDir) {
			if !rule.Include {
				wasExcluded = true
			}
			result.Included = rule.Include
			result.Rule = rule.Raw
		}
	}
	// Safe harbor: path was excluded by an earlier rule but re-included by a
	// later +: or +g: override — meaning last-match-wins gave it back.
	if result.Included && wasExcluded {
		result.SafeHarbor = true
	}
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
