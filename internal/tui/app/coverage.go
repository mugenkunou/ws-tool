package app

import (
	"charm.land/bubbles/v2/key"

	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
)

// Reason says how the TUI covers a CLI subcommand that it does not reach with
// a key or a `.` menu command.
type Reason int

const (
	// Reached: a key or a `.` menu command runs it.
	Reached Reason = iota
	// List: the screen's list is the command (ls, scan, status): the same
	// records, fields, filter and sort.
	List
	// Elsewhere: the TUI itself covers it outside a screen (the setup screen,
	// ? help, `ws tui`). Note says where.
	Elsewhere
	// Plumbing: another program calls it (git).
	Plumbing
	// Shell: its output is for a shell rc file or for scripts to eval.
	Shell
	// CLIOnly: deliberately left to the CLI. Note says why.
	CLIOnly
)

// Coverage records how the TUI covers one CLI subcommand. cmd's
// TestCLICoverage fails when a CLI subcommand has no entry here, so adding a
// command forces a TUI decision; TestCoverageKeysExist and
// TestCoverageCommandsExist fail when an entry names a key or `.` menu command
// its screen does not offer, and TestCoverageReasons when an entry with neither
// gives no reason. See HANDBOOK.md §6.9 and §9.10.
type Coverage struct {
	CLI    string     // "symlink add"
	Screen nav.Screen // where it lives: the screen of its noun (nav.ForNoun)
	Key    key.Binding
	// Cmd is the `.` menu command (the CLI's words after the noun, as the
	// screen's Commands() names them, each with a Menu letter) that runs it. A
	// command may stand beside a key (both run one function), or alone.
	Cmd  string
	Why  Reason
	Note string
}

// CLICoverage is every CLI subcommand and its TUI counterpart.
var CLICoverage = func() []Coverage {
	rp, sc := keys.Repos(), keys.Scratch()
	df, ig, se, cr, tr := keys.Symlinks(), keys.Ignore(), keys.Secrets(), keys.Cron(), keys.Trash()
	sk := keys.Skills()
	shell := "shell integration; nothing to show in a TUI"
	git := "git plumbing, called by git itself"
	ws, sy, sec := nav.ScreenWorkspace, nav.ScreenSync, nav.ScreenSecrets
	pa, crt := nav.ScreenPass, nav.ScreenCredential
	return []Coverage{
		{CLI: "init", Screen: ws, Why: Elsewhere, Note: "the TUI shows its setup screen until the workspace is initialized"},
		{CLI: "version", Screen: ws, Why: List},
		{CLI: "layout", Screen: ws, Why: List},
		{CLI: "env", Screen: ws, Why: Shell, Note: "shell lines for scripts to eval"},
		{CLI: "help", Screen: ws, Why: Elsewhere, Note: "? opens full help"},
		{CLI: "tui", Screen: ws, Why: Elsewhere, Note: "starts this TUI"},
		{CLI: "config show", Screen: ws, Cmd: "config show"},
		{CLI: "config edit", Screen: ws, Cmd: "config edit"},
		{CLI: "completion bash", Screen: ws, Why: Shell, Note: shell},
		{CLI: "completion zsh", Screen: ws, Why: Shell, Note: shell},
		{CLI: "completion fish", Screen: ws, Why: Shell, Note: shell},
		{CLI: "completion enable", Screen: ws, Cmd: "completion enable"},
		{CLI: "completion disable", Screen: ws, Cmd: "completion disable"},

		{CLI: "sync push", Screen: sy, Cmd: "push"},
		{CLI: "sync status", Screen: sy, Why: List},
		{CLI: "sync restore", Screen: sy, Why: CLIOnly, Note: "run once on a fresh machine, before the TUI has a workspace"},

		{CLI: "cron ls", Screen: nav.ScreenCron, Why: List},
		{CLI: "cron add", Screen: nav.ScreenCron, Key: cr.Add},
		{CLI: "cron rm", Screen: nav.ScreenCron, Key: cr.Remove},
		{CLI: "cron run", Screen: nav.ScreenCron, Key: cr.Run},
		{CLI: "cron log", Screen: nav.ScreenCron, Key: cr.Output},
		{CLI: "cron apply", Screen: nav.ScreenCron, Cmd: "apply"},

		{CLI: "symlink ls", Screen: nav.ScreenSymlinks, Why: List},
		{CLI: "symlink add", Screen: nav.ScreenSymlinks, Key: df.Add},
		{CLI: "symlink link", Screen: nav.ScreenSymlinks, Key: df.Link},
		{CLI: "symlink unlink", Screen: nav.ScreenSymlinks, Key: df.Unlink, Cmd: "unlink --all"},

		{CLI: "skill scan", Screen: nav.ScreenSkills, Why: List}, // every skill and the tools that have it
		{CLI: "skill install", Screen: nav.ScreenSkills, Key: sk.Install},
		{CLI: "skill uninstall", Screen: nav.ScreenSkills, Key: sk.Uninstall},
		{CLI: "skill show", Screen: nav.ScreenSkills, Why: List, Note: "the list's shelf, note, tools and path columns; --path is for scripts"},
		{CLI: "skill eject", Screen: nav.ScreenSkills, Why: CLIOnly, Note: "a one-off that hands a built-in over to you"},
		{CLI: "skill check", Screen: nav.ScreenSkills, Why: CLIOnly, Note: "a report for a terminal or CI"},

		{CLI: "ignore ls", Screen: nav.ScreenIgnore, Key: ig.NextView}, // the Muted and Ignored views
		{CLI: "ignore lint", Screen: nav.ScreenIgnore, Why: List},      // the Warnings view, the default
		{CLI: "ignore add", Screen: nav.ScreenIgnore, Key: ig.Ignore},  // Warnings, Muted
		{CLI: "ignore rm", Screen: nav.ScreenIgnore, Key: ig.StopIgnoring},
		{CLI: "ignore mute", Screen: nav.ScreenIgnore, Key: ig.Mute},
		{CLI: "ignore unmute", Screen: nav.ScreenIgnore, Key: ig.Unmute},
		{CLI: "ignore check", Screen: nav.ScreenIgnore, Cmd: "check"},
		{CLI: "ignore tree", Screen: nav.ScreenIgnore, Cmd: "tree"}, // paged
		{CLI: "ignore edit", Screen: nav.ScreenIgnore, Key: ig.EditRules},

		// The screen is agent-tmux's: its list is `ls`, and its keys run open,
		// show, create, grep, kill and rm.
		{CLI: "session", Screen: nav.ScreenSessions, Why: List},

		{CLI: "repo ls", Screen: nav.ScreenRepos, Why: List},
		{CLI: "repo show", Screen: nav.ScreenRepos, Key: rp.Open},
		{CLI: "repo git", Screen: nav.ScreenRepos, Key: rp.Git},
		{CLI: "repo run", Screen: nav.ScreenRepos, Key: rp.Git}, // ! with `git ` erased
		{CLI: "repo configure", Screen: nav.ScreenRepos, Key: rp.Configure},
		{CLI: "repo root add", Screen: nav.ScreenRepos, Cmd: "root add"},
		{CLI: "repo root ls", Screen: nav.ScreenRepos, Cmd: "root ls"}, // a read-only view
		{CLI: "repo root rm", Screen: nav.ScreenRepos, Cmd: "root rm"}, // through the checklist

		{CLI: "scratch ls", Screen: nav.ScreenScratch, Why: List},
		{CLI: "scratch new", Screen: nav.ScreenScratch, Key: sc.New},
		{CLI: "scratch add", Screen: nav.ScreenScratch, Cmd: "add"},
		{CLI: "scratch open", Screen: nav.ScreenScratch, Key: sc.Open},
		{CLI: "scratch tag", Screen: nav.ScreenScratch, Key: sc.Tag},
		{CLI: "scratch rm", Screen: nav.ScreenScratch, Key: sc.Delete},

		{CLI: "secret ls", Screen: sec, Why: List}, // --muted: the quiet findings
		{CLI: "secret show", Screen: sec, Key: se.View},
		{CLI: "secret allow", Screen: sec, Key: se.Allow},
		{CLI: "secret mute", Screen: sec, Key: se.Mute},    // toggles mute/unmute on the selected rows
		{CLI: "secret unmute", Screen: sec, Key: se.Mute},  // same key: a muted row unmutes
		{CLI: "secret skip", Screen: sec, Cmd: "skip"},     // checklist
		{CLI: "secret unskip", Screen: sec, Cmd: "unskip"}, // checklist

		// pass and credential have their own screens (three flat tabs, one per
		// CLI noun).
		{CLI: "pass init", Screen: pa, Cmd: "init"}, // hands over to the CLI wizard
		{CLI: "pass status", Screen: pa, Why: List}, // the screen's header shows the health
		{CLI: "pass audit", Screen: pa, Cmd: "audit"},
		{CLI: "pass backup enable", Screen: pa, Cmd: "backup enable"}, // hands over
		{CLI: "pass backup disable", Screen: pa, Cmd: "backup disable"},
		{CLI: "pass backup status", Screen: pa, Cmd: "backup status"},
		{CLI: "pass backup push", Screen: pa, Cmd: "backup push"},
		{CLI: "credential enable", Screen: crt, Cmd: "enable"}, // hands over
		{CLI: "credential disable", Screen: crt, Cmd: "disable"},
		{CLI: "credential status", Screen: crt, Cmd: "status"}, // paged
		{CLI: "git-credential-helper get", Screen: crt, Why: Plumbing, Note: git},
		{CLI: "git-credential-helper store", Screen: crt, Why: Plumbing, Note: git},
		{CLI: "git-credential-helper erase", Screen: crt, Why: Plumbing, Note: git},

		{CLI: "trash ls", Screen: nav.ScreenTrash, Why: List},
		{CLI: "trash restore", Screen: nav.ScreenTrash, Key: tr.Restore},
		{CLI: "trash rm", Screen: nav.ScreenTrash, Key: tr.Delete},
		{CLI: "trash empty", Screen: nav.ScreenTrash, Cmd: "empty"},
		{CLI: "trash prune", Screen: nav.ScreenTrash, Key: tr.Prune},
		{CLI: "trash enable", Screen: nav.ScreenTrash, Cmd: "enable"},
		{CLI: "trash disable", Screen: nav.ScreenTrash, Cmd: "disable"},
		{CLI: "trash status", Screen: nav.ScreenTrash, Why: List}, // the header shows the integrations and size,
	}
}()
