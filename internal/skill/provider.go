package skill

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Providers is the explicit list of tool binaries that ship a skill. ws never
// scans PATH for them. `<bin> skill` prints the skill's SKILL.md; its
// frontmatter `name` is the skill name. Tests replace it.
var Providers = []string{"agent-tmux"}

// ProviderFile marks a skill folder in agent/skills/ that ws copied from a
// provider. It holds the binary's name. ws writes only folders that have it.
const ProviderFile = ".ws-provider"

// ProviderTimeout bounds one `<bin> skill` run.
var ProviderTimeout = 5 * time.Second

// Provider states.
const (
	ProviderNew      = "new"      // the binary ships a skill with no copy yet
	ProviderStale    = "stale"    // the copy differs from the binary's output
	ProviderCurrent  = "current"  // the copy matches
	ProviderGone     = "gone"     // a copy exists but the binary is not on PATH
	ProviderConflict = "conflict" // agent/skills/<name> exists and is not this provider's copy
	ProviderFailed   = "failed"   // `<bin> skill` failed or printed no usable skill
)

// Offer states: a managed skill with no copy in agent/skills/ yet.
const (
	OfferReady    = "ready"     // the binary is on PATH: install copies the skill, then links it
	OfferNoBinary = "no-binary" // the binary is not on PATH; installing it makes the skill ready
	OfferFailed   = "failed"    // `<bin> skill` failed
)

// ProviderState is one provider's skill held against its copy in the workspace.
type ProviderState struct {
	Bin   string
	Name  string // skill name; empty when the binary gave none
	State string
	Err   error  // for ProviderFailed and ProviderConflict
	Data  []byte // the binary's output, for new and stale
}

// Dir is the copy's folder in the workspace.
func (p ProviderState) Dir(workspace string) string { return filepath.Join(Root(workspace), p.Name) }

// providerCopies maps each binary to the names of the skill folders marked as
// its copies, read from agent/skills/.
func providerCopies(workspace string) map[string][]string {
	out := map[string][]string{}
	entries, _ := os.ReadDir(Root(workspace))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(Root(workspace), e.Name(), ProviderFile))
		if err != nil {
			continue
		}
		if bin := strings.TrimSpace(string(b)); bin != "" {
			out[bin] = append(out[bin], e.Name())
		}
	}
	return out
}

// runProvider runs `<path> skill` and returns the skill name and the output.
func runProvider(path string) (string, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ProviderTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "skill")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", nil, fmt.Errorf("`skill` timed out after %s", ProviderTimeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", nil, fmt.Errorf("`skill` failed: %v: %s", err, firstLine(msg))
		}
		return "", nil, fmt.Errorf("`skill` failed: %v", err)
	}
	name := frontmatterName(stdout.Bytes())
	switch {
	case name == "":
		return "", nil, fmt.Errorf("`skill` printed no SKILL.md with a name in its frontmatter")
	case name != filepath.Base(name) || strings.HasPrefix(name, ".") || strings.ContainsAny(name, `/\`):
		return "", nil, fmt.Errorf("`skill` printed an unusable skill name %q", name)
	}
	return name, stdout.Bytes(), nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// frontmatterName reads `name:` from the leading --- block of a SKILL.md.
func frontmatterName(data []byte) string {
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "---" {
			return ""
		}
		if v, ok := strings.CutPrefix(l, "name:"); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

// InspectProviders runs each provider found on PATH and compares its skill
// with the copy in the workspace. It changes nothing. A provider that fails is
// reported and the others still run. Binaries that have a marked copy but are
// not in bins are included, so a copy whose binary left the list still shows up.
func InspectProviders(workspace string, bins []string) []ProviderState {
	copies := providerCopies(workspace)
	seen := map[string]bool{}
	all := append([]string(nil), bins...)
	for _, b := range bins {
		seen[b] = true
	}
	var extra []string
	for b := range copies {
		if !seen[b] {
			extra = append(extra, b)
		}
	}
	sort.Strings(extra)
	all = append(all, extra...)

	var out []ProviderState
	for _, bin := range all {
		path, err := exec.LookPath(bin)
		if err != nil {
			for _, n := range copies[bin] {
				out = append(out, ProviderState{Bin: bin, Name: n, State: ProviderGone})
			}
			continue
		}
		if !seen[bin] {
			continue // not in the list: only its copies matter, and the binary is there
		}
		name, data, err := runProvider(path)
		if err != nil {
			out = append(out, ProviderState{Bin: bin, State: ProviderFailed, Err: err})
			continue
		}
		st := ProviderState{Bin: bin, Name: name, Data: data}
		for _, n := range copies[bin] {
			if n != name {
				out = append(out, ProviderState{Bin: bin, Name: n, State: ProviderGone})
			}
		}
		dir := Root(workspace) + string(filepath.Separator) + name
		marker, merr := os.ReadFile(filepath.Join(dir, ProviderFile))
		switch _, lerr := os.Lstat(dir); {
		case lerr != nil:
			st.State = ProviderNew
		case merr != nil || strings.TrimSpace(string(marker)) != bin:
			st.State = ProviderConflict
			st.Err = fmt.Errorf("%s exists and was not copied from %s", dir, bin)
		default:
			have, _ := os.ReadFile(filepath.Join(dir, "SKILL.md"))
			if bytes.Equal(have, data) {
				st.State = ProviderCurrent
			} else {
				st.State = ProviderStale
			}
		}
		out = append(out, st)
	}
	return out
}

// Offered lists the managed skills that have no copy in agent/skills/ yet,
// sorted by name. A binary that is not on PATH, or whose `skill` fails, is
// listed under its own name: the skill's name is known only from its output.
// A provider whose skill name is taken by a skill of yours is left out; `skill
// check` reports it.
func Offered(workspace string, bins []string) []Skill {
	copies := providerCopies(workspace)
	var out []Skill
	for _, st := range InspectProviders(workspace, bins) {
		switch {
		case st.State == ProviderNew:
			out = append(out, Skill{Name: st.Name, Path: st.Dir(workspace), Shelf: Managed, Provider: st.Bin, Offer: OfferReady})
		case st.State == ProviderFailed && len(copies[st.Bin]) == 0:
			out = append(out, Skill{Name: st.Bin, Shelf: Managed, Provider: st.Bin, Offer: OfferFailed})
		}
	}
	for _, bin := range bins {
		if _, err := exec.LookPath(bin); err != nil && len(copies[bin]) == 0 {
			out = append(out, Skill{Name: bin, Shelf: Managed, Provider: bin, Offer: OfferNoBinary})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// copyOffer writes the copy of an offered skill, from the binary's output now.
func copyOffer(workspace string, s Skill) error {
	for _, st := range InspectProviders(workspace, []string{s.Provider}) {
		if st.Name != s.Name {
			continue
		}
		switch st.State {
		case ProviderCurrent:
			return nil
		case ProviderNew, ProviderStale:
			return WriteProvider(workspace, st)
		case ProviderConflict:
			return st.Err
		}
	}
	return fmt.Errorf("%s no longer ships a skill named %s", s.Provider, s.Name)
}

// WriteProvider writes a new or stale provider copy: SKILL.md (atomically) and
// the marker.
func WriteProvider(workspace string, p ProviderState) error {
	if p.State != ProviderNew && p.State != ProviderStale {
		return fmt.Errorf("%s: nothing to write (%s)", p.Name, p.State)
	}
	dir := p.Dir(workspace)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, ProviderFile), []byte(p.Bin+"\n"), 0o644); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".SKILL.md-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(p.Data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, "SKILL.md"))
}

// providerFindings turns provider states into `skill check` findings.
func providerFindings(workspace string, states []ProviderState) []Finding {
	var out []Finding
	for _, p := range states {
		file := p.Dir(workspace)
		if p.Name != "" {
			file = filepath.Join(file, "SKILL.md")
		}
		add := func(format string, a ...any) {
			out = append(out, Finding{Level: LevelWarning, Shelf: Managed, Skill: p.Name, File: file, Message: fmt.Sprintf(format, a...)})
		}
		switch p.State {
		case ProviderStale:
			add("differs from `%s skill`; run `ws skill install --providers` to update it", p.Bin)
		case ProviderGone:
			add("copied from %s, which is not on PATH; install %s, or remove %s", p.Bin, p.Bin, filepath.Dir(file))
		case ProviderConflict:
			add("%s ships a skill named %s, but %v; move it away, then run `ws skill install --providers`", p.Bin, p.Name, p.Err)
		case ProviderFailed:
			out = append(out, Finding{Level: LevelWarning, File: p.Bin, Message: fmt.Sprintf("%s: %v; fix the binary or drop it from ws's provider list", p.Bin, p.Err)})
		}
	}
	return out
}
