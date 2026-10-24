package app

import (
	"charm.land/bubbles/v2/key"

	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
)

// Coverage records how the TUI covers one CLI subcommand. cmd's
// TestCLICoverage fails when a CLI subcommand has no entry here, so adding a
// command forces a TUI decision; TestCoverageKeysExist fails when an entry
// names a key its screen does not offer. See HANDBOOK.md §7.5.
type Coverage struct {
	CLI    string     // "symlink add"
	Screen nav.Screen // where it lives in the TUI
	// Key runs it. Zero with no Note means the screen's list itself is the
	// coverage (ls, scan, status: the same records, fields, filter, sort).
	Key key.Binding
	// Note, when Key is zero, says why there is no key: covered elsewhere,
	// or deliberately CLI-only.
	Note string
	// Cmd is the `:` command (the CLI's words after the noun, as the screen's
	// Commands() names them) that runs it. Empty when there is none. A command
	// may stand beside a key (both run one function), or alone.
	Cmd string
}

// CLICoverage is every CLI subcommand and its TUI counterpart.
var CLICoverage = func() []Coverage {
	rp, sc, lg := keys.Repos(), keys.Scratch(), keys.Sessions()
	df, ig, se, cr, tr := keys.Symlinks(), keys.Ignore(), keys.Secrets(), keys.Cron(), keys.Trash()
	none := key.Binding{}
	shell := "shell integration; nothing to show in a TUI"
	plumbing := "git plumbing, called by git itself"
	return []Coverage{
		{"init", nav.ScreenDashboard, none, "the TUI shows its setup screen (i) until the workspace is initialized", ""},
		{"version", nav.ScreenDashboard, none, "prints the build version", ""},
		{"help", nav.ScreenDashboard, none, "the TUI has ? help", ""},
		{"tui", nav.ScreenDashboard, none, "starts this TUI", ""},

		{"completion bash", nav.ScreenDashboard, none, shell, ""},
		{"completion zsh", nav.ScreenDashboard, none, shell, ""},
		{"completion fish", nav.ScreenDashboard, none, shell, ""},
		{"completion enable", nav.ScreenDashboard, none, shell, ""},
		{"completion disable", nav.ScreenDashboard, none, shell, ""},
		{"config show", nav.ScreenDashboard, none, ":config show in the command bar (paged)", ""},
		{"config edit", nav.ScreenDashboard, none, ":config edit in the command bar (hands over to your editor)", ""},

		{"cron ls", nav.ScreenCron, none, "", ""},
		{"cron add", nav.ScreenCron, cr.Add, "", ""},
		{"cron rm", nav.ScreenCron, cr.Remove, "", ""},
		{"cron run", nav.ScreenCron, cr.Run, "", ""},
		{"cron log", nav.ScreenCron, cr.Output, "", ""},
		{"cron apply", nav.ScreenCron, none, "", "apply"}, // :apply

		{"symlink ls", nav.ScreenSymlinks, none, "", ""},
		{"symlink add", nav.ScreenSymlinks, df.Add, "", "add"},
		{"symlink link", nav.ScreenSymlinks, df.Link, "", "link"},
		{"symlink unlink", nav.ScreenSymlinks, df.Unlink, "", "unlink --all"},

		{CLI: "skill scan", Screen: nav.ScreenDashboard, Note: "CLI-only for now; skills are linked from the shell"},
		{CLI: "skill install", Screen: nav.ScreenDashboard, Note: "CLI-only for now; skills are linked from the shell"},
		{CLI: "skill uninstall", Screen: nav.ScreenDashboard, Note: "CLI-only for now; skills are linked from the shell"},

		{"ignore ls", nav.ScreenIgnore, ig.NextView, "", ""},       // the Muted and Ignored views
		{"ignore lint", nav.ScreenIgnore, none, "", "lint"},        // the Warnings view; :lint <path>
		{"ignore add", nav.ScreenIgnore, ig.Ignore, "", ""},        // Warnings, Muted
		{"ignore rm", nav.ScreenIgnore, ig.StopIgnoring, "", "rm"}, // Ignored view; :rm <path>
		{"ignore mute", nav.ScreenIgnore, ig.Mute, "", ""},         // Warnings
		{"ignore unmute", nav.ScreenIgnore, ig.Unmute, "", ""},     // Muted view
		{"ignore check", nav.ScreenIgnore, none, "", "check"},      // :check <path>
		{"ignore tree", nav.ScreenIgnore, none, "", "tree"},        // :tree [dir], paged
		{"ignore edit", nav.ScreenIgnore, ig.EditRules, "", ""},

		{"session ls", nav.ScreenSessions, none, "", ""},
		{"session record", nav.ScreenSessions, lg.Record, "", ""},
		{"session stop", nav.ScreenSessions, none, "", "stop"},
		{"session show", nav.ScreenSessions, lg.View, "", ""},
		{"session grep", nav.ScreenSessions, none, "", "grep"},
		{"session rm", nav.ScreenSessions, lg.Delete, "", ""},

		{"repo ls", nav.ScreenRepos, none, "", ""},
		{"repo show", nav.ScreenRepos, rp.Open, "", ""},
		{"repo fetch", nav.ScreenRepos, rp.Fetch, "", ""}, // :ls --fetch is the same function
		{"repo pull", nav.ScreenRepos, rp.Pull, "", ""},
		{"repo push", nav.ScreenRepos, none, ":push in the command bar (the shown or marked repos, through the checklist)", ""},
		{"repo save", nav.ScreenRepos, rp.Save, "", ""},
		{"repo configure", nav.ScreenRepos, rp.Configure, "", ""},
		{"repo run", nav.ScreenRepos, rp.Run, "", ""},
		{"repo root add", nav.ScreenRepos, rp.AddRoot, "", ""},
		{"repo root ls", nav.ScreenRepos, none, ":root ls in the command bar (read-only view)", ""},
		{"repo root rm", nav.ScreenRepos, none, ":root rm [<dir>] in the command bar (through the checklist)", ""},

		{"scratch ls", nav.ScreenScratch, none, "", ""},
		{"scratch new", nav.ScreenScratch, sc.New, "", ""},
		{"scratch add", nav.ScreenScratch, none, "", "add"},
		{"scratch open", nav.ScreenScratch, sc.Open, "", ""},
		{"scratch tag", nav.ScreenScratch, sc.Tag, "", ""},
		{"scratch rm", nav.ScreenScratch, sc.Delete, "", ""},

		{"secret ls", nav.ScreenSecrets, none, "", ""},
		{CLI: "secret show", Screen: nav.ScreenSecrets, Key: se.View},
		{CLI: "secret allow", Screen: nav.ScreenSecrets, Key: se.Allow},
		{CLI: "secret mute", Screen: nav.ScreenSecrets, Key: se.Mute},
		{CLI: "secret unmute", Screen: nav.ScreenSecrets, Note: ":unmute <dir> in the command bar (checklist)"},
		{CLI: "secret store", Screen: nav.ScreenSecrets, Key: se.Store},

		{CLI: "pass init", Screen: nav.ScreenSecrets, Note: ":pass init in the command bar (hands over to the CLI wizard)"},
		{CLI: "pass status", Screen: nav.ScreenSecrets, Note: ":pass status in the command bar (a view); the header shows the health"},
		{CLI: "pass audit", Screen: nav.ScreenSecrets, Note: ":pass audit in the command bar (a view)"},
		{CLI: "pass backup enable", Screen: nav.ScreenSecrets, Note: ":pass backup enable in the command bar (hands over to the CLI wizard)"},
		{CLI: "pass backup disable", Screen: nav.ScreenSecrets, Note: ":pass backup disable in the command bar (checklist)"},
		{CLI: "pass backup status", Screen: nav.ScreenSecrets, Note: ":pass backup status in the command bar (a view)"},
		{CLI: "pass backup push", Screen: nav.ScreenSecrets, Note: ":pass backup push in the command bar (checklist)"},

		{CLI: "credential enable", Screen: nav.ScreenSecrets, Note: ":credential enable in the command bar (hands over to the CLI wizard)"},
		{CLI: "credential disable", Screen: nav.ScreenSecrets, Note: ":credential disable in the command bar (hands over to the CLI wizard)"},
		{CLI: "credential status", Screen: nav.ScreenSecrets, Note: ":credential status in the command bar (paged)"},
		{"git-credential-helper get", nav.ScreenSecrets, none, plumbing, ""},
		{"git-credential-helper store", nav.ScreenSecrets, none, plumbing, ""},
		{"git-credential-helper erase", nav.ScreenSecrets, none, plumbing, ""},

		{"trash ls", nav.ScreenTrash, none, "", ""},
		{"trash restore", nav.ScreenTrash, tr.Restore, "", ""},
		{"trash rm", nav.ScreenTrash, tr.Delete, "", ""},
		{"trash empty", nav.ScreenTrash, none, "", "empty"}, // :empty
		{"trash prune", nav.ScreenTrash, tr.Prune, "", ""},
		{"trash enable", nav.ScreenTrash, none, "", "enable"},   // :enable
		{"trash disable", nav.ScreenTrash, none, "", "disable"}, // :disable
		{"trash status", nav.ScreenTrash, none, "", "status"},   // :status
	}
}()
