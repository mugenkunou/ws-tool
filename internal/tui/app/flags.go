package app

// FlagCoverage records how the TUI handles one flag of a CLI command: where it
// offers the flag (a filter, a `:` argument, a prompt, a checklist, a view), or
// why it does not. "CLI only: ..." marks a flag the TUI deliberately leaves out;
// "n/a: ..." one the TUI has no use for (it already does what the flag asks).
// cmd's TestFlagCoverage fails when a flag in the commands table has no entry
// here, so a new flag forces a TUI decision, and when an entry names a command
// or flag the CLI does not have. See CLICoverage for whole commands.
type FlagCoverage struct {
	CLI  string // "skill install"
	Flag string // "--providers", as flagDoc.Name in cmd/commands.go
	How  string // where the TUI offers it, or why it does not
}

// CLIFlagCoverage is one entry per flag of every CLI command that has flags.
var CLIFlagCoverage = []FlagCoverage{
	{CLI: "repo ls", Flag: "--check", How: "n/a: the list shows every check (the issues column; enter for the findings)"},
	{CLI: "repo ls", Flag: "--path", How: "the list filter on the path column (/ path=<dir>)"},
	{CLI: "repo ls", Flag: "--dirty", How: "the list filter on the dirty column (/ dirty!=0)"},
	{CLI: "repo ls", Flag: "--ahead", How: "the list filter on the ahead column (/ ahead!=0)"},
	{CLI: "repo ls", Flag: "--behind", How: "the list filter on the behind column (/ behind!=0)"},
	{CLI: "repo ls", Flag: "--detached", How: "the list filter on the detached column (/ detached=true)"},
	{CLI: "repo git", Flag: "--yes", How: "n/a: the TUI always shows a checklist before it runs"},
	{CLI: "repo git", Flag: "--path", How: "the list filter on the path column (/ path=<dir>)"},
	{CLI: "repo git", Flag: "--dirty", How: "the list filter on the dirty column (/ dirty!=0)"},
	{CLI: "repo git", Flag: "--ahead", How: "the list filter on the ahead column (/ ahead!=0)"},
	{CLI: "repo git", Flag: "--behind", How: "the list filter on the behind column (/ behind!=0)"},
	{CLI: "repo git", Flag: "--detached", How: "the list filter on the detached column (/ detached=true)"},
	{CLI: "repo run", Flag: "--yes", How: "n/a: the TUI always shows a checklist before it runs"},
	{CLI: "repo run", Flag: "--path", How: "the list filter on the path column (/ path=<dir>)"},
	{CLI: "repo run", Flag: "--dirty", How: "the list filter on the dirty column (/ dirty!=0)"},
	{CLI: "repo run", Flag: "--ahead", How: "the list filter on the ahead column (/ ahead!=0)"},
	{CLI: "repo run", Flag: "--behind", How: "the list filter on the behind column (/ behind!=0)"},
	{CLI: "repo run", Flag: "--detached", How: "the list filter on the detached column (/ detached=true)"},
	{CLI: "repo configure", Flag: "--path", How: "the list filter on the path column (/ path=<dir>)"},
	{CLI: "repo configure", Flag: "--dirty", How: "the list filter on the dirty column (/ dirty!=0)"},
	{CLI: "repo configure", Flag: "--ahead", How: "the list filter on the ahead column (/ ahead!=0)"},
	{CLI: "repo configure", Flag: "--behind", How: "the list filter on the behind column (/ behind!=0)"},
	{CLI: "repo configure", Flag: "--detached", How: "the list filter on the detached column (/ detached=true)"},
	{CLI: "symlink add", Flag: "--dir", How: "`:add <host> [<ws>] --dir`"},
	{CLI: "symlink add", Flag: "--files", How: "CLI only: the TUI opens a checklist of a folder's entries, not one entry per file"},
	{CLI: "symlink add", Flag: "--app", How: "the add checklist (a): one row per known app config, none added unasked"},
	{CLI: "symlink unlink", Flag: "--all", How: "`:unlink --all`"},
	{CLI: "skill scan", Flag: "--tool", How: "n/a: the list has a column per tool"},
	{CLI: "skill show", Flag: "--path", How: "CLI only: a script flag (prints a bare path)"},
	{CLI: "skill install", Flag: "--tool", How: "`:install <tool>`; the tool chooser asks without it"},
	{CLI: "skill install", Flag: "--dev", How: "CLI only: a developer flag for ws built from a checkout"},
	{CLI: "skill install", Flag: "--providers", How: "CLI only: copies tool-shipped skills into agent/skills/; the Skills screen lists them once present"},
	{CLI: "skill uninstall", Flag: "--tool", How: "`:uninstall <tool>`; the tool chooser asks without it"},
	{CLI: "scratch new", Flag: "--no-date", How: "CLI only: the TUI always adds the .YYYY-MM suffix"},
	{CLI: "scratch new", Flag: "--no-open", How: "CLI only: the TUI always opens the editor after creating"},
	{CLI: "scratch new", Flag: "--editor", How: "n/a: the TUI opens the editor set in ws.json"},
	{CLI: "scratch open", Flag: "--editor", How: "n/a: the TUI opens the editor set in ws.json"},
	{CLI: "scratch open", Flag: "--print-path", How: "CLI only: a script flag (prints a bare path)"},
	{CLI: "scratch tag", Flag: "--rm", How: "the tag prompt (t): `-tag` removes a tag"},
	{CLI: "ignore ls", Flag: "--muted", How: "the Muted view (v switches views)"},
	{CLI: "ignore unmute", Flag: "--stale", How: "the Muted view lists the stale entries as rows; mark them and unmute (u)"},
	{CLI: "ignore tree", Flag: "-L", How: "`:tree <dir> -L <n>`: the command passes its words to the CLI"},
	{CLI: "ignore edit", Flag: "--editor", How: "n/a: the TUI opens the editor set in ws.json"},
	{CLI: "secret ls", Flag: "--skip-dir", How: "CLI only: a one-run skip; the TUI's skip prompt saves the directory to secret.skip_dirs"},
	{CLI: "pass backup enable", Flag: "--remote", How: "`:pass backup enable`: hands over to the CLI, which asks"},
	{CLI: "pass backup enable", Flag: "--pass-entry", How: "`:pass backup enable`: hands over to the CLI, which asks"},
	{CLI: "pass backup status", Flag: "-n", How: "CLI only: `:pass backup status` shows the CLI's default of 20 commits"},
	{CLI: "pass backup push", Flag: "-m", How: "CLI only: `:pass backup push` uses the default commit message"},
	{CLI: "cron add", Flag: "--display", How: "n/a: the TUI uses the default display"},
	{CLI: "cron run", Flag: "--if-due", How: "CLI only: a scheduler flag; the TUI's run (r) always runs the job"},
	{CLI: "cron run", Flag: "--display", How: "n/a: the TUI uses the default display"},
	{CLI: "cron log", Flag: "--lines", How: "CLI only: the output view (enter) shows the last 200 lines"},
	{CLI: "trash prune", Flag: "--older-than", How: "the prune prompt (p) asks for the age"},
	{CLI: "trash enable", Flag: "--no-shell-rm", How: "CLI only: `:enable` turns on every integration; its note points to the CLI for a subset"},
	{CLI: "trash enable", Flag: "--no-vscode", How: "CLI only: `:enable` turns on every integration; its note points to the CLI for a subset"},
	{CLI: "trash enable", Flag: "--no-file-explorer", How: "CLI only: `:enable` turns on every integration; its note points to the CLI for a subset"},
	{CLI: "config show", Flag: "--defaults", How: "CLI only: `:config show` prints the resolved ws.json"},
	{CLI: "config edit", Flag: "--editor", How: "n/a: the TUI opens the editor set in ws.json"},
	{CLI: "completion enable", Flag: "--shell", How: "the Workspace screen: one row per shell; the command acts on the marked or cursor rows"},
	{CLI: "completion disable", Flag: "--shell", How: "the Workspace screen: one row per shell; the command acts on the marked or cursor rows"},
	{CLI: "version", Flag: "--short", How: "CLI only: a script flag (prints a bare version)"},
}
