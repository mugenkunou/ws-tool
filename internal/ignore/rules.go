package ignore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// UserRules represents the user-managed ignore rules stored in ws/ignore.json.
// This is the only file users edit — .megaignore is a generated output.
type UserRules struct {
	Schema           int         `json:"schema"`
	Exclude          []RuleEntry `json:"exclude"`
	SafeHarbors      []RuleEntry `json:"safe_harbors"`
	SuppressDefaults []string    `json:"suppress_defaults"`
	SuppressHarbors  []string    `json:"suppress_harbors"`
}

// RuleEntry is a single user-defined rule with an optional note.
type RuleEntry struct {
	Pattern string `json:"pattern"`
	Note    string `json:"note,omitempty"`
}

const UserRulesSchema = 1

// DefaultUserRules returns an empty UserRules with the current schema.
func DefaultUserRules() UserRules {
	return UserRules{
		Schema:           UserRulesSchema,
		Exclude:          []RuleEntry{},
		SafeHarbors:      []RuleEntry{},
		SuppressDefaults: []string{},
		SuppressHarbors:  []string{},
	}
}

// UserRulesPath returns the path to ws/ignore.json in the workspace.
func UserRulesPath(workspacePath string) string {
	return filepath.Join(workspacePath, "ws", "ignore.json")
}

// LoadUserRules loads ws/ignore.json. Returns default rules if the file
// does not exist.
func LoadUserRules(path string) (UserRules, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultUserRules(), nil
		}
		return UserRules{}, err
	}

	rules := DefaultUserRules()
	if err := json.Unmarshal(data, &rules); err != nil {
		return UserRules{}, err
	}

	if rules.Schema > UserRulesSchema {
		return UserRules{}, errors.New("unsupported ignore.json schema: upgrade ws binary")
	}

	return rules, nil
}

// SaveUserRules writes ws/ignore.json.
func SaveUserRules(path string, rules UserRules) error {
	if rules.Schema == 0 {
		rules.Schema = UserRulesSchema
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// AddUserExclude adds an exclude pattern to user rules if not already present.
// Returns true if the rule was actually added (not a duplicate).
func AddUserExclude(path string, pattern, note string) (bool, error) {
	rules, err := LoadUserRules(path)
	if err != nil {
		return false, err
	}

	for _, e := range rules.Exclude {
		if e.Pattern == pattern {
			return false, nil
		}
	}

	rules.Exclude = append(rules.Exclude, RuleEntry{Pattern: pattern, Note: note})
	return true, SaveUserRules(path, rules)
}

// AddUserSafeHarbor adds a safe harbor pattern to user rules if not already present.
// Rejects "ws/**" since the ws/ harbor cannot be suppressed.
func AddUserSafeHarbor(path string, pattern, note string) (bool, error) {
	if pattern == "ws" || pattern == "ws/**" {
		return false, errors.New("ws/ safe harbor cannot be modified via user rules")
	}

	rules, err := LoadUserRules(path)
	if err != nil {
		return false, err
	}

	for _, h := range rules.SafeHarbors {
		if h.Pattern == pattern {
			return false, nil
		}
	}

	rules.SafeHarbors = append(rules.SafeHarbors, RuleEntry{Pattern: pattern, Note: note})
	return true, SaveUserRules(path, rules)
}

// BuildEngine creates an Engine from built-in defaults + user rules.
// The generated rule set follows megaignore evaluation order:
//   1. Built-in exclude rules (minus suppress_defaults)
//   2. User exclude rules
//   3. Built-in safe harbors (minus suppress_harbors)
//   4. User safe harbors
//
// Last-match-wins semantics apply.
func BuildEngine(userRules UserRules) *Engine {
	suppressedDefaults := make(map[string]bool)
	for _, s := range userRules.SuppressDefaults {
		suppressedDefaults[s] = true
	}

	suppressedHarbors := make(map[string]bool)
	for _, s := range userRules.SuppressHarbors {
		suppressedHarbors[s] = true
	}

	var rules []Rule

	// 1. Built-in exclude rules (filtered by suppress_defaults).
	for _, dr := range defaultExcludeRules {
		if suppressedDefaults[dr.Pattern] {
			continue
		}
		rules = append(rules, dr)
	}

	// 2. User exclude rules.
	for _, e := range userRules.Exclude {
		p := strings.TrimSpace(e.Pattern)
		if p == "" {
			continue
		}
		rules = append(rules, userPatternToRule(p, false))
	}

	// 3. Built-in safe harbors (filtered by suppress_harbors).
	for _, hr := range defaultHarborRules {
		if suppressedHarbors[hr.Pattern] {
			continue
		}
		rules = append(rules, hr)
	}

	// 4. User safe harbors.
	for _, h := range userRules.SafeHarbors {
		p := strings.TrimSpace(h.Pattern)
		if p == "" {
			continue
		}
		rules = append(rules, userPatternToRule(p, true))
	}

	return &Engine{Rules: rules}
}

// userPatternToRule converts a user pattern string to a Rule.
// Patterns containing * or ** are treated as glob rules.
func userPatternToRule(pattern string, include bool) Rule {
	prefix := "-:"
	if include {
		prefix = "+:"
	}

	isGlob := strings.Contains(pattern, "*")
	if isGlob {
		prefix = "-g:"
		if include {
			prefix = "+g:"
		}
	}

	return Rule{
		Raw:     prefix + pattern,
		Include: include,
		Glob:    isGlob,
		Pattern: pattern,
	}
}

// GenerateMegaignore produces the .megaignore file content from defaults + user rules.
// The output is a generated file with a header comment.
func GenerateMegaignore(userRules UserRules) string {
	var sb strings.Builder

	sb.WriteString("# Generated by ws — do not edit manually.\n")
	sb.WriteString("# Edit user rules: ws ignore edit\n")
	sb.WriteString("# Source: ws/ignore.json\n")
	sb.WriteString("\n")

	suppressedDefaults := make(map[string]bool)
	for _, s := range userRules.SuppressDefaults {
		suppressedDefaults[s] = true
	}
	suppressedHarbors := make(map[string]bool)
	for _, s := range userRules.SuppressHarbors {
		suppressedHarbors[s] = true
	}

	// Default exclude rules.
	sb.WriteString("# Default exclude rules\n")
	for _, dr := range defaultExcludeRules {
		if suppressedDefaults[dr.Pattern] {
			continue
		}
		sb.WriteString(dr.Raw + "\n")
	}

	// User exclude rules.
	if len(userRules.Exclude) > 0 {
		sb.WriteString("\n# User exclude rules\n")
		for _, e := range userRules.Exclude {
			p := strings.TrimSpace(e.Pattern)
			if p == "" {
				continue
			}
			r := userPatternToRule(p, false)
			if e.Note != "" {
				sb.WriteString("# " + e.Note + "\n")
			}
			sb.WriteString(r.Raw + "\n")
		}
	}

	// Built-in safe harbors.
	sb.WriteString("\n# Safe harbors\n")
	for _, hr := range defaultHarborRules {
		if suppressedHarbors[hr.Pattern] {
			continue
		}
		sb.WriteString(hr.Raw + "\n")
	}

	// User safe harbors.
	if len(userRules.SafeHarbors) > 0 {
		sb.WriteString("\n# User safe harbors\n")
		for _, h := range userRules.SafeHarbors {
			p := strings.TrimSpace(h.Pattern)
			if p == "" {
				continue
			}
			r := userPatternToRule(p, true)
			if h.Note != "" {
				sb.WriteString("# " + h.Note + "\n")
			}
			sb.WriteString(r.Raw + "\n")
		}
	}

	// Final sentinel.
	sb.WriteString("\n# Sync everything else\n")
	sb.WriteString("-s:*\n")

	return sb.String()
}

// WriteMegaignore generates and writes the .megaignore file from user rules.
func WriteMegaignore(megaignorePath string, userRules UserRules) error {
	content := GenerateMegaignore(userRules)
	return os.WriteFile(megaignorePath, []byte(content), 0o644)
}

// DefaultRuleStats returns counts of default and user rules for display.
type RuleStats struct {
	DefaultExclude   int
	DefaultHarbors   int
	UserExclude      int
	UserHarbors      int
	SuppressedDefaults int
	SuppressedHarbors  int
	Total            int
}

func GetRuleStats(userRules UserRules) RuleStats {
	suppD := 0
	for _, s := range userRules.SuppressDefaults {
		for _, dr := range defaultExcludeRules {
			if dr.Pattern == s {
				suppD++
				break
			}
		}
	}
	suppH := 0
	for _, s := range userRules.SuppressHarbors {
		for _, hr := range defaultHarborRules {
			if hr.Pattern == s {
				suppH++
				break
			}
		}
	}

	stats := RuleStats{
		DefaultExclude:     len(defaultExcludeRules) - suppD,
		DefaultHarbors:     len(defaultHarborRules) - suppH,
		UserExclude:        len(userRules.Exclude),
		UserHarbors:        len(userRules.SafeHarbors),
		SuppressedDefaults: suppD,
		SuppressedHarbors:  suppH,
	}
	stats.Total = stats.DefaultExclude + stats.DefaultHarbors + stats.UserExclude + stats.UserHarbors
	return stats
}
