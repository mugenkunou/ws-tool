package ignore

import (
	"bufio"
	"os"
	"strings"
)

type FixOptions struct {
	MegaignorePath string
	Violations     []Violation
	DryRun         bool
}

type FixResult struct {
	AddedRules []string `json:"added_rules"`
	Messages   []string `json:"messages"`
	DryRun     bool     `json:"dry_run"`
}

func Fix(opts FixOptions) (FixResult, error) {
	result := FixResult{AddedRules: []string{}, Messages: []string{}, DryRun: opts.DryRun}

	if len(opts.Violations) == 0 {
		result.Messages = append(result.Messages, "No violations to fix.")
		return result, nil
	}

	// Build exclude rules for each violation.
	rules := make([]string, 0, len(opts.Violations))
	seen := make(map[string]struct{})
	for _, v := range opts.Violations {
		// Generate a -p: (path-based) exclude rule for the violation path.
		rel := v.Path
		if rel == "" {
			continue
		}
		rule := "-p:" + rel
		if _, ok := seen[rule]; ok {
			continue
		}
		seen[rule] = struct{}{}
		rules = append(rules, rule)
	}

	if len(rules) == 0 {
		result.Messages = append(result.Messages, "No rules to add.")
		return result, nil
	}

	if opts.DryRun {
		result.AddedRules = rules
		for _, r := range rules {
			result.Messages = append(result.Messages, "Would add rule: "+r)
		}
		return result, nil
	}

	added, err := AddRules(opts.MegaignorePath, rules)
	if err != nil {
		return result, err
	}
	result.AddedRules = added
	if len(added) == 0 {
		result.Messages = append(result.Messages, "All rules already present.")
	} else {
		for _, r := range added {
			result.Messages = append(result.Messages, "Added rule: "+r)
		}
	}
	return result, nil
}

func AddRules(megaignorePath string, rules []string) ([]string, error) {
	lines, err := readLines(megaignorePath)
	if err != nil {
		return nil, err
	}

	existing := make(map[string]struct{})
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		existing[t] = struct{}{}
	}

	toAdd := make([]string, 0)
	for _, r := range rules {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if _, ok := existing[r]; ok {
			continue
		}
		existing[r] = struct{}{}
		toAdd = append(toAdd, r)
	}
	if len(toAdd) == 0 {
		return nil, nil
	}

	sentinelIdx := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "-s:*" {
			sentinelIdx = i
			break
		}
	}

	if sentinelIdx < 0 {
		lines = append(lines, toAdd...)
		lines = append(lines, "-s:*")
	} else {
		merged := make([]string, 0, len(lines)+len(toAdd))
		merged = append(merged, lines[:sentinelIdx]...)
		merged = append(merged, toAdd...)
		merged = append(merged, lines[sentinelIdx:]...)
		lines = merged
	}

	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.WriteFile(megaignorePath, []byte(content), 0o644); err != nil {
		return nil, err
	}
	return toAdd, nil
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	lines := make([]string, 0)
	s := bufio.NewScanner(f)
	for s.Scan() {
		lines = append(lines, s.Text())
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}
