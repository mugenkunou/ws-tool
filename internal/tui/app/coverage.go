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
	return []Coverage{
		{"init", nav.ScreenDashboard, none, "the TUI shows its setup screen (i) until the workspace is initialized"},
		{"reset", nav.ScreenDashboard, d.Reset, ""},
		{"restore", nav.ScreenDashboard, d.Restore, ""},
		{"version", nav.ScreenDashboard, none, "prints the build version"},
		{"help", nav.ScreenDashboard, none, "the TUI has ? help"},
		{"tui", nav.ScreenDashboard, none, "starts this TUI"},

		{"completions bash", nav.ScreenDashboard, none, shell},
		{"completions zsh", nav.ScreenDashboard, none, shell},
		{"completions fish", nav.ScreenDashboard, none, shell},
		{"completions install", nav.ScreenDashboard, none, shell},
		{"completions uninstall", nav.ScreenDashboard, none, shell},
		{"config view", nav.ScreenDashboard, d.Config, ""},
		{"config defaults", nav.ScreenDashboard, none, "prints built-in defaults for scripting"},

		{"cron add", nav.ScreenCron, cr.Add, ""},
		{"cron rm", nav.ScreenCron, cr.Remove, ""},
		{"cron remove", nav.ScreenCron, cr.Remove, ""},
		{"cron run", nav.ScreenCron, none, "entry point for crontab lines, not for people"},
		{"cron apply", nav.ScreenCron, none, "installs configs/crontab wholesale; per-job add/remove covers it"},
		{"cron ls", nav.ScreenCron, none, ""},
		{"cron list", nav.ScreenCron, none, ""},
		{"cron status", nav.ScreenCron, none, ""},
		{"cron log", nav.ScreenCron, cr.Log, ""},

		{"dotfile add", nav.ScreenDotfiles, df.Add, ""},
		{"dotfile link", nav.ScreenDotfiles, none, "links an existing workspace file; rare, needs two paths"},
		{"dotfile rm", nav.ScreenDotfiles, df.Remove, ""},
		{"dotfile ls", nav.ScreenDotfiles, none, ""},
		{"dotfile scan", nav.ScreenDotfiles, none, ""},
		{"dotfile fix", nav.ScreenDotfiles, df.Fix, ""},
		{"dotfile sync", nav.ScreenDotfiles, df.Sync, ""},
		{"dotfile reset", nav.ScreenDotfiles, df.Reset, ""},
		{"dotfile migrate", nav.ScreenDotfiles, df.Migrate, ""},
		{"dotfile git", nav.ScreenDotfiles, df.GitView, ""},

		{"ignore check", nav.ScreenIgnore, ig.Check, ""},
		{"ignore ls", nav.ScreenIgnore, ig.List, ""},
		{"ignore tree", nav.ScreenIgnore, ig.Tree, ""},
		{"ignore scan", nav.ScreenIgnore, none, ""},
		{"ignore fix", nav.ScreenIgnore, ig.Exclude, ""},
		{"ignore edit", nav.ScreenIgnore, ig.EditRules, ""},

		{"log start", nav.ScreenLogs, lg.Start, ""},
		{"log stop", nav.ScreenLogs, lg.Stop, ""},
		{"log ls", nav.ScreenLogs, none, ""},
		{"log prune", nav.ScreenLogs, lg.Prune, ""},
		{"log rm", nav.ScreenLogs, lg.Delete, ""},

		{"repo ls", nav.ScreenRepos, none, ""},
		{"repo scan", nav.ScreenRepos, none, ""},
		{"repo fetch", nav.ScreenRepos, rp.Fetch, ""},
		{"repo pull", nav.ScreenRepos, rp.Pull, ""},
		{"repo sync", nav.ScreenRepos, rp.Sync, ""},
		{"repo run", nav.ScreenRepos, rp.Run, ""},
		{"repo add-root", nav.ScreenRepos, rp.AddRoot, ""},
		{"repo ls-roots", nav.ScreenRepos, rp.AddRoot, ""}, // the add-root prompt lists the roots

		{"scratch new", nav.ScreenScratch, sc.New, ""},
		{"scratch open", nav.ScreenScratch, sc.Open, ""},
		{"scratch ls", nav.ScreenScratch, none, ""},
		{"scratch tag", nav.ScreenScratch, sc.Tag, ""},
		{"scratch search", nav.ScreenScratch, sc.Search, ""},
		{"scratch rm", nav.ScreenScratch, sc.Delete, ""},

		{"secret scan", nav.ScreenSecrets, none, ""},
		{"secret fix", nav.ScreenSecrets, se.Allowlist, ""},
		{"secret setup", nav.ScreenSecrets, se.Setup, ""},
		{"secret status", nav.ScreenSecrets, none, ""},
		{"secret git", nav.ScreenSecrets, se.PassLog, ""},

		{"git-credential-helper setup", nav.ScreenSecrets, se.CredSetup, ""},
		{"git-credential-helper status", nav.ScreenSecrets, se.CredStatus, ""},
		{"git-credential-helper disconnect", nav.ScreenSecrets, se.CredDisconnect, ""},
		{"git-credential-helper get", nav.ScreenSecrets, none, plumbing},
		{"git-credential-helper store", nav.ScreenSecrets, none, plumbing},
		{"git-credential-helper erase", nav.ScreenSecrets, none, plumbing},

		{"trash enable", nav.ScreenTrash, tr.Enable, ""},
		{"trash setup", nav.ScreenTrash, tr.Enable, ""},
		{"trash disable", nav.ScreenTrash, tr.Disable, ""},
		{"trash ls", nav.ScreenTrash, none, ""},
		{"trash empty", nav.ScreenTrash, tr.Empty, ""},
		{"trash purge", nav.ScreenTrash, tr.Purge, ""},
		{"trash rm", nav.ScreenTrash, tr.DeleteItem, ""},
		{"trash status", nav.ScreenTrash, none, ""},
	}
}()
