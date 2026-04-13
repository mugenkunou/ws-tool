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
	Notify       Notify  `json:"notify"`
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
	RemoteURL    string `json:"remote_url"`
	AuthUsername string `json:"auth_username"`
	PassEntry    string `json:"pass_entry"`
	Branch       string `json:"branch"`
	AutoCommit   bool   `json:"auto_commit"`
	AutoPush     bool   `json:"auto_push"`
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
	CapMB int `json:"cap_mb"`
}

type Notify struct {
	Enabled         bool     `json:"enabled"`
	PollIntervalMin int      `json:"poll_interval_min"`
	Events          []string `json:"events"`
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
		Log: Log{CapMB: 500},
		Search: Search{
			DefaultContext: 2,
			MaxResults:     0,
		},
		Dotfile: Dotfile{Git: DotfileGit{
			Enabled:    false,
			RemoteURL:  "",
			Branch:     "main",
			AutoCommit: true,
			AutoPush:   true,
		}},
		Repo: Repo{
			Roots:           []string{"."},
			ExcludeDirs:     []string{"ws", "node_modules", ".venv"},
			MaxParallel:     8,
			ReconcileOnRead: true,
		},
		Notify: Notify{
			Enabled:         true,
			PollIntervalMin: 10,
			Events:          []string{"dotfile", "secret", "bloat", "storage"},
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
