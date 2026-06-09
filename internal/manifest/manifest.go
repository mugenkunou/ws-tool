package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/provision"
)

const CurrentSchema = 3

type Manifest struct {
	ManifestSchema int               `json:"manifest_schema"`
	Dotfiles       []DotfileRecord   `json:"dotfiles"`
	DotfileGit     DotfileGitConfig  `json:"dotfile_git"`
	Secret         ManifestSecret    `json:"secret"`
	Repo           ManifestRepo      `json:"repo"`
	ScratchTags    []string          `json:"scratch_tags,omitempty"`
	Provisions     []provision.Entry `json:"provisions"`
	Ignore         ignore.UserRules  `json:"ignore"`
}

// DotfileGitConfig holds workspace-scoped dotfile git settings that belong
// in the manifest (synced with the workspace) rather than config.json
// (machine-local). Machine-local settings (pass_entry, auth_username) remain
// in config.Dotfile.
type DotfileGitConfig struct {
	RemoteURL  string `json:"remote_url,omitempty"`
	Branch     string `json:"branch,omitempty"`
	AutoCommit bool   `json:"auto_commit"`
	AutoPush   bool   `json:"auto_push"`
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
		DotfileGit:     DotfileGitConfig{Branch: "main", AutoCommit: true, AutoPush: true},
		Secret:         ManifestSecret{Allowlist: []string{}},
		Repo:           ManifestRepo{Tracked: []RepoRecord{}},
		Provisions:     []provision.Entry{},
		Ignore:         ignore.DefaultUserRules(),
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

	// Ensure non-nil slices after unmarshal.
	if m.Provisions == nil {
		m.Provisions = []provision.Entry{}
	}

	// Run migration chain to bring the manifest up to CurrentSchema.
	m = migrate(m)

	return m, nil
}

// migrate applies all in-manifest schema upgrades in sequence.
// Each step upgrades from version N to N+1, filling in defaults for new fields.
func migrate(m Manifest) Manifest {
	// v2 → v3: promote dotfile git workspace settings into the manifest.
	// New fields default to safe values; nothing to copy from the old location
	// (config.json) since that is a separate file the manifest cannot read.
	// The DotfileGit field unmarshals to its zero value on a schema-2 document,
	// so we apply sensible defaults here.
	if m.ManifestSchema < 3 {
		if m.DotfileGit.Branch == "" {
			m.DotfileGit.Branch = "main"
		}
		m.ManifestSchema = 3
	}
	return m
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

// MigrateIfNeeded upgrades a schema-1 manifest to schema-2 by absorbing
// standalone provisions.json and ignore.json files. Old files are removed
// after successful migration. Safe to call repeatedly.
func MigrateIfNeeded(manifestPath string) error {
	m, err := Load(manifestPath)
	if err != nil {
		return nil // file missing or corrupt — nothing to migrate
	}
	if m.ManifestSchema >= CurrentSchema {
		return nil
	}

	wsDir := filepath.Dir(manifestPath)

	// Absorb provisions.json
	provPath := filepath.Join(wsDir, "provisions.json")
	if content, readErr := os.ReadFile(provPath); readErr == nil {
		var ledger provision.Ledger
		if json.Unmarshal(content, &ledger) == nil && len(ledger.Entries) > 0 {
			m.Provisions = ledger.Entries
		}
		os.Remove(provPath)
	}

	// Absorb ignore.json
	ignorePath := filepath.Join(wsDir, "ignore.json")
	if content, readErr := os.ReadFile(ignorePath); readErr == nil {
		var rules ignore.UserRules
		if json.Unmarshal(content, &rules) == nil {
			m.Ignore = rules
		}
		os.Remove(ignorePath)
	}

	m.ManifestSchema = CurrentSchema
	return Save(manifestPath, m)
}

// RecordProvision atomically appends or replaces a provision entry.
// Entries with the same type+path are replaced rather than duplicated.
func RecordProvision(manifestPath string, e provision.Entry) error {
	m, err := Load(manifestPath)
	if err != nil {
		return err
	}
	if e.Time == "" {
		e.Time = time.Now().UTC().Format(time.RFC3339)
	}
	replaced := false
	for i, existing := range m.Provisions {
		if existing.Type == e.Type && existing.Path == e.Path {
			m.Provisions[i] = e
			replaced = true
			break
		}
	}
	if !replaced {
		m.Provisions = append(m.Provisions, e)
	}
	return Save(manifestPath, m)
}

// RecordAllProvisions atomically appends or replaces multiple provision
// entries in a single load-save cycle.
func RecordAllProvisions(manifestPath string, entries []provision.Entry) error {
	m, err := Load(manifestPath)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, e := range entries {
		if e.Time == "" {
			e.Time = now
		}
		replaced := false
		for i, existing := range m.Provisions {
			if existing.Type == e.Type && existing.Path == e.Path {
				m.Provisions[i] = e
				replaced = true
				break
			}
		}
		if !replaced {
			m.Provisions = append(m.Provisions, e)
		}
	}
	return Save(manifestPath, m)
}

// RemoveProvision removes provision entries matching type+path.
func RemoveProvision(manifestPath string, typ provision.Type, path string) error {
	m, err := Load(manifestPath)
	if err != nil {
		return err
	}
	filtered := make([]provision.Entry, 0, len(m.Provisions))
	for _, e := range m.Provisions {
		if e.Type == typ && e.Path == path {
			continue
		}
		filtered = append(filtered, e)
	}
	m.Provisions = filtered
	return Save(manifestPath, m)
}

// RemoveCronJobProvision removes the provision entry for a cron job by name.
func RemoveCronJobProvision(manifestPath string, jobName string) error {
	m, err := Load(manifestPath)
	if err != nil {
		return err
	}
	filtered := make([]provision.Entry, 0, len(m.Provisions))
	for _, e := range m.Provisions {
		if e.Type == provision.TypeCronJob && e.Line == jobName {
			continue
		}
		filtered = append(filtered, e)
	}
	m.Provisions = filtered
	return Save(manifestPath, m)
}

// AddIgnoreExclude atomically adds an exclude rule to the manifest's ignore
// section. Returns true if the rule was added (not a duplicate).
func AddIgnoreExclude(manifestPath string, pattern, note string) (bool, error) {
	m, err := Load(manifestPath)
	if err != nil {
		return false, err
	}
	for _, e := range m.Ignore.Exclude {
		if e.Pattern == pattern {
			return false, nil
		}
	}
	m.Ignore.Exclude = append(m.Ignore.Exclude, ignore.RuleEntry{Pattern: pattern, Note: note})
	return true, Save(manifestPath, m)
}

// AddIgnoreSafeHarbor atomically adds a safe harbor rule to the manifest's
// ignore section. Returns true if the rule was added (not a duplicate).
func AddIgnoreSafeHarbor(manifestPath string, pattern, note string) (bool, error) {
	if pattern == "ws" || pattern == "ws/**" {
		return false, errors.New("ws/ safe harbor cannot be modified via user rules")
	}
	m, err := Load(manifestPath)
	if err != nil {
		return false, err
	}
	for _, h := range m.Ignore.SafeHarbors {
		if h.Pattern == pattern {
			return false, nil
		}
	}
	m.Ignore.SafeHarbors = append(m.Ignore.SafeHarbors, ignore.RuleEntry{Pattern: pattern, Note: note})
	return true, Save(manifestPath, m)
}

// LoadIgnoreRules loads just the ignore rules from the manifest.
func LoadIgnoreRules(manifestPath string) (ignore.UserRules, error) {
	m, err := Load(manifestPath)
	if err != nil {
		return ignore.DefaultUserRules(), err
	}
	return m.Ignore, nil
}

// SaveIgnoreRules updates just the ignore rules in the manifest.
func SaveIgnoreRules(manifestPath string, rules ignore.UserRules) error {
	m, err := Load(manifestPath)
	if err != nil {
		return err
	}
	m.Ignore = rules
	return Save(manifestPath, m)
}
