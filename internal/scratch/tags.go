package scratch

import (
	"sort"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// TagCollection is the workspace-level tag vocabulary stored in configs/ws.json.
type TagCollection struct {
	Tags []string `json:"tags"`
}

// LoadTags reads the tag collection from configs/ws.json.
// configPath is the path to configs/ws.json.
// Returns an empty collection (no error) if the config does not exist.
func LoadTags(configPath string) (TagCollection, error) {
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		return TagCollection{Tags: []string{}}, nil
	}
	tags := cfg.Scratch.Tags
	if tags == nil {
		tags = []string{}
	}
	return TagCollection{Tags: tags}, nil
}

// SaveTags writes the tag collection into configs/ws.json.
func SaveTags(configPath string, tc TagCollection) error {
	if tc.Tags == nil {
		tc.Tags = []string{}
	}
	sort.Strings(tc.Tags)

	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		cfg = wsconfig.Default()
	}
	cfg.Scratch.Tags = tc.Tags
	return wsconfig.Save(configPath, cfg)
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
