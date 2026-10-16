// Package wsconfig defines and manages configs/ws.json — the single unified
// workspace configuration file introduced in the phase-1 rearchitecture.
//
// configs/ws.json lives inside ~/Workspace/configs/ and syncs via MEGA.
// It replaces both the old XDG ~/.config/ws-tool/config.json and the old
// ws/manifest.json. Only runtime-only state that must stay machine-local
// (cron.state, log-active.json, scratch .ws-meta.json) remains outside the
// workspace.
//
// Path resolution order:
//
//	--config flag → $WS_CONFIG → <workspace>/configs/ws.json
package wsconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/ignore"
)

const (
	// CurrentSchema is the ws.json schema version written by this build.
	CurrentSchema = 1

	// ConfigRelPath is the path of configs/ws.json relative to the workspace root.
	ConfigRelPath = "configs/ws.json"
)

// WsConfig is the single workspace configuration file (configs/ws.json).
// All fields are synced with the workspace. Machine-local state is stored
// under ~/.local/state/ws-tool/ instead.
type WsConfig struct {
	Schema      int                  `json:"schema"`
	Scratch     Scratch              `json:"scratch"`
	Repo        Repo                 `json:"repo"`
	Dotfile     Dotfile              `json:"dotfile"`
	Secret      Secret               `json:"secret"`
	Ignore      Ignore               `json:"ignore"`
	IgnoreRules ignore.UserRules     `json:"ignore_rules"`
	Log         Log                  `json:"log"`
	Links       []Link               `json:"links"`
}

// Scratch holds scratch-directory settings.
type Scratch struct {
	RootDir        string   `json:"root_dir"`
	EditorCmd      string   `json:"editor_cmd"`
	PruneAfterDays int      `json:"prune_after_days"`
	Tags           []string `json:"tags,omitempty"`
}

// Repo holds repository fleet settings.
type Repo struct {
	Roots       []string `json:"roots"`
	ExcludeDirs []string `json:"exclude_dirs"`
	MaxParallel int      `json:"max_parallel"`
}

// Dotfile holds dotfile-management settings.
type Dotfile struct {
	Git DotfileGit `json:"git"`
}

// DotfileGit holds dotfile git-backup settings (synced with the workspace).
type DotfileGit struct {
	RemoteURL  string `json:"remote_url,omitempty"`
	Branch     string `json:"branch,omitempty"`
	AutoCommit bool   `json:"auto_commit"`
	AutoPush   bool   `json:"auto_push"`
	PassEntry  string `json:"pass_entry,omitempty"` // pass(1) entry for git credentials
}

// Secret holds secret-scanning settings and the allowlist.
type Secret struct {
	Allowlist []string `json:"allowlist"`
	SkipDirs  []string `json:"skip_dirs,omitempty"`
}

// Ignore holds sync-hygiene thresholds.
type Ignore struct {
	WarnSizeMB int `json:"warn_size_mb"`
	CritSizeMB int `json:"crit_size_mb"`
	MaxDepth   int `json:"max_depth"`
}

// Log holds session-recording settings.
type Log struct {
	CapMB        int `json:"cap_mb"`
	MaxSessionMB int `json:"max_session_mb"`
}


// Link is a single entry in the link registry.
// It records a system path → workspace-relative original mapping.
// When Dir is true, the system path is a directory link (e.g. ~/.password-store).
// When Sudo is true, creating the symlink requires root.
type Link struct {
	System   string `json:"system"`            // absolute system path (symlink source)
	Original string `json:"original"`          // workspace-relative path of the original
	Dir      bool   `json:"dir,omitempty"`     // true if the system path is a directory link
	Sudo     bool   `json:"sudo,omitempty"`    // true if the symlink needs sudo
	Class    string `json:"class,omitempty"`   // "config" or "secret" (informational)
}

// Default returns the out-of-the-box WsConfig written by `ws init`.
func Default() WsConfig {
	return WsConfig{
		Schema: CurrentSchema,
		Scratch: Scratch{
			RootDir:        "~/Scratch",
			EditorCmd:      "code",
			PruneAfterDays: 90,
		},
		Repo: Repo{
			Roots:       []string{"."},
			ExcludeDirs: []string{"node_modules", ".venv"},
			MaxParallel: 8,
		},
		Dotfile: Dotfile{
			Git: DotfileGit{
				Branch:     "main",
				AutoCommit: true,
				AutoPush:   true,
			},
		},
		Secret: Secret{
			Allowlist: []string{},
		},
		Ignore: Ignore{
			WarnSizeMB: 1,
			CritSizeMB: 10,
			MaxDepth:   6,
		},
		IgnoreRules: ignore.DefaultUserRules(),
		Log: Log{
			CapMB:        500,
			MaxSessionMB: 100,
		},
		Links: []Link{},
	}
}

// LoadIgnoreRules loads only the ignore rules from configs/ws.json.
// Returns default rules if the file is missing.
func LoadIgnoreRules(configPath string) (ignore.UserRules, error) {
	cfg, err := Load(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ignore.DefaultUserRules(), nil
		}
		return ignore.DefaultUserRules(), err
	}
	return cfg.IgnoreRules, nil
}

// AddIgnoreExclude atomically adds an exclude pattern to the config's ignore rules.
// Returns true if the pattern was added (not already present).
func AddIgnoreExclude(configPath, pattern, note string) (bool, error) {
	cfg, err := Load(configPath)
	if err != nil {
		return false, err
	}
	for _, e := range cfg.IgnoreRules.Exclude {
		if e.Pattern == pattern {
			return false, nil
		}
	}
	cfg.IgnoreRules.Exclude = append(cfg.IgnoreRules.Exclude, ignore.RuleEntry{Pattern: pattern, Note: note})
	return true, Save(configPath, cfg)
}

// AddIgnoreSafeHarbor atomically adds a safe-harbor pattern.
// Returns true if the pattern was added.
func AddIgnoreSafeHarbor(configPath, pattern, note string) (bool, error) {
	if pattern == "ws" || pattern == "ws/**" {
		return false, errors.New("ws/ safe harbor cannot be modified via user rules")
	}
	cfg, err := Load(configPath)
	if err != nil {
		return false, err
	}
	for _, h := range cfg.IgnoreRules.SafeHarbors {
		if h.Pattern == pattern {
			return false, nil
		}
	}
	cfg.IgnoreRules.SafeHarbors = append(cfg.IgnoreRules.SafeHarbors, ignore.RuleEntry{Pattern: pattern, Note: note})
	return true, Save(configPath, cfg)
}

// RemoveIgnoreSafeHarbor atomically removes a safe-harbor pattern.
// Returns true if the pattern was found and removed.
func RemoveIgnoreSafeHarbor(configPath, pattern string) (bool, error) {
	cfg, err := Load(configPath)
	if err != nil {
		return false, err
	}
	updated := cfg.IgnoreRules.SafeHarbors[:0:0]
	found := false
	for _, h := range cfg.IgnoreRules.SafeHarbors {
		if h.Pattern == pattern {
			found = true
			continue
		}
		updated = append(updated, h)
	}
	if !found {
		return false, nil
	}
	cfg.IgnoreRules.SafeHarbors = updated
	return true, Save(configPath, cfg)
}

// AddToAllowlist records anchor in the secret allowlist (idempotent).
func AddToAllowlist(configPath, anchor string) error {
	cfg, err := Load(configPath)
	if err != nil {
		return err
	}
	for _, a := range cfg.Secret.Allowlist {
		if a == anchor {
			return nil
		}
	}
	cfg.Secret.Allowlist = append(cfg.Secret.Allowlist, anchor)
	return Save(configPath, cfg)
}

// AllowlistMap builds the scan allowlist map from the config.
func AllowlistMap(cfg WsConfig) map[string]struct{} {
	allow := make(map[string]struct{}, len(cfg.Secret.Allowlist))
	for _, a := range cfg.Secret.Allowlist {
		allow[a] = struct{}{}
	}
	return allow
}

// ConfigPath returns the absolute path of configs/ws.json for a given workspace root.
func ConfigPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, filepath.FromSlash(ConfigRelPath))
}

// Load reads and parses configs/ws.json from the given path.
// Returns an error if the file is missing, invalid JSON, or from a newer schema.
func Load(path string) (WsConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return WsConfig{}, err
	}

	cfg := Default()
	if err := json.Unmarshal(content, &cfg); err != nil {
		return WsConfig{}, fmt.Errorf("parsing %s: %w", path, err)
	}

	if cfg.Schema <= 0 {
		return WsConfig{}, errors.New("ws.json: schema must be a positive integer")
	}
	if cfg.Schema > CurrentSchema {
		return WsConfig{}, fmt.Errorf("ws.json: unsupported schema %d (this build supports up to %d)", cfg.Schema, CurrentSchema)
	}

	// Ensure non-nil slices.
	if cfg.Links == nil {
		cfg.Links = []Link{}
	}
	if cfg.Secret.Allowlist == nil {
		cfg.Secret.Allowlist = []string{}
	}
	if cfg.Repo.Roots == nil {
		cfg.Repo.Roots = []string{"."}
	}
	if cfg.Repo.ExcludeDirs == nil {
		cfg.Repo.ExcludeDirs = []string{}
	}

	return cfg, nil
}

// Save writes cfg to path as pretty-printed JSON, creating parent dirs if needed.
func Save(path string, cfg WsConfig) error {
	if cfg.Schema == 0 {
		cfg.Schema = CurrentSchema
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o644)
}

// AddLink adds or replaces a link entry (matched by system path).
func AddLink(path string, link Link) error {
	cfg, err := Load(path)
	if err != nil {
		return err
	}
	for i, l := range cfg.Links {
		if filepath.Clean(l.System) == filepath.Clean(link.System) {
			cfg.Links[i] = link
			return Save(path, cfg)
		}
	}
	cfg.Links = append(cfg.Links, link)
	return Save(path, cfg)
}

// RemoveLink removes the link entry whose System matches systemPath.
// Returns false if no entry was found (idempotent).
func RemoveLink(path, systemPath string) (bool, error) {
	cfg, err := Load(path)
	if err != nil {
		return false, err
	}
	n := len(cfg.Links)
	filtered := cfg.Links[:0:0]
	for _, l := range cfg.Links {
		if filepath.Clean(l.System) != filepath.Clean(systemPath) {
			filtered = append(filtered, l)
		}
	}
	if len(filtered) == n {
		return false, nil
	}
	cfg.Links = filtered
	return true, Save(path, cfg)
}

// Exists reports whether the config file exists at path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ExpandUserPath expands a leading ~ to the user's home directory.
// Absolute paths are returned cleaned. Relative paths are returned as-is
// (callers that need absolute resolution should use ResolvePath).
func ExpandUserPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", errors.New("path cannot be empty")
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if trimmed == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(trimmed, "~/")), nil
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed), nil
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// ResolvePath resolves a config value against workspaceRoot:
//   - "~" / "~/..." → home-expanded absolute
//   - already absolute → cleaned as-is
//   - relative → joined with workspaceRoot
func ResolvePath(workspaceRoot, value string) (string, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return "", errors.New("value cannot be empty")
	}
	if v == "~" || strings.HasPrefix(v, "~/") {
		return ExpandUserPath(v)
	}
	if filepath.IsAbs(v) {
		return filepath.Clean(v), nil
	}
	root, err := ExpandUserPath(workspaceRoot)
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(root, v)), nil
}

// WithAbsPaths returns a copy of cfg with every path-valued setting resolved
// against workspacePath. It is used wherever config values are displayed
// (spec "Path Rules"). cfg itself is not modified. Empty values stay empty.
func WithAbsPaths(cfg WsConfig, workspacePath string) WsConfig {
	abs := func(v string) string {
		if strings.TrimSpace(v) == "" {
			return v
		}
		p, err := ResolvePath(workspacePath, v)
		if err != nil {
			return v
		}
		return p
	}
	absAll := func(vs []string) []string {
		if vs == nil {
			return nil
		}
		out := make([]string, len(vs))
		for i, v := range vs {
			out[i] = abs(v)
		}
		return out
	}

	out := cfg
	out.Scratch.RootDir = abs(cfg.Scratch.RootDir)
	out.Repo.Roots = absAll(cfg.Repo.Roots)
	out.Repo.ExcludeDirs = absAll(cfg.Repo.ExcludeDirs)
	out.Secret.SkipDirs = absAll(cfg.Secret.SkipDirs)
	return out
}

// StateDir returns the XDG-compliant state directory for ws-tool
// (machine-local, not synced).
// Resolution: $XDG_STATE_HOME/ws-tool → ~/.local/state/ws-tool
func StateDir() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "ws-tool"), nil
}

// DefaultWorkspace returns the default workspace root path (~/Workspace).
func DefaultWorkspace() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Workspace"), nil
}
