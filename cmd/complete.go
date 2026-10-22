package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/trash"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// Shell completion directives — values match Cobra's protocol so standard
// shell scripts understand them without translation.
const (
	compDirectiveDefault    = 0  // default completion behavior
	compDirectiveNoSpace    = 2  // don't add trailing space
	compDirectiveNoFileComp = 4  // suppress file name completion
	compDirectiveFilterDirs = 16 // only complete directories
)

// topLevelCommands is the list used by both __complete and the generated shell
// scripts. It is derived from the command table, so it holds exactly the
// commands help lists and never a hidden alias (dev.md §12.6).
var topLevelCommands = func() []string {
	out := nextWords(nil)
	slices.Sort(out)
	return out
}()

// resolver provides dynamic completions for one command's positional
// arguments. args are the positional words already present, toComplete is the
// current word.
type resolver func(args []string, toComplete string, ctx completionCtx) ([]string, int)

// resolvers maps a command ("repo root rm") to the resolver of its targets.
// Commands not listed here complete no positional argument.
var resolvers = map[string]resolver{
	"repo ls":        repoTargets,
	"repo show":      repoTargets,
	"repo fetch":     repoTargets,
	"repo pull":      repoTargets,
	"repo push":      repoTargets,
	"repo save":      repoTargets,
	"repo configure": repoTargets,
	"repo run":       repoTargets,
	"repo root add":  func([]string, string, completionCtx) ([]string, int) { return nil, compDirectiveFilterDirs },
	"repo root rm":   rootTargets,

	"dotfile add":    fileTargets,
	"dotfile link":   dotfileTargets,
	"dotfile unlink": dotfileTargets,
	"dotfile edit":   dotfileTargets,

	"scratch add":  fileTargets,
	"scratch new":  scratchNames,
	"scratch open": scratchNames,
	"scratch tag":  scratchFirst,
	"scratch rm":   scratchNames,

	"session show": sessionTags,
	"session rm":   sessionTags,

	"ignore lint":   fileTargets,
	"ignore add":    fileTargets,
	"ignore rm":     fileTargets,
	"ignore mute":   fileTargets,
	"ignore unmute": fileTargets,
	"ignore check":  fileTargets,
	"ignore tree":   dirTargets,

	"secret show":   fileTargets,
	"secret allow":  fileTargets,
	"secret store":  fileTargets,
	"secret mute":   dirTargets,
	"secret unmute": dirTargets,

	"cron add": cronTargets,
	"cron rm":  cronTargets,
	"cron run": cronTargets,
	"cron log": cronTargets,

	"trash restore": trashTargets,
	"trash rm":      trashTargets,
}

// completionCtx holds best-effort workspace state loaded once per
// __complete invocation. Fields may be zero-valued if loading fails.
type completionCtx struct {
	workspace  string
	scratchDir string
	logDir     string
	dotfiles   []string // dotfile system paths
	logTags    []string // session tag directory names
	scratchIDs []string // scratch directory names
	repoPaths  []string // discovered repo paths (absolute)
	repoRoots  []string // discovery roots (absolute)
	trashItems []string // names of trashed items
}

// globalFlagCompletions is the static list offered when the current word
// starts with "-".
var globalFlagCompletions = []string{
	"--workspace",
	"--config",
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

// flagValueDirective returns (true, directive) when flag takes a value argument,
// so the shell handles value completion rather than offering command completions.
func flagValueDirective(flag string) (bool, int) {
	name := strings.TrimLeft(flag, "-")
	switch name {
	case "workspace", "w":
		return true, compDirectiveFilterDirs
	case "config", "c":
		return true, compDirectiveDefault
	}
	if isStringFlag(flag) {
		return true, compDirectiveDefault
	}
	return false, 0
}

// resolveCompletions is the pure-logic core, factored out for testability.
func resolveCompletions(args []string, globals globalFlags) ([]string, int) {
	// Detect when completing a flag value: prev word is a string flag.
	if len(args) >= 2 {
		prev := args[len(args)-2]
		if strings.HasPrefix(prev, "-") && !strings.Contains(prev, "=") {
			if ok, dir := flagValueDirective(prev); ok {
				if strings.TrimLeft(prev, "-") == "shell" {
					return filterPrefix([]string{"bash", "zsh", "fish"}, args[len(args)-1]), compDirectiveNoFileComp
				}
				return nil, dir
			}
		}
	}

	// Strip consumed flags from the word list so positional resolution works.
	cleaned, toComplete := splitArgsForCompletion(args)

	// No command word yet: complete the nouns and workspace commands.
	if len(cleaned) == 0 {
		if strings.HasPrefix(toComplete, "-") {
			return filterPrefix(globalFlagCompletions, toComplete), compDirectiveNoFileComp
		}
		return filterPrefix(topLevelCommands, toComplete), compDirectiveNoFileComp
	}

	// `ws help [noun [verb…]]` completes the command words themselves.
	if cleaned[0] == "help" {
		if strings.HasPrefix(toComplete, "-") {
			return filterPrefix(globalFlagCompletions, toComplete), compDirectiveNoFileComp
		}
		return filterPrefix(nextWords(cleaned[1:]), toComplete), compDirectiveNoFileComp
	}

	cmd, positional, ok := matchCommand(cleaned)
	if !ok {
		// A noun (or sub-noun) without its verb yet: offer the verbs.
		if len(nextWords(cleaned)) == 0 {
			return nil, compDirectiveNoFileComp // unknown command
		}
		if strings.HasPrefix(toComplete, "-") {
			return filterPrefix(globalFlagCompletions, toComplete), compDirectiveNoFileComp
		}
		return filterPrefix(nextWords(cleaned), toComplete), compDirectiveNoFileComp
	}

	// Flag completion.
	if strings.HasPrefix(toComplete, "-") {
		flagComps := commandFlags(cmd)
		flagComps = append(flagComps, globalFlagCompletions...)
		return filterPrefix(dedupe(flagComps), toComplete), compDirectiveNoFileComp
	}

	// A sub-noun's own verbs (`repo root` and then a verb) are handled above;
	// here the command is complete and only its targets remain.
	if res, ok := resolvers[cmd.name()]; ok {
		ctx := loadCompletionCtx(globals)
		return res(positional, toComplete, ctx)
	}
	return nil, compDirectiveNoFileComp
}

// ── Dynamic resolvers ──────────────────────────────────────────────

// notGiven drops the candidates the user already typed.
func notGiven(candidates, given []string) []string {
	var out []string
	for _, c := range candidates {
		if !slices.Contains(given, c) {
			out = append(out, c)
		}
	}
	return out
}

func repoTargets(args []string, toComplete string, ctx completionCtx) ([]string, int) {
	return filterPrefix(notGiven(ctx.repoPaths, args), toComplete), compDirectiveNoFileComp
}

func rootTargets(args []string, toComplete string, ctx completionCtx) ([]string, int) {
	return filterPrefix(notGiven(ctx.repoRoots, args), toComplete), compDirectiveNoFileComp
}

// fileTargets lets the shell complete file names.
func fileTargets([]string, string, completionCtx) ([]string, int) { return nil, compDirectiveDefault }

// dirTargets lets the shell complete directory names.
func dirTargets([]string, string, completionCtx) ([]string, int) {
	return nil, compDirectiveFilterDirs
}

func dotfileTargets(args []string, toComplete string, ctx completionCtx) ([]string, int) {
	return filterPrefix(notGiven(ctx.dotfiles, args), toComplete), compDirectiveNoFileComp
}

func scratchNames(args []string, toComplete string, ctx completionCtx) ([]string, int) {
	return filterPrefix(notGiven(ctx.scratchIDs, args), toComplete), compDirectiveNoFileComp
}

// scratchFirst completes only the name of `scratch tag <name> [tag…]`.
func scratchFirst(args []string, toComplete string, ctx completionCtx) ([]string, int) {
	if len(args) == 0 {
		return filterPrefix(ctx.scratchIDs, toComplete), compDirectiveNoFileComp
	}
	return nil, compDirectiveNoFileComp
}

func sessionTags(args []string, toComplete string, ctx completionCtx) ([]string, int) {
	return filterPrefix(notGiven(ctx.logTags, args), toComplete), compDirectiveNoFileComp
}

func trashTargets(args []string, toComplete string, ctx completionCtx) ([]string, int) {
	return filterPrefix(notGiven(ctx.trashItems, args), toComplete), compDirectiveNoFileComp
}

// cronTargets completes job and preset names; cron run takes jobs only.
func cronTargets(args []string, toComplete string, ctx completionCtx) ([]string, int) {
	return filterPrefix(notGiven(cronJobNames(), args), toComplete), compDirectiveNoFileComp
}

// cronJobNames returns the built-in job and preset names (never the retired
// or renamed ones, which are hidden aliases).
func cronJobNames() []string {
	return []string{
		"dotfile-backup",
		"maintenance",
		"mega-sync",
		"pass-backup",
		"session-prune",
		"sync",
	}
}

// ── Flag-value completions ─────────────────────────────────────────

// commandFlags returns the flag names a command documents. Completion reads
// the same table as help, so a flag is offered exactly when help lists it.
func commandFlags(c command) []string {
	var out []string
	for _, f := range c.Flags {
		out = append(out, f.Name)
	}
	if c.Fields != nil {
		for _, f := range listFlagDocs {
			out = append(out, f.Name)
		}
	}
	if !c.RO {
		out = append(out, "--dry-run")
	}
	return out
}

// ── Context loader ─────────────────────────────────────────────────

// loadCompletionCtx performs best-effort loading of workspace state.
// Failures are silent — tab completion must never produce error output.
func loadCompletionCtx(globals globalFlags) completionCtx {
	var ctx completionCtx

	// Same (side-effect-free) resolution as every command, so completions
	// match what the command will see.
	resolved, resolvedConfig, _, err := resolveWorkspacePaths(globals)
	if err != nil {
		return ctx
	}
	ctx.workspace = resolved

	// Load config for scratch.root_dir, repo roots, and dotfile links.
	cfg, cfgErr := wsconfig.Load(resolvedConfig)
	if cfgErr == nil {
		if dir, err := wsconfig.ResolvePath(resolved, cfg.Scratch.RootDir); err == nil {
			ctx.scratchDir = dir
		}
	}
	if ctx.scratchDir == "" {
		if home, err := wsconfig.ExpandUserPath("~/Scratch"); err == nil {
			ctx.scratchDir = home
		}
	}

	// Load dotfile links from ws.json for completions.
	if cfgErr == nil {
		for _, link := range cfg.Links {
			ctx.dotfiles = append(ctx.dotfiles, link.System)
		}
	}

	// Discovery roots, as `repo root ls` prints them.
	if cfgErr == nil {
		roots := cfg.Repo.Roots
		if len(roots) == 0 {
			roots = []string{"."}
		}
		for _, r := range roots {
			if res, err := wsconfig.ResolvePath(resolved, r); err == nil {
				ctx.repoRoots = append(ctx.repoRoots, res)
			}
		}
	}

	// Trashed item names.
	if items, err := trash.Items(""); err == nil {
		for _, it := range items {
			ctx.trashItems = append(ctx.trashItems, it.Name)
		}
	}

	// Session tags: directory names under data/ws-log/.
	ctx.logDir = filepath.Join(resolved, "data", "ws-log")
	ctx.logTags = listDirNames(ctx.logDir)

	// Scratch directory names.
	ctx.scratchIDs = listDirNames(ctx.scratchDir)

	// Discovered repo paths.
	if cfgErr == nil {
		roots := make([]string, 0, len(cfg.Repo.Roots))
		for _, r := range cfg.Repo.Roots {
			if res, err := wsconfig.ResolvePath(resolved, r); err == nil {
				roots = append(roots, res)
			}
		}
		// Absolute paths, exactly as the repo commands list them (spec
		// "Path Rules").
		if repos, err := repo.Discover(resolved, roots, cfg.Repo.ExcludeDirs); err == nil {
			for _, r := range repos {
				ctx.repoPaths = append(ctx.repoPaths, r.Path)
			}
		}
	}

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

// stringFlags are the flags that take a value: the global ones, and every flag
// the command table documents with an argument.
var stringFlags = func() map[string]bool {
	m := map[string]bool{"workspace": true, "w": true, "config": true, "c": true}
	for _, c := range commands {
		for _, f := range c.Flags {
			if f.Arg != "" {
				m[strings.TrimLeft(f.Name, "-")] = true
				if f.Short != "" {
					m[strings.TrimLeft(f.Short, "-")] = true
				}
			}
		}
	}
	for _, f := range listFlagDocs {
		if f.Arg != "" {
			m[strings.TrimLeft(f.Name, "-")] = true
		}
	}
	return m
}()

// isStringFlag returns true for flags known to take a value argument.
func isStringFlag(flag string) bool {
	return stringFlags[strings.TrimLeft(flag, "-")]
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
