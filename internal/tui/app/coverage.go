package app

import (
	"charm.land/bubbles/v2/key"

	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
)

// Coverage records how the TUI covers one CLI subcommand. cmd's
// TestCLICoverage fails when a CLI subcommand has no entry here, so adding a
// command forces a TUI decision; TestCoverageKeysExist fails when an entry
// names a key its screen does not offer. See docs/adr/0001-….
type Coverage struct {
	CLI    string     // "dotfile add"
	Screen nav.Screen // where it lives in the TUI
	// Key runs it. Zero with no Note means the screen's list itself is the
	// coverage (ls, scan, status: the same records, fields, filter, sort).
	Key key.Binding
	// Note, when Key is zero, says why there is no key: covered elsewhere,
	// or deliberately CLI-only.
	Note string
}

// CLICoverage is every CLI subcommand and its TUI counterpart.
var CLICoverage = func() []Coverage {
	rp, sc, lg := keys.Repos(), keys.Scratch(), keys.Logs()
	df, ig, se, cr, tr := keys.Dotfiles(), keys.Ignore(), keys.Secrets(), keys.Cron(), keys.Trash()
	none := key.Binding{}
	shell := "shell integration; nothing to show in a TUI"
	plumbing := "git plumbing, called by git itself"
	pending := "TUI home arrives with the new key grammar (plan 0002, phase D)"
	return []Coverage{
		{"init", nav.ScreenDashboard, none, "the TUI shows its setup screen (i) until the workspace is initialized"},
		{"version", nav.ScreenDashboard, none, "prints the build version"},
		{"help", nav.ScreenDashboard, none, "the TUI has ? help"},
		{"tui", nav.ScreenDashboard, none, "starts this TUI"},

		{"completion bash", nav.ScreenDashboard, none, shell},
		{"completion zsh", nav.ScreenDashboard, none, shell},
		{"completion fish", nav.ScreenDashboard, none, shell},
		{"completion enable", nav.ScreenDashboard, none, shell},
		{"completion disable", nav.ScreenDashboard, none, shell},
		{"config show", nav.ScreenDashboard, none, ":config show in the command bar (paged)"},
		{"config edit", nav.ScreenDashboard, none, ":config edit in the command bar (hands over to your editor)"},

		{"cron ls", nav.ScreenCron, none, ""},
		{"cron add", nav.ScreenCron, cr.Add, ""},
		{"cron rm", nav.ScreenCron, cr.Remove, ""},
		{"cron run", nav.ScreenCron, cr.Run, ""},
		{"cron log", nav.ScreenCron, cr.Output, ""},
		{"cron apply", nav.ScreenCron, none, ""}, // :apply

		{"dotfile ls", nav.ScreenDotfiles, none, ""},
		{"dotfile add", nav.ScreenDotfiles, df.Add, ""},
		{"dotfile link", nav.ScreenDotfiles, df.Fix, ""},
		{"dotfile unlink", nav.ScreenDotfiles, df.Remove, ""},
		{"dotfile edit", nav.ScreenDotfiles, none, pending},
		{"dotfile backup enable", nav.ScreenDotfiles, none, pending},
		{"dotfile backup disable", nav.ScreenDotfiles, none, pending},
		{"dotfile backup status", nav.ScreenDotfiles, df.GitView, ""},
		{"dotfile backup push", nav.ScreenDotfiles, none, pending},

		{"ignore ls", nav.ScreenIgnore, ig.NextView, ""},     // the Muted and Ignored views
		{"ignore lint", nav.ScreenIgnore, none, ""},          // the Warnings view; :lint <path>
		{"ignore add", nav.ScreenIgnore, ig.Ignore, ""},      // Warnings, Muted
		{"ignore rm", nav.ScreenIgnore, ig.StopIgnoring, ""}, // Ignored view; :rm <path>
		{"ignore mute", nav.ScreenIgnore, ig.Mute, ""},       // Warnings
		{"ignore unmute", nav.ScreenIgnore, ig.Unmute, ""},   // Muted view
		{"ignore check", nav.ScreenIgnore, none, ""},         // :check <path>
		{"ignore tree", nav.ScreenIgnore, none, ""},          // :tree [dir], paged
		{"ignore edit", nav.ScreenIgnore, ig.EditRules, ""},

		{"session ls", nav.ScreenSessions, none, ""},
		{"session record", nav.ScreenSessions, lg.Start, ""},
		{"session stop", nav.ScreenSessions, lg.Stop, ""},
		{"session show", nav.ScreenSessions, none, pending},
		{"session grep", nav.ScreenSessions, none, pending},
		{"session rm", nav.ScreenSessions, lg.Delete, ""},
		{"session prune", nav.ScreenSessions, lg.Prune, ""},

		{"repo ls", nav.ScreenRepos, none, ""},
		{"repo show", nav.ScreenRepos, none, "enter on a row opens the detail view"},
		{"repo fetch", nav.ScreenRepos, rp.Fetch, ""},
		{"repo pull", nav.ScreenRepos, rp.Pull, ""},
		{"repo push", nav.ScreenRepos, none, pending},
		{"repo save", nav.ScreenRepos, rp.Sync, ""},
		{"repo configure", nav.ScreenRepos, none, pending},
		{"repo run", nav.ScreenRepos, rp.Run, ""},
		{"repo root add", nav.ScreenRepos, rp.AddRoot, ""},
		{"repo root ls", nav.ScreenRepos, rp.AddRoot, ""}, // the add-root prompt lists the roots
		{"repo root rm", nav.ScreenRepos, none, pending},

		{"scratch ls", nav.ScreenScratch, none, ""},
		{"scratch new", nav.ScreenScratch, sc.New, ""},
		{"scratch add", nav.ScreenScratch, none, pending},
		{"scratch open", nav.ScreenScratch, sc.Open, ""},
		{"scratch tag", nav.ScreenScratch, sc.Tag, ""},
		{"scratch rm", nav.ScreenScratch, sc.Delete, ""},

		{"secret ls", nav.ScreenSecrets, none, ""},
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
		{"git-credential-helper get", nav.ScreenSecrets, none, plumbing},
		{"git-credential-helper store", nav.ScreenSecrets, none, plumbing},
		{"git-credential-helper erase", nav.ScreenSecrets, none, plumbing},

		{"trash ls", nav.ScreenTrash, none, ""},
		{"trash restore", nav.ScreenTrash, tr.Restore, ""},
		{"trash rm", nav.ScreenTrash, tr.Delete, ""},
		{"trash empty", nav.ScreenTrash, none, ""}, // :empty
		{"trash prune", nav.ScreenTrash, tr.Prune, ""},
		{"trash enable", nav.ScreenTrash, none, ""},  // :enable
		{"trash disable", nav.ScreenTrash, none, ""}, // :disable
		{"trash status", nav.ScreenTrash, none, ""},  // :status
	}
}()

// CoverageCmds names the `:` command that runs a CLI subcommand, for those
// that have one (spec §6.5). TestCoverageCommandsExist checks each is a command
// of its screen. (Kept beside Coverage rather than in it so entries stay
// four-field literals.)
var CoverageCmds = map[string]string{
	"cron apply": "apply",

	"ignore lint":  "lint",
	"ignore rm":    "rm",
	"ignore check": "check",
	"ignore tree":  "tree",

	"trash empty":   "empty",
	"trash enable":  "enable",
	"trash disable": "disable",
	"trash status":  "status",
}
