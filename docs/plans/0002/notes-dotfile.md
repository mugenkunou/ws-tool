> **Read this first.** These notes were written on 2026-10-22, before the user's last round of
> decisions. Where they disagree with spec.md, **spec.md wins**. Use them for file:line
> locations and today's behaviour. Changed since:
>
> - The verbs are merged by direction. `relink` → `link` (without `--to`). `adopt` → `add` (on an overwritten entry). `link <path> <ws-path>` → `link <path> --to <ws-path>`. `add --to <ws-path>` is new.
> - **`--class` is removed** everywhere: the flag, `links[].class`, and the `ls` column. Decisions in these notes that keep or default a class are void. Secret detection still picks `secrets/dotfiles/` over `configs/dotfiles/`.
> - `link --to` onto an existing real file renames it to `.ws-backup` instead of refusing.
> - TUI: there's no `o`. `a` takes the live files of the marked overwritten rows, or opens a prompt pre-filled with the cursor row's path when that row is overwritten. `l` = link.
> - `dotfile edit` uses the shared editor rule (`ws.json` `editor` → `$VISUAL` → `$EDITOR` → `vim`).
> - spec §7.4 now matches the code: an undetermined visibility warns and continues. Bugs B6 and B8 still need fixing.

# Implementation notes: `ws dotfile` (including `dotfile backup`)

Spec: spec.md §5 (repo root). Line numbers are as of commit b77d3c8 plus the
working tree on 2026-10-22. Tests run in Docker only (dev.md; host `go test` once deleted
real trash state). Every test that adopts or unlinks must use `testSetXDG` so the trash and
state dirs are temporary.

## 1. Decisions (each is marked `(decision)` in the spec)

1. **TUI + sudo.** In the TUI, an action on a `sudo: true` entry is handed to the CLI in
   the terminal (`handover.Run(handover.Pause(handover.WS(..., "dotfile", "<verb>", path)))`).
   Today `sudoRun` (internal/dotfile/sudo.go:59) attaches `os.Stdin` while Bubble Tea owns
   the terminal, so a sudo password prompt garbles the screen.
2. **`ls` columns.** `workspace_path` (absolute) replaces `original` (workspace-relative)
   in the default columns, so printed paths follow §4.5. `original` stays a field, in
   `--wide` and `--json`.
3. **`add` refuses** a path that is itself a symlink, and a path inside the workspace
   (the hint is `ws dotfile link`).
4. **TUI `:add <path…> [--class] [--dir]`**, because the `a` prompt cannot pass flags.
   §6.5's Dotfiles `:` list does not have it yet.
5. **`link <ws-path>`** follows the §4.5 input rule (cwd-relative) and must lie inside the
   workspace. Today it is joined to the workspace root (cmd/dotfile.go:1014,
   internal/dotfile/dotfile.go:709).
6. **`link` class default** is `secret` when `<ws-path>` is under `<workspace>/secrets/`,
   else `config`. Today it is always `config` (dotfile.go:716-719).
7. **`unlink` backs up** whatever replaced the link to `.ws-backup` instead of deleting it.
8. **`unlink` copies shared originals** (made by `link`, or pointed at by two entries)
   instead of moving them out of the workspace.
9. **`relink` reports missing copies** as failed result lines, not as plan actions.
10. **`adopt` trashes** the old workspace copy (`trash.SoftDelete`) instead of `os.RemoveAll`.
11. **`adopt` keeps `original` and `class`** instead of re-detecting the class.
12. **`adopt` refuses** an `ok` or missing system path, a foreign symlink, and directories.
13. **`edit` on a directory entry** passes the directory to the editor.
14. **`backup enable` needs a remote.** There is no local-only mode, and a blank answer
    exits 1. Today `git setup` accepts a blank remote and exits 0
    (cmd/ignore_dotfile_restore_test.go:54 relies on that).
15. **`backup enable` does not commit or push.** It prints hints for `backup push` and
    `cron add dotfile-backup`.
16. **`backup enable --pass-entry <entry>`** stores the entry name in `dotfile.git.pass_entry`.
    The old `git remote --pass-entry` was parsed and dropped (cmd/dotfile.go:473-476).
17. **`backup disable`** clears `remote_url`, removes the repo's `origin`, leaves the
    `auto_*` keys alone, and removes the `dotfile-backup` cron job if it is installed.
    Today `git disconnect` (cmd/dotfile.go:797) only sets both `auto_*` to false.
18. **`backup status -n <count>`**, default 20, carries over the old `git log -n`.
19. **Auto-push checks visibility** like `backup push` does. Today `GitSync`
    (internal/dotfile/git.go:84-90) pushes without a check.
20. **Auto-commit after a partly failed plan.** It runs when `ExecutedCount() > 0`. Today it
    also requires `!HasFailures()` (cmd/dotfile.go:181, 225, 281, 1065; TUI dotfiles.go:420).

These are not marked as decisions because the code already settles them: backup is enabled
exactly when `dotfile.git.remote_url` is set (internal/dotfile/autosync.go:11), and
`backup push` ignores `auto_commit` and `auto_push` (cmd/dotfile.go:521-529: `AutoCommit:
true, AutoPush: false`, then an explicit `GitPush`). A visibility result that cannot be
determined warns and continues (cmd/dotfile.go:452-462).

## 2. Conflicts with the other chapters (please reconcile)

- **§7.4 (spec.md)** says a remote "whose visibility cannot be determined is refused
  too", and that credentials resolve as git helper → pass → prompt. The code does neither.
  An undetermined result warns and continues. The API token comes only from pass
  (`ResolveGitToken`), with no helper and no prompt. The push authenticates through git's
  own setup. My chapter follows the code. If §7.4 wins, the user's live setup breaks:
  `ws.json` has `remote_url: https://github.com/mugenkunou/ws-dotfiles.git`, and the token
  lookup is broken (bug B6 below), so the check is unauthenticated, GitHub returns 404 for
  the private repo, and the result is "undetermined". Every `ws cron run dotfile-sync` push
  (every 30 minutes) would then be refused. Self-hosted remotes and test remotes (local bare
  repos, used in internal/dotfile/git_repo_test.go:24) would also be refused.
- **§6.5 Dotfiles** `:` commands: add `:add`. **§6.8** handover list: add "actions on sudo
  dotfiles".
- **§6.1 frame** shows a `Stored as / bashrc` column. With decision 2 it becomes the
  workspace copy (shortened in the TUI, for example `~/Workspace/configs/dotfiles/bashrc`).
- **§4.7** lists only `session record` and `session stop` as plan-less RW commands.
  `dotfile edit` runs no plan either (and `ignore edit` and `config edit` probably don't).
- **§6.4 scopes**: `e` uses the "cursor" scope. `reslist` has only Global, Selected and All
  (internal/tui/reslist/actions.go:14-24), so a Cursor scope needs adding, or `e` can use
  Selected with one row.

## 3. Bugs in today's code that the spec fixes (most are data loss)

- **B1. `dotfile sync` on a healthy link destroys the only copy.** `Unregister`
  (internal/dotfile/dotfile.go:393) stats *through* the symlink (it is a file, so it
  passes) and runs `os.RemoveAll(targetAbs)` (:431), which is permanent. Then `Add` finds a
  dangling symlink at the system path, moves that symlink into the workspace (:234) and
  links back to it, which leaves a loop. The TUI `s` key (dotfiles.go:554) does the same.
  Fixed by decision 12. Add a regression test.
- **B2. `dotfile rm`/`reset` on a `link` entry moves the workspace original out of the
  workspace.** `Remove` (dotfile.go:346) and `Reset` (reset.go:109) `movePath` it.
  `ws dotfile rm ~/.claude/CLAUDE.md` takes `agent/AGENTS.md` away. Fixed by decision 8.
- **B3. `dotfile rm`/`reset` on an overwritten entry deletes the live file**:
  `os.RemoveAll(absSystem)` (dotfile.go:329, reset.go:78). Fixed by decision 7.
- **B4. `dotfile sync` re-detects the class** (`Add` without `Class`, cmd/dotfile.go:271,
  TUI dotfiles.go:574). A secret named like a config file can move from
  `secrets/dotfiles/` to `configs/dotfiles/`, which is the pushed repo. Fixed by
  decision 11.
- **B5. `dotfile link` ignores sudo.** `Link` computes `Sudo: NeedsSudo(...)` but calls
  `os.MkdirAll` and `os.Symlink` (dotfile.go:765-770). Use `sudoMkdirAll` and
  `sudoSymlink`. Link's "already linked" check (:725) compares the raw readlink target, so
  a relative target never matches. Resolve it the way `Scan` does (:507-510).
- **B6. The pass token is never found.** `secret.ResolveGitToken`
  (internal/secret/gittoken.go:8) calls `passToken(entry)` →
  `LookupCredential(CredentialRequest{Host: entry})`, and `CredentialCandidates` prefixes
  `git/` again (internal/secret/credential.go:73-85, 102). So it looks up `git/git/<host>` and
  `git/<pass_entry>`. It should call `tryPassEntry(entry)` directly. Nothing tests it today.
- **B7. `--json` loses the exit code.** `writeJSONDryRun` returns 0 (cmd/root.go:297), and
  every dotfile write command returns its result: cmd/dotfile.go:186, 229, 285, 404, 482,
  779, 828, 1059. With `--json`, return `planResult.ExitCode()` after writing the JSON.
- **B8. A missing repo reads as public.** With a token, GitHub/GitLab 404 gives
  `Checked: true, Private: false` (internal/repo/visibility.go:135, 180). Bitbucket gives
  the same even without a token (:216). Callers then print `ErrPublicRepository`. The spec
  wants `repository not found` for an authenticated 404, and "undetermined" (a warning)
  for an unauthenticated 404. Callers must check `vis.Error` before `!vis.Private`.
- **B9. `GitSync` copies `<workspace>/ws.json`** (git.go:53), not the resolved config path.
  `--config` and `$WS_CONFIG` are ignored. Pass the config path in.
- **B10. `dotfile link --dry-run`** runs the plan, then calls `Link` again with
  `DryRun: true` and returns before the JSON (cmd/dotfile.go:1043-1056). `--json` data has
  no `actions` (:1059-1062).
- **B11. `Fix`'s `Only`** matches raw `link.System` strings (dotfile.go:551). Callers must
  pass cleaned absolute paths. Match with `filepath.Clean` on both sides.
- Dead code: `writeDotfileResult` (cmd/dotfile.go:1221) has no callers.
  `printDotfileIssues` (cmd/violations.go:130) is used only by `scan` (cmd/dotfile.go:325).

## 4. Dispatch, help, aliases (cmd/dotfile.go)

- `dotfileHelp` (:19-40): Usage becomes `ws dotfile <ls|add|link|unlink|relink|adopt|edit|backup> [args]`.
  `cmd/parity_test.go:36` reads the subcommands from that `<a|b|c>` list, and
  `TestCompletersOfferEverySubcommand` compares it with `completers["dotfile"]`. Rewrite
  the subcommand lines and flags: `--class` (add, link), `--dir` (add), `--all` (unlink),
  `--dry-run`, and the ls flags.
- Add a `dotfileBackupHelp`: `ws dotfile backup <enable|disable|status|push>`, with
  `--remote`, `--pass-entry`, `-n`, `-m`. `ws dotfile backup` with no verb prints it.
- `runDotfile` switch (:65): new cases ls, add, link, unlink, relink, adopt, edit, backup.
  Hidden aliases print one stderr line through the shared alias helper (none exists yet;
  `grep "is now" cmd/` is empty):

  | Old | New |
  | --- | --- |
  | `scan` | `ls` |
  | `rm <path>` | `unlink <path>` |
  | `reset` | `unlink --all` |
  | `fix` | `relink` |
  | `sync <path>` | `adopt <path>` |
  | `git setup`, `git enable`, `git connect` | `backup enable` |
  | `git remote <url> [--pass-entry e]` | `backup enable --remote <url> [--pass-entry e]` |
  | `git disconnect` | `backup disable` |
  | `git status` | `backup status` |
  | `git log [-n N]` | `backup status -n N` |
  | `git push [-m msg] [--pass-entry e]` | `backup push [-m msg]` (`--pass-entry` accepted, ignored) |

  `migrate` (:851) stays as a hidden case, with no alias line, out of help and completion.
- JSON command names: `dotfile.ls`, `dotfile.add`, `dotfile.link`, `dotfile.unlink`,
  `dotfile.relink`, `dotfile.adopt`, `dotfile.edit`, `dotfile.backup.enable`,
  `dotfile.backup.disable`, `dotfile.backup.status`, `dotfile.backup.push`. Aliases emit
  the new name.
- Old usage strings that name old commands: :79, :200, :243, :411, :436, :442, :505, :509,
  :566, :644, :667, :789, :811, :837, :843, :848, :978, :1007.

## 5. Per command

### `ls`
- Today: cmd/dotfile.go:290 (`dotfile.Entries`, internal/dotfile/fields.go:21, then
  `renderList`). `scan` is :305 (`dotfile.Scan` + `printDotfileIssues`, exits 2 at :327).
- Change: `Schema.Columns` (fields.go:79) becomes `system, workspace_path, class, status,
  sudo`. Consider the title "Workspace copy". The `scan` alias runs ls. Delete the exit 2
  path and `printDotfileIssues`. The TUI columns (dotfiles.go:140) use the key
  `original`: switch it to `workspace_path`.
- Tests: cmd/dotfile_test.go `TestDotfileCommandAddAndScan` (expects "Dotfile scan" and
  "OK") and `TestScanExitCodeOnViolation` (expects 2, now 0). cmd/list_test.go:118-134
  needs no change, since the fields are the same.
- Risk: anything relying on `scan` exiting 2. I found no callers in
  `~/Workspace/{scripts,agent,configs}`.

### `add`
- Today: cmd/dotfile.go:68-190 (exactly one path, :78). Also `promptDirSelection` (:1076),
  `warnAndFilterSecrets` (:1184), `dotfile.Add` (dotfile.go:152), `ExpandDir` and
  `CollectFiles` (expand.go), `ClassifyFile` and `ClassifyDir` (classify.go), and
  `workspaceRelativePath` (dotfile.go:801).
- Change: accept 1 or more paths, stat and expand each, and ask the secret prompt once over
  all files. The action description becomes `Add <sys> → <abs workspace copy>[ (secret)]`,
  and `Add directory … (whole)` for `--dir`. This needs the storage path at plan time:
  export a `StoragePath(workspace, system, class)` that wraps `workspaceRelativePath` and
  `resolveClass`. Add two refusals to `dotfile.Add`: the Lstat mode is a symlink, and the path is
  inside the workspace. Use one auto-commit, message
  `dotfile add: <paths>`.
- TUI: `openAdd` (dotfiles.go:453) stays. `:add` parses `--class` and `--dir`.
- Tests: `TestDotfileAddDirAsSecret`, `TestConfigFlagRejectsDirectory` and
  `TestDotfileHelpListsAddFlags` stay. Add: several paths in one plan, symlink refusal,
  in-workspace refusal. `TestDotfileCommandAddAndScan` checks "Add dotfile", and that text
  changes.

### `link`
- Today: `runDotfileLink` (cmd/dotfile.go:986), `dotfile.Link` (dotfile.go:698).
- Change: resolve `<ws-path>` with `wsconfig.ExpandUserPath`, require it to be inside the
  workspace (`filepath.Rel`, no `..`), and pass the slash-relative path as
  `WorkspaceRelPath` (the internal API stays). When `Class == ""`, default to `secret`
  under `secrets/`. Fix B5 and B10. JSON data: `actions`, `system`, `workspace_path`.
  Auto-commit, message `dotfile link: <path>`.
- Tests: the internal tests (`TestLinkScanFixRoundTrip`, `TestLinkDirScanClean`,
  `TestLinkRecordsDir`) pass `WorkspaceRelPath` and still work. Add a cmd test that a
  relative `<ws-path>` resolves against the cwd (`t.Chdir`), and one that a path outside
  the workspace is refused.
- Risk: `ws dotfile link X agent/AGENTS.md` run outside the workspace now fails with "not
  found". There are no callers in the user's scripts. The error could add "did you mean
  /home/…/Workspace/agent/AGENTS.md?" when that path exists.

### `unlink`
- Today: `rm` (cmd/dotfile.go:191, one path) → `dotfile.Remove` (dotfile.go:253). `reset`
  (:380, one action) → `dotfile.Reset` (reset.go:35). TUI: `removePlan` (dotfiles.go:493)
  and `openReset` (:587).
- Change: rename `Remove` to `Unlink`. Owned means `original` is under `configs/dotfiles/`
  or `secrets/dotfiles/` (either root, not just the class's) **and** no other link has the
  same cleaned `original`. Otherwise the copy is shared, and ws copies it with `copyAny`
  (dotfile.go:874, which keeps perms; use `sudo cp -a` for sudo entries). Anything at the
  system path that is not the expected symlink goes to `freeBackupPath` (dotfile.go:684)
  through `os.Rename` or `sudoRename`. Add `dotfile.UnlinkPreview(entry)` returning
  (mode: move|copy|missing, backupPath) so the plan line can say what happens. `--all`
  builds one action per entry. Delete `Reset` once `ws reset` is gone: it is still used by
  internal/workspace/reset.go:62 and the Dashboard reset (internal/tui/screens/dashboard/dashboard.go:351),
  both of which the workspace chapter removes.
- Tests: `TestAddListRemoveRoundTrip`, `TestRemovePrunesEmptyDirs`,
  `TestRemovePrunesOnlyEmptyDirs` and `TestRemoveWithMissingWorkspaceCopy` need renames.
  New: unlinking a link entry copies the file and keeps the original; an overwritten entry
  gets a `.ws-backup`; a shared copy is copied, not moved; `--all` gives one action per
  entry; with `--json`, the exit code is 3 on a partial result.

### `relink`
- Today: `fix` (cmd/dotfile.go:331) calls `dotfile.Fix` (dotfile.go:536) without a plan. It
  prints a summary, and its JSON is `FixResult`. TUI `openFix` (dotfiles.go:511) makes one
  action for every broken link through `Only`. The Dashboard sends `RequestFixMsg`
  (dashboard.go:449, label "fix links").
- Change: build one plan action per non-ok entry. Extract the body of the `Fix` loop into
  `dotfile.Relink(opts, link)`. A missing workspace copy is not an action: print `✖ <sys>:
  workspace copy <path> is missing` and append a `failed` `ActionStatus`, so JSON and the
  exit code include it. Targets: resolve them, then match on cleaned paths. Print
  `All dotfile links are ok.` when there is nothing to do. Drop the `WOULD_*` statuses,
  since dry-run comes from the plan. TUI `l` uses Scope All. Rename `RequestFixMsg` and
  the "fix links" label (coordinate with the TUI chapter: §6.5 has the Dashboard open
  Dotfiles filtered to `status!=ok`).
- Tests: `TestScanAndFixBrokenSymlink`, `TestFixCreatesSymlink`,
  `TestFixOnlyTouchesNamedLinks`, `TestFixUnchanged`, `TestFixKeepsExistingBackup` and
  `TestLinkScanFixRoundTrip` need renames. New cmd test: a missing copy exits 1, or 3
  alongside a good entry.

### `adopt`
- Today: `sync` (cmd/dotfile.go:234) runs `Unregister` (dotfile.go:393), then `Add`. TUI
  `syncAction` (dotfiles.go:554).
- Change: write a new `dotfile.Adopt(opts)`. The entry must exist and be owned (keep the
  check at dotfile.go:422). `Lstat(system)` must be a regular file, and `!link.Dir`. If the
  copy exists, `trash.SoftDelete` it (internal/trash/trash.go:50). Move the live file to
  the same `workspaceAbs` (`sudoRename` for sudo entries), symlink back, and leave the link
  record unchanged. Delete `Unregister`, whose only callers are `sync` and the TUI.
- Tests: `TestDotfileSyncCommand` (cmd/dotfile_test.go:80) becomes adopt.
  `TestUnregisterKeepsLinkedOriginal` and `TestUnregisterKeepsCopyWhenLiveIsDir` move to
  `Adopt`. New: adopting an `ok` entry fails and leaves the copy intact (B1); the `secret`
  class is kept (B4); the old copy lands in the test's XDG trash.

### `edit` (new)
- Add a `case "edit"`. Resolve the editor once and share it with `ignore edit`
  (cmd/ignore_extras.go:529-537, `--editor` → `$EDITOR` → `$VISUAL` → `vi`). Split it with
  `strings.Fields`, as `handover.Editor` does (internal/tui/handover/handover.go:70).
  `ignore edit` does not split today, so `EDITOR="code -w"` breaks there. Print warnings
  for an overwritten entry and an unwritable copy.
- TUI: `e` calls `handover.Editor(editor, entry.WorkspacePath, handoverEdit)`.
- Tests: `EDITOR=true` exits 0, `EDITOR=false` exits 1, an unmanaged path exits 1, and a
  missing copy exits 1.

### `backup enable`
- Today: `git setup` (cmd/dotfile.go:672; it prompts through `promptLine`, cmd/secret.go:1049),
  `git remote` (:422), `git enable|connect` (:842, an error).
- Change: one function. Resolve the URL: the flag, then `cfg.Dotfile.Git.RemoteURL`, then
  `dotfile.GitRemoteURL(repoPath)`, then the prompt. With none, exit 1. Put the
  visibility check in one internal helper, used by enable, push and auto-push, for example
  `dotfile.CheckRemote(url, passEntry) (VisibilityResult, error)`. It wraps
  `repo.CheckRepoVisibility` and `secret.ResolveGitToken` and handles B8. Actions: init
  (`EnsureGitRepo(repoPath, "", branch)`), set the remote (`GitAddRemote`, save
  `remote_url` and `pass_entry`), turn on the autos.
- Tests: `TestDotfileGitSetupAndStatus` (cmd/ignore_dotfile_restore_test.go:42) expects
  exit 0 and "No git remote" for a blank remote. Rewrite it with a local bare repo
  (`git init --bare` in `t.TempDir()`; a path URL is an unknown provider, so it warns and
  continues). `TestCompleteDotfileGitSubSub` (cmd/complete_test.go:220) becomes backup.
- Risk: `ws dotfile git setup` with a blank answer used to succeed. The alias now exits 1.

### `backup disable`
- Today: `git disconnect` (cmd/dotfile.go:797): it needs `remote_url` (or exits 1) and sets
  both `auto_*` to false.
- Change: action 1 adds `dotfile.GitRemoveRemote` (`git remote remove origin`) and clears
  `RemoteURL`. Action 2 runs when `cron.HasJob("dotfile-backup")` or the old
  `dotfile-sync` marker block is present (internal/cron/crontab.go:64), and calls
  `cron.RemoveJob` (:115) for each. When backup is not enabled, exit 0. Count unpushed
  commits with `GitAheadBehind`.
- Risk: the alias `git disconnect` now removes the remote, where it used to keep it.

### `backup status`
- Today: `git status` (cmd/dotfile.go:589), `git log` (:555). TUI `loadGitView`
  (dotfiles.go:210) builds its own text.
- Change: add one internal `dotfile.BackupStatus(workspace string, git wsconfig.DotfileGit,
  n int) BackupState`, used by the CLI text, the JSON and the TUI view. `GitLog` becomes
  structured (`--format=%h%x09%s`). JSON keys: `git_initialized` becomes `initialized`,
  `git_remote` goes (use `remote_url != ""`), `working_tree` becomes the `uncommitted`
  list, and the old log string becomes `commits`.

### `backup push`
- Today: `git push` (cmd/dotfile.go:492) does `GitSync{AutoCommit: true, AutoPush:
  false}`, then the visibility check, then `GitPush`, with `--pass-entry` ignored (:502).
  It uses `resolveGitToken(remoteURL, "")`, which ignores `cfg.Dotfile.Git.PassEntry`,
  while the TUI `openPush` (dotfiles.go:609, a duplicate) passes it (:638).
- Change: one internal `dotfile.BackupPush(opts)` shared by the CLI, the TUI `b` and cron,
  run as a one-action plan. The default message becomes `ws dotfile backup push` (it was
  `manual push`). Use `pass_entry`. JSON: `actions`, `committed`, `pushed`, `remote_url`,
  `branch`.

### Auto-commit (`AutoSync`)
- Today: `AutoSync(workspacePath, configPath, _compat, msg)` (internal/dotfile/autosync.go:9).
  It is called by `dotfileGitAutoSync` (cmd/dotfile.go:1254) after add, rm, sync, link and
  migrate, and by the TUI `autoSync` (dotfiles.go:428), which passes `p.Manifest` for the
  compat slot, after add, rm and sync.
- Change: drop the compat parameter. Run when `ExecutedCount() > 0`. Before the push in
  `GitSync` (git.go:84), run `CheckRemote`. If it refuses, skip the push and return a
  warning. Messages are `dotfile <verb>: <paths>`. The TUI also calls it after link,
  unlink and adopt, but not after relink.
- Test: internal/dotfile/git_repo_test.go `TestAutoSyncCommitsInConfigsDotfiles` (the
  signature changes; it uses a local path remote, which warns and continues).
- Risk: one HTTP call (10 s client timeout, internal/repo/visibility.go:108) after each add
  or unlink while `auto_push` is on.

## 6. Cron job rename (dotfile-sync → dotfile-backup)

- internal/cron/jobs.go:30: rename `dotfileSyncJob` to `dotfileBackupJob`, with
  `Name: "dotfile-backup"` and the body `%s --workspace %s dotfile backup push --quiet`. Fix
  the stale description ("requires dotfile.git.enabled=true") to say it needs
  `ws dotfile backup enable`.
- internal/cron/cron.go:46: the `Builtins` key. Preset `sync` (:56) becomes
  `{"mega-sync", "dotfile-backup", "repo-sync"}`. Add an alias map `dotfile-sync` →
  `dotfile-backup` (plus the cron chapter's `log-prune` → `session-prune`), used by
  `Resolve` (:74) **and** `RunJob` (internal/cron/run.go:31 reads `Builtins[name]`
  directly). Both run silently.
- **Live crontab** (`crontab -l`, lines 6-10):
  `# >>> ws:dotfile-sync >>>`, `*/30 * * * * /home/siva-14414/.local/bin/ws cron run dotfile-sync`,
  `@reboot /home/siva-14414/.local/bin/ws cron run dotfile-sync`, `# <<< ws:dotfile-sync <<<`.
  These lines must keep working. Also: `HasJob` (crontab.go:64) finds blocks by marker
  name, so `cron ls` must count a `ws:dotfile-sync` block as `dotfile-backup` installed.
  `cron add dotfile-backup` must replace the old block, and `cron rm dotfile-backup` and
  `backup disable` must remove both. Run state: `LastRun(statePath, job.Name)`
  (state.go:90) and `ReadLog` (:107) are keyed by name. After the rename, new records use
  `dotfile-backup`, so `cron ls` and `cron log` should also read `dotfile-sync` records.
- The user's live `ws.json` has `remote_url` set and `auto_commit: false, auto_push:
  false`, and cron pushes every 30 minutes. Under the spec that stays the same: backup is
  enabled, the autos are off, and `backup push` ignores them.
- Tests: internal/cron/cron_test.go:61 (marker name), :107 (`dotfileSyncJob`), :143 (preset
  resolve), :166 (`AllNames`); cmd/cron_test.go:53 (the names list).
  internal/tui/app/app_test.go:107 uses a fake `dotfile-push` row, which is fine.
- cmd/complete.go:319 `cronJobNames()`: replace `dotfile-sync` with `dotfile-backup`.

## 7. TUI (internal/tui)

- **keys.go:199-246 `DotfilesMap`.** New: `Add` a, `Unlink` ctrl+d, `Relink` l, `Adopt` o,
  `Edit` e, `Push` b (help "back up"). **Remove:** `Remove` d (it clashes with the global
  `d` describe), `Fix` x, `Sync` s, `Reset` R, `Push` p, `GitView` v, `GitSetup` G,
  `Migrate` M. The footer, per §6.1, reads `a add • l relink • o adopt • ctrl+d unlink •
  b back up …`. `e` and `d` are global.
- **dotfiles.go actions (:338-366).** Add is Global, Unlink Selected, Relink All (shown),
  Adopt Selected, Edit cursor, Push Global. The `:` commands are `add …`, `link <path>
  <ws-path>` (one-action checklist), `unlink --all` (the `openReset` logic, every action
  unchecked, now one action per entry), `backup enable [--remote]` (handover
  `WS(..., "dotfile", "backup", "enable", …)`), `backup disable` (checklist), `backup
  status` (view), and `backup push` (same as `b`).
- **The git view becomes the `:backup status` view.** Rename `levelGit` (:89) to
  `levelBackup`, `gitViewMsg` (:82) to `backupStatusMsg`, and `loadGitView` (:210) to a
  renderer over `dotfile.BackupStatus`. `esc` still returns (:325-333).
- **Hardcoded "press G" texts:** :220 ("Press G (from the list) to run `ws dotfile git
  setup`."), :231 ("none — press G to set up"), :617 ("git not initialized — press G to
  run setup"), :620 ("no remote configured — press G to run setup"). All of them become
  `:backup enable`. The package comment (:1-7) names add/remove/fix/reset and "git view".
- **Steps (:93-103):** `stepRemove` becomes `stepUnlink`, `stepFix` `stepRelink`,
  `stepSync` `stepAdopt`, `stepReset` `stepUnlinkAll`, `stepPush` `stepBackupPush`. Add
  `stepLink` and `stepBackupDisable`. **Handover tags (:106-111):** `handoverGitSetup`
  becomes `handoverBackupEnable`. Drop `handoverMigrate`. Add `handoverEdit` and
  `handoverSudo`.
- **`dialogResult` (:400-426):** auto-commit after add, link, unlink, unlink-all and adopt,
  with messages `dotfile unlink` and `dotfile adopt` (:428-436).
- **Sudo (decision 1):** before opening a checklist, split out rows with `Link.Sudo` and
  hand those to the CLI.
- **`RequestFixMsg` (:64, :290)** is used by dashboard.go:449. Rename it, or replace it
  with the "open filtered to status!=ok" flow from §6.5.
- **coverage.go:57-66**, new dotfile entries: `dotfile ls` (screen), `dotfile add`
  (df.Add), `dotfile link` (`:link`), `dotfile unlink` (df.Unlink), `dotfile relink`
  (df.Relink), `dotfile adopt` (df.Adopt), `dotfile edit` (the global e), `dotfile backup`
  (df.Push; note `:backup enable|disable|status`). `cliCommands` (cmd/parity_test.go:49)
  goes only two levels deep, so `dotfile backup` is one entry. `Coverage` has no field for
  `:` commands, so the cross-cutting TUI work should add one.
- **Goldens** (internal/tui/app/testdata): dotfiles.golden (the footer and the column
  header), dotfiles_reset_checklist (keys `3 R` become `:unlink --all`),
  dotfiles_marked_remove (`d` becomes `ctrl+d`), dotfiles_marked_reset (`R`),
  dotfiles_detail (`i` becomes `d`), dotfiles_filter, dotfiles_sorted_marked,
  full_help.golden, and the dashboard goldens if the fix label changes. The key sequences
  are in app_test.go:222-228. internal/tui/screens/dotfiles/dotfiles_test.go
  (`TestListShowsClass`, `TestLoadReportsBrokenLinks`) is unaffected.

## 8. Completion (cmd/complete.go)

- :59 `completers["dotfile"].subcommands` becomes `ls add link unlink relink adopt edit
  backup`.
- `completeDotfile` (:212): `add` and both `link` arguments use the shell's file
  completion. `unlink`, `relink`, `adopt` and `edit` complete `ctx.dotfiles`, the system
  paths loaded at :496-499. `backup` completes `enable disable status push`.
- Flags (:389-410): add `--class --dir --dry-run`; link `--class --dry-run`; unlink
  `--all --dry-run`; relink and adopt `--dry-run`; backup enable `--remote --pass-entry
  --dry-run`; backup disable `--dry-run`; backup status `-n`; backup push `-m --dry-run`.
  The value of `--class` completes `config` or `secret`.
- Tests: complete_test.go:101 (`wantSome`), :220 (`TestCompleteDotfileGitSubSub`).

## 9. Docs and other references

- README.md:95-111 (command table), :208-210 (job and preset names), :246-248
  (`dotfile fix`, `dotfile.git.enabled`), :362-367 (`ws/dotfiles`, `dotfiles-git` layout),
  :399 (the restore walk-through). All of these are stale.
- The legacy packages internal/manifest (manifest.go:28, :106) and internal/config
  (config.go:68, :111) still describe dotfile git settings. They are not part of this
  group, but they are candidates for deletion.
- Live data to keep in mind: the user's `links[]` has sudo entries (`/etc/hosts`,
  `/etc/docker/daemon.json`; their copies are root-owned in `configs/dotfiles/etc/`), a
  whole-directory secret (`/home/siva-14414/.password-store` → `secrets/dotfiles/password-store`),
  and no `link`-made entries. Never run unlink or adopt against the real workspace while
  testing.
