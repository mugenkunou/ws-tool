package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// alias maps an old command line prefix to its current command (spec §3.5).
// Aliases are hidden: help, completion and docs never show them. A typed alias
// prints one stderr line, `ws: "<old>" is now "<new>"`. A plumbing alias is
// called by other programs (crontab lines, git, shell rc files) and stays
// silent. dev.md §12.6 requires every rename to be a row here.
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
	"dotfile": true,
	"log":     true,
	"scratch": true,
	"trash":   true,
	"cron":    true,
	"ignore":  true,
	"repo":    true,
	// Workspace commands.
	"config":      true,
	"completions": true,
	"reset":       true,
	"restore":     true,
}

// aliases is every row of spec §3.5.
var aliases = []alias{
	{Old: []string{"repo", "scan"}, New: []string{"repo", "ls"}, Hint: "repo ls --fetch", Rewrite: repoScanRewrite},
	{Old: []string{"repo", "doctor"}, New: []string{"repo", "ls"}},
	{Old: []string{"repo", "sync"}, New: []string{"repo", "save"}},
	{Old: []string{"repo", "add-root"}, New: []string{"repo", "root", "add"}},
	{Old: []string{"repo", "ls-roots"}, New: []string{"repo", "root", "ls"}},

	{Old: []string{"dotfile", "scan"}, New: []string{"dotfile", "ls"}},
	{Old: []string{"dotfile", "rm"}, New: []string{"dotfile", "unlink"}},
	{Old: []string{"dotfile", "reset"}, New: []string{"dotfile", "unlink", "--all"}},
	{Old: []string{"dotfile", "fix"}, New: []string{"dotfile", "link"}},
	{Old: []string{"dotfile", "sync"}, New: []string{"dotfile", "add"}},
	{Old: []string{"dotfile", "link"}, New: []string{"dotfile", "link"}, Hint: "dotfile link <path> --to <ws-path>", Rewrite: dotfileLinkRewrite},
	{Old: []string{"dotfile", "git", "setup"}, New: []string{"dotfile", "backup", "enable"}},
	{Old: []string{"dotfile", "git", "enable"}, New: []string{"dotfile", "backup", "enable"}},
	{Old: []string{"dotfile", "git", "connect"}, New: []string{"dotfile", "backup", "enable"}},
	{Old: []string{"dotfile", "git", "remote"}, New: []string{"dotfile", "backup", "enable"}, Hint: "dotfile backup enable --remote <url>", Rewrite: gitRemoteRewrite([]string{"dotfile", "backup", "enable"})},
	{Old: []string{"dotfile", "git", "disconnect"}, New: []string{"dotfile", "backup", "disable"}},
	{Old: []string{"dotfile", "git", "status"}, New: []string{"dotfile", "backup", "status"}},
	{Old: []string{"dotfile", "git", "log"}, New: []string{"dotfile", "backup", "status"}},
	{Old: []string{"dotfile", "git", "push"}, New: []string{"dotfile", "backup", "push"}},

	{Old: []string{"scratch", "search"}, New: []string{"scratch", "ls"}, Hint: "scratch ls --filter", Rewrite: scratchSearchRewrite},

	{Old: []string{"log", "start"}, New: []string{"session", "record"}, Rewrite: logStartRewrite},
	{Old: []string{"log", "stop"}, New: []string{"session", "stop"}},
	{Old: []string{"log", "ls"}, New: []string{"session", "ls"}},
	{Old: []string{"log", "rm"}, New: []string{"session", "rm"}},
	{Old: []string{"log", "prune"}, New: []string{"session", "prune"}},

	{Old: []string{"ignore", "scan"}, New: []string{"ignore", "lint"}, Hint: "ignore lint", Rewrite: ignoreScanRewrite},
	{Old: []string{"ignore", "fix"}, Removed: `ws: "ignore fix" is gone: use ws ignore add, ws ignore mute, ws scratch add, or rm`},

	{Old: []string{"secret", "scan"}, New: []string{"secret", "ls"}, Rewrite: secretScanRewrite},
	{Old: []string{"secret", "fix"}, Removed: `ws: "secret fix" is gone: use ws secret allow, ws ignore add, ws secret mute, or ws secret store`},
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

	{Old: []string{"reset"}, Removed: `ws: "reset" was removed; use ws dotfile unlink --all, ws trash disable, ws cron rm <job>`},
	{Old: []string{"restore"}, Removed: `ws: "restore" was removed; on a new machine run ws init, ws dotfile link, ws trash enable, ws cron apply`},
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

// repoScanRewrite: `repo scan` fetched by default, `--no-fetch` skipped it.
func repoScanRewrite(rest []string, _ *globalFlags) ([]string, bool) {
	words := []string{"repo", "ls"}
	if slices.Contains(rest, "--no-fetch") {
		return append(words, dropFlag(rest, "--no-fetch")...), true
	}
	return append(append(words, "--fetch"), rest...), true
}

// dotfileLinkRewrite: the old `dotfile link <path> <ws-path>` takes the second
// path as `--to` when it lies inside the workspace, where no system path can.
func dotfileLinkRewrite(rest []string, g *globalFlags) ([]string, bool) {
	pos := positionals(rest, "class")
	if len(pos) != 2 || slices.Contains(rest, "--to") {
		return nil, false
	}
	ws, _, err := resolveWorkspacePaths(*g)
	if err != nil {
		return nil, false
	}
	second := expandForAlias(pos[1])
	if !insideDir(ws, second) {
		return nil, false
	}
	words := []string{"dotfile", "link"}
	for _, a := range rest {
		if a == pos[1] {
			words = append(words, "--to")
		}
		words = append(words, a)
	}
	return words, true
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

// logStartRewrite: `log start --tag t` is `session record t`, and
// `--quiet-start` is `--quiet`.
func logStartRewrite(rest []string, g *globalFlags) ([]string, bool) {
	words := []string{"session", "record"}
	var tag string
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--tag" && i+1 < len(rest):
			i++
			tag = rest[i]
		case strings.HasPrefix(a, "--tag="):
			tag = strings.TrimPrefix(a, "--tag=")
		case a == "--quiet-start":
			g.quiet = true
			words = append(words, "--quiet")
		default:
			words = append(words, a)
		}
	}
	if tag != "" {
		words = append(words[:2], append([]string{tag}, words[2:]...)...)
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

// secretScanRewrite: `secret scan --pass` inspected the pass store, which is
// now `pass audit`.
func secretScanRewrite(rest []string, _ *globalFlags) ([]string, bool) {
	if slices.Contains(rest, "--pass") {
		return []string{"pass", "audit"}, true
	}
	return append([]string{"secret", "ls"}, rest...), true
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
