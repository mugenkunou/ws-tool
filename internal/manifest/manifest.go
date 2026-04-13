package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const CurrentSchema = 1

type Manifest struct {
	ManifestSchema int             `json:"manifest_schema"`
	Dotfiles       []DotfileRecord `json:"dotfiles"`
	Secret         ManifestSecret  `json:"secret"`
	Repo           ManifestRepo    `json:"repo"`
}

type DotfileRecord struct {
	System string `json:"system"`
	Name   string `json:"name"`
	Sudo   bool   `json:"sudo"`
	Note   string `json:"note,omitempty"`
}

type ManifestSecret struct {
	Allowlist   []string `json:"allowlist"`
	PassEntries []string `json:"pass_entries,omitempty"`
}

type ManifestRepo struct {
	Tracked []RepoRecord `json:"tracked"`
}

type RepoRecord struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Remote string `json:"remote"`
}

func Default() Manifest {
	return Manifest{
		ManifestSchema: CurrentSchema,
		Dotfiles:       []DotfileRecord{},
		Secret:         ManifestSecret{Allowlist: []string{}},
		Repo:           ManifestRepo{Tracked: []RepoRecord{}},
	}
}

func Load(path string) (Manifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}

	m := Default()
	if err := json.Unmarshal(content, &m); err != nil {
		return Manifest{}, err
	}

	if m.ManifestSchema > CurrentSchema {
		return Manifest{}, fmt.Errorf("unsupported manifest schema: %d (max supported: %d)", m.ManifestSchema, CurrentSchema)
	}

	if m.ManifestSchema <= 0 {
		return Manifest{}, errors.New("manifest_schema must be a positive integer")
	}

	return m, nil
}

func Save(path string, m Manifest) error {
	if m.ManifestSchema == 0 {
		m.ManifestSchema = CurrentSchema
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	encoded, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}

	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o644)
}
