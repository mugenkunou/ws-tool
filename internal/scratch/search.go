package scratch

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// SearchResult represents a scratch directory that matched a search query.
type SearchResult struct {
	Entry   Entry  `json:"entry"`
	Score   int    `json:"score"`
	MatchOn string `json:"match_on"` // "tag", "name", "content"
	Snippet string `json:"snippet,omitempty"`
}

// SearchOptions controls scratch search behavior.
type SearchOptions struct {
	RootDir    string
	Query      string
	SortBy     string
	MaxResults int
}

// Search finds scratch directories matching the query by tag, name, or file content.
// Scoring: tag match = 3, name match = 2, content match = 1.
func Search(opts SearchOptions) ([]SearchResult, error) {
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return nil, nil
	}
	tokens := strings.Fields(strings.ToLower(query))

	entries, err := List(ListOptions{RootDir: opts.RootDir, SortBy: "age"})
	if err != nil {
		return nil, err
	}

	var results []SearchResult
	for _, e := range entries {
		best := matchEntry(e, tokens)
		if best.Score > 0 {
			results = append(results, best)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Entry.Age < results[j].Entry.Age
	})

	if opts.MaxResults > 0 && len(results) > opts.MaxResults {
		results = results[:opts.MaxResults]
	}
	return results, nil
}

func matchEntry(e Entry, tokens []string) SearchResult {
	res := SearchResult{Entry: e}

	// Tag match (score 3): all tokens found across tags.
	if matchAllIn(tokens, e.Tags) {
		res.Score = 3
		res.MatchOn = "tag"
		return res
	}

	// Name match (score 2): all tokens in directory name.
	nameLower := strings.ToLower(stripDateSuffix(e.Name))
	if containsAllTokens(nameLower, tokens) {
		res.Score = 2
		res.MatchOn = "name"
		return res
	}

	// Content match (score 1): grep text files for tokens.
	if snippet := searchContent(e.Path, tokens); snippet != "" {
		res.Score = 1
		res.MatchOn = "content"
		res.Snippet = snippet
		return res
	}

	return res
}

// matchAllIn returns true if every token is a substring of at least one tag.
func matchAllIn(tokens []string, tags []string) bool {
	if len(tags) == 0 {
		return false
	}
	for _, t := range tokens {
		found := false
		for _, tag := range tags {
			if strings.Contains(strings.ToLower(tag), t) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func containsAllTokens(haystack string, tokens []string) bool {
	for _, t := range tokens {
		if !strings.Contains(haystack, t) {
			return false
		}
	}
	return true
}

// searchContent scans text files in a scratch directory for all tokens.
// Returns the first matching line as a snippet, or empty string.
func searchContent(dir string, tokens []string) string {
	const maxFiles = 50
	const maxFileSize = 512 * 1024 // 512KB

	visited := 0
	var snippet string

	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if visited >= maxFiles {
			return filepath.SkipAll
		}
		if d.Name() == metaFile {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxFileSize {
			return nil
		}
		visited++

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		// Quick binary check on first 512 bytes.
		probe := make([]byte, 512)
		n, _ := f.Read(probe)
		if n > 0 && !utf8.Valid(probe[:n]) {
			return nil
		}
		if _, err := f.Seek(0, 0); err != nil {
			return nil
		}

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			lower := strings.ToLower(line)
			if containsAllTokens(lower, tokens) {
				snippet = strings.TrimSpace(line)
				if len(snippet) > 120 {
					snippet = snippet[:120] + "…"
				}
				return filepath.SkipAll
			}
		}
		return nil
	})
	return snippet
}
