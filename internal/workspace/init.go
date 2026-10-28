package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// LayoutDir is one folder of the workspace layout: its path relative to the
// workspace, with forward slashes, and what belongs in it.
type LayoutDir struct {
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
}

// Layout is the workspace layout (spec §2.2). `ws init` creates these folders
// and `ws layout` prints them with their purpose; skills and scripts ask ws
// rather than copy the list.
var Layout = []LayoutDir{
	{"notes", "the user's own notes; agents read them and write elsewhere"},
	{"experiments", "code projects (git repos too) and hand-written or multi-step scripts"},
	{"artifacts", "raw blobs: PDFs, slides, images, datasets, tarballs"},
	{"references", "structured docs for agents: API specs, converted docs, WISDOM.md"},
	{"scripts", "small composable blocks, one action each"},
	{"scripts/lib", "code the scripts share; common.sh sets the WS_* paths"},
	{"agent", "what coding agents use"},
	{"agent/skills", "one folder per skill, with a SKILL.md; ws skill install links them"},
	{"configs", "settings the user edits, one <system>-<instance>/ each"},
	{"symlinks", "originals of files that belong at system paths (ws symlink add)"},
	{"data", "program output only, where losing it loses work"},
	{"archive", "put away for good, at the same relative path"},
}

// TopDirs are the workspace's top-level layout folders: notes, scripts, agent, …
func TopDirs() []string {
	var out []string
	for _, d := range layoutDirs {
		if !strings.Contains(d, "/") {
			out = append(out, d)
		}
	}
	return out
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
func PlanInit(workspacePath string) (InitPlan, error) {
	if workspacePath == "" {
		return InitPlan{}, fmt.Errorf("workspace path is required")
	}
	configPath := wsconfig.ConfigPath(workspacePath)
	p := InitPlan{
		Workspace:   workspacePath,
		Config:      configPath,
		Megaignore:  filepath.Join(workspacePath, ".megaignore"),
		MissingDirs: []string{},
		Existing:    []string{},
		MissingKeys: []string{},
	}
	for _, d := range Layout {
		abs := filepath.Join(workspacePath, filepath.FromSlash(d.Path))
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
