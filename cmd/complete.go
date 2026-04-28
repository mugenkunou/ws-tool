package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/manifest"
)

// Shell completion directives — values match Cobra's protocol so standard
// shell scripts understand them without translation.
const (
	compDirectiveDefault    = 0  // default completion behavior
	compDirectiveNoSpace    = 2  // don't add trailing space
	compDirectiveNoFileComp = 4  // suppress file name completion
	compDirectiveFilterDirs = 16 // only complete directories
)

// topLevelCommands is the authoritative list used by both __complete
// and the generated shell scripts. Keep alphabetised.
var topLevelCommands = []string{
	"capture",
	"completions",
	"config",
	"context",
	"dotfile",
	"git-credential-helper",
	"help",
	"ignore",
	"init",
	"log",
	"repo",
	"restore",
	"scan",
	"scratch",
	"search",
	"secret",
	"trash",
	"tui",
	"reset",
	"version",
}

// completer describes completion behavior for one top-level command.
type completer struct {
	subcommands []string
	// resolve provides dynamic completions after the subcommand position.
	// sub is the resolved subcommand (empty if none yet), args are the
	// positional arguments already present, toComplete is the current word.
	resolve func(sub string, args []string, toComplete string, ctx completionCtx) ([]string, int)
}

// completers is the data-driven dispatch table for all commands.
var completers = map[string]completer{
	"capture":               {subcommands: []string{"ls"}, resolve: completeCapture},
	"completions":           {subcommands: []string{"bash", "zsh", "fish", "install", "uninstall"}},
	"config":                {subcommands: []string{"view", "defaults"}},
	"context":               {subcommands: []string{"create", "list", "rm"}, resolve: completeContext},
	"dotfile":               {subcommands: []string{"add", "rm", "ls", "scan", "fix", "reset", "git"}, resolve: completeDotfile},
	"git-credential-helper": {subcommands: []string{"setup", "status", "disconnect"}},
	"ignore":                {subcommands: []string{"check", "scan", "fix", "ls", "tree", "edit"}, resolve: completeIgnore},
	"log":                   {subcommands: []string{"start", "stop", "ls", "prune", "rm"}, resolve: completeLog},
	"repo":                  {subcommands: []string{"ls", "scan", "fetch", "pull", "sync", "run"}},
	"scratch":               {subcommands: []string{"new", "open", "ls", "tag", "search", "prune", "rm"}, resolve: completeScratch},
	"secret":                {subcommands: []string{"scan", "fix", "setup", "status", "git"}, resolve: completeSecret},
	"search":                {resolve: completeSearch},
	"trash":                 {subcommands: []string{"enable", "disable", "status"}},
}

// completionCtx holds best-effort workspace state loaded once per
// __complete invocation. Fields may be zero-valued if loading fails.
type completionCtx struct {
	workspace        string
	scratchDir       string
	logDir           string
	dotfiles         []string // dotfile names from manifest
	logTags          []string // session tag directory names
	scratchIDs       []string // scratch directory names
	captureLocations []string // configured capture location names
}

// globalFlagCompletions is the static list offered when the current word
// starts with "-".
var globalFlagCompletions = []string{
	"--workspace",
	"--config",
	"--manifest",
	"--quiet",
	"--verbose",
	"--json",
	"--dry-run",
	"--no-color",
	"--help",
}

// runComplete implements the hidden __complete command. It is called by
// the shell completion scripts to resolve dynamic completions at tab-time.
//
// Protocol (stdout, one entry per line):
//
//	completion1
//	completion2
//	:<directive>
//
// The directive integer on the last line tells the shell how to behave.
func runComplete(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	// The shell sends the words after "ws" including the partial word being
	// completed. For example "ws ignore check ./con" arrives as:
	//   args = ["ignore", "check", "./con"]
	// When the user has typed "ws ignore " (trailing space), the shell sends:
	//   args = ["ignore", ""]

	comps, directive := resolveCompletions(args, globals)
	for _, c := range comps {
		fmt.Fprintln(stdout, c)
	}
	fmt.Fprintf(stdout, ":%d\n", directive)
	return 0
}

// resolveCompletions is the pure-logic core, factored out for testability.
func resolveCompletions(args []string, globals globalFlags) ([]string, int) {
	// Strip consumed flags from the word list so positional resolution works.
	cleaned, toComplete := splitArgsForCompletion(args)

	// No command word yet — complete top-level commands.
	if len(cleaned) == 0 {
		if strings.HasPrefix(toComplete, "-") {
			return filterPrefix(globalFlagCompletions, toComplete), compDirectiveNoFileComp
		}
		return filterPrefix(topLevelCommands, toComplete), compDirectiveNoFileComp
	}

	command := cleaned[0]
	rest := cleaned[1:]

	comp, known := completers[command]
	if !known {
		// Unknown command — nothing to suggest.
		return nil, compDirectiveNoFileComp
	}

	// Flag completion at any depth.
	if strings.HasPrefix(toComplete, "-") {
		flagComps := commandFlags(command, rest)
		flagComps = append(flagComps, globalFlagCompletions...)
		return filterPrefix(dedupe(flagComps), toComplete), compDirectiveNoFileComp
	}

	// Subcommand position — first positional after the command.
	if len(rest) == 0 && len(comp.subcommands) > 0 {
		return filterPrefix(comp.subcommands, toComplete), compDirectiveNoFileComp
	}

	// Determine resolved subcommand and remaining positional args.
	sub := ""
	positional := rest
	if len(rest) > 0 && len(comp.subcommands) > 0 && contains(comp.subcommands, rest[0]) {
		sub = rest[0]
		positional = rest[1:]
	}

	if comp.resolve != nil {
		ctx := loadCompletionCtx(globals)
		return comp.resolve(sub, positional, toComplete, ctx)
	}

	return nil, compDirectiveNoFileComp
}

// ── Dynamic resolvers ──────────────────────────────────────────────

func completeIgnore(sub string, args []string, toComplete string, ctx completionCtx) ([]string, int) {
	switch sub {
	case "check":
		// File path completion — return default directive to let the shell
		// perform its native file completion.
		return nil, compDirectiveDefault
	case "edit":
		// Could complete --editor but that's a flag; positional needs nothing.
		return nil, compDirectiveNoFileComp
	default:
		return nil, compDirectiveNoFileComp
	}
}

func completeDotfile(sub string, args []string, toComplete string, ctx completionCtx) ([]string, int) {
	switch sub {
	case "add":
		// System file path — let the shell do native file completion.
		return nil, compDirectiveDefault
	case "rm":
		// Complete from registered dotfile names or system paths.
		if len(args) == 0 {
			return filterPrefix(ctx.dotfiles, toComplete), compDirectiveNoFileComp
		}
		return nil, compDirectiveNoFileComp
	case "git":
		// Sub-subcommands.
		if len(args) == 0 {
			return filterPrefix([]string{"remote", "push", "log", "status", "setup", "disconnect"}, toComplete), compDirectiveNoFileComp
		}
		return nil, compDirectiveNoFileComp
	default:
		return nil, compDirectiveNoFileComp
	}
}

func completeLog(sub string, args []string, toComplete string, ctx completionCtx) ([]string, int) {
	switch sub {
	case "show", "remove":
		if len(args) == 0 {
			return filterPrefix(ctx.logTags, toComplete), compDirectiveNoFileComp
		}
		return nil, compDirectiveNoFileComp
	case "search":
		// free-form query — no completions, but no file fallback either.
		return nil, compDirectiveNoFileComp
	default:
		return nil, compDirectiveNoFileComp
	}
}

func completeScratch(sub string, args []string, toComplete string, ctx completionCtx) ([]string, int) {
	switch sub {
	case "rm", "tag", "open":
		if len(args) == 0 {
			return filterPrefix(ctx.scratchIDs, toComplete), compDirectiveNoFileComp
		}
		return nil, compDirectiveNoFileComp
	case "new":
		// Free-form name — show existing scratch names as context hints,
		// but don't suppress file completion (user may want to skip).
		return filterPrefix(ctx.scratchIDs, toComplete), compDirectiveNoFileComp
	case "search":
		return filterPrefix(ctx.scratchIDs, toComplete), compDirectiveNoFileComp
	case "ls":
		return nil, compDirectiveNoFileComp
	case "prune":
		return nil, compDirectiveNoFileComp
	default:
		return nil, compDirectiveNoFileComp
	}
}

func completeSecret(sub string, args []string, toComplete string, ctx completionCtx) ([]string, int) {
	switch sub {
	case "git":
		if len(args) == 0 {
			return filterPrefix([]string{"push", "log", "remote", "status"}, toComplete), compDirectiveNoFileComp
		}
		return nil, compDirectiveNoFileComp
	default:
		return nil, compDirectiveNoFileComp
	}
}

func completeSearch(sub string, args []string, toComplete string, ctx completionCtx) ([]string, int) {
	// free-form query — nothing to complete.
	return nil, compDirectiveNoFileComp
}

func completeCapture(sub string, args []string, toComplete string, ctx completionCtx) ([]string, int) {
	switch sub {
	case "ls":
		return nil, compDirectiveNoFileComp
	default:
		// Pin mode — complete configured location names as first positional.
		if len(args) == 0 {
			return filterPrefix(ctx.captureLocations, toComplete), compDirectiveNoFileComp
		}
		return nil, compDirectiveNoFileComp
	}
}

func completeContext(sub string, args []string, toComplete string, ctx completionCtx) ([]string, int) {
	switch sub {
	case "create":
		// task name is free-form; --path is handled as a flag.
		return nil, compDirectiveNoFileComp
	default:
		return nil, compDirectiveNoFileComp
	}
}

// ── Flag-value completions ─────────────────────────────────────────

// commandFlags returns command- and subcommand-specific flag names.
func commandFlags(command string, rest []string) []string {
	sub := ""
	if len(rest) > 0 {
		sub = rest[0]
	}

	// Common write-command flags.
	dryRun := "--dry-run"

	switch command {
	case "capture":
		return []string{"-e", "--edit", "-a", "--amend", "--dry-run"}
	case "version":
		return []string{"--short"}
	case "search":
		return []string{"--type", "--path", "--context", "--max-results", "--no-pager"}
	case "scratch":
		switch sub {
		case "new":
			return []string{"--no-open", "--editor", "--no-date", "--dry-run"}
		case "open":
			return []string{"--editor"}
		case "ls":
			return []string{"--sort"}
		case "prune":
			return []string{"--older-than", "--all", "--name", dryRun}
		case "delete":
			return []string{dryRun}
		}
	case "log":
		switch sub {
		case "start":
			return []string{"--tag", "--quiet-start", "--no-prompt"}
		case "search":
			return []string{"--tag", "--since"}
		case "show":
			return []string{"--commands-only", "--output-only", "--merged"}
		case "prune":
			return []string{"--older-than", "--all"}
		}
	case "ignore":
		switch sub {
		case "generate":
			return []string{"--merge", "--force", dryRun}
		case "ls":
			return []string{"--path", "--rule"}
		case "tree":
			return []string{"--path", "--depth"}
		case "edit":
			return []string{"--editor"}
		}
	case "dotfile":
		switch sub {
		case "add":
			return []string{"--sudo", "--name", dryRun}
		case "rm":
			return []string{dryRun}
		case "fix":
			return []string{"--sudo", dryRun}
		}
		if sub == "git" {
			// rest[0] is "git", check for sub-sub in rest[1:]
			if len(rest) > 1 {
				switch rest[1] {
				case "remote":
					return []string{"--pass-entry", dryRun}
				case "init", "setup", "disconnect":
					return []string{dryRun}
				case "log":
					return []string{"-n"}
				case "sync":
					return []string{"-m"}
				}
			}
		}
	case "secret":
		if sub == "fix" {
			return []string{"--mode"}
		}
		if sub == "git" {
			if len(rest) > 1 {
				switch rest[1] {
				case "remote":
					return []string{dryRun}
				case "log":
					return []string{"-n"}
				}
			}
		}
	case "git-credential-helper":
		if sub == "setup" || sub == "disconnect" {
			return []string{dryRun}
		}
	case "context":
		if sub == "init" {
			return []string{"--path", dryRun}
		}
	case "trash":
		if sub == "enable" || sub == "setup" {
			return []string{"--root-dir", "--no-shell-rm", "--no-vscode", "--no-file-explorer", dryRun}
		}
	case "completions":
		if sub == "install" || sub == "uninstall" {
			return []string{"--shell", dryRun}
		}
	case "repo":
		switch sub {
		case "pull":
			return []string{"--all", dryRun}
		case "sync":
			return []string{"--rebase", dryRun}
		case "fix":
			return []string{dryRun}
		case "run":
			return []string{dryRun}
		}
	}
	return nil
}

// ── Context loader ─────────────────────────────────────────────────

// loadCompletionCtx performs best-effort loading of workspace state.
// Failures are silent — tab completion must never produce error output.
func loadCompletionCtx(globals globalFlags) completionCtx {
	var ctx completionCtx

	workspace := globals.workspace
	if workspace == "" {
		workspace = "~/Workspace"
	}
	resolved, err := config.ExpandUserPath(workspace)
	if err != nil {
		return ctx
	}
	ctx.workspace = resolved

	// Load config for scratch.root_dir.
	configPath := globals.config
	if configPath == "" {
		configPath = filepath.Join(resolved, "ws", "config.json")
	}
	if cfg, err := config.Load(configPath); err == nil {
		if dir, err := config.ResolvePath(resolved, cfg.Scratch.RootDir); err == nil {
			ctx.scratchDir = dir
		}
		for name := range cfg.Capture.Locations {
			ctx.captureLocations = append(ctx.captureLocations, name)
		}
	}
	if ctx.scratchDir == "" {
		if home, err := config.ExpandUserPath("~/Scratch"); err == nil {
			ctx.scratchDir = home
		}
	}

	// Load manifest for dotfile names.
	manifestPath := globals.manifest
	if manifestPath == "" {
		manifestPath = filepath.Join(resolved, "ws", "manifest.json")
	}
	if m, err := manifest.Load(manifestPath); err == nil {
		for _, d := range m.Dotfiles {
			ctx.dotfiles = append(ctx.dotfiles, d.Name)
			ctx.dotfiles = append(ctx.dotfiles, d.System)
		}
	}

	// Log session tags: directory names under ws/ws-log/.
	ctx.logDir = filepath.Join(resolved, "ws", "ws-log")
	ctx.logTags = listDirNames(ctx.logDir)

	// Scratch directory names.
	ctx.scratchIDs = listDirNames(ctx.scratchDir)

	return ctx
}

// ── Helpers ────────────────────────────────────────────────────────

// splitArgsForCompletion separates already-typed words from the word
// currently being completed. Flag arguments (--foo, --foo=val, and the
// value following a non-bool --flag) are stripped so positional indexing
// works correctly.
func splitArgsForCompletion(raw []string) (positional []string, toComplete string) {
	if len(raw) == 0 {
		return nil, ""
	}

	// The last element is always the word being completed (may be "").
	toComplete = raw[len(raw)-1]
	words := raw[:len(raw)-1]

	for i := 0; i < len(words); i++ {
		w := words[i]
		if w == "--" {
			positional = append(positional, words[i+1:]...)
			return positional, toComplete
		}
		if strings.HasPrefix(w, "-") {
			// --flag=value form — single token, skip it.
			if strings.Contains(w, "=") {
				continue
			}
			// Bool flags are single-token; string flags consume the next token.
			// We use a simple heuristic: if the next token does not start with "-"
			// and the flag looks like a known string flag, consume it.
			if i+1 < len(words) && !strings.HasPrefix(words[i+1], "-") && isStringFlag(w) {
				i++ // skip the value
			}
			continue
		}
		positional = append(positional, w)
	}
	return positional, toComplete
}

// isStringFlag returns true for flags known to take a value argument.
func isStringFlag(flag string) bool {
	name := strings.TrimLeft(flag, "-")
	switch name {
	case "workspace", "w", "config", "c", "manifest",
		"tag", "since", "type", "path", "context", "max-results",
		"sort", "older-than", "name", "editor", "terminal",
		"root-dir", "secret-mode", "mode",
		"remote-url", "username", "pass-entry", "branch",
		"shell", "depth", "rule",
		"location", "l":
		return true
	}
	return false
}

// filterPrefix returns entries from candidates that have the given prefix.
func filterPrefix(candidates []string, prefix string) []string {
	if prefix == "" {
		return candidates
	}
	var out []string
	for _, c := range candidates {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// contains checks if a string slice includes a value.
func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// dedupe removes duplicate strings, preserving order.
func dedupe(ss []string) []string {
	seen := make(map[string]struct{}, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// listDirNames lists immediate subdirectory names under a path.
// Returns nil on any error.
func listDirNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}
