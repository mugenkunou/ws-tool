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
	d, rp, sc, lg := keys.Dashboard(), keys.Repos(), keys.Scratch(), keys.Logs()
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
		{"config show", nav.ScreenDashboard, d.Config, ""},
		{"config edit", nav.ScreenDashboard, none, pending},

		{"cron ls", nav.ScreenCron, none, ""},
		{"cron add", nav.ScreenCron, cr.Add, ""},
		{"cron rm", nav.ScreenCron, cr.Remove, ""},
		{"cron run", nav.ScreenCron, none, pending},
		{"cron log", nav.ScreenCron, cr.Log, ""},
		{"cron apply", nav.ScreenCron, none, pending},

		{"dotfile ls", nav.ScreenDotfiles, none, ""},
		{"dotfile add", nav.ScreenDotfiles, df.Add, ""},
		{"dotfile link", nav.ScreenDotfiles, df.Fix, ""},
		{"dotfile unlink", nav.ScreenDotfiles, df.Remove, ""},
		{"dotfile edit", nav.ScreenDotfiles, none, pending},
		{"dotfile backup enable", nav.ScreenDotfiles, none, pending},
		{"dotfile backup disable", nav.ScreenDotfiles, none, pending},
		{"dotfile backup status", nav.ScreenDotfiles, df.GitView, ""},
		{"dotfile backup push", nav.ScreenDotfiles, none, pending},

		{"ignore ls", nav.ScreenIgnore, ig.List, ""},
		{"ignore lint", nav.ScreenIgnore, none, ""},
		{"ignore add", nav.ScreenIgnore, ig.Exclude, ""},
		{"ignore rm", nav.ScreenIgnore, none, pending},
		{"ignore mute", nav.ScreenIgnore, none, pending},
		{"ignore unmute", nav.ScreenIgnore, none, pending},
		{"ignore check", nav.ScreenIgnore, ig.Check, ""},
		{"ignore tree", nav.ScreenIgnore, ig.Tree, ""},
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
		{"secret show", nav.ScreenSecrets, none, pending},
		{"secret allow", nav.ScreenSecrets, se.Allowlist, ""},
		{"secret mute", nav.ScreenSecrets, none, pending},
		{"secret unmute", nav.ScreenSecrets, none, pending},
		{"secret store", nav.ScreenSecrets, none, pending},

		{"pass init", nav.ScreenSecrets, se.Setup, ""},
		{"pass status", nav.ScreenSecrets, none, "the Secrets header shows it"},
		{"pass audit", nav.ScreenSecrets, none, pending},
		{"pass backup enable", nav.ScreenSecrets, none, pending},
		{"pass backup disable", nav.ScreenSecrets, none, pending},
		{"pass backup status", nav.ScreenSecrets, se.PassLog, ""},
		{"pass backup push", nav.ScreenSecrets, none, pending},

		{"credential enable", nav.ScreenSecrets, se.CredSetup, ""},
		{"credential disable", nav.ScreenSecrets, se.CredDisconnect, ""},
		{"credential status", nav.ScreenSecrets, se.CredStatus, ""},
		{"git-credential-helper get", nav.ScreenSecrets, none, plumbing},
		{"git-credential-helper store", nav.ScreenSecrets, none, plumbing},
		{"git-credential-helper erase", nav.ScreenSecrets, none, plumbing},

		{"trash ls", nav.ScreenTrash, none, ""},
		{"trash restore", nav.ScreenTrash, none, pending},
		{"trash rm", nav.ScreenTrash, tr.DeleteItem, ""},
		{"trash empty", nav.ScreenTrash, tr.Empty, ""},
		{"trash prune", nav.ScreenTrash, tr.Purge, ""},
		{"trash enable", nav.ScreenTrash, tr.Enable, ""},
		{"trash disable", nav.ScreenTrash, tr.Disable, ""},
		{"trash status", nav.ScreenTrash, none, ""},
	}
}()
