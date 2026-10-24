package cmd

import (
	"slices"
	"strings"

	cronpkg "github.com/mugenkunou/ws-tool/internal/cron"
	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/repo"
	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/scratch"
	"github.com/mugenkunou/ws-tool/internal/secret"
	"github.com/mugenkunou/ws-tool/internal/session"
	"github.com/mugenkunou/ws-tool/internal/symlink"
	"github.com/mugenkunou/ws-tool/internal/trash"
)

// flagDoc documents one flag of a command. It drives help, and completion
// reads it too, so the two cannot drift (HANDBOOK.md §9.7).
type flagDoc struct {
	Name  string // "--rebase"
	Short string // "-m" (optional)
	Arg   string // "<msg>"; empty for a boolean flag
	Desc  string
}

// command is one line of spec §3.4. The table below is the single source for
// help, shell completion and the TUI coverage test.
type command struct {
	Path    []string // {"repo", "root", "add"}
	Args    string   // "<path…> [--to <ws-path>]" positional part of the usage line
	Summary string
	RO      bool
	Flags   []flagDoc
	// Examples are command lines `ws <cmd> --help` shows under "Examples:".
	Examples []string
	// Fields lists the field names --filter, --sort and --wide accept. It is
	// set for ls and lint, from the domain's resource schema.
	Fields func() []string
}

// name is the command as typed, without "ws": "repo root add".
func (c command) name() string { return strings.Join(c.Path, " ") }

// usage is the usage line: "ws repo root add <dir>".
func (c command) usage() string {
	u := "ws " + c.name()
	if c.Args != "" {
		u += " " + c.Args
	}
	return u
}

var (
	// repoFilters are the filter flags of every command that selects repos
	// (spec "Selecting repos").
	repoFilters = []flagDoc{
		{Name: "--path", Arg: "<dir>", Desc: "only repos at or under <dir> (whole path components)"},
		{Name: "--dirty", Desc: "only repos with uncommitted changes"},
		{Name: "--ahead", Desc: "only repos ahead of their upstream"},
		{Name: "--behind", Desc: "only repos behind their upstream"},
		{Name: "--detached", Desc: "only repos on a detached HEAD"},
	}
	strategyFlags = []flagDoc{
		{Name: "--rebase", Desc: "rebase diverged repos, without asking"},
		{Name: "--merge", Desc: "merge diverged repos, without asking"},
	}
	editorFlag  = flagDoc{Name: "--editor", Arg: "<cmd>", Desc: "editor for this run (default: ws.json editor, $VISUAL, $EDITOR, vim)"}
	remoteFlag  = flagDoc{Name: "--remote", Arg: "<url>", Desc: "the private remote"}
	passEntry   = flagDoc{Name: "--pass-entry", Arg: "<entry>", Desc: "the pass entry holding an API token for the visibility check"}
	commitsFlag = flagDoc{Short: "-n", Name: "-n", Arg: "<count>", Desc: "recent commits to show (default 20)"}
	messageFlag = func(def string) flagDoc {
		return flagDoc{Short: "-m", Name: "-m", Arg: "<message>", Desc: "the commit message (default: " + def + ")"}
	}
	olderThan    = flagDoc{Name: "--older-than", Arg: "<age>", Desc: "age such as 30d, 12h or 2w; a bare number means days"}
	displayFlag  = flagDoc{Name: "--display", Arg: "<d>", Desc: "X display written into the mega-sync lines (default $DISPLAY, else :1)"}
	allFlag      = func(d string) flagDoc { return flagDoc{Name: "--all", Desc: d} }
	noEditorOpen = flagDoc{Name: "--no-open", Desc: "do not start the editor"}
)

func fieldsOf[T any](s func() resource.Schema[T]) func() []string {
	return func() []string { return s().Keys() }
}

// commands is every command of spec §3.4, one row per line. Hidden aliases
// live in aliases.go and never appear here.
var commands = []command{
	{Path: []string{"repo", "ls"}, Args: "[repo…]", Summary: "repos with status and hygiene findings", RO: true,
		Flags: append([]flagDoc{
			{Name: "--fetch", Desc: "fetch each repo before reading its status"},
			{Name: "--check", Arg: "<id>", Desc: "run only this hygiene check (repeatable)"},
		}, repoFilters...),
		Fields: fieldsOf(func() resource.Schema[repo.Entry] { return repo.Schema(resource.CLIView()) })},
	{Path: []string{"repo", "show"}, Args: "<repo>", Summary: "one repo in full", RO: true},
	{Path: []string{"repo", "fetch"}, Args: "[repo…]", Summary: "git fetch", Flags: repoFilters},
	{Path: []string{"repo", "pull"}, Args: "[repo…]", Summary: "git pull; asks rebase or merge where diverged",
		Flags: append(slices.Clone(strategyFlags), repoFilters...)},
	{Path: []string{"repo", "push"}, Args: "[repo…]", Summary: "git push, nothing else", Flags: repoFilters},
	{Path: []string{"repo", "save"}, Args: "[repo…]", Summary: "stage, commit (timestamped), pull, push",
		Flags: append(append([]flagDoc{messageFlag("ws save <date time> (<host>)")}, strategyFlags...), repoFilters...)},
	{Path: []string{"repo", "configure"}, Args: "[repo…]", Summary: "ask for missing identity, upstream, default branch, credentials",
		Flags: repoFilters},
	{Path: []string{"repo", "run"}, Args: "[repo…] -- <cmd…>", Summary: "run a command in each repo", Flags: repoFilters},
	{Path: []string{"repo", "root", "add"}, Args: "<dir>", Summary: "add a discovery root"},
	{Path: []string{"repo", "root", "ls"}, Summary: "list discovery roots", RO: true},
	{Path: []string{"repo", "root", "rm"}, Args: "<dir>", Summary: "remove a discovery root"},

	{Path: []string{"symlink", "ls"}, Summary: "managed symlinks, problems, and what was found", RO: true,
		Fields:   fieldsOf(func() resource.Schema[symlink.Entry] { return symlink.Schema(resource.CLIView()) }),
		Examples: []string{"ws symlink ls", "ws symlink ls --filter status=found"}},
	{Path: []string{"symlink", "add"}, Args: "[<host-path> [<ws-path>]]", Summary: "take into the workspace; no path: a checklist",
		Flags: []flagDoc{
			{Name: "--dir", Desc: "take a folder whole, as one entry"},
			{Name: "--files", Desc: "take every file in a folder as its own entry"},
			{Name: "--app", Arg: "<name>", Desc: "add everything found for one app, without asking"},
		},
		Examples: []string{
			"ws symlink add                                a checklist of what was found",
			"ws symlink add ~/.gitconfig                   to symlinks/home/.gitconfig",
			"ws symlink add ~/Screenshots artifacts/shots  to artifacts/shots",
			"ws symlink add --app git",
			"ws symlink add ~/.password-store --dir",
		}},
	{Path: []string{"symlink", "link"}, Args: "[<host-path>…]", Summary: "make or repair links from the workspace copies",
		Examples: []string{"ws symlink link", "ws symlink link ~/.zshrc"}},
	{Path: []string{"symlink", "unlink"}, Args: "<host-path>… | --all", Summary: "put the real content back, stop managing it",
		Flags:    []flagDoc{allFlag("every entry")},
		Examples: []string{"ws symlink unlink ~/.gitconfig", "ws symlink unlink --all"}},

	{Path: []string{"scratch", "ls"}, Summary: "scratch directories", RO: true,
		Fields: fieldsOf(func() resource.Schema[scratch.Entry] { return scratch.Schema(resource.CLIView()) })},
	{Path: []string{"scratch", "new"}, Args: "[name]", Summary: "create one and open it",
		Flags: []flagDoc{{Name: "--no-date", Desc: "no .YYYY-MM suffix"}, noEditorOpen, editorFlag}},
	{Path: []string{"scratch", "add"}, Args: "<path…>", Summary: "move an existing path into scratch"},
	{Path: []string{"scratch", "open"}, Args: "<name>", Summary: "open one in the editor", RO: true,
		Flags: []flagDoc{editorFlag, {Name: "--print-path", Desc: "print only the resolved path"}}},
	{Path: []string{"scratch", "tag"}, Args: "<name> [tag…]", Summary: "label one (tags are for you to read)",
		Flags: []flagDoc{{Name: "--rm", Arg: "<tag>", Desc: "remove this tag (repeatable)"}}},
	{Path: []string{"scratch", "rm"}, Args: "<name…>", Summary: "soft-delete"},

	{Path: []string{"session", "ls"}, Summary: "recorded sessions", RO: true,
		Fields: fieldsOf(func() resource.Schema[session.Session] { return session.Schema(resource.CLIView()) })},
	{Path: []string{"session", "record"}, Args: "[tag]", Summary: "record a shell session",
		Flags: []flagDoc{{Name: "--no-prompt", Desc: "do not add the prompt indicator"}}},
	{Path: []string{"session", "stop"}, Summary: "stop the running recording"},
	{Path: []string{"session", "show"}, Args: "<tag>", Summary: "print a transcript", RO: true,
		Flags: []flagDoc{{Name: "--commands", Desc: "print only the commands"}}},
	{Path: []string{"session", "grep"}, Args: "<text>", Summary: "search transcripts", RO: true,
		Flags: []flagDoc{{Name: "--max", Arg: "<n>", Desc: "stop after n matches (0: no limit)"}}},
	{Path: []string{"session", "rm"}, Args: "<tag…>", Summary: "delete sessions, or those older than an age",
		Flags: []flagDoc{olderThan}},

	{Path: []string{"ignore", "ls"}, Summary: "every path that doesn't sync, and its rule; --muted: muted warnings", RO: true,
		Flags: []flagDoc{{Name: "--muted", Desc: "the muted warnings instead of the ignored paths"}},
		Fields: func() []string {
			// The ignored paths' fields, then the warning fields --muted adds.
			keys := ignore.IgnoredSchema(resource.CLIView(), "").Keys()
			for _, k := range ignore.Schema(resource.CLIView(), "").Keys() {
				if !slices.Contains(keys, k) {
					keys = append(keys, k)
				}
			}
			return keys
		}},
	{Path: []string{"ignore", "lint"}, Args: "[path…]", Summary: "check synced files and warn about the unhygienic ones", RO: true,
		Fields: fieldsOf(func() resource.Schema[ignore.Violation] { return ignore.Schema(resource.CLIView(), "") })},
	{Path: []string{"ignore", "add"}, Args: "<path…>", Summary: "stop syncing a path"},
	{Path: []string{"ignore", "rm"}, Args: "<path…>", Summary: "sync a path again"},
	{Path: []string{"ignore", "mute"}, Args: "<path…>", Summary: "hide a path's warnings (it keeps syncing)"},
	{Path: []string{"ignore", "unmute"}, Args: "<path…>", Summary: "show them again"},
	{Path: []string{"ignore", "check"}, Args: "<path>", Summary: "will this path sync, and which rule decides?", RO: true},
	{Path: []string{"ignore", "tree"}, Args: "[dir]", Summary: "browse the tree with sync status", RO: true,
		Flags: []flagDoc{{Name: "-L", Short: "-L", Arg: "<n>", Desc: "show n levels below <dir>"}}},
	{Path: []string{"ignore", "edit"}, Summary: "edit the rules in your editor", Flags: []flagDoc{editorFlag}},

	{Path: []string{"secret", "ls"}, Summary: "secret findings", RO: true,
		Flags:  []flagDoc{{Name: "--skip-dir", Arg: "<dir>", Desc: "also skip this directory, for this run only (repeatable)"}},
		Fields: fieldsOf(func() resource.Schema[secret.Violation] { return secret.Schema(resource.CLIView(), "") })},
	{Path: []string{"secret", "show"}, Args: "<file:line>", Summary: "a finding in context", RO: true},
	{Path: []string{"secret", "allow"}, Args: "<file:line…>", Summary: "mark findings as false positives"},
	{Path: []string{"secret", "mute"}, Args: "<dir…>", Summary: "stop scanning a directory"},
	{Path: []string{"secret", "unmute"}, Args: "<dir…>", Summary: "scan it again"},
	{Path: []string{"secret", "store"}, Args: "<file:line> [entry]", Summary: "copy the value into pass and allow the finding"},

	{Path: []string{"pass", "init"}, Summary: "create the pass store (GPG key, git)"},
	{Path: []string{"pass", "status"}, Summary: "store, git, remote and credential-helper health", RO: true},
	{Path: []string{"pass", "audit"}, Summary: "entries with problems", RO: true},
	{Path: []string{"pass", "backup", "enable"}, Summary: "set the private remote", Flags: []flagDoc{remoteFlag, passEntry}},
	{Path: []string{"pass", "backup", "disable"}, Summary: "remove the remote"},
	{Path: []string{"pass", "backup", "status"}, Summary: "remote, ahead/behind, recent commits", RO: true, Flags: []flagDoc{commitsFlag}},
	{Path: []string{"pass", "backup", "push"}, Summary: "push the store", Flags: []flagDoc{messageFlag("ws pass backup push")}},

	{Path: []string{"credential", "enable"}, Summary: "make git use ws (pass) for credentials"},
	{Path: []string{"credential", "disable"}, Summary: "stop it"},
	{Path: []string{"credential", "status"}, Summary: "helper config, pass entries, remote coverage", RO: true},

	{Path: []string{"cron", "ls"}, Summary: "jobs: schedule, installed, last run, exit, next run", RO: true,
		Fields: fieldsOf(func() resource.Schema[cronpkg.Job] { return cronpkg.Schema(resource.CLIView()) })},
	{Path: []string{"cron", "add"}, Args: "<job…>", Summary: "install jobs or presets", Flags: []flagDoc{displayFlag}},
	{Path: []string{"cron", "rm"}, Args: "<job…>", Summary: "uninstall them"},
	{Path: []string{"cron", "run"}, Args: "<job>", Summary: "run a job now (crontab lines add --if-due)",
		Flags: []flagDoc{{Name: "--if-due", Desc: "run only when the job is due"}, displayFlag}},
	{Path: []string{"cron", "log"}, Args: "[job…]", Summary: "recent output of jobs (all when none named)", RO: true,
		Flags: []flagDoc{{Name: "--lines", Arg: "<n>", Desc: "lines to show; 0 shows all (default 20)"}}},
	{Path: []string{"cron", "apply"}, Summary: "install configs/crontab into the live crontab"},

	{Path: []string{"trash", "ls"}, Summary: "trashed items", RO: true,
		Fields: fieldsOf(func() resource.Schema[trash.Item] { return trash.Schema(resource.CLIView()) })},
	{Path: []string{"trash", "restore"}, Args: "<item…>", Summary: "put items back where they were"},
	{Path: []string{"trash", "rm"}, Args: "<item…>", Summary: "delete items forever"},
	{Path: []string{"trash", "empty"}, Summary: "delete everything forever"},
	{Path: []string{"trash", "prune"}, Summary: "delete items trashed before then",
		Flags: []flagDoc{{Name: "--older-than", Arg: "<age>", Desc: "required: 30d, 12h or 2w; a bare number means days"}}},
	{Path: []string{"trash", "enable"}, Summary: "make shell rm, VS Code and the file explorer soft-delete",
		Flags: []flagDoc{
			{Name: "--no-shell-rm", Desc: "leave the shell integration out"},
			{Name: "--no-vscode", Desc: "leave VS Code out"},
			{Name: "--no-file-explorer", Desc: "leave the trash directories out"},
		}},
	{Path: []string{"trash", "disable"}, Summary: "undo that"},
	{Path: []string{"trash", "status"}, Summary: "integrations and trash size", RO: true},

	{Path: []string{"config", "show"}, Summary: "resolved config, or the built-in defaults", RO: true,
		Flags: []flagDoc{{Name: "--defaults", Desc: "print the built-in defaults, in stored form"}}},
	{Path: []string{"config", "edit"}, Summary: "edit ws.json in your editor", Flags: []flagDoc{editorFlag}},

	{Path: []string{"completion", "bash"}, Summary: "print the bash completion script", RO: true},
	{Path: []string{"completion", "zsh"}, Summary: "print the zsh completion script", RO: true},
	{Path: []string{"completion", "fish"}, Summary: "print the fish completion script", RO: true},
	{Path: []string{"completion", "enable"}, Summary: "load completion from your shell rc file",
		Flags: []flagDoc{{Name: "--shell", Arg: "<s>", Desc: "bash, zsh or fish (default: from $SHELL)"}}},
	{Path: []string{"completion", "disable"}, Summary: "remove it",
		Flags: []flagDoc{{Name: "--shell", Arg: "<s>", Desc: "bash, zsh or fish (default: from $SHELL)"}}},

	{Path: []string{"init"}, Summary: "create the workspace layout and ws.json"},
	{Path: []string{"tui"}, Summary: "the full-screen interface", RO: true},
	{Path: []string{"version"}, Summary: "version, schema and platform", RO: true,
		Flags: []flagDoc{{Name: "--short", Desc: "print only the version"}}},
	{Path: []string{"help"}, Args: "[noun [verb…]]", Summary: "help", RO: true},
}

// nounSummaries are the lines of `ws help`, in display order.
var nounSummaries = []struct{ Name, Summary string }{
	{"repo", "Git repositories under the repo roots"},
	{"symlink", "Files and folders kept in the workspace and linked back"},
	{"scratch", "Scratch directories"},
	{"session", "Recorded terminal sessions"},
	{"ignore", "MEGA sync rules and warnings"},
	{"secret", "Secret findings in the workspace"},
	{"pass", "The pass password store"},
	{"credential", "Git credential helper backed by pass"},
	{"cron", "Scheduled ws jobs"},
	{"trash", "Trashed files and soft-delete integrations"},
}

// workspaceSummaries are the commands without a noun, as `ws help` lists them.
var workspaceSummaries = []struct{ Name, Summary string }{
	{"init", "Create the workspace layout and ws.json"},
	{"config", "Show or edit ws.json"},
	{"completion", "Shell completion"},
	{"tui", "Interactive terminal UI"},
	{"version", "Print version information"},
	{"help", "Show help"},
}

// findCommand returns the command whose path equals words exactly.
func findCommand(words []string) (command, bool) {
	for _, c := range commands {
		if slices.Equal(c.Path, words) {
			return c, true
		}
	}
	return command{}, false
}

// matchCommand finds the longest command path that is a prefix of words and
// returns it with the words after it.
func matchCommand(words []string) (command, []string, bool) {
	for n := len(words); n > 0; n-- {
		if c, ok := findCommand(words[:n]); ok {
			return c, words[n:], true
		}
	}
	return command{}, words, false
}

// nextWords lists the words that may follow prefix: the next path element of
// every command under it. With an empty prefix it is every first word.
func nextWords(prefix []string) []string {
	var out []string
	for _, c := range commands {
		if len(c.Path) <= len(prefix) || !slices.Equal(c.Path[:len(prefix)], prefix) {
			continue
		}
		w := c.Path[len(prefix)]
		if !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

// commandsUnder returns the commands whose path starts with prefix.
func commandsUnder(prefix []string) []command {
	var out []command
	for _, c := range commands {
		if len(c.Path) >= len(prefix) && slices.Equal(c.Path[:len(prefix)], prefix) {
			out = append(out, c)
		}
	}
	return out
}

// commandNames is every "noun verb…" the table holds, as the coverage test and
// the glossary test read it.
func commandNames() []string {
	out := make([]string, len(commands))
	for i, c := range commands {
		out[i] = c.name()
	}
	return out
}
