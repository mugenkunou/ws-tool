// Package skill links skills into the skill folders of the coding tools
// installed on this machine. A skill is a folder (or a symlink to one) that
// holds a SKILL.md. Skills sit on two shelves: the built-ins ship inside the
// binary and are extracted to a managed folder (builtin.go); yours are kept in
// <workspace>/agent/skills/. Installing one makes a symlink to it inside one
// tool's skills folder; ws never overwrites.
package skill

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Tool is one row of the built-in matrix. It lives only in the binary.
type Tool struct {
	Name string
	// Config is the tool's own folder, ~ for $HOME. A tool counts as detected when
	// it exists: its skills folder is often missing on a fresh install.
	Config string
	// Dir is where the tool reads personal skills from.
	Dir string
	// Unverified marks a path the vendor's own docs do not confirm.
	Unverified bool
}

// Tools is every tool ws knows, sorted by name. Paths come from each vendor's own
// documentation. ws does not use the shared ~/.agents/skills folder: Claude Code
// ignores it, so every tool gets its own link.
var Tools = []Tool{
	{Name: "amp", Config: "~/.config/amp", Dir: "~/.config/agents/skills"},
	{Name: "claude", Config: "~/.claude", Dir: "~/.claude/skills"},
	{Name: "cline", Config: "~/.cline", Dir: "~/.cline/skills"},
	{Name: "codex", Config: "~/.codex", Dir: "~/.codex/skills", Unverified: true}, // the Codex docs list only ~/.agents/skills
	{Name: "copilot", Config: "~/.copilot", Dir: "~/.copilot/skills"},
	{Name: "cursor", Config: "~/.cursor", Dir: "~/.cursor/skills"},
	{Name: "gemini", Config: "~/.gemini", Dir: "~/.gemini/skills"},
	{Name: "junie", Config: "~/.junie", Dir: "~/.junie/skills", Unverified: true}, // the user-level path is not in the docs
	{Name: "kimi", Config: "~/.kimi-code", Dir: "~/.kimi-code/skills"},
	{Name: "kiro", Config: "~/.kiro", Dir: "~/.kiro/skills"},
	{Name: "opencode", Config: "~/.config/opencode", Dir: "~/.config/opencode/skills"},
	{Name: "roo", Config: "~/.roo", Dir: "~/.roo/skills"},
	{Name: "windsurf", Config: "~/.codeium/windsurf", Dir: "~/.codeium/windsurf/skills"},
}

// FindTool returns the tool with this name.
func FindTool(name string) (Tool, bool) {
	for _, t := range Tools {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// ToolNames lists every tool name.
func ToolNames() []string {
	names := make([]string, len(Tools))
	for i, t := range Tools {
		names[i] = t.Name
	}
	return names
}

func expand(home, p string) string {
	if p == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return p
}

// ConfigPath is the tool's config folder on this machine.
func (t Tool) ConfigPath(home string) string { return expand(home, t.Config) }

// DirPath is the tool's skills folder on this machine.
func (t Tool) DirPath(home string) string { return expand(home, t.Dir) }

// Detected reports whether the tool's config folder exists.
func (t Tool) Detected(home string) bool {
	_, err := os.Stat(t.ConfigPath(home))
	return err == nil
}

// Skill is one skill on either shelf.
type Skill struct {
	Name string
	// Path is what a tool link points at: <workspace>/agent/skills/<Name> (the
	// entry itself, not where it points), or a built-in's managed copy.
	Path string
	// Broken is a symlink whose target is gone. It is listed but cannot be installed.
	Broken bool
	Shelf  Shelf
	// Dev is the checkout folder a built-in links to in dev mode.
	Dev string
	// Ejected is the ws version a skill of yours was ejected from.
	Ejected string
	// Shadowed is a skill of yours with a built-in's name and no .ws-ejected
	// file. It is listed with a warning; the built-in wins.
	Shadowed bool
}

// Root is the folder that holds the workspace's skills.
func Root(workspace string) string { return filepath.Join(workspace, "agent", "skills") }

// List returns the skills in the workspace, sorted by name. An entry is a skill
// when it is a folder, or a symlink to one, that holds a SKILL.md; a dangling
// symlink is listed as Broken. Anything else (README.md, a folder without a
// SKILL.md) is not a skill and is left out.
func List(workspace string) ([]Skill, error) {
	root := Root(workspace)
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Skill
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(root, e.Name())
		info, err := os.Stat(p) // follows a symlink
		if err != nil {
			if e.Type()&os.ModeSymlink != 0 {
				out = append(out, Skill{Name: e.Name(), Path: p, Broken: true, Shelf: Yours})
			}
			continue
		}
		if !info.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(p, "SKILL.md")); err != nil {
			continue
		}
		s := Skill{Name: e.Name(), Path: p, Shelf: Yours}
		if v, err := os.ReadFile(filepath.Join(p, EjectedFile)); err == nil {
			s.Ejected = strings.TrimSpace(string(v))
			if s.Ejected == "" {
				s.Ejected = "unknown"
			}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Installed reports whether anything exists at the skill's place in the tool's
// skills folder: a symlink (even a broken one), a real folder, anything.
func Installed(home string, t Tool, name string) bool {
	_, err := os.Lstat(filepath.Join(t.DirPath(home), name))
	return err == nil
}

// Linked reports whether the tool's link for the skill points at s.Path.
func Linked(home string, t Tool, s Skill) bool {
	return s.Path != "" && linkTarget(filepath.Join(t.DirPath(home), s.Name)) == filepath.Clean(s.Path)
}

// Cell is one tool's answer for one skill.
type Cell struct {
	Tool      string `json:"tool"`
	Installed bool   `json:"installed"`
}

// Row is one skill across every tool.
type Row struct {
	Skill  string `json:"skill"`
	Shelf  Shelf  `json:"shelf"`
	Broken bool   `json:"broken,omitempty"`
	// Version is a built-in's: the ws version, or "dev".
	Version  string `json:"version,omitempty"`
	Dev      string `json:"dev,omitempty"`
	Ejected  string `json:"ejected_from,omitempty"`
	Shadowed bool   `json:"shadowed,omitempty"`
	Tools    []Cell `json:"tools"`
}

// ToolState is one tool's column header.
type ToolState struct {
	Name       string `json:"name"`
	Detected   bool   `json:"detected"`
	Unverified bool   `json:"unverified,omitempty"`
}

// Result is what Scan found. Rows hold the built-ins first, then yours.
type Result struct {
	// Version is the ws version the built-ins come from.
	Version string `json:"version"`
	// Managed is where the built-ins are extracted; Root is agent/skills/.
	Managed string      `json:"managed"`
	Root    string      `json:"root"`
	Tools   []ToolState `json:"tools"`
	Rows    []Row       `json:"skills"`
}

// Scan reads the filesystem and changes nothing. tools is the columns to show
// (nil: every known tool).
func Scan(home, workspace string, tools []Tool) (Result, error) {
	if tools == nil {
		tools = Tools
	}
	skills, err := All(home, workspace)
	if err != nil {
		return Result{}, err
	}
	res := Result{Version: Version, Managed: ManagedRoot(home), Root: Root(workspace), Tools: []ToolState{}, Rows: []Row{}}
	for _, t := range tools {
		res.Tools = append(res.Tools, ToolState{Name: t.Name, Detected: t.Detected(home), Unverified: t.Unverified})
	}
	for _, s := range skills {
		row := Row{Skill: s.Name, Shelf: s.Shelf, Broken: s.Broken, Version: s.VersionOf(), Dev: s.Dev, Ejected: s.Ejected, Shadowed: s.Shadowed}
		for _, t := range tools {
			row.Tools = append(row.Tools, Cell{Tool: t.Name, Installed: Installed(home, t, s.Name)})
		}
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}

// Install links the skill into the tool's skills folder. It creates the folder
// when needed and never replaces anything that is already there.
func Install(home, workspace string, t Tool, s Skill) error {
	if s.Broken {
		return fmt.Errorf("%s: its target is gone", s.Name)
	}
	if s.Shadowed {
		return fmt.Errorf("%s: same name as a built-in; the built-in wins", s.Name)
	}
	target := s.Path
	if target == "" {
		target = filepath.Join(Root(workspace), s.Name)
	}
	dir := t.DirPath(home)
	link := filepath.Join(dir, s.Name)
	if _, err := os.Lstat(link); err == nil {
		return fmt.Errorf("%s already exists", link)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.Symlink(target, link)
}

// Ours reports whether the skill's place in the tool's folder is a symlink that
// ws made: one that points into agent/skills/, into the managed folder of the
// built-ins, or at the checkout a built-in links to in dev mode. The link's own
// target is read, one hop: the chain behind a skill that is itself a symlink
// does not matter.
func Ours(home, workspace string, t Tool, name string) bool {
	target := linkTarget(filepath.Join(t.DirPath(home), name))
	if target == "" {
		return false
	}
	if within(Root(workspace), target) || within(ManagedRoot(home), target) {
		return true
	}
	dev, ok := DevTarget(home, name)
	return ok && filepath.Clean(dev) == target
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

// Uninstall removes the link. It removes only a link ws made (see Ours); a
// real folder or a link elsewhere is left alone. The skill itself stays.
func Uninstall(home, workspace string, t Tool, name string) error {
	link := filepath.Join(t.DirPath(home), name)
	if !Ours(home, workspace, t, name) {
		return fmt.Errorf("%s is not a link ws made", link)
	}
	return os.Remove(link)
}

// InstalledOurs lists the skills of the workspace that are linked into the tool.
func InstalledOurs(home, workspace string, t Tool) []string {
	var out []string
	entries, err := os.ReadDir(t.DirPath(home))
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.Type()&os.ModeSymlink != 0 && Ours(home, workspace, t, e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}
