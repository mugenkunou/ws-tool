package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const CurrentSchema = 1

// DefaultPath returns the XDG-compliant config file path.
// Resolution: $WS_CONFIG → $XDG_CONFIG_HOME/ws-tool/config.json → ~/.config/ws-tool/config.json
func DefaultPath() (string, error) {
	if p := os.Getenv("WS_CONFIG"); p != "" {
		return ExpandUserPath(p)
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "ws-tool", "config.json"), nil
}

// StateDir returns the XDG-compliant state directory for ws-tool.
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

// DataDir returns the XDG-compliant data directory for ws-tool.
// Resolution: $XDG_DATA_HOME/ws-tool → ~/.local/share/ws-tool
func DataDir() (string, error) {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "ws-tool"), nil
}

type Config struct {
	ConfigSchema int     `json:"config_schema"`
	Workspace    string  `json:"workspace,omitempty"`
	Ignore       Ignore  `json:"ignore"`
	Secret       Secret  `json:"secret"`
	Scratch      Scratch `json:"scratch"`
	Trash        Trash   `json:"trash"`
	Log          Log     `json:"log"`
	Search       Search  `json:"search"`
	Dotfile      Dotfile `json:"dotfile"`
	Repo         Repo    `json:"repo"`
	Capture      Capture `json:"capture"`
}

type Scratch struct {
	RootDir        string `json:"root_dir"`
	EditorCmd      string `json:"editor_cmd"`
	NameSuffix     string `json:"name_suffix"`
	PruneAfterDays int    `json:"prune_after_days"`
}

type Trash struct {
	RootDir    string     `json:"root_dir"`
	WarnSizeMB int        `json:"warn_size_mb"`
	Setup      TrashSetup `json:"setup"`
}

type TrashSetup struct {
	PromptOnInit     bool `json:"prompt_on_init"`
	ShellRM          bool `json:"shell_rm"`
	VSCodeDelete     bool `json:"vscode_delete"`
	FileExplorer     bool `json:"file_explorer_delete"`
	WarnUnconfigured bool `json:"warn_if_unconfigured"`
}

type Repo struct {
	Roots           []string `json:"roots"`
	ExcludeDirs     []string `json:"exclude_dirs"`
	MaxParallel     int      `json:"max_parallel"`
	ReconcileOnRead bool     `json:"reconcile_on_read"`
}

type Search struct {
	DefaultContext int `json:"default_context"`
	MaxResults     int `json:"max_results"`
}

type Dotfile struct {
	Git DotfileGit `json:"git"`
}

type DotfileGit struct {
	Enabled      bool   `json:"enabled"`
	AuthUsername string `json:"auth_username,omitempty"`
	PassEntry    string `json:"pass_entry,omitempty"`
}

type Ignore struct {
	WarnSizeMB int    `json:"warn_size_mb"`
	CritSizeMB int    `json:"crit_size_mb"`
	MaxDepth   int    `json:"max_depth"`
	Template   string `json:"template"`
}

type Secret struct {
	Enabled   bool     `json:"enabled"`
	PassNudge bool     `json:"pass_nudge"`
	SkipDirs  []string `json:"skip_dirs,omitempty"`
}

type Log struct {
	CapMB        int `json:"cap_mb"`
	MaxSessionMB int `json:"max_session_mb"`
}

type Capture struct {
	MaxAttachMB int               `json:"max_attach_mb"`
	Locations   map[string]string `json:"locations,omitempty"`
}

func Default() Config {
	return Config{
		ConfigSchema: CurrentSchema,
		Ignore: Ignore{
			WarnSizeMB: 1,
			CritSizeMB: 10,
			MaxDepth:   6,
			Template:   "builtin",
		},
		Secret: Secret{Enabled: true, PassNudge: true},
		Scratch: Scratch{
			RootDir:        "~/Scratch",
			EditorCmd:      "code",
			NameSuffix:     "auto",
			PruneAfterDays: 90,
		},
		Trash: Trash{
			RootDir:    "~/.Trash",
			WarnSizeMB: 1024,
			Setup: TrashSetup{
				PromptOnInit:     true,
				ShellRM:          true,
				VSCodeDelete:     true,
				FileExplorer:     true,
				WarnUnconfigured: true,
			},
		},
		Log: Log{CapMB: 500, MaxSessionMB: 100},
		Search: Search{
			DefaultContext: 2,
			MaxResults:     0,
		},
		Dotfile: Dotfile{Git: DotfileGit{
			Enabled: false,
		}},
		Repo: Repo{
			Roots:           []string{"."},
			ExcludeDirs:     []string{"ws", "node_modules", ".venv"},
			MaxParallel:     8,
			ReconcileOnRead: true,
		},
		Capture: Capture{
			MaxAttachMB: 5,
		},
	}
}

func Load(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	cfg := Default()
	if err := json.Unmarshal(content, &cfg); err != nil {
		return Config{}, err
	}

	if cfg.ConfigSchema > CurrentSchema {
		return Config{}, fmt.Errorf("unsupported config schema: %d (max supported: %d)", cfg.ConfigSchema, CurrentSchema)
	}

	if cfg.ConfigSchema <= 0 {
		return Config{}, errors.New("config_schema must be a positive integer")
	}

	return cfg, nil
}

func Save(path string, cfg Config) error {
	if cfg.ConfigSchema == 0 {
		cfg.ConfigSchema = CurrentSchema
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

func ResolvePath(baseWorkspace, value string) (string, error) {
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

	workspacePath, err := ExpandUserPath(baseWorkspace)
	if err != nil {
		return "", err
	}

	return filepath.Clean(filepath.Join(workspacePath, v)), nil
}

// WithAbsPaths returns a copy of cfg with every path-valued field resolved to a
// cleaned absolute path: "~" is expanded and relative values are resolved
// against workspacePath (the config path convention). It is used wherever
// config values are displayed (spec "Path Rules") or consumed as filesystem
// locations. cfg itself is not modified. Empty values stay empty.
func WithAbsPaths(cfg Config, workspacePath string) Config {
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
	if cfg.Workspace != "" {
		if p, err := ExpandUserPath(cfg.Workspace); err == nil {
			out.Workspace = p
		}
	}
	out.Scratch.RootDir = abs(cfg.Scratch.RootDir)
	out.Trash.RootDir = abs(cfg.Trash.RootDir)
	out.Repo.Roots = absAll(cfg.Repo.Roots)
	out.Repo.ExcludeDirs = absAll(cfg.Repo.ExcludeDirs)
	out.Secret.SkipDirs = absAll(cfg.Secret.SkipDirs)
	if cfg.Capture.Locations != nil {
		out.Capture.Locations = make(map[string]string, len(cfg.Capture.Locations))
		for name, dir := range cfg.Capture.Locations {
			out.Capture.Locations[name] = abs(dir)
		}
	}
	return out
}

// WorkspaceRel converts a config path value (workspace-relative, absolute, or
// ~-prefixed) to the cleaned, forward-slash, workspace-relative form used for
// prefix matching (repo.exclude_dirs, secret.skip_dirs). ok is false when the
// value is empty, invalid, or outside the workspace.
func WorkspaceRel(workspacePath, value string) (rel string, ok bool) {
	abs, err := ResolvePath(workspacePath, value)
	if err != nil {
		return "", false
	}
	r, err := filepath.Rel(filepath.Clean(workspacePath), abs)
	if err != nil {
		return "", false
	}
	r = filepath.ToSlash(r)
	if r == ".." || strings.HasPrefix(r, "../") {
		return "", false
	}
	return r, true
}
