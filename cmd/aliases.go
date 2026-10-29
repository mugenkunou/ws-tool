package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/secret"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// alias maps an old command line prefix to its current command (spec §3.5).
// Aliases are hidden: help, completion and docs never show them. A typed alias
// prints one stderr line, `ws: "<old>" is now "<new>"`. A plumbing alias is
// called by other programs (crontab lines, git, shell rc files) and stays
// silent. HANDBOOK.md §9.7 requires every rename to be a row here.
type alias struct {
	Old []string
	New []string
	// Plumbing marks aliases that other programs call; they never print.
	Plumbing bool
	// Hint overrides the new command shown in the stderr line.
	Hint string
	// Rewrite computes the full replacement for Old plus the words after it.
	// applies is false when the alias does not cover these arguments. When nil,
	// the replacement is New followed by the words after Old.
	Rewrite func(rest []string, g *globalFlags) (words []string, applies bool)
	// Removed is set for an old command with no single successor: typed, it
	// prints this line on stderr and exits 1.
	Removed string
}

// aliasLive lists the old first words whose aliases are switched on. Each
// noun's rows go live in the commit that implements its new commands, so the
// old and new handlers never disagree about which names exist.
var aliasLive = map[string]bool{
	"scratch":               true,
	"trash":                 true,
	"cron":                  true,
	"ignore":                true,
	"repo":                  true,
	"secret":                true,
	"git-credential-helper": true,
	"credential":            true,
	// Workspace commands.
	"config":      true,
	"completions": true,
	"reset":       true,
	"restore":     true,
}

// aliases is every row of spec §3.5.
var aliases = []alias{
	{Old: []string{"repo", "scan"}, New: []string{"repo", "ls"}, Rewrite: repoScanRewrite},
	{Old: []string{"repo", "doctor"}, New: []string{"repo", "ls"}},
	// The fleet verbs became plain git, which behaves differently enough
	// (no fetch first, no rebase-or-merge question) that an alias would mislead.
	{Old: []string{"repo", "fetch"}, Removed: `ws: "repo fetch" is gone: use ws repo git fetch`},
	{Old: []string{"repo", "pull"}, Removed: `ws: "repo pull" is gone: use ws repo git pull`},
	{Old: []string{"repo", "push"}, Removed: `ws: "repo push" is gone: use ws repo git push`},
	{Old: []string{"repo", "save"}, Removed: `ws: "repo save" is gone: use ws repo git, e.g. ws repo git commit -am <msg>, then ws repo git push`},
	{Old: []string{"repo", "sync"}, Removed: `ws: "repo sync" is gone: use ws repo git, e.g. ws repo git pull, then ws repo git push`},
	{Old: []string{"repo", "add-root"}, New: []string{"repo", "root", "add"}},
	{Old: []string{"repo", "ls-roots"}, New: []string{"repo", "root", "ls"}},

	{Old: []string{"scratch", "search"}, New: []string{"scratch", "ls"}, Hint: "scratch ls --filter", Rewrite: scratchSearchRewrite},

	{Old: []string{"ignore", "scan"}, New: []string{"ignore", "lint"}, Hint: "ignore lint", Rewrite: ignoreScanRewrite},
	{Old: []string{"ignore", "fix"}, Removed: `ws: "ignore fix" is gone: use ws ignore add, ws ignore mute, ws scratch add, or rm`},

	{Old: []string{"secret", "scan"}, New: []string{"pass", "audit"}, Rewrite: secretScanPassRewrite},
	{Old: []string{"secret", "scan"}, New: []string{"secret", "ls"}, Rewrite: secretScanRewrite},
	// The batch forms of `secret fix` stay as aliases (spec §3.5); without
	// --secret-mode the interactive command is gone (the row after this one).
	{Old: []string{"secret", "fix"}, New: []string{"secret", "allow"}, Hint: "secret allow <each finding>", Rewrite: secretFixRewrite("allowlist")},
	{Old: []string{"secret", "fix"}, New: []string{"ignore", "add"}, Hint: "ignore add <file of each finding>", Rewrite: secretFixRewrite("exclude")},
	{Old: []string{"secret", "fix"}, Removed: `ws: "secret fix" is gone: use ws secret allow, ws ignore add, or ws secret skip`},
	{Old: []string{"secret", "setup"}, New: []string{"pass", "init"}},
	{Old: []string{"secret", "status"}, New: []string{"pass", "status"}},
	{Old: []string{"secret", "git", "remote"}, New: []string{"pass", "backup", "enable"}, Hint: "pass backup enable --remote <url>", Rewrite: gitRemoteRewrite([]string{"pass", "backup", "enable"})},
	{Old: []string{"secret", "git", "status"}, New: []string{"pass", "backup", "status"}},
	{Old: []string{"secret", "git", "log"}, New: []string{"pass", "backup", "status"}},
	{Old: []string{"secret", "git", "push"}, New: []string{"pass", "backup", "push"}},

	{Old: []string{"git-credential-helper", "setup"}, New: []string{"credential", "enable"}},
	{Old: []string{"git-credential-helper", "status"}, New: []string{"credential", "status"}},
	{Old: []string{"git-credential-helper", "disconnect"}, New: []string{"credential", "disable"}},
	{Old: []string{"credential", "setup"}, New: []string{"credential", "enable"}},
	{Old: []string{"credential", "disconnect"}, New: []string{"credential", "disable"}},
	// git configs written by older versions say `!ws credential`.
	{Old: []string{"credential", "get"}, New: []string{"git-credential-helper", "get"}, Plumbing: true},
	{Old: []string{"credential", "store"}, New: []string{"git-credential-helper", "store"}, Plumbing: true},
	{Old: []string{"credential", "erase"}, New: []string{"git-credential-helper", "erase"}, Plumbing: true},

	{Old: []string{"cron", "list"}, New: []string{"cron", "ls"}},
	{Old: []string{"cron", "remove"}, New: []string{"cron", "rm"}},
	{Old: []string{"cron", "status"}, New: []string{"cron", "ls"}, Hint: "cron ls", Rewrite: cronStatusRewrite},

	{Old: []string{"trash", "setup"}, New: []string{"trash", "enable"}},
	{Old: []string{"trash", "purge"}, New: []string{"trash", "prune"}},

	{Old: []string{"config", "view"}, New: []string{"config", "show"}},
	{Old: []string{"config", "defaults"}, New: []string{"config", "show", "--defaults"}},
	{Old: []string{"config"}, New: []string{"config", "show", "--defaults"}, Rewrite: configDefaultsFlagRewrite},

	// Shell rc files run `eval "$(ws completions bash)"` at every shell start.
	{Old: []string{"completions", "bash"}, New: []string{"completion", "bash"}, Plumbing: true},
	{Old: []string{"completions", "zsh"}, New: []string{"completion", "zsh"}, Plumbing: true},
	{Old: []string{"completions", "fish"}, New: []string{"completion", "fish"}, Plumbing: true},
	{Old: []string{"completions", "install"}, New: []string{"completion", "enable"}},
	{Old: []string{"completions", "uninstall"}, New: []string{"completion", "disable"}},

	{Old: []string{"reset"}, Removed: `ws: "reset" was removed; use ws symlink unlink --all, ws trash disable, ws cron rm <job>`},
	{Old: []string{"restore"}, Removed: `ws: "restore" was removed; on a new machine run ws init, ws symlink link, ws trash enable, ws cron apply`},
}

// leadingPrefix reports whether the first len(old) words of rest are old.
func leadingPrefix(rest, old []string) bool {
	return len(rest) >= len(old) && slices.Equal(rest[:len(old)], old)
}

// findAlias returns the live alias with the longest Old prefix of rest that
// applies to its arguments, and the words that replace the whole line.
func findAlias(rest []string, g *globalFlags) (a alias, words []string, ok bool) {
	for _, cand := range aliases {
		if !aliasLive[cand.Old[0]] || !leadingPrefix(rest, cand.Old) {
			continue
		}
		if ok && len(cand.Old) <= len(a.Old) {
			continue
		}
		after := rest[len(cand.Old):]
		var w []string
		if cand.Removed != "" {
			w = nil
		} else if cand.Rewrite != nil {
			var applies bool
			if w, applies = cand.Rewrite(after, g); !applies {
				continue
			}
		} else {
			w = append(slices.Clone(cand.New), after...)
		}
		a, words, ok = cand, w, true
	}
	return a, words, ok
}

// resolveAlias rewrites a hidden alias into its current command before the
// command is dispatched. done is true when the line was a removed command (it
// has printed its pointer; exit is the exit code to return).
func resolveAlias(rest []string, globals *globalFlags, stderr io.Writer) (out []string, exit int, done bool) {
	a, words, ok := findAlias(rest, globals)
	if !ok {
		return rest, 0, false
	}
	if a.Removed != "" {
		fmt.Fprintln(stderr, a.Removed)
		return nil, 1, true
	}
	if !a.Plumbing && !globals.quiet && !globals.json {
		fmt.Fprintf(stderr, "ws: %q is now %q\n", strings.Join(a.Old, " "), aliasNew(a))
	}
	return words, 0, false
}

// aliasNew is the new command as the stderr line names it.
func aliasNew(a alias) string {
	if a.Hint != "" {
		return a.Hint
	}
	return strings.Join(a.New, " ")
}

// positionals returns the words that are not flags. valueFlags lists the flags
// that take the next word as their value, so that word is not a positional.
func positionals(args []string, valueFlags ...string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return append(out, args[i+1:]...)
		}
		if strings.HasPrefix(a, "-") {
			if !strings.Contains(a, "=") && slices.Contains(valueFlags, strings.TrimLeft(a, "-")) {
				i++
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

// dropFlag removes every occurrence of a boolean flag.
func dropFlag(args []string, name string) []string {
	return slices.DeleteFunc(slices.Clone(args), func(a string) bool { return a == name })
}

// repoScanRewrite: `repo scan [--no-fetch]` is `repo ls`, which never fetches.
func repoScanRewrite(rest []string, _ *globalFlags) ([]string, bool) {
	return append([]string{"repo", "ls"}, dropFlag(rest, "--no-fetch")...), true
}

// expandForAlias resolves a path argument the way every command does: `~`
// expanded, relative to the current directory.
func expandForAlias(p string) string {
	if abs, err := wsconfig.ExpandUserPath(p); err == nil {
		return abs
	}
	return p
}

// insideDir reports whether p is dir or lies under it.
func insideDir(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// gitRemoteRewrite: `<x> git remote <url> [--pass-entry e]` became
// `<backup> enable --remote <url>`.
func gitRemoteRewrite(target []string) func([]string, *globalFlags) ([]string, bool) {
	return func(rest []string, _ *globalFlags) ([]string, bool) {
		pos := positionals(rest, "pass-entry")
		words := slices.Clone(target)
		for _, a := range rest {
			if len(pos) > 0 && a == pos[0] {
				words = append(words, "--remote", a)
				pos = pos[1:]
				continue
			}
			words = append(words, a)
		}
		return words, true
	}
}

// scratchSearchRewrite: `scratch search a b` is `scratch ls --filter a --filter b`.
func scratchSearchRewrite(rest []string, _ *globalFlags) ([]string, bool) {
	words := []string{"scratch", "ls"}
	pos := positionals(rest, "sort", "filter")
	for _, a := range rest {
		if len(pos) > 0 && a == pos[0] {
			words = append(words, "--filter", a)
			pos = pos[1:]
			continue
		}
		words = append(words, a)
	}
	return words, true
}

// ignoreScanRewrite: `ignore scan` is `ignore lint`; the expand flags showed
// the muted rows, which are now `ignore ls --muted`.
func ignoreScanRewrite(rest []string, _ *globalFlags) ([]string, bool) {
	for _, f := range []string{"--expand-suppressed", "--expand-harbors"} {
		if slices.Contains(rest, f) {
			words := []string{"ignore", "ls", "--muted"}
			return append(words, dropFlag(dropFlag(rest, "--expand-suppressed"), "--expand-harbors")...), true
		}
	}
	return append([]string{"ignore", "lint"}, rest...), true
}

// secretScanPassRewrite: `secret scan --pass` inspected the pass store, which
// is now `pass audit`. Without --pass this row does not apply (the next one).
func secretScanPassRewrite(rest []string, _ *globalFlags) ([]string, bool) {
	if slices.Contains(rest, "--pass") {
		return []string{"pass", "audit"}, true
	}
	return nil, false
}

// secretScanRewrite: `secret scan` is `secret ls`; `--pass` is `pass audit`.
func secretScanRewrite(rest []string, _ *globalFlags) ([]string, bool) {
	if slices.Contains(rest, "--pass") {
		return []string{"pass", "audit"}, true
	}
	return append([]string{"secret", "ls"}, rest...), true
}

// secretFixRewrite: `secret fix --secret-mode allowlist` is `secret allow` on
// every current finding, and `--secret-mode exclude` is `ignore add` on the
// file of each. `--skip-dir` hides directories from the scan that picks the
// findings. With no findings (or no workspace) it is `secret ls`, which says
// so. The `pass` mode has no batch successor: it is not covered and prints the
// removed-command line.
func secretFixRewrite(mode string) func([]string, *globalFlags) ([]string, bool) {
	return func(rest []string, g *globalFlags) ([]string, bool) {
		var got string
		var skip, others []string
		for i := 0; i < len(rest); i++ {
			a := rest[i]
			switch {
			case a == "--secret-mode" && i+1 < len(rest):
				i++
				got = rest[i]
			case strings.HasPrefix(a, "--secret-mode="):
				got = strings.TrimPrefix(a, "--secret-mode=")
			case a == "--skip-dir" && i+1 < len(rest):
				i++
				skip = append(skip, rest[i])
			case strings.HasPrefix(a, "--skip-dir="):
				skip = append(skip, strings.TrimPrefix(a, "--skip-dir="))
			default:
				others = append(others, a)
			}
		}
		if got != mode {
			return nil, false
		}
		fallback := append([]string{"secret", "ls"}, others...)
		env, ok := loadSecretEnv(*g, io.Discard)
		if !ok {
			return fallback, true
		}
		extra, err := skipDirFlagsToRel(env.workspace, skip)
		if err != nil {
			return fallback, true
		}
		findings, err := env.findings(extra)
		if err != nil || len(findings) == 0 {
			return fallback, true
		}
		words := []string{"secret", "allow"}
		if mode == "exclude" {
			words = []string{"ignore", "add"}
		}
		seen := map[string]bool{}
		for _, v := range findings {
			target := secret.AbsAnchor(env.workspace, v)
			if mode == "exclude" {
				target = filepath.Join(env.workspace, filepath.FromSlash(v.Path))
			}
			if !seen[target] {
				seen[target] = true
				words = append(words, target)
			}
		}
		return append(words, others...), true
	}
}

// cronStatusRewrite: `cron status <job>` is `cron ls --filter name=<job>`.
func cronStatusRewrite(rest []string, _ *globalFlags) ([]string, bool) {
	words := []string{"cron", "ls"}
	pos := positionals(rest, "sort", "filter")
	for _, a := range rest {
		if len(pos) > 0 && a == pos[0] {
			words = append(words, "--filter", "name="+a)
			pos = pos[1:]
			continue
		}
		words = append(words, a)
	}
	return words, true
}

// configDefaultsFlagRewrite: `config --defaults` is `config show --defaults`.
func configDefaultsFlagRewrite(rest []string, _ *globalFlags) ([]string, bool) {
	if len(rest) == 0 || rest[0] != "--defaults" {
		return nil, false
	}
	return append([]string{"config", "show"}, rest...), true
}
