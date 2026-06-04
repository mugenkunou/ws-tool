package scratch

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/manifest"
)

// TagCollection is the workspace-level tag vocabulary stored in manifest.json.
type TagCollection struct {
	Tags []string `json:"tags"`
}

// LoadTags reads the tag collection from the manifest at wsDir/manifest.json.
// Returns an empty collection (no error) if the manifest does not exist.
func LoadTags(wsDir string) (TagCollection, error) {
	p := filepath.Join(wsDir, "manifest.json")
	m, err := manifest.Load(p)
	if err != nil {
		if os.IsNotExist(err) {
			return TagCollection{Tags: []string{}}, nil
		}
		return TagCollection{}, err
	}
	tags := m.ScratchTags
	if tags == nil {
		tags = []string{}
	}
	return TagCollection{Tags: tags}, nil
}

// SaveTags writes the tag collection into the manifest at wsDir/manifest.json.
func SaveTags(wsDir string, tc TagCollection) error {
	if tc.Tags == nil {
		tc.Tags = []string{}
	}
	sort.Strings(tc.Tags)

	p := filepath.Join(wsDir, "manifest.json")
	m, err := manifest.Load(p)
	if err != nil {
		if os.IsNotExist(err) {
			m = manifest.Default()
		} else {
			return err
		}
	}
	m.ScratchTags = tc.Tags
	return manifest.Save(p, m)
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
