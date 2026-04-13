package scratch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const metaFile = ".ws-meta.json"

// Meta holds per-scratch-directory metadata.
type Meta struct {
	Tags    []string  `json:"tags"`
	Created time.Time `json:"created"`
}

// LoadMeta reads the .ws-meta.json file from a scratch directory.
// Returns a zero Meta (no error) if the file does not exist.
func LoadMeta(scratchDir string) (Meta, error) {
	p := filepath.Join(scratchDir, metaFile)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return Meta{Tags: []string{}}, nil
		}
		return Meta{}, err
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return Meta{}, err
	}
	if m.Tags == nil {
		m.Tags = []string{}
	}
	return m, nil
}

// SaveMeta writes the .ws-meta.json file into a scratch directory.
func SaveMeta(scratchDir string, m Meta) error {
	if m.Tags == nil {
		m.Tags = []string{}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(scratchDir, metaFile), data, 0o644)
}
