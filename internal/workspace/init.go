package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// layoutDirs are the workspace directories created by `ws init` (spec §2.2).
var layoutDirs = []string{
	"notes",
	"experiments",
	"artifacts",
	"references",
	"scripts",
	"scripts/lib",
	"agent",
	"agent/skills",
	"configs",
	"secrets",
	"symlinks",
	"data",
	"data/sessions",
	"archive",
}

// InitPlan is what `ws init` finds and what it would change. Everything is an
// absolute path. Nothing is written until the Create, Write and Fill methods run.
type InitPlan struct {
	Workspace   string
	Config      string
	Megaignore  string
	MissingDirs []string
	Existing    []string // layout directories, ws.json and .megaignore that are already there
	// ConfigMissing is true when ws.json does not exist. Otherwise MissingKeys
	// lists the keys an older ws.json lacks.
	ConfigMissing     bool
	MissingKeys       []string
	MegaignoreMissing bool
}

// Empty reports whether the workspace is already fully initialized.
func (p InitPlan) Empty() bool {
	return len(p.MissingDirs) == 0 && !p.ConfigMissing && len(p.MissingKeys) == 0 && !p.MegaignoreMissing
}

// PlanInit reads the workspace and reports what is missing. It fails when an
// existing ws.json cannot be parsed, since init never replaces one.
func PlanInit(workspacePath, configPath string) (InitPlan, error) {
	if workspacePath == "" {
		return InitPlan{}, fmt.Errorf("workspace path is required")
	}
	if configPath == "" {
		configPath = wsconfig.ConfigPath(workspacePath)
	}
	p := InitPlan{
		Workspace:   workspacePath,
		Config:      configPath,
		Megaignore:  filepath.Join(workspacePath, ".megaignore"),
		MissingDirs: []string{},
		Existing:    []string{},
		MissingKeys: []string{},
	}
	for _, d := range layoutDirs {
		abs := filepath.Join(workspacePath, filepath.FromSlash(d))
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			p.Existing = append(p.Existing, abs)
		} else {
			p.MissingDirs = append(p.MissingDirs, abs)
		}
	}
	if ConfigExists(configPath) {
		content, err := os.ReadFile(configPath)
		if err != nil {
			return p, err
		}
		if _, err := wsconfig.Load(configPath); err != nil {
			return p, err
		}
		if p.MissingKeys, err = wsconfig.MissingKeys(content); err != nil {
			return p, err
		}
		p.Existing = append(p.Existing, configPath)
	} else {
		p.ConfigMissing = true
	}
	if _, err := os.Stat(p.Megaignore); err == nil {
		p.Existing = append(p.Existing, p.Megaignore)
	} else {
		p.MegaignoreMissing = true
	}
	return p, nil
}

// CreateDirs creates the missing layout directories.
func (p InitPlan) CreateDirs() error {
	for _, abs := range p.MissingDirs {
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", abs, err)
		}
	}
	return nil
}

// WriteConfig writes a complete ws.json of defaults. It never replaces a file.
func (p InitPlan) WriteConfig() error {
	if ConfigExists(p.Config) {
		return fmt.Errorf("%s already exists", p.Config)
	}
	return wsconfig.Save(p.Config, wsconfig.Default())
}

// FillConfig adds the missing keys to an existing ws.json, keeping every value
// already set.
func (p InitPlan) FillConfig() error {
	cfg, err := wsconfig.Load(p.Config)
	if err != nil {
		return err
	}
	return wsconfig.Save(p.Config, cfg)
}

// WriteMegaignore generates .megaignore from ws.json's ignore rules. It never
// replaces a file.
func (p InitPlan) WriteMegaignore() error {
	if _, err := os.Stat(p.Megaignore); err == nil {
		return fmt.Errorf("%s already exists", p.Megaignore)
	}
	rules, err := wsconfig.LoadIgnoreRules(p.Config)
	if err != nil {
		rules = ignore.DefaultUserRules()
	}
	return ignore.WriteMegaignore(p.Megaignore, rules)
}
