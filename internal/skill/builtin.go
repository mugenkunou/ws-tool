package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mugenkunou/ws-tool/skills"
)

// Shelf is where a skill comes from.
type Shelf string

const (
	// Builtin skills ship inside the ws binary. Only ws writes them.
	Builtin Shelf = "built-in"
	// Yours are the skills in <workspace>/agent/skills/.
	Yours Shelf = "yours"
	// Managed skills ship with a tool binary (Providers). ws copies them into
	// agent/skills/ and keeps the copy in step with the binary. Before the
	// first copy they are listed as offers (see Offered).
	Managed Shelf = "managed"
)

var (
	// BuiltinFS holds the built-in skills, one folder per skill. Tests replace it.
	BuiltinFS fs.FS = skills.FS
	// Version is the ws version the managed copy is stamped with. cmd sets it.
	Version = "dev"
)

// EjectedFile marks a workspace skill copied from a built-in. It holds the ws
// version the copy was made from.
const EjectedFile = ".ws-ejected"

// ManagedRoot is where the built-ins are extracted: $XDG_DATA_HOME/ws/skills,
// default ~/.local/share/ws/skills. Tool links point here, so a new ws
// updates every tool without a reinstall.
func ManagedRoot(home string) string {
	data := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(data) {
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "ws", "skills")
}

// BuiltinNames lists the built-in skills, sorted: the top-level folders of
// BuiltinFS that hold a SKILL.md. Anything else there is skipped.
func BuiltinNames() []string {
	entries, err := fs.ReadDir(BuiltinFS, ".")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := fs.Stat(BuiltinFS, path.Join(e.Name(), "SKILL.md")); err != nil {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

// IsBuiltin reports whether a built-in has this name.
func IsBuiltin(name string) bool {
	for _, n := range BuiltinNames() {
		if n == name {
			return true
		}
	}
	return false
}

// DevTarget is the checkout folder a built-in links to in dev mode.
func DevTarget(home, name string) (string, bool) {
	target, err := os.Readlink(filepath.Join(ManagedRoot(home), name))
	if err != nil {
		return "", false
	}
	return target, true
}

func stampPath(home, name string) string {
	return filepath.Join(ManagedRoot(home), "."+name+".stamp")
}

// stamp is what an extracted copy is made from: the ws version and a digest
// of the embedded files.
func stamp(name string) (string, error) {
	h := sha256.New()
	err := fs.WalkDir(BuiltinFS, name, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		f, err := BuiltinFS.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		fmt.Fprintf(h, "%s\x00", p)
		_, err = io.Copy(h, f)
		return err
	})
	if err != nil {
		return "", err
	}
	return Version + " " + hex.EncodeToString(h.Sum(nil)) + "\n", nil
}

// Refresh extracts every built-in whose managed copy is missing or was made by
// another version or from other content. A built-in in dev mode is left alone.
// It is cheap when nothing changed: one digest and one small read per skill.
func Refresh(home string) error {
	var errs []error
	for _, name := range BuiltinNames() {
		if _, dev := DevTarget(home, name); dev {
			continue
		}
		want, err := stamp(name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		got, _ := os.ReadFile(stampPath(home, name))
		if _, err := os.Stat(filepath.Join(ManagedRoot(home), name, "SKILL.md")); err == nil && string(got) == want {
			continue
		}
		if err := extract(home, name, want); err != nil {
			errs = append(errs, fmt.Errorf("extract built-in skill %s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

// extract writes the built-in into a temp folder beside the managed copy, then
// swaps it in: files 0444, folders 0755.
func extract(home, name, stampText string) error {
	managed := ManagedRoot(home)
	if err := os.MkdirAll(managed, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(managed, "."+name+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	err = fs.WalkDir(BuiltinFS, name, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(name, filepath.FromSlash(p))
		dst := filepath.Join(tmp, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := fs.ReadFile(BuiltinFS, p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o444)
	})
	if err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(managed, name)
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	return os.WriteFile(stampPath(home, name), []byte(stampText), 0o644)
}

// Dev makes the managed copy of a built-in a symlink to <source>/skills/<name>,
// so edits in the checkout are live in every tool linked to it.
func Dev(home, source, name string) error {
	if !IsBuiltin(name) {
		return fmt.Errorf("%s is not a built-in skill", name)
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	target := filepath.Join(source, "skills", name)
	if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
		return fmt.Errorf("%s has no SKILL.md", target)
	}
	managed := ManagedRoot(home)
	if err := os.MkdirAll(managed, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(managed, name)
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	_ = os.Remove(stampPath(home, name))
	return os.Symlink(target, dest)
}

// Undev switches a built-in in dev mode back to the copy in the binary.
func Undev(home, name string) error {
	if _, dev := DevTarget(home, name); !dev {
		return nil
	}
	if err := os.Remove(filepath.Join(ManagedRoot(home), name)); err != nil {
		return err
	}
	return Refresh(home)
}

// BuiltinLinks lists the tools whose link for name points at the managed copy.
func BuiltinLinks(home, name string) []Tool {
	want := filepath.Join(ManagedRoot(home), name)
	var out []Tool
	for _, t := range Tools {
		if linkTarget(filepath.Join(t.DirPath(home), name)) == want {
			out = append(out, t)
		}
	}
	return out
}

// Eject copies a built-in into agent/skills/<name>, writable, marks it with
// .ws-ejected and repoints the tools linked to the managed copy at the
// workspace copy. From then on the skill is yours and ws stops updating it.
func Eject(home, workspace, name string) error {
	if !IsBuiltin(name) {
		return fmt.Errorf("%s is not a built-in skill", name)
	}
	dest := filepath.Join(Root(workspace), name)
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	src := filepath.Join(ManagedRoot(home), name)
	// os.DirFS follows src when it is a dev-mode symlink.
	srcFS := os.DirFS(src)
	err := fs.WalkDir(srcFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(dest, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := fs.ReadFile(srcFS, p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dest, EjectedFile), []byte(Version+"\n"), 0o644); err != nil {
		return err
	}
	for _, t := range BuiltinLinks(home, name) {
		link := filepath.Join(t.DirPath(home), name)
		if err := os.Remove(link); err != nil {
			return err
		}
		if err := os.Symlink(dest, link); err != nil {
			return err
		}
	}
	return nil
}

// linkTarget is where a symlink points, made absolute; empty when p is not one.
func linkTarget(p string) string {
	target, err := os.Readlink(p)
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(p), target)
	}
	return filepath.Clean(target)
}

// Builtins lists the built-in skills. Path is the managed copy, which may not
// exist before the first Refresh.
func Builtins(home string) []Skill {
	var out []Skill
	for _, name := range BuiltinNames() {
		s := Skill{Name: name, Path: filepath.Join(ManagedRoot(home), name), Shelf: Builtin}
		if target, dev := DevTarget(home, name); dev {
			s.Dev = target
		}
		if _, err := os.Stat(filepath.Join(s.Path, "SKILL.md")); err != nil {
			s.Broken = true
		}
		out = append(out, s)
	}
	return out
}

// All lists both shelves: the built-ins, then yours, each sorted by name. An
// ejected copy replaces its built-in. A skill of yours with a built-in's name
// and no .ws-ejected file is kept but Shadowed: the built-in wins.
func All(home, workspace string) ([]Skill, error) {
	yours, err := List(workspace)
	if err != nil {
		return nil, err
	}
	ejected := map[string]bool{}
	for _, s := range yours {
		if s.Ejected != "" {
			ejected[s.Name] = true
		}
	}
	var out []Skill
	builtin := map[string]bool{}
	for _, s := range Builtins(home) {
		if ejected[s.Name] {
			continue
		}
		builtin[s.Name] = true
		out = append(out, s)
	}
	for _, s := range yours {
		s.Shadowed = builtin[s.Name]
		out = append(out, s)
	}
	return out, nil
}

// Available is All plus the managed skills not copied yet (Offered), so a
// listing shows them before the first install. An offer whose name a skill
// already has is left out.
func Available(home, workspace string) ([]Skill, error) {
	all, err := All(home, workspace)
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	for _, s := range all {
		taken[s.Name] = true
	}
	for _, o := range Offered(workspace, Providers) {
		if !taken[o.Name] {
			all = append(all, o)
		}
	}
	return all, nil
}

// Find returns the skill a name means: the one that wins a name clash.
func Find(skills []Skill, name string) (Skill, bool) {
	for _, s := range skills {
		if s.Name == name && !s.Shadowed {
			return s, true
		}
	}
	return Skill{}, false
}

// VersionOf is what a skill's version reads as: the ws version for a built-in,
// "dev" in dev mode, empty for yours.
func (s Skill) VersionOf() string {
	switch {
	case s.Shelf != Builtin:
		return ""
	case s.Dev != "":
		return "dev"
	}
	return Version
}
