---
status: accepted
date: 2026-10-24
---

# `ws symlink`: one mirrored storage tree and a built-in catalog

The command for linking host files into the workspace started as "symlink my config files
into the workspace". By October 2026 it held
SSH private keys, kube configs, whole folders (VS Code snippets, `~/.claude/skills`) and
`/etc` files. Three problems showed:

- **The noun was wrong.** The previous noun does not describe `/etc/hosts`, a secret, or a folder.
- **The user had to know what to link.** `add` waited for a path. Nothing told
  a new user that `~/.gitconfig` or `~/.config/nvim` are worth keeping, so the old
  workspace tracked 12 links and missed 12 more (see `~/ws.todo` §1).
- **The user had to choose where to store each file.** Originals went to
  a config folder or a secrets folder by a guess about the content, or anywhere with
  `--to`. The split existed so the config folder could be a git repo pushed to GitHub. That
  backup is removed (below), so the split now only costs a decision.
- **Links already in the workspace were invisible.** Twelve hand-made links into the
  workspace were not in `ws.json`, and ws had no way to find them.

## Decision

### 1. The noun is `symlink`

`ws symlink <verb>`, singular like every other noun. The verbs are `ls`, `add`, `link`, `unlink`. The TUI
screen is "Symlinks".

### 2. One storage tree that mirrors the host path

Every original lives under a single top-level folder, `symlinks/`, at the path it has on
the host:

```
~/.gitconfig           →  symlinks/home/.gitconfig
~/.ssh/id_rsa          →  symlinks/home/.ssh/id_rsa
~/.config/nvim/        →  symlinks/home/.config/nvim/
/etc/hosts             →  symlinks/system/etc/hosts
```

- The user never chooses a place. The path is a pure function of the host path, so it never
  collides (`Code/User/settings.json` and `Cursor/User/settings.json` are different
  paths), and any file under `symlinks/` maps back to its host path by removing the prefix.
- There is no `configs`/`secrets` split. The whole workspace syncs through MEGA, so
  `symlinks/` syncs like everything else.
- **One built-in include, `symlinks/**`.** The mirror keeps host names, so the tree holds
  `.gitconfig`, `.ssh/` and `.config/`, and the built-in `-:.*` exclude would drop all of
  them. A fixed `+g:symlinks/**` rule keeps them syncing. It is the only built-in included
  directory (the ignore rework removed the others), it is not stored in `ws.json`, and a
  user's ignored path inside it still wins. Storing names without the leading dot was
  rejected: the stored path would no longer match the host path.
- To store something elsewhere, the user gives two paths: `ws symlink add <host-path>
  <ws-path>`. The second path is any path inside the workspace (`artifacts/screenshots`).
  This replaces `--to`.
- `ws.json` stores the host path with `~` for `$HOME`, so a laptop with another user
  name still works.

### 3. A catalog in the binary suggests what to link

`internal/symlink/catalog.go` holds a flat Go slice of about 130 well-known configs:
host path or glob, file or folder, secret flag, sudo flag, OS and an app label. The data is
in [docs/plans/0003/catalog.md](../plans/0003/catalog.md).

- It lives only in the binary. It is never written to any file, and the user cannot edit it.
- **Every symlink the user has is in `ws.json`**, whether it came from the catalog or was
  typed by hand. The catalog is a source of suggestions, not of state.
- ws checks the catalog silently and reports only what exists on this machine and is not
  managed yet (the same quiet way `ignore lint` works). The user is never shown the 130
  entries. `ws symlink add` with no arguments shows only what was found. The Dashboard
  adds one info line ("4 known configs found, not symlinked"). `ls` ends with a footer.
- There is no heuristic scan. Anything outside the catalog goes through `add <path>`.
- Secret and sudo entries start unchecked in the checklist. Everything else starts checked.
- Mixed folders (`~/.ssh`, `~/.claude`, `~/.config/Code`, `~/.gnupg`, `~/.docker`) are never
  listed whole, because they hold known_hosts, history and credentials. The catalog lists
  their safe children. A test fails if an entry names a path from the never-suggested list.

### 4. Stateless discovery with seven statuses

`ls` compares four things on every run and keeps no state: `ws.json`, the files under
`symlinks/`, the catalog paths on the host, and (for `ls` only) a bounded walk of `$HOME`
for links that point into the workspace.

| Status | Meaning | Command |
| --- | --- | --- |
| `ok` | In `ws.json`, linked and healthy | none |
| `found` | A catalog item exists here and is not managed | `add` |
| `untracked` | A host link points into the workspace but is not in `ws.json` | `add <host-path>` registers it, moves nothing |
| `orphan` | Content sits under `symlinks/` with no entry in `ws.json` | `link <host-path>` |
| `missing` | In `ws.json`, nothing at the host path (a new laptop) | `link` |
| `broken` | The host link points at something that is gone | `link` |
| `drifted` | A tool replaced the link with a real file | `add <host-path>` takes the live file |

Links that sit inside the workspace (`data/bruno/temp/environments`) stay out of scope:
rsync keeps them.

### 5. Verbs say which copy wins

- `add` keeps what is on the host: the live file goes into the workspace.
- `link` keeps what is in the workspace: the workspace copy goes out to the host.
- `unlink` puts the real content back at the host path and removes the entry.

This is the grammar rule from [ADR 0002](0002-one-command-grammar-for-cli-and-tui.md),
unchanged.

### 6. What is removed

- The git backup of the linked files (its commands, its code, its `ws.json` key and its
  cron job), the `sync` preset entry for it, the TUI `b` key and `:backup`.
  Symlinked files are now protected by MEGA only. A user who wants history can `git init`
  `symlinks/` and `ws repo` will see it. `internal/backup` and `repo/visibility.go` stay
  because `ws pass backup` uses them.
- The one-off migration command, the edit command (open the file under `symlinks/` in
  your editor), `--class` and `--to`.

## Considered options

- **Group storage by app** (`symlinks/git/.gitconfig`). Rejected: every custom file needs an
  app name, and two apps can share a file name.
- **Split by class** (`configs/symlinks`, `secrets/symlinks`). Rejected: it needs secret
  detection at add time, which is the decision we are removing.
- **The user types a destination every time.** Rejected: it is today's pain.
- **A heuristic scan of `$HOME` for things that look like config.** Rejected: noisy, and it
  suggests things ws cannot vouch for. The catalog is explicit and testable.
- **A catalog file in the workspace or the config.** Rejected for now: the catalog is code, so
  it is reviewed and tested like code. User-defined suggestions can come later.
- **Keep the previous noun.** Rejected: wrong for secrets, folders and `/etc`.
- **A different noun** (`rig`, `kit`, `setup`). Rejected by the owner in favour of the
  literal `symlink`.

## Decided by default

The owner left these open. They are the defaults the plan builds. Each is cheap to change.

1. One mirrored `symlinks/` folder, with secrets in the same tree. `ws init` creates it, and
   a built-in include rule keeps its dot-files syncing (decided by the owner, §2).
2. No backward compatibility: `symlinks/` is the only owned root, and `ws.json` keys of the
   earlier layout (such as `links`) are rejected as unknown fields.
3. `~/.claude/skills`, `~/.claude/agents` and the like are one folder entry each, not one
   entry per skill.
4. The `$HOME` walk for `untracked` links goes four levels deep, and `ls` does it, not the Dashboard.
5. macOS paths are in the catalog. They cost nothing on Linux, because a path that does
   not exist is never reported.

## Tools that rewrite their file

`btop.conf` and `htoprc` are replaced on every save, which replaces the symlink with a real
file. They are not in the catalog. For any such file the `drifted` status shows it, and
`add <host-path>` adopts the live content and links it again. Two further ideas are left for
later, in order of cost: `add --copy` (store a copy instead of a link, refreshed by a cron
job when the live file is newer), and a per-entry `heal` flag that runs the adopt step
unattended. The second can silently overwrite, so it needs its own decision.

## Consequences

- **Good:** one obvious place for every original. New users are shown what to keep. Hand-made
  links are found. `ls` can say what is wrong with each entry. Fewer concepts: no class, no
  `--to`, no backup.
- **Cost:** private keys now sit beside configs in `symlinks/`. `ws secret ls` has to treat
  that tree sensibly (see plan 0003, step 4). Linked files lose their git history and GitHub copy.
- **No compatibility layer:** the previous noun, its old command words, its config keys and
  its storage folders are gone, not aliased. A workspace from before this change has to
  be re-linked with `ws symlink add`.
