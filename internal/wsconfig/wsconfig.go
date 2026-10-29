// Package wsconfig defines and manages ws.json — the single unified
// workspace configuration (state) file.
//
// ws.json lives at the workspace root (e.g. ~/Workspace/ws.json) and syncs
// with the workspace. There is no separate machine-global config file; this
// single file is the source of truth. Only runtime-only state that must stay
// machine-local (cron.state, scratch .ws-meta.json) remains
// outside the workspace.
//
// Path resolution order:
//
//	<workspace>/ws.json
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
	Secret      Secret           `json:"secret"`
	Pass        Pass             `json:"pass"`
	Ignore      Ignore           `json:"ignore"`
	IgnoreRules ignore.UserRules `json:"ignore_rules"`
	Trash       Trash            `json:"trash"`
	Sync        Sync             `json:"sync"`
	Symlinks    []Link           `json:"symlinks"`
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

// Trash holds trash settings.
type Trash struct {
	WarnSizeMB int `json:"warn_size_mb"` // `trash status` warns above this size
}

// Sync holds the one-way mirror settings (spec Factor XII, plan 0004).
// Target has no default: `ws sync` refuses until the user sets it.
type Sync struct {
	Mirror       SyncMirror         `json:"mirror"`
	Exclude      []ignore.RuleEntry `json:"exclude"`        // extra patterns, mirror only (ignore_rules syntax)
	MaxDeletePct int                `json:"max_delete_pct"` // a push deleting more than this share of the mirror asks first
}

// SyncMirror is the folder ws copies the workspace into; a sync client uploads it.
type SyncMirror struct {
	Target string `json:"target"`
}

// DefaultMaxDeletePct is the delete guard used when max_delete_pct is 0.
const DefaultMaxDeletePct = 20

// Link is a single entry in the symlinks registry.
// It records a host path → workspace-relative original mapping.
// When Dir is true, the host path is a directory link (e.g. ~/.password-store).
// When Sudo is true, creating the symlink requires root.
//
// In ws.json System is written with "~" for $HOME, so a machine with another
// user name still works. Load expands it, so every Link a caller sees is absolute.
type Link struct {
	System   string `json:"system"`   // host path (symlink source); absolute in memory, "~/…" in the file
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
		Secret: Secret{
			Allowlist: []string{},
			SkipDirs:  []string{},
		},
		Ignore: Ignore{
			WarnSizeMB: 5,
			CritSizeMB: 10,
			MaxDepth:   6,
			Muted:      []string{},
		},
		IgnoreRules: ignore.DefaultUserRules(),
		Trash:       Trash{WarnSizeMB: 10240},
		Sync:        Sync{Exclude: []ignore.RuleEntry{}, MaxDeletePct: DefaultMaxDeletePct},
		Symlinks:    []Link{},
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
	if cfg.IgnoreRules.Include == nil {
		cfg.IgnoreRules.Include = []ignore.RuleEntry{}
	}
	if cfg.IgnoreRules.SuppressDefaults == nil {
		cfg.IgnoreRules.SuppressDefaults = []string{}
	}
	if cfg.Sync.Exclude == nil {
		cfg.Sync.Exclude = []ignore.RuleEntry{}
	}
	if cfg.Symlinks == nil {
		cfg.Symlinks = []Link{}
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
// them.
func Load(path string) (WsConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return WsConfig{}, err
	}

	cfg := Default()
	if err := json.Unmarshal(content, &cfg); err != nil {
		return WsConfig{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	// Files written before the noun became "symlink" hold the entries under
	// "links". Read them silently; the next Save writes "symlinks".
	var old struct {
		Symlinks json.RawMessage `json:"symlinks"`
		Links    []Link          `json:"links"`
	}
	if err := json.Unmarshal(content, &old); err == nil && old.Symlinks == nil && len(old.Links) > 0 {
		cfg.Symlinks = old.Links
	}
	cfg.Symlinks = mapSystems(cfg.Symlinks, expandHome)

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
	cfg.Symlinks = mapSystems(cfg.Symlinks, collapseHome)
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o644)
}

// mapSystems returns a copy of links with fn applied to every System path.
func mapSystems(links []Link, fn func(string) string) []Link {
	if links == nil {
		return nil
	}
	out := make([]Link, len(links))
	for i, l := range links {
		l.System = fn(l.System)
		out[i] = l
	}
	return out
}

// expandHome turns a leading "~" into $HOME. Any other path is returned as is.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
}

// collapseHome writes a path inside $HOME as "~/…". A path outside it, or $HOME
// itself, is returned as is.
func collapseHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	home = filepath.Clean(home)
	if c := filepath.Clean(p); strings.HasPrefix(c, home+string(os.PathSeparator)) {
		return "~/" + filepath.ToSlash(strings.TrimPrefix(c, home+string(os.PathSeparator)))
	}
	return p
}

// AddLink adds or replaces a link entry (matched by system path).
func AddLink(path string, link Link) error {
	cfg, err := Load(path)
	if err != nil {
		return err
	}
	for i, l := range cfg.Symlinks {
		if filepath.Clean(l.System) == filepath.Clean(link.System) {
			cfg.Symlinks[i] = link
			return Save(path, cfg)
		}
	}
	cfg.Symlinks = append(cfg.Symlinks, link)
	return Save(path, cfg)
}

// RemoveLink removes the link entry whose System matches systemPath.
// Returns false if no entry was found (idempotent).
func RemoveLink(path, systemPath string) (bool, error) {
	cfg, err := Load(path)
	if err != nil {
		return false, err
	}
	n := len(cfg.Symlinks)
	filtered := cfg.Symlinks[:0:0]
	for _, l := range cfg.Symlinks {
		if filepath.Clean(l.System) != filepath.Clean(systemPath) {
			filtered = append(filtered, l)
		}
	}
	if len(filtered) == n {
		return false, nil
	}
	cfg.Symlinks = filtered
	return true, Save(path, cfg)
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
	out.Symlinks = make([]Link, len(cfg.Symlinks))
	for i, l := range cfg.Symlinks {
		l.Original = abs(l.Original)
		out.Symlinks[i] = l
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
