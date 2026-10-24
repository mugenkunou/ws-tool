// Package wsconfig defines and manages ws.json — the single unified
// workspace configuration (state) file.
//
// ws.json lives at the workspace root (e.g. ~/Workspace/ws.json) and syncs
// with the workspace. There is no separate machine-global config file; this
// single file is the source of truth. Only runtime-only state that must stay
// machine-local (cron.state, log-active.json, scratch .ws-meta.json) remains
// outside the workspace.
//
// Path resolution order:
//
//	--config flag → $WS_CONFIG → <workspace>/ws.json
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

	// ConfigRelPath is the path of ws.json relative to the workspace root.
	ConfigRelPath = "ws.json"
)

// WsConfig is the single workspace configuration file (ws.json at the
// workspace root). All fields are synced with the workspace. Machine-local
// state is stored under ~/.local/state/ws-tool/ instead.
//
// ws.json always holds every key (spec §2.3): no field is omitted because it
// is empty or still the default, and the field order below is the file's order.
type WsConfig struct {
	Schema      int              `json:"schema"`
	Editor      string           `json:"editor"`
	Scratch     Scratch          `json:"scratch"`
	Repo        Repo             `json:"repo"`
	Dotfile     Dotfile          `json:"dotfile"`
	Secret      Secret           `json:"secret"`
	Pass        Pass             `json:"pass"`
	Ignore      Ignore           `json:"ignore"`
	IgnoreRules ignore.UserRules `json:"ignore_rules"`
	Session     Session          `json:"session"`
	Links       []Link           `json:"links"`
}

// Scratch holds scratch-directory settings.
type Scratch struct {
	RootDir string   `json:"root_dir"`
	Tags    []string `json:"tags"`
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
	RemoteURL  string `json:"remote_url"`
	Branch     string `json:"branch"`
	AutoCommit bool   `json:"auto_commit"`
	AutoPush   bool   `json:"auto_push"`
	PassEntry  string `json:"pass_entry"` // pass(1) entry holding an API token for the visibility check
}

// Secret holds secret-scanning settings and the allowlist.
type Secret struct {
	Allowlist []string `json:"allowlist"`
	SkipDirs  []string `json:"skip_dirs"`
}

// Pass holds pass-store settings.
type Pass struct {
	PassEntry string `json:"pass_entry"` // pass(1) entry holding an API token for the pass backup's visibility check
}

// Ignore holds sync-hygiene thresholds and the muted paths.
type Ignore struct {
	WarnSizeMB int      `json:"warn_size_mb"`
	CritSizeMB int      `json:"crit_size_mb"`
	MaxDepth   int      `json:"max_depth"`
	Muted      []string `json:"muted"` // paths whose sync warnings are hidden (they keep syncing)
}

// Session holds session-recording settings (the old "log" key).
type Session struct {
	CapMB        int `json:"cap_mb"`
	MaxSessionMB int `json:"max_session_mb"`
}

// Link is a single entry in the link registry.
// It records a system path → workspace-relative original mapping.
// When Dir is true, the system path is a directory link (e.g. ~/.password-store).
// When Sudo is true, creating the symlink requires root.
type Link struct {
	System   string `json:"system"`   // absolute system path (symlink source)
	Original string `json:"original"` // workspace-relative path of the original
	Dir      bool   `json:"dir"`      // true if the system path is a directory link
	Sudo     bool   `json:"sudo"`     // true if the symlink needs sudo
}

// Default returns the out-of-the-box WsConfig written by `ws init`.
func Default() WsConfig {
	return WsConfig{
		Schema: CurrentSchema,
		Scratch: Scratch{
			RootDir: "~/Scratch",
			Tags:    []string{},
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
			SkipDirs:  []string{},
		},
		Ignore: Ignore{
			WarnSizeMB: 1,
			CritSizeMB: 10,
			MaxDepth:   6,
			Muted:      []string{},
		},
		IgnoreRules: ignore.DefaultUserRules(),
		Session: Session{
			CapMB:        500,
			MaxSessionMB: 100,
		},
		Links: []Link{},
	}
}

// normalize replaces nil lists with empty ones, so a write always shows every
// key as [] rather than null.
func normalize(cfg *WsConfig) {
	if cfg.Scratch.Tags == nil {
		cfg.Scratch.Tags = []string{}
	}
	if cfg.Repo.Roots == nil {
		cfg.Repo.Roots = []string{"."}
	}
	if cfg.Repo.ExcludeDirs == nil {
		cfg.Repo.ExcludeDirs = []string{}
	}
	if cfg.Secret.Allowlist == nil {
		cfg.Secret.Allowlist = []string{}
	}
	if cfg.Secret.SkipDirs == nil {
		cfg.Secret.SkipDirs = []string{}
	}
	if cfg.Ignore.Muted == nil {
		cfg.Ignore.Muted = []string{}
	}
	if cfg.IgnoreRules.Exclude == nil {
		cfg.IgnoreRules.Exclude = []ignore.RuleEntry{}
	}
	if cfg.IgnoreRules.SafeHarbors == nil {
		cfg.IgnoreRules.SafeHarbors = []ignore.RuleEntry{}
	}
	if cfg.IgnoreRules.SuppressDefaults == nil {
		cfg.IgnoreRules.SuppressDefaults = []string{}
	}
	if cfg.IgnoreRules.SuppressHarbors == nil {
		cfg.IgnoreRules.SuppressHarbors = []string{}
	}
	if cfg.Links == nil {
		cfg.Links = []Link{}
	}
}

// LoadIgnoreRules loads only the ignore rules from ws.json.
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

// RemoveIgnoreExclude atomically removes the exclude entry whose pattern is
// pattern. Returns true if one was found and removed.
func RemoveIgnoreExclude(configPath, pattern string) (bool, error) {
	cfg, err := Load(configPath)
	if err != nil {
		return false, err
	}
	updated := make([]ignore.RuleEntry, 0, len(cfg.IgnoreRules.Exclude))
	found := false
	for _, e := range cfg.IgnoreRules.Exclude {
		if e.Pattern == pattern {
			found = true
			continue
		}
		updated = append(updated, e)
	}
	if !found {
		return false, nil
	}
	cfg.IgnoreRules.Exclude = updated
	return true, Save(configPath, cfg)
}

// AddIgnoreMuted atomically adds a workspace-relative path to ignore.muted.
// Returns true if it was added (not already listed).
func AddIgnoreMuted(configPath, rel string) (bool, error) {
	cfg, err := Load(configPath)
	if err != nil {
		return false, err
	}
	for _, m := range cfg.Ignore.Muted {
		if m == rel {
			return false, nil
		}
	}
	cfg.Ignore.Muted = append(cfg.Ignore.Muted, rel)
	return true, Save(configPath, cfg)
}

// RemoveIgnoreMuted atomically removes the ignore.muted entry that names rel
// (in any written form: relative, absolute or with ~). Returns true if one was
// removed.
func RemoveIgnoreMuted(configPath, workspacePath, rel string) (bool, error) {
	cfg, err := Load(configPath)
	if err != nil {
		return false, err
	}
	updated := make([]string, 0, len(cfg.Ignore.Muted))
	found := false
	for _, m := range cfg.Ignore.Muted {
		n := ignore.NormalizeMuted(workspacePath, []string{m})
		if len(n) == 1 && n[0] == rel {
			found = true
			continue
		}
		updated = append(updated, m)
	}
	if !found {
		return false, nil
	}
	cfg.Ignore.Muted = updated
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

// ConfigPath returns the absolute path of ws.json for a given workspace root.
func ConfigPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, filepath.FromSlash(ConfigRelPath))
}

// Load reads and parses ws.json from the given path.
// Returns an error if the file is missing, invalid JSON, or from a newer schema.
// Keys missing from the file take their defaults, and the next Save writes
// them. The old "log" key is read when "session" is absent.
func Load(path string) (WsConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return WsConfig{}, err
	}

	cfg := Default()
	if err := json.Unmarshal(content, &cfg); err != nil {
		return WsConfig{}, fmt.Errorf("parsing %s: %w", path, err)
	}

	// Renamed keys are still read; only the new name is written.
	var legacy struct {
		Session json.RawMessage `json:"session"`
		Log     json.RawMessage `json:"log"`
	}
	if err := json.Unmarshal(content, &legacy); err == nil && legacy.Session == nil && legacy.Log != nil {
		_ = json.Unmarshal(legacy.Log, &cfg.Session)
	}

	if cfg.Schema <= 0 {
		return WsConfig{}, errors.New("ws.json: schema must be a positive integer")
	}
	if cfg.Schema > CurrentSchema {
		return WsConfig{}, fmt.Errorf("ws.json: unsupported schema %d (this build supports up to %d)", cfg.Schema, CurrentSchema)
	}

	normalize(&cfg)
	return cfg, nil
}

// Save writes cfg to path as pretty-printed JSON, creating parent dirs if needed.
func Save(path string, cfg WsConfig) error {
	if cfg.Schema == 0 {
		cfg.Schema = CurrentSchema
	}
	normalize(&cfg)
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
	out.Links = make([]Link, len(cfg.Links))
	for i, l := range cfg.Links {
		l.Original = abs(l.Original)
		out.Links[i] = l
	}
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
