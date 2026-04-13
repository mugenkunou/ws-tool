package scratch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// TagCollection is the workspace-level tag vocabulary stored in ws/tags.json.
type TagCollection struct {
	Tags []string `json:"tags"`
}

// LoadTags reads the tag collection from wsDir/tags.json.
// Returns an empty collection (no error) if the file does not exist.
func LoadTags(wsDir string) (TagCollection, error) {
	p := filepath.Join(wsDir, "tags.json")
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return TagCollection{Tags: []string{}}, nil
		}
		return TagCollection{}, err
	}
	var tc TagCollection
	if err := json.Unmarshal(data, &tc); err != nil {
		return TagCollection{}, err
	}
	if tc.Tags == nil {
		tc.Tags = []string{}
	}
	return tc, nil
}

// SaveTags writes the tag collection to wsDir/tags.json.
func SaveTags(wsDir string, tc TagCollection) error {
	if tc.Tags == nil {
		tc.Tags = []string{}
	}
	sort.Strings(tc.Tags)
	data, err := json.MarshalIndent(tc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(wsDir, "tags.json"), data, 0o644)
}

// MergeTags adds new tags to the collection, deduplicating and sorting.
// Returns true if any new tags were added.
func MergeTags(tc *TagCollection, newTags []string) bool {
	existing := make(map[string]struct{}, len(tc.Tags))
	for _, t := range tc.Tags {
		existing[t] = struct{}{}
	}
	added := false
	for _, t := range newTags {
		t = NormalizeTag(t)
		if t == "" {
			continue
		}
		if _, ok := existing[t]; !ok {
			tc.Tags = append(tc.Tags, t)
			existing[t] = struct{}{}
			added = true
		}
	}
	sort.Strings(tc.Tags)
	return added
}

// NormalizeTag lowercases and trims a tag string.
func NormalizeTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}
