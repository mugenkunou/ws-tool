# Plan 0004: `ws sync`, a one-way mirror for folder-sync clients

> **Status: implemented** in `internal/mirror` + `cmd/sync.go` (not `internal/sync/` as §8
> says). HANDBOOK §5 `ws sync` is authoritative. Deviations from this plan:
>
> - **No `--force`** (HANDBOOK Factor X, consent model). The delete guard, a foreign stamp
>   and a restore over an existing `ws.json` ask interactively, and are refused under
>   `--quiet`/`--json`, so cron refuses.
> - **`ws sync restore [<mirror-dir>]`** takes an optional mirror dir (defaults to
>   `sync.mirror.target`; needed on a fresh machine without `ws.json`). It adopts the
>   mirror's stamp afterwards.
> - **Empty dirs are mirrored**: no `--prune-empty-dirs`.
> - **The filter uses `--from0`** (NUL-separated rules).
> - The `sync` block is in the default `ws.json` (empty target, empty `exclude`, 20), since
>   `ws.json` always holds every key (HANDBOOK §2.3); §3's "init does not write it" no
>   longer holds.

**For:** the agent who changes the code. Read this whole file and HANDBOOK Factor XII
("Sync is one-way") before touching anything. If something cannot be built as written, stop
and report it instead of choosing silently.

**Goal:** let `~/Workspace` stay where it is, with its name, while a sync client that can
only sync its own folder (Zoho WorkDrive Sync, OneDrive, a USB stick) carries it. ws copies
the workspace one way into that folder; the client uploads it.

---

## 0. Ground rules

1. **Tests run in Docker only: `make test`.** Never run bare `go test` on the host (see
   plan 0003 §0 for why). Quick host build: `TMPDIR=$HOME/tmp GOCACHE=$HOME/.gocache
   GOTMPDIR=$HOME/gotmp go build ./...`.
2. **One way only** (Factor XII). Nothing ever flows from the mirror into `~/Workspace`
   except `ws sync restore`, run by hand on a fresh machine.
3. **ws never syncs symbolic links.** `.megaignore` ends with `-s:*`, WorkDrive drops them,
   and `ws ignore lint` warns about any synced link (commit 3649a5a). The mirror skips them.
4. **ws's own rule engine decides what goes out.** Do not translate ignore rules into rsync
   syntax rule by rule; rsync is first-match, ws is last-match, and the globs differ.

## 1. Decisions (settled with the user, 2026-10-26)

| # | Decision |
|---|---|
| D1 | One-way: `~/Workspace` → mirror folder → cloud. The mirror is never edited. |
| D2 | The `symlinks/` storage tree (`.ssh`, `.kube`, `.docker`, …) **is** mirrored. The user accepts that an office WorkDrive is readable by its admins. |
| D3 | No default for `sync.mirror.target` and no default `sync.exclude`. Unset target → `ws sync` refuses with a hint. |
| D4 | File modes are not carried. No manifest. The user fixes executable bits by hand after a restore. |
| D5 | Symbolic links are skipped (ground rule 3). |

## 2. Shape

```
┌──────────────┐   ws sync push    ┌───────────────────────────────┐  sync client  ┌───────────┐
│ ~/Workspace  │ ────────────────► │ <sync.mirror.target>          │ ────────────► │   cloud   │
│ the truth    │ rsync, exact list │ e.g. ~/Zoho WorkDrive/        │  uploads by   │           │
│              │ from ws's rules   │      My Folders/Workspace     │  itself       │           │
└──────────────┘                   └───────────────────────────────┘               └───────────┘
       ▲                                                                                 │
       └──────── ws sync restore (fresh machine only) ◄── mirror ◄── client downloads ◄──┘
```

## 3. Config (`ws.json`)

```jsonc
"sync": {
  "mirror":  { "target": "~/Zoho WorkDrive/My Folders/Workspace" },   // required, no default
  "exclude": [],                                                      // extra, mirror-only; no default entries
  "max_delete_pct": 20                                                // delete guard, 1–100
}
```

Validation (`internal/wsconfig/validate.go`):
- `target` expands `~`, is absolute after expansion, and is **outside** the workspace
  (and the workspace is not inside it).
- `exclude` entries use the same pattern syntax as `ignore_rules.exclude`; they are applied
  after the ignore rules, for the mirror only.
- `max_delete_pct` 1–100; 0 or missing means 20.

`ws init` and the TUI setup do not write a `sync` block. The user adds it (a later step may
add `ws sync setup`; out of scope here).

## 4. `ws sync push`

```
ws sync push
 │
 ├─ 1. lock        flock ~/.local/state/ws-tool/sync/lock      busy → skip, exit 0 (cron overlap)
 ├─ 2. checks      sync.mirror.target set?                       ❌ refuse with hint
 │                 <workspace>/ws.json exists?                   ❌ refuse (source missing/empty)
 │                 parent of target exists?                      ❌ refuse (client gone/moved)
 ├─ 3. stamp read  mirror/.ws-sync-stamp vs state/sync/last-push.json   (see §6)
 ├─ 4. file list   walk with ignore.BuildEngine + sync.exclude, skip symlinks
 │                 → state/sync/filter  ("+ /dir/" for each parent, "+ /file", then "- *")
 ├─ 5. dry run     rsync -n --itemize-changes …   count "*deleting" lines
 │                 > max_delete_pct of mirror files (and > 10 files)?  ⚠️ ask y/N; refuse under --quiet/--json
 ├─ 6. copy        rsync for real
 ├─ 7. stamp write mirror/.ws-sync-stamp + state/sync/last-push.json
 └─ 8. report      "pushed: 12 changed, 3 deleted, 1,650 files, 118 MB"
```

```bash
rsync -rt --delete --delete-excluded --prune-empty-dirs \
      --filter="P /.ws-sync-stamp" \
      --filter="merge $STATE/sync/filter" \
      --temp-dir="$STATE/sync/tmp" \
      "$WORKSPACE/"  "$TARGET/"
```

| Choice | Why |
|---|---|
| Generated filter, ends with `- *` | Exactly the files `ws ignore check` says sync. `--files-from` was rejected: it does not delete stale files from the mirror. |
| Escape `* ? [ \` in paths | rsync treats them as wildcards in filter rules. |
| `-rt`, no `-l`, no `-p` | Links skipped (D5); modes not carried (D4); times kept so unchanged files are skipped. |
| `--update` (added for Zoho TrueSync) | TrueSync's virtual drive stamps copies with upload time, so `-t` alone re-sends every file each push. A mirror copy newer than the source is skipped. |
| `--delete --delete-excluded` | Files deleted or newly ignored in the workspace leave the mirror. |
| `--temp-dir` outside the mirror | The client must never see rsync's half-written temp files. |
| `P /.ws-sync-stamp` | Protects the stamp from `--delete-excluded`. |

Flags: `-n/--dry-run` (show the itemized changes and the delete count, change nothing),
`--quiet` (cron; print only errors and §6 warnings). There is no `--force`: the delete
guard and a foreign stamp ask y/N, and refuse under `--quiet`/`--json`.

## 5. `ws sync status` and `ws sync restore`

- **status:** target, last push (time, files, bytes, deleted), stamp state (§6), and a
  dry-run count of files waiting to go out.
- **restore:** rsync `target/` → workspace with the same flags minus the delete flags and
  the filter. Over an existing workspace (ws.json present) it asks y/N first,
  and refuses under `--quiet`/`--json`. Never deletes anything in the workspace. Prints a
  reminder that executable bits are not restored (D4), then suggests `ws symlink link`.

## 6. The stamp: detecting a wiped or foreign mirror

`.ws-sync-stamp` in the mirror root holds `{push_id, host, time}`; `last-push.json` in the
state dir holds the same. Before each push:

| Mirror stamp | Meaning | Action |
|---|---|---|
| matches last push | normal | push |
| missing, but a last push exists | mirror was emptied (locally or via the cloud) | ⚠️ warn loudly ("mirror was wiped since <time>; re-uploading N files, restore older copies from the WorkDrive trash if needed"), then push |
| from another host or push_id | a second machine pushes to this mirror, against Factor XII | ❓ ask y/N; refuse under `--quiet`/`--json` |
| missing, no last push | first push | push |

## 7. Cron

`internal/cron/jobs.go`: `sync-push`, every 30 minutes,
`<ws> --workspace <ws> sync push --quiet`, same shape as `pass-backup`. Not installed by
default; `ws cron add sync-push`.

## 8. Code layout

```
internal/sync/
  filter.go      walk + engine + sync.exclude → rsync filter (escaped)
  push.go        lock, checks, stamp, dry run, delete guard, rsync, report
  restore.go     reverse copy with the empty-workspace guard
  status.go
cmd/sync.go      noun `sync`: push | status | restore  (+ commands.go, completion, help)
internal/wsconfig   Sync struct + validation
internal/cron/jobs.go   syncPushJob
```

## 9. Steps

| # | Step | Tests (Docker) |
|---|---|---|
| 1 | `wsconfig.Sync` + validation | unset target; target inside workspace; workspace inside target; bad pct |
| 2 | `filter.go` | for a fixture tree, the filter admits exactly the paths `Engine.Evaluate` includes; symlinks and `sync.exclude` left out; wildcard chars escaped |
| 3 | `push.go` | push; delete a file and push → gone from mirror; ignore a dir and push → gone; mass delete asks, refused under `--quiet`; missing ws.json refused; missing target parent refused |
| 4 | stamp (§6) | wiped mirror warns and refills; foreign stamp refused |
| 5 | `restore.go`, `status.go` | push → empty workspace → restore → same tree; non-empty workspace refused |
| 6 | `cmd/sync.go`, cron job, completion, help | parity and complete tests |
| 7 | Docs: HANDBOOK command list and §2.6, README WorkDrive section rewritten around `ws sync push` (drop rclone bisync) | — |

## 10. Before coding: checks on the user's machine

- [ ] WorkDrive Sync shows a **delete confirmation dialog** for local deletes. Tick "Don't
      show delete confirmation dialog", or every push that deletes will pop one.
- [ ] Dotfiles upload: after a test push, `grep gitconfig ~/.zohoworkdrive/logs/synclog.txt`.
- [ ] Web deletions reach the local folder (Zoho's docs only document local → cloud):
      delete a test file on the web, check it disappears from the mirror. If it does not,
      §6 cannot see a cloud-side wipe (see §11).

## 11. Failure scenarios

**A. The local mirror folder is emptied.** The client propagates the deletes to the cloud
(documented; items go to the WorkDrive trash, restorable from the web app). The workspace
is untouched. The next push finds no stamp, warns, and copies everything back; the client
re-uploads. Exposure: the cloud is empty until that push (≤ 30 min with cron).

**B. Everything is deleted on the web.** If the client propagates web deletes down (check
§10), the mirror empties and it plays out as A. The workspace is untouched: one-way means
the wipe cannot reach the truth. If the client does not propagate them, the mirror still
looks complete, rsync sees nothing to do, and the cloud stays empty until files change;
the fix is a manual re-upload or a rename of the mirror folder.

**C. The workspace is emptied** (the one that matters). `ws.json` gone → push refuses.
Partial loss above `max_delete_pct` → push refuses. Smaller losses do propagate; recover
from the WorkDrive trash.
