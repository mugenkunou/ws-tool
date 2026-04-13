package search

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type Match struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Line    int    `json:"line,omitempty"`
	Snippet string `json:"snippet"`
}

type Options struct {
	WorkspacePath string
	Query         string
	PathFilter    string
	TypeFilter    string
	Context       int
	MaxResults    int
}

func Run(opts Options) ([]Match, error) {
	q := strings.TrimSpace(opts.Query)
	if q == "" {
		return nil, errors.New("query cannot be empty")
	}
	needle := strings.ToLower(q)
	typeFilter := strings.TrimSpace(strings.ToLower(opts.TypeFilter))
	if typeFilter == "" {
		typeFilter = "all"
	}
	maxResults := opts.MaxResults
	unlimited := maxResults <= 0

	pathFilter := strings.TrimSpace(filepath.ToSlash(opts.PathFilter))
	pathFilter = strings.Trim(pathFilter, "/")

	matches := make([]Match, 0)

	err := filepath.WalkDir(opts.WorkspacePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == opts.WorkspacePath {
			return nil
		}

		rel, err := filepath.Rel(opts.WorkspacePath, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "../") {
			return nil
		}

		if pathFilter != "" {
			if rel != pathFilter && !strings.HasPrefix(rel, pathFilter+"/") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		if d.IsDir() {
			return nil
		}

		kind := classify(rel)
		if typeFilter != "all" && kind != typeFilter {
			return nil
		}

		if strings.Contains(strings.ToLower(filepath.Base(rel)), needle) {
			matches = append(matches, Match{
				Kind:    kind,
				Path:    rel,
				Snippet: "filename match",
			})
			if !unlimited && len(matches) >= maxResults {
				return errStop
			}
		}

		if !isTextFile(rel) {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		s := bufio.NewScanner(f)
		buf := make([]byte, 64*1024)
		s.Buffer(buf, 16*1024*1024)
		lineNo := 0
		for s.Scan() {
			lineNo++
			line := s.Text()
			if !utf8.ValidString(line) || strings.ContainsRune(line, '\x00') {
				continue
			}
			if strings.Contains(strings.ToLower(line), needle) {
				matches = append(matches, Match{
					Kind:    kind,
					Path:    rel,
					Line:    lineNo,
					Snippet: strings.TrimSpace(line),
				})
				if !unlimited && len(matches) >= maxResults {
					return errStop
				}
			}
		}
		return nil
	})

	if err != nil && !errors.Is(err, errStop) {
		return nil, err
	}
	return matches, nil
}

var errStop = errors.New("search limit reached")

func classify(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".txt":
		return "note"
	case ".sh", ".bash", ".zsh", ".py", ".go", ".js", ".ts":
		return "script"
	case ".json", ".yaml", ".yml", ".toml", ".ini", ".conf", ".env":
		return "config"
	case ".pdf":
		return "pdf"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
		return "image"
	default:
		return "all"
	}
}

func isTextFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".pdf", ".zip", ".tar", ".gz", ".7z", ".rar", ".exe", ".so", ".dll", ".dylib":
		return false
	default:
		return true
	}
}
