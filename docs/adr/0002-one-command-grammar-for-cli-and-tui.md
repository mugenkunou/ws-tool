---
status: accepted
date: 2026-10-22
---

# One command grammar for the CLI and the TUI

The CLI grew one subcommand at a time, and the TUI took its labels and keys from it. By
October 2026 the result showed on every screen:

- One TUI key meant up to five things. `x` was quick fix (Dashboard), run a command
  (Repos), fix links (Dotfiles), stop recording (Logs), and empty the trash (Trash). `p`
  was pull, git push, move to pass, or purge.
- 21 actions used shift+letter keys. Two of them took the list's movement keys: on
  Secrets `g` pushed the pass store instead of going to the top, and on Dotfiles `G`
  launched a wizard instead of going to the bottom.
- "sync" meant four things: git pull+push (`repo sync`), adopt the live file
  (`dotfile sync`), commit and push the dotfile repo (the `dotfile-sync` cron job), and
  MEGA's sync.
- Umbrella verbs hid what changes. `scan` and `fix` meant something different on every
  noun, and `fix` was an interactive loop with its own letters, which disagreed with the TUI.
  `ls` versus `scan` had no rule: `repo` and `dotfile` had both, while `ignore` and
  `secret` had only `scan`, and `ignore ls` listed something else.
- The same idea used different verbs and formats: `trash purge --older-than 30` took days,
  `log prune --older-than 30d` took a duration. Aliases existed in some groups only.
- Dead commands lived on: completion offered `reset` and `restore`, and the Dashboard's
  `O` handed over to `ws restore`, which phase-1 had removed ("unknown command").

An earlier pass tried to fix this by giving every noun the same verbs. It made things
worse. Generic verbs (`scan`, `fix`, `sync`) were forced onto domains where they meant
different things, so the uniformity was only on the surface.

## Decision

The CLI and the TUI share one grammar, written down in spec.md Part 3 and §6:

- **Shape:** `ws <noun> <verb> [targets…]`, with singular nouns. Each noun is one TUI
  screen, and `ws <noun> ls` prints exactly that screen's rows.
- **A closed verb glossary.** Each verb means one thing everywhere. Domain words win where
  the domain has them: git's `fetch`/`pull`, the trash's `restore`/`empty`, `link`/`unlink`
  for symlinks, `allow` for false positives, `mute` for warnings, `apply` for writing
  declared state into the live system. Where two copies can conflict, the verb names the
  direction: `dotfile add` keeps the live file, `dotfile link` keeps the workspace copy.
  This merges five dotfile verbs (add, link, fix, sync, rm) into three (add, link,
  unlink).
- **No umbrella verbs.** `scan`, `fix`, `reset` and `setup` are gone. Each command names what
  it changes. Finding problems is not an error, so exit code `2` is retired.
- **New nouns where an old one was overloaded:** `session` (was `log`, which also meant cron
  output and git history), and `pass` and `credential` (split out of `secret`).
- **Old names stay as hidden aliases.** They print a one-line rename hint when a person
  types them, and stay silent when crontab, git or a shell hook calls them.
- **TUI keys follow k9s:** global keys that never change meaning (`enter`, `esc`, `:`, `/`,
  `?`, `d` describe, `y` JSON, `e` edit, `a` add, `ctrl+d` delete through the checklist,
  `space`/`ctrl+space`/`ctrl+\`/`ctrl+a` marks, `ctrl+r` refresh), and lowercase screen keys
  named after their verbs (usually the first letter). No shift+letter actions. Rare actions and
  wizards go to a `:` command bar that takes the CLI's own words.
- **Layout never shifts.** The list status line (filter, sort, marks) is reserved at the
  bottom, and the command bar replaces the footer.

## Considered options

- **Fix only the TUI keys.** Rejected. The keys and labels come from the CLI verbs, so
  inconsistent verbs keep reappearing as inconsistent keys.
- **One generic verb set for every noun** (the earlier pass). Rejected, for the reason
  above.
- **A global `f` fix key with a picker, and `scan` as the problem-finding verb.** Rejected.
  Nobody needs a generic fix or scan. Named actions on marked rows are clearer, and `ls`
  already shows status.
- **Rename without aliases.** Rejected. Crontab lines (`ws cron run …`), git
  (`ws git-credential-helper get`), shell hooks and muscle memory would break.
- **k9s's shift+column sort.** Rejected. Column initials collide (System path, Status, Sudo,
  Size). `>` and `<` (as in htop) stay.
- **Keep `r` for refresh.** Rejected. Refresh moves to `ctrl+r` (browser reload), which
  frees `r` for run, record and restore.

## Consequences

- The JSON envelope goes to `schema: 3`. Command names follow the grammar, and `repo ls`,
  `ignore lint` and `secret ls` return the records the old `scan` commands returned.
- Built-in cron jobs: `dotfile-sync` becomes `dotfile-backup` and `log-prune` becomes
  `session-prune`, and the old names still run from existing crontab lines. `secret-scan`
  and `ignore-scan` are removed, because they existed only to turn exit code 2 into a
  notification.
- New commands fill gaps the old grammar hid: `trash restore`, `repo root rm`, `ignore rm`,
  `dotfile edit`, `scratch add`, `config edit`, `session show|grep`, `secret show`.
- `repo sync` is replaced by `repo push` (push only) and `repo save` (commit with a
  timestamped message, pull, push). ws never chooses rebase or merge for a diverged repo:
  it asks, or skips the repo when it can't ask. `repo configure` asks for what a repo's
  setup is missing. The `repo-sync` cron job is retired, because nothing commits work
  unattended.
- `ignore lint` reports sync warnings, and `ignore ls` lists what doesn't sync. `mute`
  only hides warnings and never changes what syncs.
- The editor is one setting (`ws.json` `editor`, then `$VISUAL`, `$EDITOR`, then `vim`).
  `ws.json` always holds every key.
- The TUI Logs screen becomes Sessions. Every screen's keys change (spec.md §6.5).
- Tests enforce the grammar: help and completion list only documented commands; every
  verb is in the glossary; no screen rebinds a global key or uses a shift+letter action;
  every CLI command has a TUI key or `:` command; marking a row moves no row.
- The work is planned in [docs/plans/0002-command-grammar.md](../plans/0002-command-grammar.md).
