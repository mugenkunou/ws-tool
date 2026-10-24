package trash

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// The three integrations that route deletes into the trash (spec §5 trash
// enable). Each is checked against the live system, never a state file.
const (
	ShellRM      = "shell-rm"
	VSCode       = "vscode"
	FileExplorer = "file-explorer"
)

// aliasLine is the line `enable` adds to the shell rc files. It is guarded, so
// `rm` keeps working on a machine whose rc file arrived through sync but which has
// no ws-trash-rm.
const aliasLine = "command -v ws-trash-rm >/dev/null 2>&1 && alias rm='ws-trash-rm'"

// legacyAliasLine is what older ws wrote, unguarded.
const legacyAliasLine = "alias rm='ws-trash-rm'"

// Integration is one soft-delete integration and whether it is on.
type Integration struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Detail  string `json:"detail"`
}

// Status is the machine's soft-delete state: the integrations, the trash location
// and its totals.
type Status struct {
	Root          string        `json:"root"`
	Integrations  []Integration `json:"integrations"`
	Items         int           `json:"items"`
	Files         int           `json:"files"`
	SizeBytes     int64         `json:"size_bytes"`
	WarnSizeBytes int64         `json:"warn_size_bytes"`
	OverLimit     bool          `json:"over_limit"`
}

// Enabled reports whether the named integration is on.
func (s Status) Enabled(name string) bool {
	for _, i := range s.Integrations {
		if i.Name == name {
			return i.Enabled
		}
	}
	return false
}

// WarningCount is the number of integrations that are off.
func (s Status) WarningCount() int {
	n := 0
	for _, i := range s.Integrations {
		if !i.Enabled {
			n++
		}
	}
	return n
}

// GetStatus reports each integration from the live system, with the trash
// directory and its totals. It writes nothing.
func GetStatus() (Status, error) {
	root, err := Root()
	if err != nil {
		return Status{}, err
	}
	sum, err := Summarize()
	if err != nil {
		return Status{}, err
	}
	st := Status{
		Root: root, Items: sum.Items, Files: sum.Files, SizeBytes: sum.SizeBytes,
		WarnSizeBytes: WarnSizeBytes, OverLimit: sum.SizeBytes > WarnSizeBytes,
	}
	st.Integrations = []Integration{shellRMStatus(), vscodeStatus(), fileExplorerStatus(root)}
	return st, nil
}

// ── shell-rm ───────────────────────────────────────────────────────

func scriptPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin", "ws-trash-rm"), nil
}

// rcFiles are the shell rc files ws edits, whether they exist or not.
func rcFiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".bashrc"), filepath.Join(home, ".zshrc")}
}

// rcWithAlias lists the rc files that have ws's alias line, guarded or not.
func rcWithAlias() []string {
	var out []string
	for _, rc := range rcFiles() {
		if b, err := os.ReadFile(rc); err == nil && hasAlias(string(b)) {
			out = append(out, rc)
		}
	}
	return out
}

func hasAlias(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if t := strings.TrimSpace(line); t == aliasLine || t == legacyAliasLine {
			return true
		}
	}
	return false
}

func shellRMStatus() Integration {
	in := Integration{Name: ShellRM}
	script, err := scriptPath()
	if err != nil {
		in.Detail = err.Error()
		return in
	}
	if st, err := os.Stat(script); err != nil || st.Mode()&0o111 == 0 {
		in.Detail = script + " is missing; run ws trash enable"
		return in
	}
	rcs := rcWithAlias()
	if len(rcs) == 0 {
		in.Detail = "no shell rc file aliases rm to ws-trash-rm; run ws trash enable"
		return in
	}
	in.Enabled = true
	in.Detail = "rm moves files to the trash (" + strings.Join(rcs, ", ") + ")"
	return in
}

// shellScript is the ws-trash-rm helper: it moves each argument into the trash
// with a .trashinfo. It ignores flags and silently skips paths that do not exist.
func shellScript(root string) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

trash_root=%q
files_dir="$trash_root/files"
info_dir="$trash_root/info"
mkdir -p "$files_dir" "$info_dir"

if [[ $# -eq 0 ]]; then
  echo "usage: rm <path...>" >&2
  exit 1
fi

for arg in "$@"; do
  if [[ "$arg" == -- ]]; then
    continue
  fi
  if [[ "$arg" == -* ]]; then
    continue
  fi
  if [[ ! -e "$arg" && ! -L "$arg" ]]; then
    continue
  fi

  base="$(basename -- "$arg")"
  name="$base"
  n=2
  while [[ -e "$files_dir/$name" || -e "$info_dir/$name.trashinfo" ]]; do
    name="$base.$n"
    n=$((n + 1))
  done

  orig="$(realpath -m -- "$arg" 2>/dev/null || echo "$arg")"
  encoded="$(python3 -c 'import sys,urllib.parse; print(urllib.parse.quote(sys.argv[1], safe="/"))' "$orig")"
  mv -- "$arg" "$files_dir/$name"
  {
    echo "[Trash Info]"
    echo "Path=$encoded"
    echo "DeletionDate=$(date +%%Y-%%m-%%dT%%H:%%M:%%S)"
  } > "$info_dir/$name.trashinfo"
done
`, root)
}

// enableShellRM writes the script and adds the guarded alias to each existing
// file among ~/.bashrc and ~/.zshrc, or creates the rc file for $SHELL when
// neither exists. A symlinked rc file is edited through the link.
func enableShellRM(root string) error {
	script, err := scriptPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(script, []byte(shellScript(root)), 0o755); err != nil {
		return err
	}

	var targets []string
	for _, rc := range rcFiles() {
		if _, err := os.Stat(rc); err == nil {
			targets = append(targets, rc)
		}
	}
	if len(targets) == 0 {
		home, _ := os.UserHomeDir()
		rc := filepath.Join(home, ".bashrc")
		if filepath.Base(os.Getenv("SHELL")) == "zsh" {
			rc = filepath.Join(home, ".zshrc")
		}
		targets = []string{rc}
	}
	for _, rc := range targets {
		if err := addAlias(rc); err != nil {
			return err
		}
	}
	return nil
}

// addAlias appends the guarded alias to rc unless it, or the legacy line, is
// already there. A legacy unguarded line is replaced by the guarded one.
func addAlias(rc string) error {
	b, err := os.ReadFile(rc)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(b)
	if strings.Contains(content, aliasLine) {
		return nil
	}
	if strings.Contains(content, legacyAliasLine) {
		return os.WriteFile(rc, []byte(replaceLine(content, legacyAliasLine, aliasLine)), 0o644)
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(rc, []byte(content+aliasLine+"\n"), 0o644)
}

// replaceLine replaces the lines of content that are exactly old (after trimming)
// with new.
func replaceLine(content, old, new string) string {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == old {
			lines[i] = new
		}
	}
	return strings.Join(lines, "\n")
}

// removeAlias removes ws's alias line, in both its forms, from rc.
func removeAlias(rc string) error {
	b, err := os.ReadFile(rc)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var kept []string
	changed := false
	for _, l := range strings.Split(string(b), "\n") {
		if t := strings.TrimSpace(l); t == aliasLine || t == legacyAliasLine {
			changed = true
			continue
		}
		kept = append(kept, l)
	}
	if !changed {
		return nil
	}
	return os.WriteFile(rc, []byte(strings.Join(kept, "\n")), 0o644)
}

// ── vscode ─────────────────────────────────────────────────────────

func vscodeSettings() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return filepath.Join(dir, "Code", "User", "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "Code", "User", "settings.json"), nil
}

// trashOffPattern matches an explicit files.enableTrash: false. settings.json is
// JSONC (it may hold comments), so it is edited as text, never re-encoded.
var trashOffPattern = regexp.MustCompile(`("files\.enableTrash"\s*:\s*)false`)

func vscodeStatus() Integration {
	in := Integration{Name: VSCode, Enabled: true, Detail: "VS Code deletes to the trash"}
	path, err := vscodeSettings()
	if err != nil {
		return in
	}
	if b, err := os.ReadFile(path); err == nil && trashOffPattern.Match(b) {
		in.Enabled = false
		in.Detail = path + " sets files.enableTrash to false; run ws trash enable"
	}
	return in
}

func enableVSCode() error {
	path, err := vscodeSettings()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, trashOffPattern.ReplaceAll(b, []byte("${1}true")), 0o644)
}

// ── file-explorer ──────────────────────────────────────────────────

func fileExplorerStatus(root string) Integration {
	in := Integration{Name: FileExplorer}
	for _, dir := range []string{filesDir(root), infoDir(root)} {
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			in.Detail = dir + " is missing; run ws trash enable"
			return in
		}
	}
	in.Enabled = true
	in.Detail = "the file manager and ws share " + root
	return in
}

func enableFileExplorer(root string) error {
	for _, dir := range []string{filesDir(root), infoDir(root)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// ── enable and disable ─────────────────────────────────────────────

// EnableOptions chooses the integrations enable leaves out.
type EnableOptions struct {
	NoShellRM, NoVSCode, NoFileExplorer bool
}

// EnableActions builds one action per integration that is off and not left out.
// An empty result means soft-delete is already on.
func EnableActions(opts EnableOptions) ([]plan.Action, error) {
	st, err := GetStatus()
	if err != nil {
		return nil, err
	}
	var actions []plan.Action
	if !opts.NoShellRM && !st.Enabled(ShellRM) {
		script, _ := scriptPath()
		actions = append(actions, plan.Action{
			ID:          "trash-enable-" + ShellRM,
			Description: fmt.Sprintf("Make shell rm move files to the trash (%s, and an alias in your shell rc file)", script),
			Execute:     func() error { return enableShellRM(st.Root) },
		})
	}
	if !opts.NoVSCode && !st.Enabled(VSCode) {
		path, _ := vscodeSettings()
		actions = append(actions, plan.Action{
			ID:          "trash-enable-" + VSCode,
			Description: fmt.Sprintf("Set files.enableTrash to true in %s", path),
			Execute:     enableVSCode,
		})
	}
	if !opts.NoFileExplorer && !st.Enabled(FileExplorer) {
		actions = append(actions, plan.Action{
			ID:          "trash-enable-" + FileExplorer,
			Description: fmt.Sprintf("Create %s and %s", filesDir(st.Root), infoDir(st.Root)),
			Execute:     func() error { return enableFileExplorer(st.Root) },
		})
	}
	return actions, nil
}

// legacyStateFile is where older ws recorded the trash setup. It is no longer
// read; disable deletes it.
func legacyStateFile() (string, error) {
	dir, err := wsconfig.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "trash-setup.json"), nil
}

// DisableActions builds the actions that turn off the shell integration: one
// that removes ws's alias line (in both its forms) from ~/.bashrc and ~/.zshrc and
// then deletes ~/.local/bin/ws-trash-rm, and one for the legacy state file. It
// leaves the trash and its directories alone, and VS Code's setting too: those
// are the system's defaults, and ws keeps soft-deleting either way.
func DisableActions() []plan.Action {
	var actions []plan.Action
	script, _ := scriptPath()
	if rcs := rcWithAlias(); len(rcs) > 0 || fileExists(script) {
		actions = append(actions, plan.Action{
			ID:          "trash-disable-" + ShellRM,
			Description: fmt.Sprintf("Stop shell rm from using the trash (remove the alias from %s, delete %s)", strings.Join(rcs, ", "), script),
			Execute: func() error {
				for _, rc := range rcFiles() {
					if err := removeAlias(rc); err != nil {
						return err
					}
				}
				if err := os.Remove(script); err != nil && !os.IsNotExist(err) {
					return err
				}
				return nil
			},
		})
	}
	if state, err := legacyStateFile(); err == nil && fileExists(state) {
		actions = append(actions, plan.Action{
			ID:          "trash-disable-state",
			Description: fmt.Sprintf("Delete the legacy state file %s", state),
			Execute: func() error {
				if err := os.Remove(state); err != nil && !os.IsNotExist(err) {
					return err
				}
				return nil
			},
		})
	}
	return actions
}

func fileExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
